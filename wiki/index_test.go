package wiki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildIndex_EndToEnd(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspecDir, "changes", "my-change")
	os.MkdirAll(changeDir, 0755)
	os.WriteFile(filepath.Join(changeDir, "proposal.md"), []byte("# Proposal\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Design\n"), 0644)

	ws := []WorkspaceConfig{{Alias: "miao", Path: openspecDir, Color: "#0063f8"}}
	g, err := BuildIndex(ws, "")
	if err != nil {
		t.Fatal(err)
	}

	designPath := filepath.Join(changeDir, "design.md")
	if _, ok := g.Component(designPath); !ok {
		t.Fatalf("expected design.md to be indexed as a component")
	}
	back := g.Backlinks(designPath)
	if len(back) < 3 {
		t.Fatalf("expected at least 3 backlinks to design.md (.comet.yaml + proposal.md convention edge + vector similarity edge), got %+v", back)
	}

	// The change directory itself must be a TypeChange component keyed by
	// its .comet.yaml path — that's the From endpoint ExtractYAMLLinks
	// uses for edges, so without this node the change has no identity in
	// the graph and BacklinksPanel can never resolve it (Phase③ closeout).
	yamlPath := filepath.Join(changeDir, ".comet.yaml")
	changeComp, ok := g.Component(yamlPath)
	if !ok {
		t.Fatalf("expected a component for the change directory keyed by %s", yamlPath)
	}
	if changeComp.Type != TypeChange {
		t.Fatalf("expected change component Type to be TypeChange, got %q", changeComp.Type)
	}
	if changeComp.Title != "my-change" {
		t.Fatalf("expected change component Title to be %q, got %q", "my-change", changeComp.Title)
	}

	// It must have a resolvable ownership edge to design.md. Vector neighbors
	// are intentionally data-dependent and may include proposal.md as the
	// semantic extractor evolves, so they are not asserted by position/count.
	fwd := g.Forward(yamlPath)
	foundImplements := false
	for _, edge := range fwd {
		if edge.To == designPath && edge.Kind == "implements" && edge.Source == "yaml" {
			foundImplements = true
			break
		}
	}
	if !foundImplements {
		t.Fatalf("expected change component to implement design.md, got %+v", fwd)
	}
}

func TestBuildIndex_ToleratesRepoRootPath(t *testing.T) {
	// A workspace registered as the repo ROOT (Path has no changes/ but
	// does have openspec/changes/) must still yield a change component and
	// its edges by descending into openspec/ — mirrors scanAllChanges.
	root := t.TempDir()
	changeDir := filepath.Join(root, "openspec", "changes", "my-change")
	os.MkdirAll(changeDir, 0755)
	os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Design\n"), 0644)

	ws := []WorkspaceConfig{{Alias: "miao", Path: root, Color: "#0063f8"}}
	g, err := BuildIndex(ws, "")
	if err != nil {
		t.Fatal(err)
	}

	yamlPath := filepath.Join(changeDir, ".comet.yaml")
	changeComp, ok := g.Component(yamlPath)
	if !ok {
		t.Fatalf("expected a change component keyed by %s when workspace Path is the repo root", yamlPath)
	}
	if changeComp.Type != TypeChange || changeComp.Title != "my-change" {
		t.Fatalf("expected TypeChange component titled 'my-change', got %+v", changeComp)
	}

	designPath := filepath.Join(changeDir, "design.md")
	fwd := g.Forward(yamlPath)
	if len(fwd) != 2 || fwd[0].To != designPath || fwd[1].To != designPath {
		t.Fatalf("expected 2 forward edges (implements + vector similarity) from change component to design.md, got %+v", fwd)
	}
}

func TestBuildIndex_ArchiveChangesGetYAMLEdges(t *testing.T) {
	dir := t.TempDir()
	// Create workspace structure: <dir>/openspec/changes/archive/2026-06-04-test-change/
	openspecDir := filepath.Join(dir, "openspec")
	archiveChangeDir := filepath.Join(openspecDir, "changes", "archive", "2026-06-04-test-change")
	os.MkdirAll(archiveChangeDir, 0755)

	// Create a target spec file that .comet.yaml references
	specDir := filepath.Join(dir, "docs", "superpowers", "specs")
	os.MkdirAll(specDir, 0755)
	os.WriteFile(filepath.Join(specDir, "test-design.md"), []byte("# Test Design\n"), 0644)

	// Create .comet.yaml with design_doc reference
	os.WriteFile(filepath.Join(archiveChangeDir, ".comet.yaml"), []byte(
		"phase: archive\ndesign_doc: docs/superpowers/specs/test-design.md\n",
	), 0644)

	// Create the design.md so ScanComponents picks it up
	os.WriteFile(filepath.Join(archiveChangeDir, "design.md"), []byte("# Design\n"), 0644)

	ws := []WorkspaceConfig{{Alias: "test", Path: openspecDir}}
	g, err := BuildIndex(ws, "")
	if err != nil {
		t.Fatal(err)
	}

	// The .comet.yaml node should have forward edges
	yamlID := filepath.Join(archiveChangeDir, ".comet.yaml")
	edges := g.Forward(yamlID)
	if len(edges) == 0 {
		t.Errorf("expected YAML edges from archived change, got 0")
	}
}

func TestBuildIndexEnrichesLiveGraphButStripsSyntheticTagsFromCache(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspecDir, "changes", "kmc-rollout")
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(changeDir, ".comet.yaml")
	designPath := filepath.Join(changeDir, "design.md")
	yamlSource := []byte("tags: [orin]\ndesign_doc: design.md\n")
	designSource := []byte("# Design\n\nSource content stays unchanged.\n")
	if err := os.WriteFile(yamlPath, yamlSource, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(designPath, designSource, 0o644); err != nil {
		t.Fatal(err)
	}
	installFakeBun(t, yamlPath, make([]float64, 384))

	cacheDir := t.TempDir()
	graph, err := BuildIndex(
		[]WorkspaceConfig{{Alias: "test", Path: openspecDir}},
		cacheDir,
	)
	if err != nil {
		t.Fatal(err)
	}

	change, ok := graph.Component(yamlPath)
	if !ok || change.Frontmatter[derivedTagsKey] == nil {
		t.Fatalf("live change component lacks derived provenance: %+v, ok=%v", change, ok)
	}
	design, ok := graph.Component(designPath)
	if !ok || design.Frontmatter[inheritedTagsKey] == nil {
		t.Fatalf("live design component lacks inherited provenance: %+v, ok=%v", design, ok)
	}

	cacheData, err := os.ReadFile(filepath.Join(cacheDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cached []Component
	if err := json.Unmarshal(cacheData, &cached); err != nil {
		t.Fatal(err)
	}
	for _, component := range cached {
		if _, exists := component.Frontmatter[derivedTagsKey]; exists {
			t.Fatalf("cache persisted %s for %s", derivedTagsKey, component.ID)
		}
		if _, exists := component.Frontmatter[inheritedTagsKey]; exists {
			t.Fatalf("cache persisted %s for %s", inheritedTagsKey, component.ID)
		}
	}

	gotYAML, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	gotDesign, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotYAML) != string(yamlSource) || string(gotDesign) != string(designSource) {
		t.Fatal("full indexing modified source artifacts")
	}
}

func TestBuildIndexIncludesTagEdgesInGraphAndCache(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changesDir := filepath.Join(openspecDir, "changes")
	taggedIDs := make(map[string]bool, 3)
	for i := range 100 {
		changeDir := filepath.Join(changesDir, fmt.Sprintf("change-%03d", i))
		if err := os.MkdirAll(changeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := []byte("{}\n")
		if i < 3 {
			content = []byte("tags: [orin]\n")
		}
		id := filepath.Join(changeDir, ".comet.yaml")
		if err := os.WriteFile(id, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			taggedIDs[id] = true
		}
	}

	firstTaggedID := filepath.Join(changesDir, "change-000", ".comet.yaml")
	installFakeBun(t, firstTaggedID, make([]float64, 384))
	cacheDir := t.TempDir()
	graph, err := BuildIndex(
		[]WorkspaceConfig{{Alias: "test", Path: openspecDir}},
		cacheDir,
	)
	if err != nil {
		t.Fatal(err)
	}

	var graphTagEdges []Edge
	for id := range graph.Components() {
		for _, edge := range graph.Forward(id) {
			if edge.Source == "tag" {
				graphTagEdges = append(graphTagEdges, edge)
			}
		}
	}
	if len(graphTagEdges) != 3 {
		t.Fatalf("full graph tag edge count = %d, want one 3-member cycle: %+v", len(graphTagEdges), graphTagEdges)
	}
	for _, edge := range graphTagEdges {
		if !taggedIDs[edge.From] || !taggedIDs[edge.To] {
			t.Fatalf("tag edge connects an untagged component: %+v", edge)
		}
		if edge.Kind != "shares-tag:orin" || edge.Weight <= 0 {
			t.Fatalf("unexpected tag edge identity or weight: %+v", edge)
		}
	}

	cacheData, err := os.ReadFile(filepath.Join(cacheDir, "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cachedEdges []Edge
	if err := json.Unmarshal(cacheData, &cachedEdges); err != nil {
		t.Fatal(err)
	}
	var cachedTagEdges []Edge
	for _, edge := range cachedEdges {
		if edge.Source == "tag" {
			cachedTagEdges = append(cachedTagEdges, edge)
		}
	}
	if len(cachedTagEdges) != len(graphTagEdges) {
		t.Fatalf("cache tag edge count = %d, graph has %d: cache=%+v graph=%+v",
			len(cachedTagEdges), len(graphTagEdges), cachedTagEdges, graphTagEdges)
	}
}

func TestPersistIndexCacheLogsEachWriteFailure(t *testing.T) {
	cacheDir := t.TempDir()
	indexPath := filepath.Join(cacheDir, "index.json")
	graphPath := filepath.Join(cacheDir, "graph.json")
	if err := os.Mkdir(indexPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(graphPath, 0o755); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
	})

	persistIndexCache(
		cacheDir,
		[]Component{{ID: "a"}},
		[]Edge{{From: "a", To: "b", Kind: "references", Source: "yaml"}},
	)

	output := logs.String()
	if want := "could not write index cache " + indexPath; !strings.Contains(output, want) {
		t.Fatalf("missing index write failure %q in log:\n%s", want, output)
	}
	if want := "could not write graph cache " + graphPath; !strings.Contains(output, want) {
		t.Fatalf("missing graph write failure %q in log:\n%s", want, output)
	}
}
