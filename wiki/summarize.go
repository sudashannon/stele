package wiki

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"stele/chat"
	"stele/chat/provider"
)

func summaryCachePath(cacheDir, componentID string) string {
	h := sha256.Sum256([]byte(componentID))
	return filepath.Join(cacheDir, hex.EncodeToString(h[:])[:16]+".md")
}

// CachedSummary returns a previously generated summary when one exists and is
// still newer than the source file, without ever calling the LLM. The viewer
// needs this: it wants to show an existing summary the moment a document
// opens, and it cannot use Summarize for that because a cache miss there
// silently bills a generation for every document you merely look at.
func CachedSummary(c Component, cacheDir string) (string, bool) {
	cachePath := summaryCachePath(cacheDir, c.ID)
	cacheInfo, err := os.Stat(cachePath)
	if err != nil {
		return "", false
	}
	srcInfo, err := os.Stat(c.Path)
	if err != nil {
		return "", false
	}
	if !cacheInfo.ModTime().After(srcInfo.ModTime()) {
		return "", false
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// Summarize returns a cached LLM summary if it exists and is newer than the
// source file's mtime; otherwise it calls the currently-active chat
// provider (same config the Chat feature uses — no separate LLM plumbing)
// and persists the result.
func Summarize(ctx context.Context, c Component, cacheDir string) (string, error) {
	if cached, ok := CachedSummary(c, cacheDir); ok {
		return cached, nil
	}

	summary, err := generateSummary(ctx, c)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(summaryCachePath(cacheDir, c.ID), []byte(summary), 0644); err != nil {
		return "", err
	}
	return summary, nil
}

func generateSummary(ctx context.Context, c Component) (string, error) {
	cfg, err := chat.LoadConfig()
	if err != nil {
		return "", err
	}
	pc, ok := cfg.Providers[cfg.ActiveProvider]
	if !ok {
		return "", fmt.Errorf("no active provider configured (active_provider=%q)", cfg.ActiveProvider)
	}
	p := provider.Get(cfg.ActiveProvider)
	if p == nil {
		return "", fmt.Errorf("provider %q not registered", cfg.ActiveProvider)
	}

	content, err := os.ReadFile(c.Path)
	if err != nil {
		return "", err
	}

	events, err := p.ChatStream(ctx, pc.APIKey, pc.APIBase, pc.Model,
		"用一段简洁的中文摘要概括这份工程文档的核心内容，不超过150字。",
		[]provider.Message{{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: string(content)}},
		}},
		provider.ChatOptions{Temperature: pc.Temperature, MaxTokens: 300},
	)
	if err != nil {
		return "", err
	}

	var out string
	for ev := range events {
		if ev.Error != "" {
			return "", fmt.Errorf("provider error: %s", ev.Error)
		}
		out += ev.Content
	}
	return out, nil
}
