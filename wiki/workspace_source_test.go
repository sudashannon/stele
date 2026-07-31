package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"stele/internal/source"
)

func TestTrellisIndexSourceBuildsDurableComponentsAndEdges(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, ".trellis", "tasks", "07-26-beta")
	if err := os.MkdirAll(filepath.Join(taskDir, "research"), 0o755); err != nil {
		t.Fatal(err)
	}
	taskJSON, err := json.Marshal(map[string]any{
		"id": "beta", "title": "Beta Task", "status": "in_progress",
		"createdAt": "2026-07-26",
	})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(taskDir, "task.json"):                               string(taskJSON),
		filepath.Join(taskDir, "prd.md"):                                  "# Beta PRD\n",
		filepath.Join(taskDir, "design.md"):                               "# Beta Design\n",
		filepath.Join(taskDir, "implement.md"):                            "# Beta Implementation\n",
		filepath.Join(taskDir, "implement.jsonl"):                         "{\"file\":\".trellis/spec/backend/index.md\"}\n",
		filepath.Join(taskDir, "research", "tradeoffs.md"):                "# Tradeoffs\n",
		filepath.Join(root, ".trellis", "spec", "backend", "index.md"):    "# Backend Spec\n",
		filepath.Join(root, ".trellis", "workspace", "dev", "journal.md"): "# Journal\n",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	components, edges, err := (trellisIndexSource{}).Scan(WorkspaceConfig{
		Alias: "trellis", Path: root, Type: source.KindTrellis,
	})
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]Component, len(components))
	for _, component := range components {
		byPath[filepath.Clean(component.Path)] = component
		if component.Frontmatter["_source"] != string(source.KindTrellis) {
			t.Fatalf("component missing Trellis source marker: %+v", component)
		}
	}
	taskPath := filepath.Join(taskDir, "task.json")
	prdPath := filepath.Join(taskDir, "prd.md")
	designPath := filepath.Join(taskDir, "design.md")
	implementPath := filepath.Join(taskDir, "implement.md")
	specPath := filepath.Join(root, ".trellis", "spec", "backend", "index.md")
	if byPath[taskPath].Type != TypeChange || byPath[taskPath].Title != "Beta Task" {
		t.Fatalf("unexpected task component: %+v", byPath[taskPath])
	}
	if byPath[prdPath].Type != TypeProposal || byPath[designPath].Type != TypeDesign || byPath[implementPath].Type != TypeTasks {
		t.Fatalf("unexpected workflow artifact types: %+v", byPath)
	}

	hasEdge := func(from, to, kind, edgeSource string) bool {
		for _, edge := range edges {
			if edge.From == from && edge.To == to && edge.Kind == kind && edge.Source == edgeSource {
				return true
			}
		}
		return false
	}
	for _, target := range []string{prdPath, designPath, implementPath} {
		if !hasEdge(taskPath, target, "generates", "convention-internal") {
			t.Fatalf("missing task generation edge to %s: %+v", target, edges)
		}
	}
	if hasEdge(taskPath, taskPath, "generates", "convention-internal") {
		t.Fatal("task.json must not generate itself")
	}
	if !hasEdge(prdPath, designPath, "implements", "convention-internal") ||
		!hasEdge(designPath, implementPath, "implements", "convention-internal") {
		t.Fatalf("missing Trellis workflow chain: %+v", edges)
	}
	if !hasEdge(taskPath, specPath, "references", "task-context") {
		t.Fatalf("missing context reference edge: %+v", edges)
	}
	if _, exists := byPath[filepath.Join(taskDir, "implement.jsonl")]; exists {
		t.Fatal("runtime context JSONL must not become a graph node")
	}
}

func TestDeduplicateEdgesPreservesFirstOccurrence(t *testing.T) {
	first := Edge{From: "parent", To: "child", Kind: "parent-of", Source: "task-json"}
	second := Edge{From: "task", To: "prd", Kind: "generates", Source: "convention-internal"}
	got := deduplicateEdges([]Edge{first, second, first})
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("unexpected deduplicated edges: %+v", got)
	}
}

func TestSuperpowersIndexSourceBuildsDurableComponentsAndEdges(t *testing.T) {
	root := t.TempDir()
	paths := map[string]string{
		"spec":      filepath.Join(root, "docs", "superpowers", "specs", "2026-07-20-cache-design.md"),
		"plan":      filepath.Join(root, "docs", "superpowers", "plans", "2026-07-21-cache.md"),
		"artifact":  filepath.Join(root, "docs", "superpowers", "artifacts", "cache", "review.md"),
		"report":    filepath.Join(root, "docs", "superpowers", "reports", "2026-07-22-cache-verify.md"),
		"knowledge": filepath.Join(root, "knowledge", "2026-07-23-cache-operations.md"),
	}
	for label, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# "+label+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	components, edges, err := (superpowersIndexSource{}).Scan(WorkspaceConfig{
		Alias: "superpowers", Path: root, Type: source.KindSuperpowers,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 5 {
		t.Fatalf("expected four workflow components plus knowledge, got %+v", components)
	}
	for _, component := range components {
		if component.Frontmatter["_source"] != string(source.KindSuperpowers) {
			t.Fatalf("component missing Superpowers ownership: %+v", component)
		}
		if component.Path == paths["knowledge"] && component.Type != TypeKnowledge {
			t.Fatalf("knowledge document classified as %q", component.Type)
		}
	}
	if !containsWorkspaceEdge(edges, paths["plan"], paths["spec"], "implements") ||
		!containsWorkspaceEdge(edges, paths["plan"], paths["artifact"], "generates") ||
		!containsWorkspaceEdge(edges, paths["report"], paths["plan"], "traces-back") ||
		!containsWorkspaceEdge(edges, paths["report"], paths["artifact"], "traces-back") {
		t.Fatalf("missing Superpowers convention edges: %+v", edges)
	}
}

func TestOpenSpecOwnsSuperpowersDocumentsInMixedProject(t *testing.T) {
	root := t.TempDir()
	changeDir := filepath.Join(root, "openspec", "changes", "cache")
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("phase: design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designPath := filepath.Join(root, "docs", "superpowers", "specs", "2026-07-20-cache-design.md")
	if err := os.MkdirAll(filepath.Dir(designPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(designPath, []byte("# Cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, resolved, err := indexSourceFor(WorkspaceConfig{Alias: "mixed", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(openSpecIndexSource); !ok || resolved.Type != source.KindOpenSpec {
		t.Fatalf("mixed project must resolve to OpenSpec, got %T %+v", adapter, resolved)
	}
	components, _, err := adapter.Scan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, component := range components {
		if filepath.Clean(component.Path) != designPath {
			continue
		}
		found = true
		if component.Frontmatter["_source"] != string(source.KindOpenSpec) {
			t.Fatalf("Comet-owned Superpowers document was reclassified: %+v", component)
		}
	}
	if !found {
		t.Fatalf("OpenSpec adapter did not index linked Superpowers document %q", designPath)
	}
}

func containsWorkspaceEdge(edges []Edge, from, to, kind string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Kind == kind && edge.Source == "superpowers-convention" {
			return true
		}
	}
	return false
}
