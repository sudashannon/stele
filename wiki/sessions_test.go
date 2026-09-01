package wiki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stele/internal/sessions"
)

// sessionFixture builds a workspace with one indexed document plus a transcript
// that read and edited it, and returns the wired API.
func sessionFixture(t *testing.T) (*API, string, string) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "proj")
	openspec := filepath.Join(project, "openspec")
	if err := os.MkdirAll(filepath.Join(openspec, "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(project, "docs", "design.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("# PCIe tri-channel design\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	transcriptDir := filepath.Join(root, "sessions", "-proj")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(transcriptDir, "2026-07-30T00-00-00-000Z_sess.jsonl")
	lines := []string{
		fmt.Sprintf(`{"type":"session","version":3,"id":"sess-1","timestamp":"2026-07-30T01:00:00.000Z","cwd":%q,"title":"修 PCIe 三通道"}`, project),
		`{"type":"message","timestamp":"2026-07-30T01:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"看下 pcie"}]}}`,
		fmt.Sprintf(`{"type":"message","timestamp":"2026-07-30T01:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","name":"read","intent":"读取设计","arguments":{"path":%q}}]}}`, docPath),
		fmt.Sprintf(`{"type":"message","timestamp":"2026-07-30T01:00:03.000Z","message":{"role":"assistant","content":[{"type":"toolCall","name":"write","intent":"更新设计","arguments":{"path":%q}}]}}`, docPath),
	}
	if err := os.WriteFile(transcriptPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []WorkspaceConfig{{Alias: "proj", Path: openspec}}
	graph := BuildGraph([]Component{{
		ID: docPath, Path: docPath, Type: TypeDesign, Title: "PCIe tri-channel design", Workspace: "proj",
	}}, nil)
	api := &API{graph: graph, ws: workspaces, ready: true}
	index := NewSessionsIndex(filepath.Join(root, "cache", "sessions.json"), sessions.Source{Provider: sessions.OMPProvider(), Root: filepath.Join(root, "sessions")})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	api.SetSessionsIndex(index)
	return api, docPath, transcriptPath
}

// A subagent's work reaches the graph and the API through the session that
// dispatched it: one session node, one set of edges, totals that include the
// subagent's edits.
func TestSubagentWorkFoldsIntoTheDispatchingSession(t *testing.T) {
	api, docPath, transcriptPath := sessionFixture(t)

	planPath := filepath.Join(filepath.Dir(docPath), "plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.graph.AddComponent(Component{ID: planPath, Path: planPath, Type: TypePlan, Title: "Plan", Workspace: "proj"})
	api.mu.Unlock()

	// OMP nests one transcript per dispatched subagent beside the parent's.
	artifacts := strings.TrimSuffix(transcriptPath, ".jsonl")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"session","version":3,"id":"sub-1","timestamp":"2026-07-30T01:30:00.000Z","cwd":"` + filepath.Dir(filepath.Dir(docPath)) + `"}`,
		fmt.Sprintf(`{"type":"message","timestamp":"2026-07-30T01:30:01.000Z","message":{"role":"assistant","content":[{"type":"toolCall","name":"edit","intent":"补计划","arguments":{"input":"[%s#1A2B]\nSWAP 1.=1:\n+x\n"}}]}}`, planPath),
	}
	if err := os.WriteFile(filepath.Join(artifacts, "PlanWriter.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := api.SessionsIndexSnapshot().Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	api.ApplySessions()

	if _, ok := api.graph.Component(filepath.Join(artifacts, "PlanWriter.jsonl")); ok {
		t.Fatal("a subagent transcript must not become its own session node")
	}
	summaries := api.sessionSummaries()
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want the one dispatching session", len(summaries))
	}
	summary := summaries[0]
	if len(summary.Subagents) != 1 || summary.Subagents[0] != "PlanWriter" {
		t.Fatalf("Subagents = %v", summary.Subagents)
	}
	if len(summary.Edits) != 1 || summary.Edits[0] != planPath {
		t.Fatalf("Edits = %v, want the subagent's patched document", summary.Edits)
	}
	if summary.Source != "omp" {
		t.Fatalf("Source = %q, want omp", summary.Source)
	}
	if summary.UserTurns != 1 {
		t.Fatalf("UserTurns = %d: a subagent prompt is not a human turn", summary.UserTurns)
	}
	// The edge lands on the dispatching session, so the document's backlinks
	// name the session a person can actually open.
	var found bool
	for _, edge := range api.graph.Forward(transcriptPath) {
		if edge.To == planPath && edge.Kind == EdgeKindEdits {
			found = true
		}
	}
	if !found {
		t.Fatal("the folded edit must produce a session→document edge from the parent")
	}
}

func TestApplySessionsGraftsComponentAndEdges(t *testing.T) {
	api, docPath, transcriptPath := sessionFixture(t)

	component, ok := api.graph.Component(transcriptPath)
	if !ok {
		t.Fatalf("session component must be grafted onto the graph")
	}
	if component.Type != TypeSession || component.Workspace != "proj" {
		t.Fatalf("component = %+v, want session/proj", component)
	}
	if component.Title != "修 PCIe 三通道" {
		t.Fatalf("Title = %q", component.Title)
	}

	forward := api.graph.Forward(transcriptPath)
	if len(forward) != 1 {
		t.Fatalf("a document both read and edited must yield one edge, got %+v", forward)
	}
	if forward[0].Kind != EdgeKindEdits || forward[0].Source != SourceSession || forward[0].Weight != 0 {
		t.Fatalf("edge = %+v, want edits/session/weight 0", forward[0])
	}
	if forward[0].To != docPath {
		t.Fatalf("edge target = %q, want %q", forward[0].To, docPath)
	}

	backlinks := api.graph.Backlinks(docPath)
	if len(backlinks) != 1 || backlinks[0].From != transcriptPath {
		t.Fatalf("document must gain a session backlink, got %+v", backlinks)
	}

	// Re-applying must not duplicate anything.
	api.ApplySessions()
	if got := len(api.graph.Forward(transcriptPath)); got != 1 {
		t.Fatalf("ApplySessions must be idempotent, got %d edges", got)
	}
}

func TestSessionComponentRequiresRegisteredWorkspace(t *testing.T) {
	digest := sessions.Digest{Path: "/x/s.jsonl", Cwd: "/tmp/scratch", Title: "throwaway"}
	if _, ok := SessionComponent(digest, []WorkspaceConfig{{Alias: "proj", Path: "/home/proj/openspec"}}); ok {
		t.Fatalf("a transcript outside every workspace must be dropped")
	}
}

func TestSessionComponentPrefersNestedWorkspace(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "miao")
	nested := filepath.Join(parent, "rx101")
	if err := os.MkdirAll(filepath.Join(nested, "openspec", "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "openspec", "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	workspaces := []WorkspaceConfig{
		{Alias: "miao", Path: filepath.Join(parent, "openspec")},
		{Alias: "rx101", Path: filepath.Join(nested, "openspec")},
	}
	digest := sessions.Digest{Path: "/x/s.jsonl", Cwd: filepath.Join(nested, "sub"), Title: "t"}
	component, ok := SessionComponent(digest, workspaces)
	if !ok {
		t.Fatalf("nested cwd must attribute to a workspace")
	}
	if component.Workspace != "rx101" {
		t.Fatalf("Workspace = %q, want rx101 (nested wins over its parent)", component.Workspace)
	}
}

func TestSessionEdgesOnlyTargetIndexedDocuments(t *testing.T) {
	documents := map[string]Component{
		"/repo/a.md": {ID: "/repo/a.md", Path: "/repo/a.md", Type: TypeDesign},
	}
	digest := sessions.Digest{
		Path:  "/x/s.jsonl",
		Reads: []string{"/repo/a.md", "/repo/missing.md", "/repo/main.go"},
		Edits: []string{"/repo/untracked.md"},
	}
	edges := SessionEdges(digest, Component{ID: "/x/s.jsonl"}, documents)
	if len(edges) != 1 || edges[0].To != "/repo/a.md" || edges[0].Kind != EdgeKindReads {
		t.Fatalf("edges = %+v, want a single reads edge to the indexed document", edges)
	}
}

func TestSessionsAreExcludedFromDocumentSearch(t *testing.T) {
	api, _, transcriptPath := sessionFixture(t)

	request := httptest.NewRequest(http.MethodPost, "/api/wiki/search-semantic",
		strings.NewReader(`{"query":"修 PCIe 三通道"}`))
	recorder := httptest.NewRecorder()
	api.HandleSemanticSearch(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var results []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode: %v (%s)", err, recorder.Body.String())
	}
	for _, result := range results {
		if result.Type == string(TypeSession) || result.ID == transcriptPath {
			t.Fatalf("session must never appear in document search: %+v", results)
		}
	}
}

func TestSessionsDoNotAffectLintOrCommunities(t *testing.T) {
	api, docPath, _ := sessionFixture(t)

	// The document has no authored edges, so it must still be an orphan even
	// though an agent session now links to it.
	orphan := false
	for _, issue := range api.graph.Lint() {
		if issue.Rule == "orphan" && issue.ComponentID == docPath {
			orphan = true
		}
		if strings.HasSuffix(issue.ComponentID, ".jsonl") {
			t.Fatalf("session components must not be linted: %+v", issue)
		}
	}
	if !orphan {
		t.Fatalf("session backlinks must not clear a document's orphan status")
	}

	communities := DetectCommunities(api.graph)
	for id := range communities {
		if strings.HasSuffix(id, ".jsonl") {
			t.Fatalf("session components must not enter community detection: %q", id)
		}
	}
	if weight := edgeWeight(Edge{Source: SourceSession, Weight: 0.9}, nil); weight != 0 {
		t.Fatalf("session edges must always weigh 0, got %v", weight)
	}
}

// Content-quality rules read a component's bytes. A transcript is measured up
// to 157 MB and is full of words the placeholder rule matches, so every
// body-reading rule must skip sessions outright.
func TestLintNeverReadsSessionBodies(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s.jsonl")
	body := strings.Repeat("TODO FIXME 待补充\n", 50)
	if err := os.WriteFile(transcriptPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	graph := BuildGraph([]Component{{
		ID: transcriptPath, Path: transcriptPath, Type: TypeSession, Title: "noisy session", Workspace: "proj",
	}}, nil)

	for _, issue := range graph.Lint() {
		if issue.ComponentID == transcriptPath {
			t.Fatalf("lint must not inspect a session transcript, got %+v", issue)
		}
	}
	if !lintableBody(Component{Path: "/repo/a.md", Type: TypeDesign}) {
		t.Fatalf("documents must stay lintable")
	}
	if lintableBody(Component{Path: transcriptPath, Type: TypeSession}) {
		t.Fatalf("sessions must never be lintable")
	}
}

func TestSessionsAreNotMirrored(t *testing.T) {
	repo := t.TempDir()
	mirror := NewMirror(filepath.Join(repo, "mirror"), "")
	workspaces := []WorkspaceConfig{{Alias: "proj", Path: filepath.Join(repo, "proj", "openspec")}}
	docPath := filepath.Join(repo, "proj", "docs", "a.md")
	components := map[string]Component{
		docPath:      {ID: docPath, Path: docPath, Type: TypeDesign, Workspace: "proj"},
		"/x/s.jsonl": {ID: "/x/s.jsonl", Path: "/x/s.jsonl", Type: TypeSession, Workspace: "proj"},
	}
	mirror.SyncAll(components, workspaces)

	mirror.mu.Lock()
	pending := make(map[string]string, len(mirror.pending))
	for key, value := range mirror.pending {
		pending[key] = value.dest
	}
	mirror.mu.Unlock()

	if _, queued := pending["/x/s.jsonl"]; queued {
		t.Fatalf("transcripts must never be queued for the mirror: %v", pending)
	}
	if dest, queued := pending[docPath]; !queued || strings.HasPrefix(dest, "proj/"+string(filepath.Separator)) {
		t.Fatalf("documents must still mirror workspace-relative, got %q", dest)
	}
}

func TestRelativeToWorkspaceFailsClosedOutsideRoot(t *testing.T) {
	workspaces := []WorkspaceConfig{{Alias: "proj", Path: "/home/proj/openspec"}}
	if got := relativeToWorkspace("/home/other/a.md", "proj", workspaces); got != "" {
		t.Fatalf("a path outside the mirror root must yield \"\", got %q", got)
	}
	if got := relativeToWorkspace("/home/proj/docs/a.md", "proj", workspaces); got != filepath.Join("docs", "a.md") {
		t.Fatalf("relative path = %q", got)
	}
}

func TestHandleSessionsAndSession(t *testing.T) {
	api, docPath, transcriptPath := sessionFixture(t)

	recorder := httptest.NewRecorder()
	api.HandleSessions(recorder, httptest.NewRequest(http.MethodGet, "/api/wiki/sessions", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var listBody struct {
		Sessions []SessionSummary `json:"sessions"`
		Enabled  bool             `json:"enabled"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !listBody.Enabled {
		t.Fatal("enabled = false, want true with a transcript directory configured")
	}
	if len(listBody.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(listBody.Sessions))
	}
	summary := listBody.Sessions[0]
	if summary.ID != "sess-1" || summary.Workspace != "proj" || summary.UserTurns != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	// The fixture writes the document (produced) and reads it, so it lands in
	// Writes rather than the patch-only Edits list.
	if len(summary.Writes) != 1 || summary.Writes[0] != docPath {
		t.Fatalf("Writes = %v, want the indexed document", summary.Writes)
	}
	if len(summary.Edits) != 0 {
		t.Fatalf("Edits = %v, want empty (no edit patches in this session)", summary.Edits)
	}
	if summary.ToolCalls["read"] != 1 || summary.ToolCalls["write"] != 1 {
		t.Fatalf("ToolCalls = %v", summary.ToolCalls)
	}

	recorder = httptest.NewRecorder()
	api.HandleSession(recorder, httptest.NewRequest(http.MethodGet, "/api/wiki/session?id="+transcriptPath, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("single status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var single SessionSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &single); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if single.Path != transcriptPath || len(single.Intents) != 2 {
		t.Fatalf("single = %+v", single)
	}

	recorder = httptest.NewRecorder()
	api.HandleSession(recorder, httptest.NewRequest(http.MethodGet, "/api/wiki/session?id=/nope.jsonl", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown session status = %d, want 404", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	api.HandleSession(recorder, httptest.NewRequest(http.MethodGet, "/api/wiki/session", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing id status = %d, want 400", recorder.Code)
	}
}

func TestBuildContextPacketExpandsToSessions(t *testing.T) {
	api, docPath, transcriptPath := sessionFixture(t)

	packet := api.BuildContextPacket("PCIe tri-channel design", nil, 5)
	if len(packet.Documents) != 1 || packet.Documents[0].ID != docPath {
		t.Fatalf("documents = %+v", packet.Documents)
	}
	if len(packet.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want the session that touched the document", packet.Sessions)
	}
	hit := packet.Sessions[0]
	if hit.Path != transcriptPath || hit.ID != "sess-1" {
		t.Fatalf("session hit = %+v", hit)
	}
	if len(hit.Documents) != 1 || hit.Documents[0] != docPath {
		t.Fatalf("session hit documents = %v", hit.Documents)
	}
	if len(hit.Intents) == 0 {
		t.Fatalf("session hit must carry intents for the caller to judge relevance")
	}

	markdown := MarkdownContextPacket(packet)
	for _, want := range []string{"# Context:", "## Documents", "## Sessions that worked on these documents", "读取设计"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown packet missing %q:\n%s", want, markdown)
		}
	}

	empty := api.BuildContextPacket("完全不相关的查询词", nil, 5)
	if len(empty.Sessions) != 0 {
		t.Fatalf("a query with no document hit must not expand to sessions: %+v", empty.Sessions)
	}
	if !strings.Contains(MarkdownContextPacket(empty), "No indexed document") {
		t.Fatalf("empty packet must say so:\n%s", MarkdownContextPacket(empty))
	}
}

func TestContextPacketIncludesMemoryArtifacts(t *testing.T) {
	api, _, _ := sessionFixture(t)
	memoryDir := t.TempDir()
	project := filepath.Join(memoryDir, "-proj")
	if err := os.MkdirAll(filepath.Join(project, "skills", "flash-recovery"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "memory_summary.md"),
		[]byte("# Summary\nPCIe bring-up needs the 3.3V rail enabled first.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "skills", "flash-recovery", "SKILL.md"),
		[]byte("# Flash recovery\nUnrelated content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	api.SetMemoryDir(memoryDir)

	packet := api.BuildContextPacket("PCIe bring-up", nil, 5)
	if len(packet.Memory) != 1 {
		t.Fatalf("memory = %+v, want only the artifact mentioning the query", packet.Memory)
	}
	if packet.Memory[0].Kind != "summary" || !strings.Contains(packet.Memory[0].Excerpt, "3.3V rail") {
		t.Fatalf("memory artifact = %+v", packet.Memory[0])
	}

	api.SetMemoryDir(filepath.Join(memoryDir, "missing"))
	if got := api.BuildContextPacket("PCIe bring-up", nil, 5); len(got.Memory) != 0 {
		t.Fatalf("a missing memory dir must yield no memory section, got %+v", got.Memory)
	}
}

func TestMCPSessionToolsExposeDigestsNotTranscripts(t *testing.T) {
	api, docPath, transcriptPath := sessionFixture(t)

	result := api.mcpWikiSessions(map[string]any{})
	if result.IsError {
		t.Fatalf("wiki_sessions errored: %+v", result)
	}
	text := result.Content[0].Text
	for _, want := range []string{"修 PCIe 三通道", "workspace: proj", "read×1", docPath, transcriptPath} {
		if !strings.Contains(text, want) {
			t.Fatalf("wiki_sessions output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"role"`) || strings.Contains(text, "toolCall") {
		t.Fatalf("wiki_sessions must not leak transcript lines:\n%s", text)
	}

	if filtered := api.mcpWikiSessions(map[string]any{"workspace": "other"}); !strings.Contains(filtered.Content[0].Text, "No indexed agent sessions") {
		t.Fatalf("workspace filter must exclude other workspaces: %s", filtered.Content[0].Text)
	}

	if missing := api.mcpWikiContext(map[string]any{}); !missing.IsError {
		t.Fatalf("wiki_context without a query must error")
	}
}

func TestSessionsIndexDisabledWithoutUsableSource(t *testing.T) {
	if index := NewSessionsIndex("/tmp/x.json"); index != nil {
		t.Fatal("no source must disable the layer")
	}
	if index := NewSessionsIndex("/tmp/x.json", sessions.Source{Provider: sessions.OMPProvider(), Root: ""}); index != nil {
		t.Fatal("an empty root must disable the layer")
	}
	if index := NewSessionsIndex("/tmp/x.json", sessions.Source{Root: "/tmp/sessions"}); index != nil {
		t.Fatal("a source without a provider must disable the layer")
	}
	var index *SessionsIndex
	if changed, err := index.Refresh(); err != nil || changed != 0 {
		t.Fatalf("a nil layer must be a no-op: %d %v", changed, err)
	}
	if components, edges := index.Apply(BuildGraph(nil, nil), nil); components != 0 || edges != 0 {
		t.Fatalf("a nil layer must graft nothing: %d/%d", components, edges)
	}

	api := &API{graph: BuildGraph(nil, nil), ready: true}
	recorder := httptest.NewRecorder()
	api.HandleSessions(recorder, httptest.NewRequest(http.MethodGet, "/api/wiki/sessions", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"sessions":[]`) {
		t.Fatalf("disabled layer must serve an empty list, got %d %s", recorder.Code, recorder.Body.String())
	}
	// An empty list is ambiguous on its own, so the flag has to carry the
	// difference between "off" and "idle" for the panel's empty state.
	if !strings.Contains(recorder.Body.String(), `"enabled":false`) {
		t.Fatalf("disabled layer must report enabled=false, got %s", recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	api.HandleSessionsRefresh(recorder, httptest.NewRequest(http.MethodPost, "/api/wiki/sessions/refresh", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("refresh without a layer must 404, got %d", recorder.Code)
	}
}

func TestSessionRefreshPicksUpAppendedActivity(t *testing.T) {
	api, docPath, transcriptPath := sessionFixture(t)
	index := api.SessionsIndexSnapshot()

	secondDoc := filepath.Join(filepath.Dir(docPath), "plan.md")
	if err := os.WriteFile(secondDoc, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.graph.AddComponent(Component{ID: secondDoc, Path: secondDoc, Type: TypePlan, Title: "Plan", Workspace: "proj"})
	api.mu.Unlock()

	file, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	appended := fmt.Sprintf(`{"type":"message","timestamp":"2026-07-30T02:00:00.000Z","message":{"role":"assistant","content":[{"type":"toolCall","name":"edit","intent":"改计划","arguments":{"input":"[%s#1A2B]\nDEL 1\n"}}]}}`, secondDoc)
	if _, err := file.WriteString(appended + "\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	// Refresh keys on size+mtime; make the change unambiguous on coarse clocks.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(transcriptPath, future, future); err != nil {
		t.Fatal(err)
	}

	changed, err := index.Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if changed != 1 {
		t.Fatalf("appended transcript must be re-parsed, changed=%d", changed)
	}
	api.ApplySessions()

	found := false
	for _, edge := range api.graph.Forward(transcriptPath) {
		if edge.To == secondDoc && edge.Kind == EdgeKindEdits {
			found = true
		}
	}
	if !found {
		t.Fatalf("re-graft must add the newly edited document: %+v", api.graph.Forward(transcriptPath))
	}
}

// The recall query is embedded through the Bun script and ranked against the
// whole corpus, so an unbounded query is the most expensive input this API
// takes. Both entry points must reject it before any embedding happens.
func TestContextQueryIsBounded(t *testing.T) {
	api, _, _ := sessionFixture(t)
	oversized := strings.Repeat("x", contextQueryMaxBytes+1)

	if _, ok := normalizeContextQuery(oversized); ok {
		t.Fatalf("an oversized query must be rejected")
	}
	if _, ok := normalizeContextQuery("   "); ok {
		t.Fatalf("a blank query must be rejected")
	}
	if query, ok := normalizeContextQuery("  pcie  "); !ok || query != "pcie" {
		t.Fatalf("a normal query must be trimmed and accepted, got %q %v", query, ok)
	}

	recorder := httptest.NewRecorder()
	api.HandleContext(recorder, httptest.NewRequest(http.MethodGet, "/api/wiki/context?q="+oversized, nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("REST status = %d, want 400", recorder.Code)
	}

	result := api.mcpWikiContext(map[string]any{"query": oversized})
	if !result.IsError {
		t.Fatalf("MCP must reject an oversized query")
	}

	// limit clamping: a caller cannot ask for an unbounded packet, and a
	// non-positive fallback can never leak through intArg.
	if got := intArg(map[string]any{"limit": float64(9999)}, "limit", 5, mcpContextLimitMax); got != mcpContextLimitMax {
		t.Fatalf("limit = %d, want clamp to %d", got, mcpContextLimitMax)
	}
	if got := intArg(map[string]any{}, "limit", 0, mcpContextLimitMax); got < 1 {
		t.Fatalf("fallback must stay positive, got %d", got)
	}
}

// A produced document (write) and a patched document (edit) are different
// answers to "what did this session do", but both mean the session changed the
// document, so they share one edge kind.
func TestSessionEdgesTreatWritesAndEditsAsOneRelationship(t *testing.T) {
	documents := map[string]Component{
		"/repo/new.md":  {ID: "/repo/new.md", Path: "/repo/new.md", Type: TypeKnowledge},
		"/repo/old.md":  {ID: "/repo/old.md", Path: "/repo/old.md", Type: TypeDesign},
		"/repo/seen.md": {ID: "/repo/seen.md", Path: "/repo/seen.md", Type: TypeSpec},
	}
	digest := sessions.Digest{
		Path:   "/x/s.jsonl",
		Writes: []string{"/repo/new.md"},
		Edits:  []string{"/repo/old.md"},
		Reads:  []string{"/repo/seen.md", "/repo/new.md"},
	}
	byTarget := map[string]Edge{}
	for _, edge := range SessionEdges(digest, Component{ID: "/x/s.jsonl"}, documents) {
		byTarget[edge.To] = edge
	}
	if len(byTarget) != 3 {
		t.Fatalf("edges = %+v, want one per document", byTarget)
	}
	if byTarget["/repo/new.md"].Kind != EdgeKindEdits {
		t.Fatalf("a produced document must carry the edits relationship, got %q", byTarget["/repo/new.md"].Kind)
	}
	if byTarget["/repo/old.md"].Kind != EdgeKindEdits {
		t.Fatalf("a patched document must carry the edits relationship, got %q", byTarget["/repo/old.md"].Kind)
	}
	if byTarget["/repo/seen.md"].Kind != EdgeKindReads {
		t.Fatalf("a read document must carry the reads relationship, got %q", byTarget["/repo/seen.md"].Kind)
	}

	summary := SessionSummaryOf(digest, Component{ID: "/x/s.jsonl", Workspace: "proj"}, documents)
	if len(summary.Writes) != 1 || len(summary.Edits) != 1 || len(summary.Reads) != 2 {
		t.Fatalf("summary must keep the three groups apart: %+v", summary)
	}
}
