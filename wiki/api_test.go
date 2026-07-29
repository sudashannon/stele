package wiki

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandleWikiComponent_ReturnsBacklinks(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspecDir, "changes", "my-change")
	os.MkdirAll(changeDir, 0755)
	os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Design\n"), 0644)

	g, _ := BuildIndex([]WorkspaceConfig{{Alias: "miao", Path: openspecDir}}, "")
	api := NewAPI(g)

	designPath := filepath.Join(changeDir, "design.md")
	req := httptest.NewRequest("GET", "/api/wiki/component/x?id="+designPath, nil)
	w := httptest.NewRecorder()
	api.HandleComponent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWikiComponent_NotFoundReturns404(t *testing.T) {
	g := BuildGraph(nil, nil)
	api := NewAPI(g)
	req := httptest.NewRequest("GET", "/api/wiki/component/x?id=/nonexistent", nil)
	w := httptest.NewRecorder()
	api.HandleComponent(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleRecent_ReturnsSortedByUpdatedAtDescendingTop50(t *testing.T) {
	now := time.Now()
	var comps []Component
	for i := range 60 {
		id := strconv.Itoa(i)
		comps = append(comps, Component{
			ID:        id,
			Title:     "doc" + id,
			Type:      TypeSpec,
			Workspace: "ws",
			Path:      "/p/" + id,
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	g := BuildGraph(comps, nil)
	api := NewAPI(g)
	req := httptest.NewRequest("GET", "/api/wiki/recent", nil)
	w := httptest.NewRecorder()
	api.HandleRecent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var items []recentItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(items) != 50 {
		t.Fatalf("expected 50 items, got %d", len(items))
	}
	if items[0].ID != strconv.Itoa(59) {
		t.Fatalf("expected newest item first, got %s", items[0].ID)
	}
	for i := 1; i < len(items); i++ {
		if items[i].UpdatedAt.After(items[i-1].UpdatedAt) {
			t.Fatalf("items not sorted descending at index %d", i)
		}
	}
}

// TestHandleWikiComponent_ZeroBacklinksReturnsEmptyArrayNotNull guards
// against the same nil-vs-null bug already fixed for HandleLint (see
// TestHandleLint_CleanGraphReturnsEmptyArrayNotNull below): a component
// with zero backlinks — the common real-world case for a change's own
// TypeChange node, since nothing currently links TO a .comet.yaml — hits a
// map miss in (*Graph).Backlinks/Forward and gets back the unmodified nil
// slice. encoding/json serializes that as the literal `null`, and
// BacklinksPanel.tsx's useState<WikiEdge[] | null>(null) treats a `null`
// backlinks value as "not yet fetched", so it would render nothing forever
// instead of "暂无反向引用" for every well-formed but link-free change. We
// assert on the raw response bytes for the same reason the Lint test does:
// decoding `null` into a Go slice also yields nil/empty, which would mask
// this exact bug.
func TestHandleWikiComponent_ZeroBacklinksReturnsEmptyArrayNotNull(t *testing.T) {
	root := Component{ID: "root", Title: "Root Change", Type: TypeChange}
	linked := Component{ID: "linked", Title: "Linked", Type: TypeSpec}
	g := BuildGraph(
		[]Component{root, linked},
		[]Edge{{From: "root", To: "linked", Kind: "references", Source: "yaml"}},
	)
	api := NewAPI(g)

	// "root" has a forward edge but nothing points back to it — zero
	// backlinks, the exact shape of a real change's own component.
	req := httptest.NewRequest("GET", "/api/wiki/component/x?id=root", nil)
	w := httptest.NewRecorder()
	api.HandleComponent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `"backlinks":null`) {
		t.Fatalf("expected backlinks to serialize as [] not null, got %s", body)
	}
	if !strings.Contains(body, `"backlinks":[]`) {
		t.Fatalf("expected raw response to contain the empty JSON array literal for backlinks, got %s", body)
	}
}

func TestHandleLint_ReturnsIssues(t *testing.T) {
	orphan := Component{ID: "orphan", Title: "Orphan", Type: TypeSpec}
	g := BuildGraph([]Component{orphan}, nil)
	api := NewAPI(g)

	req := httptest.NewRequest("GET", "/api/wiki/lint", nil)
	w := httptest.NewRecorder()
	api.HandleLint(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var issues []LintIssue
	json.NewDecoder(w.Body).Decode(&issues)
	if len(issues) == 0 {
		t.Fatal("expected at least the orphan issue")
	}
}

// TestHandleLint_CleanGraphReturnsEmptyArrayNotNull guards against a subtle
// nil-vs-null bug: (*Graph).Lint() returns a nil slice when there are zero
// issues (Go's `var issues []LintIssue` never gets appended to), and encoding/json
// serializes a nil slice as the JSON literal `null`, not `[]`. LintPanel.tsx
// uses `useState<LintIssue[] | null>(null)` to distinguish "not yet fetched"
// from "fetched, zero issues" — if the handler ever regresses to encoding the
// raw nil slice, a clean wiki would decode to `null` and get stuck rendering
// nothing forever instead of showing "未发现问题". We assert on the raw
// response bytes (not a JSON-decoded value) because decoding `null` into a Go
// slice also yields nil/empty, which would mask this exact bug.
func TestHandleLint_CleanGraphReturnsEmptyArrayNotNull(t *testing.T) {
	root := Component{ID: "root", Title: "Root Change", Type: TypeChange}
	linked := Component{ID: "linked", Title: "Linked", Type: TypeSpec}
	g := BuildGraph(
		[]Component{root, linked},
		[]Edge{{From: "root", To: "linked", Kind: "references", Source: "yaml"}},
	)
	api := NewAPI(g)

	req := httptest.NewRequest("GET", "/api/wiki/lint", nil)
	w := httptest.NewRecorder()
	api.HandleLint(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := strings.TrimSpace(w.Body.String())
	if body != "[]" {
		t.Fatalf("expected raw response body to be the empty JSON array literal \"[]\" for a clean graph, got %q", body)
	}
}

// fakeLister is a test double for WorkspaceLister returning a fixed set of
// workspaces regardless of what was passed at construction time.
type fakeLister struct {
	workspaces []WorkspaceConfig
}

func (f *fakeLister) List() []WorkspaceConfig {
	return f.workspaces
}

// TestHandleRebuild_UsesLiveListerNotConstructionSnapshot guards against
// HandleRebuild always rebuilding from the frozen a.ws slice captured at
// NewAPIWithWorkspaces time. When a lister is set, HandleRebuild must pull
// the CURRENT workspace registry via lister.List() so that workspaces
// added/changed after startup are picked up on rebuild, instead of silently
// re-indexing the stale startup snapshot forever.
func TestHandleRebuild_UsesLiveListerNotConstructionSnapshot(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspecDir, "changes", "old-change")
	os.MkdirAll(changeDir, 0755)
	os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Old\n"), 0644)

	api, err := NewAPIWithWorkspaces([]WorkspaceConfig{{Alias: "old", Path: openspecDir}}, "")
	if err != nil {
		t.Fatalf("NewAPIWithWorkspaces: %v", err)
	}

	// New workspace registered live, after construction, containing a
	// different component than the one baked into a.ws.
	newRoot := t.TempDir()
	newOpenspecDir := filepath.Join(newRoot, "openspec")
	newChangeDir := filepath.Join(newOpenspecDir, "changes", "new-change")
	os.MkdirAll(newChangeDir, 0755)
	os.WriteFile(filepath.Join(newChangeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(newChangeDir, "design.md"), []byte("# New\n"), 0644)

	api.SetLister(&fakeLister{workspaces: []WorkspaceConfig{{Alias: "new", Path: newOpenspecDir}}})

	rebuildReq := httptest.NewRequest("POST", "/api/wiki/rebuild", nil)
	rebuildW := httptest.NewRecorder()
	api.HandleRebuild(rebuildW, rebuildReq)
	if rebuildW.Code != http.StatusOK {
		t.Fatalf("HandleRebuild: expected 200, got %d: %s", rebuildW.Code, rebuildW.Body.String())
	}

	indexReq := httptest.NewRequest("GET", "/api/wiki/index", nil)
	indexW := httptest.NewRecorder()
	api.HandleIndex(indexW, indexReq)

	var components []Component
	if err := json.Unmarshal(indexW.Body.Bytes(), &components); err != nil {
		t.Fatalf("decode index response: %v", err)
	}

	newChangeID := filepath.Join(newChangeDir, ".comet.yaml")
	oldChangeID := filepath.Join(changeDir, ".comet.yaml")
	var foundNew, foundOld bool
	for _, c := range components {
		if c.ID == newChangeID {
			foundNew = true
		}
		if c.ID == oldChangeID {
			foundOld = true
		}
	}
	if !foundNew {
		t.Errorf("expected rebuilt index to contain live-listed component %q, got %+v", newChangeID, components)
	}
	if foundOld {
		t.Errorf("expected rebuilt index to NOT contain construction-time-only component %q after lister took over", oldChangeID)
	}
}

// TestHandleRebuild_NilListerFallsBackToConstructionWorkspaces preserves the
// pre-existing behavior for APIs that never call SetLister: HandleRebuild
// must keep rebuilding from a.ws exactly as before.
func TestHandleRebuild_NilListerFallsBackToConstructionWorkspaces(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspecDir, "changes", "my-change")
	os.MkdirAll(changeDir, 0755)
	os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Design\n"), 0644)

	api, err := NewAPIWithWorkspaces([]WorkspaceConfig{{Alias: "miao", Path: openspecDir}}, "")
	if err != nil {
		t.Fatalf("NewAPIWithWorkspaces: %v", err)
	}

	rebuildReq := httptest.NewRequest("POST", "/api/wiki/rebuild", nil)
	rebuildW := httptest.NewRecorder()
	api.HandleRebuild(rebuildW, rebuildReq)
	if rebuildW.Code != http.StatusOK {
		t.Fatalf("HandleRebuild: expected 200, got %d: %s", rebuildW.Code, rebuildW.Body.String())
	}

	indexReq := httptest.NewRequest("GET", "/api/wiki/index", nil)
	indexW := httptest.NewRecorder()
	api.HandleIndex(indexW, indexReq)

	var components []Component
	if err := json.Unmarshal(indexW.Body.Bytes(), &components); err != nil {
		t.Fatalf("decode index response: %v", err)
	}

	wantID := filepath.Join(changeDir, ".comet.yaml")
	var found bool
	for _, c := range components {
		if c.ID == wantID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected rebuild-from-a.ws fallback to keep component %q, got %+v", wantID, components)
	}
}

// TestHandleGraph_ReturnsComponentsAndEdges guards against /api/wiki/graph
// regressing to a nodes-only response (the original HandleIndex gap this
// endpoint exists to fix): a change with a design_doc produces at least
// one "implements" edge (yaml-sourced, .comet.yaml -> design.md), so a
// correct HandleGraph must return non-empty components AND non-empty
// edges with that kind present.
func TestHandleGraph_ReturnsComponentsAndEdges(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspecDir, "changes", "my-change")
	os.MkdirAll(changeDir, 0755)
	os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Design\n"), 0644)

	g, _ := BuildIndex([]WorkspaceConfig{{Alias: "miao", Path: openspecDir}}, "")
	api := NewAPI(g)

	req := httptest.NewRequest("GET", "/api/wiki/graph", nil)
	w := httptest.NewRecorder()
	api.HandleGraph(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp graphResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Components) == 0 {
		t.Fatalf("expected non-empty components, got 0")
	}
	if len(resp.Edges) == 0 {
		t.Fatalf("expected non-empty edges, got 0")
	}
	foundImplements := false
	for _, e := range resp.Edges {
		if e.Kind == "implements" {
			foundImplements = true
			break
		}
	}
	if !foundImplements {
		t.Fatalf("expected an 'implements' edge among %+v", resp.Edges)
	}
}

func TestHandleFixDeadLinksRewritesExactMarkdownDestinationsAndRefreshesGraph(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "lz100", "reports", "source.md")
	oldPath := filepath.Join(root, "lz100", "secure", "v2", "LZ100_产线生产与密钥预置方案设计.md")
	newPath := filepath.Join(root, "lz100", "knowledge", "LZ100_产线生产与密钥预置方案设计.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	encodedOld := (&url.URL{Path: oldPath}).EscapedPath()
	source := strings.Join([]string{
		"[relative](../secure/v2/LZ100_产线生产与密钥预置方案设计.md)",
		"[absolute](" + oldPath + ")",
		"[encoded](file://" + encodedOld + ")",
		"`[not a link](" + oldPath + ")`",
	}, "\n")
	if err := os.WriteFile(sourcePath, []byte(source), 0o640); err != nil {
		t.Fatal(err)
	}

	component := Component{ID: sourcePath, Path: sourcePath, Type: TypeSpec, Workspace: "test"}
	oldEdges, err := ExtractMarkdownLinks(component)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldEdges) != 3 {
		t.Fatalf("expected three links to repair, got %d", len(oldEdges))
	}
	api := NewAPI(BuildGraph(
		[]Component{
			component,
			{ID: newPath, Path: newPath, Type: TypeSpec, Workspace: "test"},
		},
		oldEdges,
	))

	body, err := json.Marshal([]fixDeadLinkRequest{{SourceID: sourcePath, OldPath: oldPath, NewPath: newPath}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/wiki/fix-dead-links", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleFixDeadLinks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Results []fixDeadLinkResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || !response.Results[0].Fixed {
		t.Fatalf("expected successful repair, got %+v", response.Results)
	}

	updated, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	encodedNew := (&url.URL{Path: newPath}).EscapedPath()
	for _, want := range []string{
		"[relative](../knowledge/LZ100_产线生产与密钥预置方案设计.md)",
		"[absolute](" + newPath + ")",
		"[encoded](file://" + encodedNew + ")",
		"`[not a link](" + oldPath + ")`",
	} {
		if !strings.Contains(string(updated), want) {
			t.Errorf("repaired source missing %q:\n%s", want, updated)
		}
	}

	for _, edge := range api.graph.Forward(sourcePath) {
		if edge.To == oldPath {
			t.Fatalf("stale dead-link edge remained after repair: %+v", edge)
		}
		if edge.To != newPath {
			t.Fatalf("unexpected repaired edge: %+v", edge)
		}
	}
}

func TestHandleFixDeadLinksRewritesYAMLArtifactReferencesAndRefreshesGraph(t *testing.T) {
	root := t.TempDir()
	openspec := filepath.Join(root, "openspec")
	sourcePath := filepath.Join(openspec, "changes", "archive", "moved-change", ".comet.yaml")
	oldPath := filepath.Join(openspec, "changes", "moved-change", "design.md")
	newPath := filepath.Join(openspec, "changes", "archive", "moved-change", "design.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("design_doc: openspec/changes/moved-change/design.md\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	component := Component{ID: sourcePath, Path: sourcePath, Type: TypeChange, Workspace: "test"}
	oldEdges, err := ExtractYAMLLinks(filepath.Dir(sourcePath), root)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(BuildGraph(
		[]Component{
			component,
			{ID: newPath, Path: newPath, Type: TypeDesign, Workspace: "test"},
		},
		oldEdges,
	))
	api.ws = []WorkspaceConfig{{Alias: "test", Path: openspec}}

	body, err := json.Marshal([]fixDeadLinkRequest{{SourceID: sourcePath, OldPath: oldPath, NewPath: newPath}})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	api.HandleFixDeadLinks(w, httptest.NewRequest(http.MethodPost, "/api/wiki/fix-dead-links", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Results []fixDeadLinkResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || !response.Results[0].Fixed {
		t.Fatalf("expected successful repair, got %+v", response.Results)
	}
	updated, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if want := "design_doc: openspec/changes/archive/moved-change/design.md\n"; string(updated) != want {
		t.Fatalf("unexpected repaired YAML: got %q, want %q", updated, want)
	}
	edges := api.graph.Forward(sourcePath)
	if len(edges) != 1 || edges[0].To != newPath || edges[0].Source != "yaml" {
		t.Fatalf("unexpected refreshed edges: %+v", edges)
	}
}

func TestHandleFixDeadLinksRepairsMultipleYAMLFieldsFromOneSource(t *testing.T) {
	root := t.TempDir()
	openspec := filepath.Join(root, "openspec")
	sourcePath := filepath.Join(openspec, "changes", "archive", "moved-change", ".comet.yaml")
	oldDesign := filepath.Join(openspec, "changes", "moved-change", "design.md")
	newDesign := filepath.Join(openspec, "changes", "archive", "moved-change", "design.md")
	oldVerification := filepath.Join(openspec, "changes", "moved-change", "verification-report.md")
	newVerification := filepath.Join(openspec, "changes", "archive", "moved-change", "verification-report.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	source := "design_doc: openspec/changes/moved-change/design.md\n" +
		"verification_report: openspec/changes/moved-change/verification-report.md\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o640); err != nil {
		t.Fatal(err)
	}
	component := Component{ID: sourcePath, Path: sourcePath, Type: TypeChange, Workspace: "test"}
	oldEdges, err := ExtractYAMLLinks(filepath.Dir(sourcePath), root)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(BuildGraph(
		[]Component{
			component,
			{ID: newDesign, Path: newDesign, Type: TypeDesign, Workspace: "test"},
			{ID: newVerification, Path: newVerification, Type: TypeSpec, Workspace: "test"},
		},
		oldEdges,
	))
	api.ws = []WorkspaceConfig{{Alias: "test", Path: openspec}}

	body, err := json.Marshal([]fixDeadLinkRequest{
		{SourceID: sourcePath, OldPath: oldDesign, NewPath: newDesign},
		{SourceID: sourcePath, OldPath: oldVerification, NewPath: newVerification},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	api.HandleFixDeadLinks(w, httptest.NewRequest(http.MethodPost, "/api/wiki/fix-dead-links", strings.NewReader(string(body))))
	var response struct {
		Results []fixDeadLinkResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 2 || !response.Results[0].Fixed || !response.Results[1].Fixed {
		t.Fatalf("expected both repairs to succeed, got %+v", response.Results)
	}
	updated, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	want := "design_doc: openspec/changes/archive/moved-change/design.md\n" +
		"verification_report: openspec/changes/archive/moved-change/verification-report.md\n"
	if string(updated) != want {
		t.Fatalf("unexpected repaired YAML: got %q, want %q", updated, want)
	}
	edges := api.graph.Forward(sourcePath)
	if len(edges) != 2 || edges[0].To == oldDesign || edges[1].To == oldVerification {
		t.Fatalf("unexpected refreshed edges: %+v", edges)
	}
}

func TestRankSemanticSearch_ExactFilenameRanksAheadWithoutEmbedding(t *testing.T) {
	const targetID = "/workspace/knowledge/2026-07-14-rx101-orin-bsp-build-system-research.md"
	components := map[string]Component{
		targetID: {
			ID:        targetID,
			Path:      targetID,
			Title:     "结论摘要",
			Type:      TypeKnowledge,
			Workspace: "miao",
		},
		"semantic-result": {
			ID:        "semantic-result",
			Path:      "/workspace/knowledge/orin-build-guide.md",
			Title:     "Orin build guide",
			Type:      TypeKnowledge,
			Workspace: "miao",
		},
	}
	embeddings := map[string][]float32{
		"semantic-result": {1, 0},
	}

	results := rankSemanticSearch(
		"2026-07-14-rx101-orin-bsp-build-system-research",
		[]float32{1, 0},
		components,
		embeddings,
	)

	if len(results) != 2 {
		t.Fatalf("expected filename and semantic results, got %+v", results)
	}
	if results[0].id != targetID {
		t.Fatalf("exact filename result must rank first, got %+v", results)
	}
	if results[0].score != 1 {
		t.Fatalf("exact filename score = %v, want 1", results[0].score)
	}
}

func TestRankSemanticSearch_FilenameSubstringBypassesSimilarityFloor(t *testing.T) {
	const targetID = "/workspace/knowledge/2026-07-14-rx101-orin-bsp-build-system-research.md"
	components := map[string]Component{
		targetID: {
			ID:    targetID,
			Path:  targetID,
			Title: "结论摘要",
			Type:  TypeKnowledge,
		},
	}
	embeddings := map[string][]float32{
		targetID: {-1, 0},
	}

	results := rankSemanticSearch("rx101-orin-bsp", []float32{1, 0}, components, embeddings)
	if len(results) != 1 || results[0].id != targetID {
		t.Fatalf("filename substring must survive semantic floor, got %+v", results)
	}
}
