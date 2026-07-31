package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"stele/chat"
	"stele/chat/provider"
	"stele/wiki"
)

type reportTestProvider struct{ name string }

func (p reportTestProvider) Name() string       { return p.name }
func (reportTestProvider) Models() []string     { return []string{"test-model"} }
func (reportTestProvider) SupportsImages() bool { return false }
func (reportTestProvider) ChatStream(context.Context, string, string, string, string, []provider.Message, provider.ChatOptions) (<-chan provider.StreamEvent, error) {
	stream := make(chan provider.StreamEvent)
	close(stream)
	return stream, nil
}

func installReportTestRuntime(t *testing.T, dir string, drain func(string, string) string) reportTestProvider {
	t.Helper()
	name := "report-test-provider"
	fake := reportTestProvider{name: name}
	provider.Register(fake)
	previousConfig := chat.LoadConfig
	previousDir := reportsDirFn
	previousDrain := chatStreamDrain
	chat.LoadConfig = func() (*chat.Config, error) {
		return &chat.Config{
			ActiveProvider: name,
			Providers: map[string]chat.ProviderConfig{name: {
				APIKey: "test-key", Model: "test-model", MaxTokens: 2048,
			}},
		}, nil
	}
	reportsDirFn = func() (string, error) { return dir, nil }
	chatStreamDrain = func(_ context.Context, _ provider.Provider, _ chat.ProviderConfig, systemPrompt, userText string) (string, error) {
		return drain(systemPrompt, userText), nil
	}
	t.Cleanup(func() {
		chat.LoadConfig = previousConfig
		reportsDirFn = previousDir
		chatStreamDrain = previousDrain
	})
	return fake
}

func reportTestAPI(t *testing.T, documents []wiki.Component, edges []wiki.Edge, vectors map[string][]float32) *wiki.API {
	t.Helper()
	graph := wiki.BuildGraph(documents, edges)
	graph.SetEmbeddings(vectors)
	return wiki.NewAPI(graph)
}

func TestHandleReportUsesWikiDocumentsAndPersistsManifest(t *testing.T) {
	dir := t.TempDir()
	docA := filepath.Join(dir, "a-design.md")
	docB := filepath.Join(dir, "b-evidence.md")
	if err := os.WriteFile(docA, []byte("# Cache Design\n\n## Outcome\n\nImplemented deterministic cache keys.\n\n## Next\n\n- [ ] Verify migration."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docB, []byte("# Cache Evidence\n\n## Result\n\nCache invalidation test passed."), 0o644); err != nil {
		t.Fatal(err)
	}
	updated := time.Date(2026, 6, 15, 10, 0, 0, 0, time.Local)
	api := reportTestAPI(t, []wiki.Component{
		{ID: docA, Path: docA, Title: "Cache Design", Type: wiki.TypeDesign, Workspace: "miao", UpdatedAt: updated},
		{ID: docB, Path: docB, Title: "Cache Evidence", Type: wiki.TypeArtifact, Workspace: "miao", UpdatedAt: updated},
	}, []wiki.Edge{{From: docA, To: docB, Kind: "generates", Source: "convention-internal"}}, map[string][]float32{
		docA: {1, 0}, docB: {0.9, 0.1},
	})
	installReportTestRuntime(t, dir, func(systemPrompt, userText string) string {
		if !strings.Contains(systemPrompt, "工程文档证据归纳器") || !strings.Contains(userText, "Cache Design") {
			t.Fatalf("unexpected prompt: system=%q user=%q", systemPrompt, userText)
		}
		return `{"label":"缓存一致性","summary":{"text":"缓存键和失效验证已形成闭环。","evidenceIds":["D1","D2"]},"claims":[{"kind":"outcome","text":"失效验证已通过。","evidenceIds":["D2"]},{"kind":"next","text":"继续验证迁移。","evidenceIds":["D1"]}]}`
	})

	request := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(`{"type":"weekly","start":"2026-06-15","end":"2026-06-15","workspace":"miao"}`))
	response := httptest.NewRecorder()
	handleReport(response, request, api)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Format             string         `json:"format"`
		Body               string         `json:"body"`
		SavedName          string         `json:"savedName"`
		InputDocumentCount int            `json:"inputDocumentCount"`
		ClusterCount       int            `json:"clusterCount"`
		Coverage           reportCoverage `json:"coverage"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Format != "md" || payload.InputDocumentCount != 2 || payload.ClusterCount != 1 {
		t.Fatalf("unexpected response metadata: %+v", payload)
	}
	if !strings.Contains(payload.Body, "本周纳入 2 份活动文档") || !strings.Contains(payload.Body, "### 完成与推进项（2 项）") || !strings.Contains(payload.Body, "[D1, D2]") || !strings.Contains(payload.Body, "继续验证迁移") {
		t.Fatalf("unexpected weekly body:\n%s", payload.Body)
	}
	manifestPath := filepath.Join(dir, payload.SavedName+".manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	var manifest reportPeriodDigest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != reportManifestSchemaVersion || len(manifest.Documents) != 2 || manifest.Counts.WorkItems != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestHandleReportRejectsUngroundedModelOutputWithoutPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "design.md")
	if err := os.WriteFile(path, []byte("# Design\n\nObserved result."), 0o644); err != nil {
		t.Fatal(err)
	}
	api := reportTestAPI(t, []wiki.Component{{ID: path, Path: path, Title: "Design", Type: wiki.TypeDesign, Workspace: "miao", UpdatedAt: time.Date(2026, 6, 15, 0, 0, 0, 0, time.Local)}}, nil, map[string][]float32{path: {1, 0}})
	installReportTestRuntime(t, dir, func(string, string) string {
		return `{"label":"伪造主题","summary":{"text":"不存在的事实","evidenceIds":["D999"]},"claims":[]}`
	})
	request := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(`{"type":"weekly","start":"2026-06-15","end":"2026-06-15"}`))
	response := httptest.NewRecorder()
	handleReport(response, request, api)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "weekly-") {
			t.Fatalf("invalid model output was persisted as %s", entry.Name())
		}
	}
}

func TestHandleReportNoProviderReturns400(t *testing.T) {
	previous := chat.LoadConfig
	chat.LoadConfig = func() (*chat.Config, error) {
		return &chat.Config{ActiveProvider: "missing", Providers: map[string]chat.ProviderConfig{"missing": {}}}, nil
	}
	t.Cleanup(func() { chat.LoadConfig = previous })
	request := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(`{"type":"weekly","start":"2026-06-01","end":"2026-06-30"}`))
	response := httptest.NewRecorder()
	handleReport(response, request, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "请先配置 LLM provider") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestClusterReportCorpusPreservesLifecycleMustLinks(t *testing.T) {
	corpus := reportCorpus{
		Documents: []reportDocument{
			{EvidenceID: "D1", SourceID: "a", Title: "Proposal", Vector: []float32{1, 0}},
			{EvidenceID: "D2", SourceID: "b", Title: "Design", Vector: []float32{0, 1}},
			{EvidenceID: "D3", SourceID: "c", Title: "Independent", Vector: []float32{-1, 0}},
		},
		Edges:  []wiki.Edge{{From: "a", To: "b", Kind: "generates", Source: "convention-internal"}},
		Counts: documentReportCounts{Types: make(map[string]int)},
	}
	themes := clusterReportCorpus(&corpus)
	if len(themes) != 2 || corpus.Counts.WorkItems != 2 {
		t.Fatalf("themes=%+v counts=%+v", themes, corpus.Counts)
	}
	foundPair := false
	for _, theme := range themes {
		if strings.Join(theme.EvidenceIDs, ",") == "D1,D2" {
			foundPair = true
		}
	}
	if !foundPair {
		t.Fatalf("must-linked documents split across themes: %+v", themes)
	}
}

func TestSummarizeReportThemesRunsBoundedCallsConcurrently(t *testing.T) {
	previousDrain := chatStreamDrain
	var mutex sync.Mutex
	active := 0
	maxActive := 0
	chatStreamDrain = func(_ context.Context, _ provider.Provider, _ chat.ProviderConfig, _ string, userText string) (string, error) {
		var prompt reportThemePrompt
		if err := json.Unmarshal([]byte(userText), &prompt); err != nil {
			return "", err
		}
		mutex.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mutex.Unlock()
		time.Sleep(20 * time.Millisecond)
		mutex.Lock()
		active--
		mutex.Unlock()
		response, err := json.Marshal(reportThemeResponse{
			Label: prompt.SuggestedLabel,
			Summary: reportPromptSummary{
				Text:        "Grounded summary.",
				EvidenceIDs: []string{prompt.Documents[0].EvidenceID},
			},
		})
		return string(response), err
	}
	t.Cleanup(func() { chatStreamDrain = previousDrain })
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	corpus := reportCorpus{
		Start: start,
		End:   start.AddDate(0, 0, 7),
		Documents: []reportDocument{
			{EvidenceID: "D1", SourceID: "a", Title: "A", ActivityAt: start},
			{EvidenceID: "D2", SourceID: "b", Title: "B", ActivityAt: start},
			{EvidenceID: "D3", SourceID: "c", Title: "C", ActivityAt: start},
		},
	}
	themes := []reportTheme{
		{ID: "T1", Label: "A", EvidenceIDs: []string{"D1"}, RepresentativeIDs: []string{"D1"}, DocumentIndexes: []int{0}},
		{ID: "T2", Label: "B", EvidenceIDs: []string{"D2"}, RepresentativeIDs: []string{"D2"}, DocumentIndexes: []int{1}},
		{ID: "T3", Label: "C", EvidenceIDs: []string{"D3"}, RepresentativeIDs: []string{"D3"}, DocumentIndexes: []int{2}},
	}
	digests, err := summarizeReportThemes(context.Background(), &corpus, themes, chat.ProviderConfig{}, reportTestProvider{name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 3 || digests[0].ID != "T1" || digests[2].ID != "T3" {
		t.Fatalf("results lost deterministic order: %+v", digests)
	}
	if maxActive < 2 || maxActive > reportClusterConcurrency {
		t.Fatalf("max concurrent calls=%d, want 2..%d", maxActive, reportClusterConcurrency)
	}
}

func TestSummarizeReportThemesReportsRootErrorInsteadOfCanceledPeer(t *testing.T) {
	previousDrain := chatStreamDrain
	chatStreamDrain = func(ctx context.Context, _ provider.Provider, _ chat.ProviderConfig, _ string, userText string) (string, error) {
		var prompt reportThemePrompt
		if err := json.Unmarshal([]byte(userText), &prompt); err != nil {
			return "", err
		}
		if prompt.ThemeID == "T1" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return `{`, nil
	}
	t.Cleanup(func() { chatStreamDrain = previousDrain })

	start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	corpus := reportCorpus{
		Start: start, End: start.AddDate(0, 0, 7),
		Documents: []reportDocument{
			{EvidenceID: "D1", SourceID: "a", Title: "A", ActivityAt: start},
			{EvidenceID: "D2", SourceID: "b", Title: "B", ActivityAt: start},
		},
	}
	themes := []reportTheme{
		{ID: "T1", Label: "A", EvidenceIDs: []string{"D1"}, DocumentIndexes: []int{0}},
		{ID: "T2", Label: "B", EvidenceIDs: []string{"D2"}, DocumentIndexes: []int{1}},
	}
	_, err := summarizeReportThemes(context.Background(), &corpus, themes, chat.ProviderConfig{}, reportTestProvider{name: "test"})
	if err == nil || !strings.Contains(err.Error(), "主题 T2") || strings.Contains(err.Error(), "主题 T1") {
		t.Fatalf("root error was hidden by cancellation: %v", err)
	}
}

func TestSummarizeReportThemeRetriesTruncatedJSONWithStructuredOptions(t *testing.T) {
	previousDrain := chatStreamDrain
	calls := 0
	var retryPrompt string
	chatStreamDrain = func(_ context.Context, _ provider.Provider, cfg chat.ProviderConfig, systemPrompt, _ string) (string, error) {
		calls++
		if cfg.Model != "test-model" || cfg.MaxTokens < reportStructuredMaxTokens || cfg.Thinking != "disabled" || cfg.Temperature != 0.1 {
			t.Fatalf("unexpected structured config: %+v", cfg)
		}
		if calls == 1 {
			return `{"label":"缓存","summary":{"text":"输出被截断","evidenceIds":["D1"]}`, nil
		}
		retryPrompt = systemPrompt
		return `{"label":"缓存","summary":{"text":"重试后输出完整。","evidenceIds":["D1"]},"claims":[]}`, nil
	}
	t.Cleanup(func() { chatStreamDrain = previousDrain })

	start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	corpus := reportCorpus{
		Start: start,
		End:   start.AddDate(0, 0, 7),
		Documents: []reportDocument{{
			EvidenceID: "D1", SourceID: "a", Title: "Cache", Type: wiki.TypeDesign,
			Workspace: "miao", ActivityAt: start, SemanticText: "Cache result.",
		}},
	}
	theme := reportTheme{
		ID: "T1", Label: "Cache", EvidenceIDs: []string{"D1"},
		RepresentativeIDs: []string{"D1"}, DocumentIndexes: []int{0},
	}
	digest, err := summarizeReportTheme(context.Background(), &corpus, theme, chat.ProviderConfig{MaxTokens: 1024}, reportTestProvider{name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || digest.Summary.Text != "重试后输出完整。" || !strings.Contains(retryPrompt, "上一次响应被截断") {
		t.Fatalf("calls=%d digest=%+v retryPrompt=%q", calls, digest, retryPrompt)
	}
}

func TestSummarizeReportThemeRetriesEvidenceValidationFailure(t *testing.T) {
	previousDrain := chatStreamDrain
	calls := 0
	var retryPrompt string
	chatStreamDrain = func(_ context.Context, _ provider.Provider, _ chat.ProviderConfig, systemPrompt, _ string) (string, error) {
		calls++
		if calls == 1 {
			return `{"label":"结果","summary":{"text":"已完成。","evidenceIds":["D1"]},"claims":[{"kind":"progress","text":"错误引用。","evidenceIds":["D9"]}]}`, nil
		}
		retryPrompt = systemPrompt
		return `{"label":"结果","summary":{"text":"已完成。","evidenceIds":["D1"]},"claims":[]}`, nil
	}
	t.Cleanup(func() { chatStreamDrain = previousDrain })

	start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	corpus := reportCorpus{
		Start: start, End: start.AddDate(0, 0, 7),
		Documents: []reportDocument{{
			EvidenceID: "D1", SourceID: "a", Title: "Result", ActivityAt: start,
			SemanticText: "Completed successfully.",
		}},
	}
	theme := reportTheme{ID: "T1", Label: "Result", EvidenceIDs: []string{"D1"}, DocumentIndexes: []int{0}}
	digest, err := summarizeReportTheme(context.Background(), &corpus, theme, chat.ProviderConfig{}, reportTestProvider{name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(digest.Claims) != 0 || !strings.Contains(retryPrompt, "重新核对每个 evidenceId") {
		t.Fatalf("calls=%d digest=%+v retryPrompt=%q", calls, digest, retryPrompt)
	}
}

func TestValidateReportThemeDropsUnsupportedNextAction(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{{EvidenceID: "D1", SourceID: "a", Title: "Result", SemanticText: "Completed successfully."}}}
	theme := reportTheme{ID: "T1", EvidenceIDs: []string{"D1"}, DocumentIndexes: []int{0}}
	digest, err := validateReportThemeResponse(reportThemeResponse{
		Label:   "Result",
		Summary: reportPromptSummary{Text: "Completed.", EvidenceIDs: []string{"D1"}},
		Claims:  []reportPromptClaim{{Kind: "next", Text: "Deploy tomorrow.", EvidenceIDs: []string{"D1"}}},
	}, theme, &corpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.Claims) != 0 {
		t.Fatalf("unsupported next claim was retained: %+v", digest.Claims)
	}
}

func TestRenderWeeklyDocumentReportUsesSkillStyleHierarchy(t *testing.T) {
	digest := reportPeriodDigest{
		Start: "2026-07-20", End: "2026-07-27",
		Counts: documentReportCounts{Documents: 4, WorkItems: 2, Themes: 1, Workspaces: 2, Reports: 1},
		Documents: []reportDigestDocument{
			{EvidenceID: "D1", Title: "Cache | Design", Type: wiki.TypeDesign, Workspace: "miao", ActivityAt: "2026-07-21T10:00:00+08:00", Metadata: reportEvidenceMetadata{ChecklistDone: 2}},
			{EvidenceID: "D2", Title: "Cache Artifact", Type: wiki.TypeArtifact, Workspace: "miao", ActivityAt: "2026-07-22T10:00:00+08:00"},
			{EvidenceID: "D3", Title: "Cache Report", Type: wiki.TypeReport, Workspace: "model", ActivityAt: "2026-07-23T10:00:00+08:00"},
			{EvidenceID: "D4", Title: "Cache Plan", Type: wiki.TypePlan, Workspace: "model", ActivityAt: "2026-07-24T10:00:00+08:00", Metadata: reportEvidenceMetadata{ChecklistOpen: 1}},
		},
		Themes: []reportThemeDigest{{
			ID: "T1", Label: "缓存交付",
			Summary:     reportEvidenceClaim{Kind: "summary", Text: "缓存主线形成可验证交付。", EvidenceIDs: []string{"D1", "D2"}},
			EvidenceIDs: []string{"D1", "D2", "D3", "D4"},
			Claims: []reportEvidenceClaim{
				{Kind: "outcome", Text: "缓存设计已经完成。", EvidenceIDs: []string{"D1"}},
				{Kind: "progress", Text: "实现产物持续补齐。", EvidenceIDs: []string{"D2"}},
				{Kind: "risk", Text: "验证报告仍有风险。", EvidenceIDs: []string{"D3"}},
				{Kind: "next", Text: "完成剩余验证清单。", EvidenceIDs: []string{"D4"}},
			},
		}},
	}

	body := string(renderWeeklyDocumentReport(digest))
	for _, expected := range []string{
		"# 本周工作周报（2026-07-20 ~ 2026-07-27）",
		"## 概述",
		"本周纳入 4 份活动文档，归并为 2 个逻辑工作项和 1 个主题，覆盖 2 个 Workspace",
		"## 一、缓存交付",
		"### 本周里程碑",
		"### 完成与推进项（4 项）",
		"| Cache \\| Design | 2026-07-21 | 已完成 |",
		"| Cache Artifact | 2026-07-22 | 已产出 |",
		"| Cache Plan | 2026-07-24 | 推进中 |",
		"### 关键成果",
		"### 产出清单",
		"## 下周计划",
		"完成剩余验证清单",
		"## 附录：文档来源",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("weekly report missing %q:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "# 工程文档周报") || strings.Contains(body, "## 明确后续行动") {
		t.Fatalf("legacy hierarchy remains:\n%s", body)
	}
}

func TestReportBundleRoundTripListGetDelete(t *testing.T) {
	dir := t.TempDir()
	previousDir := reportsDirFn
	reportsDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { reportsDirFn = previousDir })
	corpus := reportCorpus{Counts: documentReportCounts{Types: make(map[string]int)}}
	digest := newReportPeriodDigest("weekly", "2026-06-01", "2026-06-07", "", &corpus, nil, "test", "model")
	body := []byte("# 周报\n\n内容。")
	name, err := saveReportBundle(dir, &digest, body)
	if err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	handleListReports(list, httptest.NewRequest(http.MethodGet, "/api/reports", nil))
	var reports []reportMeta
	if err := json.Unmarshal(list.Body.Bytes(), &reports); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Name != name {
		t.Fatalf("manifest must not appear in history: %+v", reports)
	}
	get := httptest.NewRecorder()
	handleGetReport(get, httptest.NewRequest(http.MethodGet, "/api/reports/get?name="+name, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "内容") {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	remove := httptest.NewRecorder()
	handleGetReport(remove, httptest.NewRequest(http.MethodDelete, "/api/reports/get?name="+name, nil))
	if remove.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, name+".manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("sidecar remains after delete: %v", err)
	}
}

func TestReportInputSnapshotHashIncludesRelationSemantics(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	corpus := reportCorpus{
		Documents: []reportDocument{{
			EvidenceID: "D1", SourceID: "a", Title: "A", Type: wiki.TypeDesign,
			Workspace: "miao", ActivityAt: start, ContentHash: "hash",
			RelationIDs: []string{"z", "b"},
		}},
		Edges:  []wiki.Edge{{From: "a", To: "b", Kind: "implements", Source: "convention-internal"}},
		Counts: documentReportCounts{Types: make(map[string]int)},
	}
	first := newReportPeriodDigest("weekly", "2026-06-01", "2026-06-07", "miao", &corpus, nil, "test", "model")
	corpus.Edges[0].Source = "vector"
	second := newReportPeriodDigest("weekly", "2026-06-01", "2026-06-07", "miao", &corpus, nil, "test", "model")
	if first.InputSnapshotHash == second.InputSnapshotHash || first.DigestID == second.DigestID {
		t.Fatal("relation source changed without invalidating report identity")
	}
	if got := strings.Join(first.Documents[0].RelationIDs, ","); got != "b,z" {
		t.Fatalf("relation IDs are not deterministic: %s", got)
	}
}

func TestGetReportPathTraversalRejected(t *testing.T) {
	previousDir := reportsDirFn
	reportsDirFn = func() (string, error) { return t.TempDir(), nil }
	t.Cleanup(func() { reportsDirFn = previousDir })
	response := httptest.NewRecorder()
	handleGetReport(response, httptest.NewRequest(http.MethodGet, "/api/reports/get?name=../evil.md", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", response.Code)
	}
}

func TestRenderMonthlyJSONProducesEscapedHTML(t *testing.T) {
	raw := []byte(`{
		"title":"2026年6月工程文档月报","overview":"覆盖 <script>文档</script>","mainline":"证据驱动",
		"total":12,"active":4,"themes":2,"reports":3,
		"themesDetail":[{"name":"缓存","count":4,"items":["失效验证 [D1]"]}],
		"focusProjects":[{"name":"缓存","points":["确定性键 [D2]"]}],
		"highlights":["验证通过 [D1]"],"milestones":[{"date":"2026-06-10","text":"验证完成 [D1]"}]
	}`)
	body, err := renderMonthlyFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	if !strings.Contains(html, "2026年6月工程文档月报") || !strings.Contains(html, "缓存") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("unexpected HTML: %s", html)
	}
	if _, err := renderMonthlyFromJSON([]byte("not-json")); err == nil {
		t.Fatal("invalid monthly JSON should fail")
	}
}

func TestMissingReportPeriodsClipsMonthBoundaries(t *testing.T) {
	reused := []reportPeriodDigest{{Start: "2026-06-02", End: "2026-06-08"}, {Start: "2026-06-10", End: "2026-06-29"}}
	periods, err := missingReportPeriods("2026-06-01", "2026-06-30", reused)
	if err != nil {
		t.Fatal(err)
	}
	want := []reportPeriod{{Start: "2026-06-01", End: "2026-06-01"}, {Start: "2026-06-09", End: "2026-06-09"}, {Start: "2026-06-30", End: "2026-06-30"}}
	if len(periods) != len(want) {
		t.Fatalf("periods=%+v", periods)
	}
	for index := range want {
		if periods[index] != want[index] {
			t.Fatalf("period[%d]=%+v want %+v", index, periods[index], want[index])
		}
	}
}

func TestCanonicalizeMonthlySourcesDeduplicatesIDAndContentHash(t *testing.T) {
	document := reportDigestDocument{EvidenceID: "D1", SourceID: "/docs/a.md", Path: "/docs/a.md", Title: "A", Type: wiki.TypeDesign, Workspace: "miao", ActivityAt: "2026-06-03T00:00:00Z", ContentHash: "same"}
	theme := reportThemeDigest{ID: "T1", Label: "A", Summary: reportEvidenceClaim{Kind: "summary", Text: "A", EvidenceIDs: []string{"D1"}}, EvidenceIDs: []string{"D1"}, RepresentativeIDs: []string{"D1"}}
	sources := []reportPeriodDigest{
		{DigestID: "w1", Start: "2026-06-01", End: "2026-06-07", Documents: []reportDigestDocument{document}, Themes: []reportThemeDigest{theme}, Counts: documentReportCounts{WorkItems: 1}},
		{DigestID: "w2", Start: "2026-06-08", End: "2026-06-14", Documents: []reportDigestDocument{document}, Themes: []reportThemeDigest{theme}, Counts: documentReportCounts{WorkItems: 1}},
	}
	full := reportCorpus{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local), End: time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)}
	corpus, remapped := canonicalizeMonthlySources(sources, &full)
	if len(corpus.Documents) != 1 || corpus.Documents[0].EvidenceID != "D1" || len(remapped) != 2 {
		t.Fatalf("corpus=%+v themes=%+v", corpus.Documents, remapped)
	}
	for _, source := range remapped {
		if strings.Join(source.Theme.Summary.EvidenceIDs, ",") != "D1" {
			t.Fatalf("evidence was not remapped: %+v", source)
		}
	}
}

func TestGenerateMonthlyReportUsesStructuredSlices(t *testing.T) {
	dir := t.TempDir()
	fake := installReportTestRuntime(t, dir, func(systemPrompt, _ string) string {
		if strings.Contains(systemPrompt, "工程月报的分层归并器") {
			return `{"label":"缓存演进","summary":{"text":"缓存能力在本月完成验证。","evidenceIds":["D1"]},"claims":[{"kind":"outcome","text":"验证结果已记录。","evidenceIds":["D1"]}]}`
		}
		return `{"label":"缓存","summary":{"text":"缓存验证已完成。","evidenceIds":["D1"]},"claims":[{"kind":"outcome","text":"验证结果已记录。","evidenceIds":["D1"]}]}`
	})
	previousEmbed := reportEmbedClusterDigests
	reportEmbedClusterDigests = func(components []wiki.Component) (map[string][]float32, error) {
		vectors := make(map[string][]float32, len(components))
		for _, component := range components {
			vectors[component.ID] = []float32{1, 0}
		}
		return vectors, nil
	}
	t.Cleanup(func() { reportEmbedClusterDigests = previousEmbed })
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	corpus := reportCorpus{
		Start: start, End: start.AddDate(0, 1, 0), Workspace: "miao",
		Documents: []reportDocument{{EvidenceID: "D1", SourceID: "/docs/cache.md", Path: "/docs/cache.md", Title: "Cache", Type: wiki.TypeDesign, Workspace: "miao", ActivityAt: time.Date(2026, 6, 15, 0, 0, 0, 0, time.Local), ContentHash: "hash", SemanticText: "Validation completed.", Vector: []float32{1, 0}}},
		Counts:    documentReportCounts{Documents: 1, Workspaces: 1, Types: map[string]int{"design": 1}},
		Coverage:  reportCoverage{SourceDocuments: 1, ReadableDocuments: 1},
	}
	body, digest, err := generateMonthlyDocumentReport(context.Background(), &corpus, dir, "2026-06-01", "2026-06-30", chat.ProviderConfig{Model: "test-model"}, fake)
	if err != nil {
		t.Fatal(err)
	}
	if digest.Type != "monthly" || len(digest.GeneratedSlices) != 1 || digest.GeneratedSlices[0] != (reportPeriod{Start: "2026-06-01", End: "2026-06-30"}) {
		t.Fatalf("unexpected monthly lineage: %+v", digest)
	}
	if !strings.Contains(string(body), "缓存演进") || !strings.Contains(string(body), "[D1]") {
		t.Fatalf("unexpected monthly HTML: %s", body)
	}
}

func TestHandleReportMonthlyEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "monthly-design.md")
	if err := os.WriteFile(path, []byte("# Monthly Design\n\n## Result\n\nThe monthly path is verified."), 0o644); err != nil {
		t.Fatal(err)
	}
	api := reportTestAPI(t, []wiki.Component{{
		ID: path, Path: path, Title: "Monthly Design", Type: wiki.TypeDesign,
		Workspace: "miao", UpdatedAt: time.Date(2026, 6, 20, 0, 0, 0, 0, time.Local),
	}}, nil, map[string][]float32{path: {1, 0}})
	installReportTestRuntime(t, dir, func(systemPrompt, _ string) string {
		if strings.Contains(systemPrompt, "工程月报的分层归并器") {
			return `{"label":"月报链路","summary":{"text":"月报链路已完成验证。","evidenceIds":["D1"]},"claims":[{"kind":"outcome","text":"月报端到端结果已记录。","evidenceIds":["D1"]}]}`
		}
		return `{"label":"月报输入","summary":{"text":"月报输入文档已验证。","evidenceIds":["D1"]},"claims":[{"kind":"outcome","text":"输入结果已记录。","evidenceIds":["D1"]}]}`
	})
	previousEmbed := reportEmbedClusterDigests
	reportEmbedClusterDigests = func(components []wiki.Component) (map[string][]float32, error) {
		vectors := make(map[string][]float32, len(components))
		for _, component := range components {
			vectors[component.ID] = []float32{1, 0}
		}
		return vectors, nil
	}
	t.Cleanup(func() { reportEmbedClusterDigests = previousEmbed })

	request := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(`{"type":"monthly","start":"2026-06-01","end":"2026-06-30","workspace":"miao"}`))
	response := httptest.NewRecorder()
	handleReport(response, request, api)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Format             string `json:"format"`
		Body               string `json:"body"`
		SavedName          string `json:"savedName"`
		InputDocumentCount int    `json:"inputDocumentCount"`
		ClusterCount       int    `json:"clusterCount"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Format != "html" || payload.InputDocumentCount != 1 || payload.ClusterCount != 1 {
		t.Fatalf("unexpected response: %+v", payload)
	}
	if !strings.Contains(payload.Body, "月报链路") || !strings.Contains(payload.Body, "输入文档") {
		t.Fatalf("unexpected monthly body: %s", payload.Body)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(dir, payload.SavedName+".manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest reportPeriodDigest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Type != "monthly" || len(manifest.GeneratedSlices) != 1 {
		t.Fatalf("unexpected monthly manifest: %+v", manifest)
	}
}

func TestGenerateMonthlyReportReusesContainedWeeklyDigest(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	fake := installReportTestRuntime(t, dir, func(systemPrompt, _ string) string {
		calls++
		if !strings.Contains(systemPrompt, "工程月报的分层归并器") {
			t.Fatalf("monthly reuse unexpectedly regenerated a weekly slice: %s", systemPrompt)
		}
		return `{"label":"复用主题","summary":{"text":"周摘要已直接重组。","evidenceIds":["D1"]},"claims":[{"kind":"outcome","text":"周报证据被月报复用。","evidenceIds":["D1"]}]}`
	})
	previousEmbed := reportEmbedClusterDigests
	reportEmbedClusterDigests = func(components []wiki.Component) (map[string][]float32, error) {
		vectors := make(map[string][]float32, len(components))
		for _, component := range components {
			vectors[component.ID] = []float32{1, 0}
		}
		return vectors, nil
	}
	t.Cleanup(func() { reportEmbedClusterDigests = previousEmbed })

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	document := reportDocument{
		EvidenceID: "D1", SourceID: "/docs/reused.md", Path: "/docs/reused.md",
		Title: "Reused", Type: wiki.TypeDesign, Workspace: "miao",
		ActivityAt: start.AddDate(0, 0, 2), ContentHash: "stable-hash",
		Metadata: reportEvidenceMetadata{Headings: []string{"# Reused"}},
	}
	weeklyCorpus := reportCorpus{
		Start: start, End: start.AddDate(0, 0, 7), Workspace: "miao",
		Documents: []reportDocument{document},
		Counts:    documentReportCounts{Documents: 1, WorkItems: 1, Themes: 1, Workspaces: 1, Types: map[string]int{"design": 1}},
		Coverage:  reportCoverage{SourceDocuments: 1, ReadableDocuments: 1, ClusteringMode: "vector"},
	}
	weeklyTheme := reportThemeDigest{
		ID: "T1", Label: "Weekly",
		Summary:     reportEvidenceClaim{Kind: "summary", Text: "Weekly evidence.", EvidenceIDs: []string{"D1"}, Date: "2026-06-03"},
		Claims:      []reportEvidenceClaim{{Kind: "outcome", Text: "Weekly outcome.", EvidenceIDs: []string{"D1"}, Date: "2026-06-03"}},
		EvidenceIDs: []string{"D1"}, RepresentativeIDs: []string{"D1"},
	}
	weekly := newReportPeriodDigest("weekly", "2026-06-01", "2026-06-07", "miao", &weeklyCorpus, []reportThemeDigest{weeklyTheme}, fake.Name(), "test-model")
	if _, err := saveReportBundle(dir, &weekly, []byte("# Weekly")); err != nil {
		t.Fatal(err)
	}

	fullCorpus := weeklyCorpus
	body, monthly, err := generateMonthlyDocumentReport(context.Background(), &fullCorpus, dir, "2026-06-01", "2026-06-07", chat.ProviderConfig{Model: "test-model"}, fake)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("LLM calls=%d, want one monthly reducer call", calls)
	}
	if len(monthly.SourceReportIDs) != 1 || monthly.SourceReportIDs[0] != weekly.DigestID || len(monthly.GeneratedSlices) != 0 {
		t.Fatalf("monthly lineage=%+v", monthly)
	}
	if !strings.Contains(string(body), "复用主题") {
		t.Fatalf("unexpected monthly body: %s", body)
	}
}
