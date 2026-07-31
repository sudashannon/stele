package wiki

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"stele/chat"
	"stele/chat/provider"
)

const overviewPrefix = "> ⚠️ 本页由 AI 自动生成，非人工产物，仅供参考导航。\n\n"

// overviewCacheKey computes a hash of sorted member IDs to detect
// membership changes: the cache is keyed by this hash rather than by
// mtime (as Summarize does for a single file), since a community has no
// single source file whose mtime can signal "membership changed".
func overviewCacheKey(members []Component) string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	sort.Strings(ids)
	h := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(h[:])[:16]
}

func overviewCachePath(cacheDir string, communityID int, key string) string {
	return filepath.Join(cacheDir, fmt.Sprintf("community-%d-%s.md", communityID, key))
}

// GenerateOverview returns a cached or freshly-generated LLM overview for a
// community of 3+ members. The cache is keyed by a hash of the sorted
// member IDs (overviewCacheKey), so any membership change (join, leave,
// re-clustering) invalidates the cache and triggers regeneration; stale
// cache files for the same community under a different hash are removed on
// successful regeneration so the cache directory doesn't accumulate
// abandoned entries.
func GenerateOverview(ctx context.Context, communityID int, members []Component, cacheDir string) (string, error) {
	if len(members) < 3 {
		return "", fmt.Errorf("community too small (%d members, need at least 3)", len(members))
	}

	key := overviewCacheKey(members)
	cachePath := overviewCachePath(cacheDir, communityID, key)

	if data, err := os.ReadFile(cachePath); err == nil {
		return string(data), nil
	}

	overview, err := generateOverview(ctx, members)
	if err != nil {
		return "", err
	}
	result := overviewPrefix + overview

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	removeStaleOverviewCache(cacheDir, communityID, cachePath)
	if err := os.WriteFile(cachePath, []byte(result), 0644); err != nil {
		return "", err
	}
	return result, nil
}

// removeStaleOverviewCache deletes any previously cached overview for
// communityID whose hash no longer matches the current membership (i.e.
// every "community-<id>-*.md" file except the one we're about to write).
func removeStaleOverviewCache(cacheDir string, communityID int, keep string) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	prefix := fmt.Sprintf("community-%d-", communityID)
	keepName := filepath.Base(keep)
	for _, e := range entries {
		if e.IsDir() || e.Name() == keepName || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		os.Remove(filepath.Join(cacheDir, e.Name()))
	}
}

func generateOverview(ctx context.Context, members []Component) (string, error) {
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

	var memberList strings.Builder
	for i, m := range members {
		phase := ""
		if v, ok := m.Frontmatter["phase"].(string); ok {
			phase = v
		}
		fmt.Fprintf(&memberList, "%d. [%s] %s（工作区：%s，阶段：%s）\n", i+1, m.Type, m.Title, m.Workspace, phase)
	}

	prompt := fmt.Sprintf(`你是工程知识库的综述编辑。以下是同一主题社区下的 %d 个工程变更/产物，请撰写一篇不超过 300 字的中文综述，需覆盖：
1. 这个主题域包含哪些核心变更；
2. 这些变更之间的关系与演进脉络；
3. 当前整体进展；
4. 潜在风险。

成员列表：
%s`, len(members), memberList.String())

	events, err := p.ChatStream(ctx, pc.APIKey, pc.APIBase, pc.Model,
		"你是工程知识库综述编辑，输出简洁、准确的中文综述，不超过 300 字。",
		[]provider.Message{{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: prompt}},
		}},
		provider.ChatOptions{Temperature: pc.Temperature, MaxTokens: 600},
	)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for ev := range events {
		if ev.Error != "" {
			return "", fmt.Errorf("provider error: %s", ev.Error)
		}
		out.WriteString(ev.Content)
	}
	return out.String(), nil
}
