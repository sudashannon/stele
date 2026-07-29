package wiki

import (
	"os"
	"path/filepath"
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
