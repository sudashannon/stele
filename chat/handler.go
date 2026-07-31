package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"stele/chat/provider"
)

const defaultSystemPrompt = "你是 Comet Change 产物分析助手。请基于提供的文档内容回答问题。\n若答案不在文档中，请诚实告知。如果用户要求画架构图、流程图，请使用 mermaid 语法。\n用中文回答。"

// WikiGraphAccessor provides read access to the wiki knowledge graph
// for context injection into chat prompts.
type WikiGraphAccessor interface {
	// Neighborhood returns the 2-hop neighborhood of a change:
	// direct neighbors' IDs+titles, and 2nd-hop titles.
	Neighborhood(changeID string) (direct []NeighborInfo, secondHop []string)
	// CommunityOverview returns the cached overview for a change's community, or "".
	CommunityOverview(changeID string) string
}

// NeighborInfo describes one direct (1-hop) graph neighbor of a change.
type NeighborInfo struct {
	ID    string
	Title string
	Kind  string // edge kind
}

func HandleMessage(baseDir, openspecDir string, wikiGraph WikiGraphAccessor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var req struct {
			Change       string   `json:"change"`
			Message      string   `json:"message"`
			ContextFiles []string `json:"context_files"`
			Images       []string `json:"images"`
			IncludeGraph bool     `json:"includeGraph"`
			ComponentID  string   `json:"componentId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, "invalid request")
			return
		}
		if req.Change == "" || req.Message == "" {
			writeJSON(w, 400, "change and message required")
			return
		}

		cfg, _ := LoadConfig()
		pcfg, ok := cfg.Providers[cfg.ActiveProvider]
		if !ok || pcfg.APIKey == "" {
			writeJSON(w, 400, "请先在设置中配置 API Key")
			return
		}
		p := provider.Get(cfg.ActiveProvider)
		if p == nil {
			writeJSON(w, 500, "provider not found")
			return
		}

		sessionKey := req.Change
		if req.ComponentID != "" {
			sessionKey = req.ComponentID
		}
		sess := GetSession(sessionKey)

		systemPrompt := buildSystemPrompt(baseDir, openspecDir, req.Change, req.ContextFiles)
		systemPrompt += buildGraphContextForComponent(openspecDir, req.Change, req.ComponentID, req.IncludeGraph, wikiGraph)

		content := []provider.ContentBlock{{Type: "text", Text: req.Message}}
		for _, img := range req.Images {
			if strings.HasPrefix(img, "data:image/") {
				parts := strings.SplitN(img, ";base64,", 2)
				mediaType := strings.TrimPrefix(parts[0], "data:")
				if len(parts) == 2 {
					content = append(content, provider.ContentBlock{
						Type: "image",
						Source: &provider.ImageSource{
							Type:      "base64",
							MediaType: mediaType,
							Data:      parts[1],
						},
					})
				}
			}
		}

		userMsg := provider.Message{Role: "user", Content: content}
		sess.AddMessage(userMsg)

		messages := append([]provider.Message{}, sess.Messages...)

		opts := provider.ChatOptions{
			Temperature: pcfg.Temperature,
			MaxTokens:   pcfg.MaxTokens,
			Thinking:    pcfg.Thinking,
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		eventCh, err := p.ChatStream(ctx, pcfg.APIKey, pcfg.APIBase, pcfg.Model, systemPrompt, messages, opts)
		if err != nil {
			writeJSON(w, 500, err.Error())
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, 500, "streaming not supported")
			return
		}

		var thinkingBuf, textBuf strings.Builder
		for event := range eventCh {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			switch event.Type {
			case "thinking":
				thinkingBuf.WriteString(event.Content)
			case "delta":
				textBuf.WriteString(event.Content)
			}
		}

		assistantContent := []provider.ContentBlock{}
		if thinkingBuf.Len() > 0 {
			assistantContent = append(assistantContent, provider.ContentBlock{Type: "thinking", Thinking: thinkingBuf.String()})
		}
		assistantContent = append(assistantContent, provider.ContentBlock{Type: "text", Text: textBuf.String()})
		assistantMsg := provider.Message{Role: "assistant", Content: assistantContent}
		sess.AddMessage(assistantMsg)
	}
}

func HandleSession(w http.ResponseWriter, r *http.Request) {
	change := r.URL.Query().Get("change")
	if change == "" {
		writeJSON(w, 400, "missing change")
		return
	}

	switch r.Method {
	case "GET":
		sess := GetSession(change)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sess)
	case "DELETE":
		DeleteSession(change)
		writeJSON(w, 200, "ok")
	default:
		writeJSON(w, 405, "method not allowed")
	}
}

func HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		cfg, err := LoadConfig()
		if err != nil {
			writeJSON(w, 500, err.Error())
			return
		}
		masked := maskConfig(cfg)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(masked)
	case "PUT":
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSON(w, 400, "invalid config")
			return
		}
		existing, _ := LoadConfig()
		if existing != nil {
			for k, v := range existing.Providers {
				if _, ok := cfg.Providers[k]; !ok {
					if cfg.Providers == nil {
						cfg.Providers = make(map[string]ProviderConfig)
					}
					cfg.Providers[k] = v
				}
			}
			for k, v := range cfg.Providers {
				if v.APIKey == "" || isMasked(v.APIKey) {
					if old, ok := existing.Providers[k]; ok {
						v.APIKey = old.APIKey
						cfg.Providers[k] = v
					}
				}
			}
		}
		if cfg.ActiveProvider == "" {
			cfg.ActiveProvider = "minimax"
		}
		if err := SaveConfig(&cfg); err != nil {
			writeJSON(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, "ok")
	default:
		writeJSON(w, 405, "method not allowed")
	}
}

func HandleProviders(w http.ResponseWriter, r *http.Request) {
	cfg, _ := LoadConfig()
	active := ""
	if cfg != nil {
		active = cfg.ActiveProvider
	}
	resp := map[string]interface{}{
		"active":    active,
		"providers": provider.List(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func buildSystemPrompt(baseDir, openspecDir, change string, contextFiles []string) string {
	var b strings.Builder
	b.WriteString(defaultSystemPrompt)
	b.WriteString("\n\n---\n\n可用产物文件：\n")

	for _, f := range contextFiles {
		b.WriteString(fmt.Sprintf("\n### %s\n\n", filepath.Base(f)))
		content, err := os.ReadFile(f)
		if err == nil {
			b.Write(content)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// buildGraphContext preserves the legacy OpenSpec lookup; source-neutral
// callers pass a component ID to buildGraphContextForComponent instead.
// Both return the component's 1-hop neighbors, 2nd-hop titles, and cached
// community overview, or "" when graph context is unavailable.
func buildGraphContext(openspecDir, change string, includeGraph bool, wikiGraph WikiGraphAccessor) string {
	return buildGraphContextForComponent(openspecDir, change, "", includeGraph, wikiGraph)
}

func buildGraphContextForComponent(openspecDir, change, componentID string, includeGraph bool, wikiGraph WikiGraphAccessor) string {
	if !includeGraph || wikiGraph == nil {
		return ""
	}
	changeID := componentID
	if changeID == "" {
		changeDir := findChangeDir(openspecDir, change)
		if changeDir == "" {
			return ""
		}
		changeID = filepath.Join(changeDir, ".comet.yaml")
	}
	var b strings.Builder
	direct, secondHop := wikiGraph.Neighborhood(changeID)
	if len(direct) > 0 || len(secondHop) > 0 {
		b.WriteString("\n\n---\n\n## 图谱上下文\n\n")
		b.WriteString("当前 change 的直接关联：\n")
		for _, n := range direct {
			fmt.Fprintf(&b, "- [%s] %s\n", n.Kind, n.Title)
		}
		if len(secondHop) > 0 {
			b.WriteString("\n间接关联(2-hop)：\n")
			for _, title := range secondHop {
				fmt.Fprintf(&b, "- %s\n", title)
			}
		}
	}
	if overview := wikiGraph.CommunityOverview(changeID); overview != "" {
		b.WriteString("\n\n## 所属主题综述\n\n")
		b.WriteString(overview)
	}
	return b.String()
}

func findChangeDir(openspecDir, name string) string {
	dir := filepath.Join(openspecDir, "changes", name)
	if _, err := os.Stat(filepath.Join(dir, ".comet.yaml")); err == nil {
		return dir
	}
	archiveDir := filepath.Join(openspecDir, "changes", "archive")
	entries, _ := os.ReadDir(archiveDir)
	for _, e := range entries {
		if e.IsDir() && (e.Name() == name || (len(e.Name()) > 11 && e.Name()[11:] == name)) {
			return filepath.Join(archiveDir, e.Name())
		}
	}
	return ""
}

func maskConfig(cfg *Config) *Config {
	clone := *cfg
	clone.Providers = make(map[string]ProviderConfig)
	for k, v := range cfg.Providers {
		if len(v.APIKey) > 8 {
			v.APIKey = v.APIKey[:4] + "****" + v.APIKey[len(v.APIKey)-4:]
		}
		clone.Providers[k] = v
	}
	return &clone
}

func isMasked(key string) bool {
	return strings.Contains(key, "****")
}

func writeJSON(w http.ResponseWriter, code int, msg interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{"message": msg})
}
