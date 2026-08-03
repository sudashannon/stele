package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stele/chat"
	"stele/chat/provider"
	"stele/internal/appdir"
	"stele/wiki"
)

// reportsDirFn resolves <data dir>/reports/, overridable in tests.
var reportsDirFn = func() (string, error) {
	return appdir.Path("reports"), nil
}

// chatStreamDrain is the LLM injection seam used by focused report tests.
var chatStreamDrain = func(ctx context.Context, p provider.Provider, pcfg chat.ProviderConfig, systemPrompt, userText string) (string, error) {
	messages := []provider.Message{{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: userText}}}}
	stream, err := p.ChatStream(ctx, pcfg.APIKey, pcfg.APIBase, pcfg.Model, systemPrompt, messages, provider.ChatOptions{
		Temperature: pcfg.Temperature,
		MaxTokens:   pcfg.MaxTokens,
		Thinking:    pcfg.Thinking,
	})
	if err != nil {
		return "", err
	}
	var output strings.Builder
	done := false
	for event := range stream {
		switch event.Type {
		case "delta":
			output.WriteString(event.Content)
		case "error":
			if event.Error == "" {
				event.Error = "模型流返回未知错误"
			}
			return output.String(), errors.New(event.Error)
		case "done":
			done = true
		}
	}
	if !done {
		if err := ctx.Err(); err != nil {
			return output.String(), err
		}
		return output.String(), errors.New("模型流在完成事件前结束")
	}
	return output.String(), nil
}

func ext(type_ string) string {
	if type_ == "monthly" {
		return "html"
	}
	return "md"
}

type reportRequest struct {
	Type      string `json:"type"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Workspace string `json:"workspace"`
}

// handleReport snapshots the existing Wiki index, then performs file IO and
// bounded map-reduce LLM calls after releasing the graph lock. ChangeSummary
// scanning is intentionally absent: Wiki documents are the sole report corpus.
func handleReport(w http.ResponseWriter, r *http.Request, wikiAPI *wiki.API) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request reportRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if request.Type != "weekly" && request.Type != "monthly" {
		writeJSONError(w, "invalid report type", http.StatusBadRequest)
		return
	}
	start, inclusiveEnd, err := parseInclusiveReportDates(request.Start, request.End)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	config, err := chat.LoadConfig()
	if err != nil || config == nil {
		writeJSONError(w, "请先配置 LLM provider", http.StatusBadRequest)
		return
	}
	providerConfig, ok := config.Providers[config.ActiveProvider]
	if !ok || providerConfig.APIKey == "" {
		writeJSONError(w, "请先配置 LLM provider", http.StatusBadRequest)
		return
	}
	llmProvider := provider.Get(config.ActiveProvider)
	if llmProvider == nil {
		writeJSONError(w, "provider not available", http.StatusInternalServerError)
		return
	}
	if wikiAPI == nil {
		writeJSONError(w, "wiki index is not ready", http.StatusServiceUnavailable)
		return
	}

	snapshot, err := wikiAPI.SnapshotDocuments(wiki.DocumentWindowFilter{
		Start:               start,
		End:                 inclusiveEnd.AddDate(0, 0, 1),
		Workspace:           request.Workspace,
		IncludeContext:      true,
		MaxContextDocuments: 64,
	})
	if errors.Is(err, wiki.ErrIndexNotReady) {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	corpus := extractReportCorpus(snapshot, start, inclusiveEnd.AddDate(0, 0, 1), request.Workspace)
	// The effort axis: which sessions worked in this window and what they wrote.
	// Sessions never enter the corpus as evidence - they decide how the corpus is
	// grouped and ordered, because document count is not work.
	sessionWork := wikiAPI.SnapshotSessionWork(start, inclusiveEnd.AddDate(0, 0, 1), request.Workspace)
	reportsDir, err := reportsDirFn()
	if err != nil {
		writeJSONError(w, "报告目录不可用: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	var body []byte
	var digest reportPeriodDigest
	if request.Type == "weekly" {
		attribution := attributeReportCorpus(&corpus, sessionWork)
		themes := buildSessionThemes(&corpus, &attribution)
		corpus.Counts.Themes = len(themes)
		body, digest, err = generateWeeklyDocumentReport(ctx, &corpus, themes, attribution, request.Start, request.End, providerConfig, llmProvider)
	} else {
		body, digest, err = generateMonthlyDocumentReport(ctx, &corpus, sessionWork, reportsDir, request.Start, request.End, providerConfig, llmProvider)
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeJSONError(w, err.Error(), status)
		return
	}

	savedName, err := saveReportBundle(reportsDir, &digest, body)
	if err != nil {
		writeJSONError(w, "落盘失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"format":             ext(request.Type),
		"body":               string(body),
		"savedName":          savedName,
		"coverage":           digest.Coverage,
		"inputDocumentCount": digest.Counts.Documents,
		"clusterCount":       digest.Counts.Themes,
		"sourceReportIDs":    digest.SourceReportIDs,
	})
}

//go:embed assets/report.tmpl.html
var monthlyTemplate string

type monthlyJSON struct {
	Title      string `json:"title"`
	Overview   string `json:"overview"`
	Total      int    `json:"total"`
	Active     int    `json:"active"`
	Themes     int    `json:"themes"`
	Reports    int    `json:"reports"`
	Mainline   string `json:"mainline"`
	Milestones []struct {
		Date string `json:"date"`
		Text string `json:"text"`
	} `json:"milestones"`
	ThemesDetail []struct {
		Name  string   `json:"name"`
		Count int      `json:"count"`
		Items []string `json:"items"`
	} `json:"themesDetail"`
	FocusProjects []struct {
		Name   string   `json:"name"`
		Points []string `json:"points"`
	} `json:"focusProjects"`
	// Deterministic sections, no model text.
	SessionsHTML string `json:"sessionsHtml"`
	OpenWorkHTML string `json:"openWorkHtml"`
	ActiveLabel  string `json:"activeLabel"`
}

func renderMonthlyFromJSON(raw []byte) ([]byte, error) {
	var report monthlyJSON
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("月报数据解析失败: %w", err)
	}
	var themesHTML strings.Builder
	for _, theme := range report.ThemesDetail {
		themesHTML.WriteString(`<div class="theme"><div class="theme-head">`)
		fmt.Fprintf(&themesHTML, `<span class="t">%s</span>`, html.EscapeString(theme.Name))
		fmt.Fprintf(&themesHTML, `<span class="count">%d</span>`, theme.Count)
		themesHTML.WriteString(`</div><ul class="theme-items">`)
		for _, item := range theme.Items {
			fmt.Fprintf(&themesHTML, `<li>%s</li>`, html.EscapeString(item))
		}
		themesHTML.WriteString(`</ul></div>`)
	}
	// A heading with nothing under it is a rendering bug, and the reader sees it as
	// a broken report: 重点主题 and 关键成果 both rendered as bare headings once the
	// sections that fed them were starved. Every section is emitted with its own
	// heading only when it has content.
	section := func(heading, inner, body string) string {
		if strings.TrimSpace(body) == "" {
			return ""
		}
		return fmt.Sprintf("<section>\n  <h2>%s</h2>\n  %s\n</section>", heading, fmt.Sprintf(inner, body))
	}
	var milestonesHTML strings.Builder
	for _, milestone := range report.Milestones {
		fmt.Fprintf(&milestonesHTML, `<li><span class="ms-date">%s</span> %s</li>`, html.EscapeString(milestone.Date), html.EscapeString(milestone.Text))
	}
	var focusHTML strings.Builder
	for _, project := range report.FocusProjects {
		fmt.Fprintf(&focusHTML, `<div class="focus-project"><h3>%s</h3><ul>`, html.EscapeString(project.Name))
		for _, point := range project.Points {
			fmt.Fprintf(&focusHTML, `<li>%s</li>`, html.EscapeString(point))
		}
		focusHTML.WriteString(`</ul></div>`)
	}
	output := monthlyTemplate
	replacements := map[string]string{
		"{{TITLE}}":              html.EscapeString(report.Title),
		"{{OVERVIEW}}":           html.EscapeString(report.Overview),
		"{{MAINLINE}}":           html.EscapeString(report.Mainline),
		"{{TOTAL}}":              fmt.Sprintf("%d", report.Total),
		"{{ACTIVE}}":             fmt.Sprintf("%d", report.Active),
		"{{THEMES}}":             fmt.Sprintf("%d", report.Themes),
		"{{REPORTS}}":            fmt.Sprintf("%d", report.Reports),
		"{{THEMES_SECTION}}":     section("主题", `<div class="themes">%s</div>`, themesHTML.String()),
		"{{MILESTONES_SECTION}}": section("里程碑", `<ul class="milestones">%s</ul>`, milestonesHTML.String()),
		"{{FOCUS_SECTION}}":      section("重点主题", `<div class="focus">%s</div>`, focusHTML.String()),
		"{{ACTIVE_LABEL}}":       html.EscapeString(report.ActiveLabel),
		// These two already carry their own heading.
		"{{SESSIONS_SECTION}}":  wrapSection(report.SessionsHTML),
		"{{OPEN_WORK_SECTION}}": wrapSection(report.OpenWorkHTML),
	}
	for key, value := range replacements {
		output = strings.ReplaceAll(output, key, value)
	}
	return []byte(output), nil
}

type reportMeta struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Start     string `json:"start"`
	End       string `json:"end"`
	CreatedAt string `json:"createdAt"`
}

func handleListReports(w http.ResponseWriter, r *http.Request) {
	dir, err := reportsDirFn()
	output := []reportMeta{}
	if err == nil {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			parsed, ok := parseReportName(entry.Name())
			if !ok {
				continue
			}
			info, statErr := entry.Info()
			createdAt := ""
			if statErr == nil {
				createdAt = info.ModTime().UTC().Format(time.RFC3339)
			}
			output = append(output, reportMeta{entry.Name(), parsed.Type, parsed.Start, parsed.End, createdAt})
		}
	}
	sort.Slice(output, func(i, j int) bool { return output[i].CreatedAt > output[j].CreatedAt })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(output)
}

func handleGetReport(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		writeJSONError(w, "invalid name", http.StatusBadRequest)
		return
	}
	dir, err := reportsDirFn()
	if err != nil {
		writeJSONError(w, "not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(dir, filepath.Base(name))
	switch r.Method {
	case http.MethodDelete:
		if err := os.Remove(path); err != nil {
			writeJSONError(w, "not found", http.StatusNotFound)
			return
		}
		_ = os.Remove(path + ".manifest.json")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case http.MethodGet:
		body, err := os.ReadFile(path)
		if err != nil {
			writeJSONError(w, "not found", http.StatusNotFound)
			return
		}
		format := "md"
		if strings.HasSuffix(name, ".html") {
			format = "html"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"format": format, "body": string(body)})
	default:
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func parseReportName(name string) (struct{ Type, Start, End string }, bool) {
	zero := struct{ Type, Start, End string }{}
	extension := filepath.Ext(name)
	if extension != ".md" && extension != ".html" {
		return zero, false
	}
	base := strings.TrimSuffix(name, extension)
	parts := strings.SplitN(base, "-", 2)
	if len(parts) != 2 || (parts[0] != "weekly" && parts[0] != "monthly") {
		return zero, false
	}
	type_ := parts[0]
	rest := parts[1]
	under := strings.Index(rest, "_")
	if under < 0 {
		return zero, false
	}
	start := rest[:under]
	afterUnder := rest[under+1:]
	lastDash := strings.LastIndex(afterUnder, "-")
	if lastDash < 0 {
		return zero, false
	}
	end := afterUnder[:lastDash]
	if _, _, err := parseInclusiveReportDates(start, end); err != nil {
		return zero, false
	}
	return struct{ Type, Start, End string }{type_, start, end}, true
}

// wrapSection emits a <section> only when its pre-rendered body has content. The
// effort and open-work builders already include their own heading, so an empty
// return means the data was absent and the section must disappear entirely.
func wrapSection(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return "<section>\n  " + body + "\n</section>"
}
