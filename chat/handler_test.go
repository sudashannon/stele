package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeWikiGraph is a minimal WikiGraphAccessor stub for exercising
// buildGraphContext without depending on the real wiki package.
type fakeWikiGraph struct {
	direct    []NeighborInfo
	secondHop []string
	overview  string
}

func (f *fakeWikiGraph) Neighborhood(changeID string) ([]NeighborInfo, []string) {
	return f.direct, f.secondHop
}

func (f *fakeWikiGraph) CommunityOverview(changeID string) string {
	return f.overview
}

func setupChangeDir(t *testing.T, name string) string {
	t.Helper()
	openspecDir := t.TempDir()
	changeDir := filepath.Join(openspecDir, "changes", name)
	if err := os.MkdirAll(changeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return openspecDir
}

func TestBuildGraphContext_DisabledReturnsEmpty(t *testing.T) {
	openspecDir := setupChangeDir(t, "my-change")
	wg := &fakeWikiGraph{direct: []NeighborInfo{{ID: "x", Title: "X", Kind: "references"}}}

	got := buildGraphContext(openspecDir, "my-change", false, wg)
	if got != "" {
		t.Errorf("expected empty string when includeGraph=false, got %q", got)
	}
}

func TestBuildGraphContext_NilAccessorReturnsEmpty(t *testing.T) {
	openspecDir := setupChangeDir(t, "my-change")

	got := buildGraphContext(openspecDir, "my-change", true, nil)
	if got != "" {
		t.Errorf("expected empty string when wikiGraph is nil, got %q", got)
	}
}

func TestBuildGraphContext_UnknownChangeReturnsEmpty(t *testing.T) {
	openspecDir := setupChangeDir(t, "my-change")
	wg := &fakeWikiGraph{direct: []NeighborInfo{{ID: "x", Title: "X", Kind: "references"}}}

	got := buildGraphContext(openspecDir, "no-such-change", true, wg)
	if got != "" {
		t.Errorf("expected empty string for a change with no .comet.yaml, got %q", got)
	}
}

func TestBuildGraphContext_IncludesDirectAndSecondHopAndOverview(t *testing.T) {
	openspecDir := setupChangeDir(t, "my-change")
	wg := &fakeWikiGraph{
		direct:    []NeighborInfo{{ID: "b", Title: "Design B", Kind: "implements"}},
		secondHop: []string{"Spec C"},
		overview:  "这是社区综述内容",
	}

	got := buildGraphContext(openspecDir, "my-change", true, wg)

	if !strings.Contains(got, "## 图谱上下文") {
		t.Errorf("expected graph context header, got %q", got)
	}
	if !strings.Contains(got, "[implements] Design B") {
		t.Errorf("expected direct neighbor entry, got %q", got)
	}
	if !strings.Contains(got, "间接关联(2-hop)") || !strings.Contains(got, "Spec C") {
		t.Errorf("expected 2-hop section with Spec C, got %q", got)
	}
	if !strings.Contains(got, "## 所属主题综述") || !strings.Contains(got, "这是社区综述内容") {
		t.Errorf("expected community overview section, got %q", got)
	}
}

func TestBuildGraphContext_NoNeighborsOrOverviewReturnsEmpty(t *testing.T) {
	openspecDir := setupChangeDir(t, "my-change")
	wg := &fakeWikiGraph{}

	got := buildGraphContext(openspecDir, "my-change", true, wg)
	if got != "" {
		t.Errorf("expected empty string when accessor reports nothing, got %q", got)
	}
}

func TestBuildGraphContextForComponent_UsesSourceNeutralID(t *testing.T) {
	wg := &fakeWikiGraph{
		direct:   []NeighborInfo{{ID: "prd", Title: "Task PRD", Kind: "generates"}},
		overview: "Trellis task community",
	}
	got := buildGraphContextForComponent("", "", "/repo/.trellis/tasks/07-26-beta/task.json", true, wg)
	if !strings.Contains(got, "[generates] Task PRD") || !strings.Contains(got, "Trellis task community") {
		t.Fatalf("expected component-id graph context, got %q", got)
	}
}
