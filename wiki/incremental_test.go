package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stele/internal/source"
)

func TestPrepareIncrementalTagChangesClassifiesRebuildsAndPreservesSyntheticTags(t *testing.T) {
	taxonomy := LoadTaxonomy()
	basePath := filepath.Join(t.TempDir(), "openspec", "changes", "orin-rollout", "design.md")
	base := Component{
		ID:        basePath,
		Path:      basePath,
		Type:      TypeDesign,
		Title:     "Before",
		Workspace: "test",
		Frontmatter: map[string]any{
			"tags":           []any{"Jetson Orin", "Local Label"},
			derivedTagsKey:   []string{"orin"},
			inheritedTagsKey: []string{"kmc"},
		},
	}
	api := &API{graph: BuildGraph([]Component{base}, nil)}

	safe := base
	safe.Title = "After"
	safe.UpdatedAt = time.Now()
	safe.Frontmatter = map[string]any{"tags": []any{"ORIN", "local label"}}
	safeChanges := []incrementalChange{{path: basePath, component: &safe}}
	if api.prepareIncrementalTagChanges(safeChanges, taxonomy) {
		t.Fatal("title/timestamp and canonical-equivalent explicit tags must stay incremental")
	}
	if got := safeChanges[0].component.Frontmatter[derivedTagsKey]; got == nil {
		t.Fatal("safe replacement did not preserve derived tag provenance")
	}
	if got := safeChanges[0].component.Frontmatter[inheritedTagsKey]; got == nil {
		t.Fatal("safe replacement did not preserve inherited tag provenance")
	}

	changedType := safe
	changedType.Type = TypePlan
	changedPath := safe
	changedPath.Path = filepath.Join(filepath.Dir(basePath), "renamed.md")
	changedExplicit := safe
	changedExplicit.Frontmatter = map[string]any{"tags": []any{"kmc", "Local Label"}}
	changedDerived := safe
	changedDerived.Path = filepath.Join(filepath.Dir(filepath.Dir(basePath)), "kmc-rollout", "design.md")
	createdPath := filepath.Join(filepath.Dir(basePath), "new.md")
	created := safe
	created.ID, created.Path = createdPath, createdPath

	for _, tc := range []struct {
		name   string
		change incrementalChange
		graph  *Graph
	}{
		{name: "create", change: incrementalChange{path: createdPath, component: &created}, graph: BuildGraph([]Component{base}, nil)},
		{name: "delete", change: incrementalChange{path: basePath}, graph: BuildGraph([]Component{base}, nil)},
		{name: "type", change: incrementalChange{path: basePath, component: &changedType}, graph: BuildGraph([]Component{base}, nil)},
		{name: "path", change: incrementalChange{path: basePath, component: &changedPath}, graph: BuildGraph([]Component{base}, nil)},
		{name: "canonical explicit inputs", change: incrementalChange{path: basePath, component: &changedExplicit}, graph: BuildGraph([]Component{base}, nil)},
		{name: "derived tags", change: incrementalChange{path: basePath, component: &changedDerived}, graph: BuildGraph([]Component{base}, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testAPI := &API{graph: tc.graph}
			if !testAPI.prepareIncrementalTagChanges([]incrementalChange{tc.change}, taxonomy) {
				t.Fatalf("%s change must force a full rebuild", tc.name)
			}
		})
	}

	if !containsChangeMetadata([]string{filepath.Join(filepath.Dir(basePath), ".comet.yaml")}) {
		t.Fatal(".comet.yaml changes must force a full rebuild")
	}
}

func TestIncrementalUpdatePreservesSyntheticTagsForContentOnlyChanges(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(specDir, "existing.md")
	targetPath := filepath.Join(specDir, "target.md")
	source := []byte("# Updated title\n\nChanged body.\n")
	if err := os.WriteFile(specPath, source, 0o644); err != nil {
		t.Fatal(err)
	}
	installFakeBun(t, specPath, make([]float64, 384))

	before := Component{
		ID:        specPath,
		Path:      specPath,
		Type:      TypeSpec,
		Title:     "Old title",
		Workspace: "test",
		Frontmatter: map[string]any{
			derivedTagsKey:   []string{"orin"},
			inheritedTagsKey: []string{"kmc"},
		},
	}
	beforeTags := EffectiveComponentTags(before, LoadTaxonomy())
	target := Component{ID: targetPath, Path: targetPath, Type: TypeSpec, Workspace: "test"}
	tagEdge := Edge{
		From: specPath, To: targetPath,
		Kind: "shares-tag:orin", Source: "tag", Weight: 0.37,
	}
	api := &API{
		graph: BuildGraph([]Component{before, target}, []Edge{tagEdge}),
		ws:    []WorkspaceConfig{{Alias: "test", Path: root}},
	}

	if err := api.IncrementalUpdate([]string{specPath}); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}
	after, ok := api.graph.Component(specPath)
	if !ok {
		t.Fatal("content-only update removed component")
	}
	if after.Title != "Updated title" {
		t.Fatalf("updated title = %q, want %q", after.Title, "Updated title")
	}
	if after.Frontmatter[derivedTagsKey] == nil || after.Frontmatter[inheritedTagsKey] == nil {
		t.Fatalf("content-only update lost synthetic tags: %+v", after.Frontmatter)
	}
	if afterTags := EffectiveComponentTags(after, LoadTaxonomy()); !equalStrings(afterTags, beforeTags) {
		t.Fatalf("effective tags after incremental update = %v, want full-build tags %v", afterTags, beforeTags)
	}
	forward := api.graph.Forward(specPath)
	if len(forward) != 1 || forward[0] != tagEdge {
		t.Fatalf("outgoing tag edge after content update = %#v; want %#v", forward, tagEdge)
	}
	backlinks := api.graph.Backlinks(targetPath)
	if len(backlinks) != 1 || backlinks[0] != tagEdge {
		t.Fatalf("tag backlink after content update = %#v; want %#v", backlinks, tagEdge)
	}
	persisted, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted) != string(source) {
		t.Fatalf("incremental enrichment modified source: %q", persisted)
	}
}

func TestIncrementalUpdateAddsIncomingConventionEdge(t *testing.T) {
	root := t.TempDir()
	openspec := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspec, "changes", "new-change")
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	proposalPath := filepath.Join(changeDir, "proposal.md")
	designPath := filepath.Join(changeDir, "design.md")
	if err := os.WriteFile(proposalPath, []byte("# Proposal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(designPath, []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installFakeBun(t, designPath, make([]float64, 384))

	api := &API{
		graph: BuildGraph([]Component{{
			ID: proposalPath, Path: proposalPath, Type: TypeProposal, Workspace: "test",
		}}, nil),
		ws: []WorkspaceConfig{{Alias: "test", Path: openspec}},
	}
	if err := api.IncrementalUpdate([]string{proposalPath, designPath}); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}
	edges := api.graph.Forward(proposalPath)
	if len(edges) != 1 || edges[0].To != designPath || edges[0].Source != "convention-internal" {
		t.Fatalf("proposal edges = %+v, want incoming convention edge to design", edges)
	}
}

func TestIncrementalUpdateRefreshesIncomingVectorEdges(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aPath := filepath.Join(specDir, "a.md")
	bPath := filepath.Join(specDir, "b.md")
	cPath := filepath.Join(specDir, "c.md")
	if err := os.WriteFile(aPath, []byte("# A changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldA := make([]float32, 384)
	bVec := make([]float32, 384)
	cVec := make([]float32, 384)
	oldA[0], bVec[0], cVec[1] = 1, 1, 1
	oldEmbeddings := map[string][]float32{aPath: oldA, bPath: bVec, cPath: cVec}
	components := []Component{
		{ID: aPath, Path: aPath, Type: TypeSpec},
		{ID: bPath, Path: bPath, Type: TypeSpec},
		{ID: cPath, Path: cPath, Type: TypeSpec},
	}
	graph := BuildGraph(components, ComputeVectorSimilarityEdges(oldEmbeddings, 3, 0.5))
	graph.SetEmbeddings(oldEmbeddings)

	newA := make([]float64, 384)
	newA[1] = 1
	installFakeBun(t, aPath, newA)
	api := &API{graph: graph, ws: []WorkspaceConfig{{Alias: "test", Path: root}}}
	if err := api.IncrementalUpdate([]string{aPath}); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}

	if hasEdge(graph.Forward(bPath), aPath, "vector") {
		t.Fatal("stale vector edge b->a remains after a embedding changed")
	}
	if !hasEdge(graph.Forward(cPath), aPath, "vector") {
		t.Fatalf("new vector edge c->a missing: %+v", graph.Forward(cPath))
	}
}

func installFakeBun(t *testing.T, id string, vector []float64) {
	t.Helper()
	payload, err := json.Marshal([]embedOutput{{ID: id, Vector: vector}})
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	fakeBun := filepath.Join(binDir, "bun")
	if err := os.WriteFile(fakeBun, []byte("#!/bin/sh\nprintf '%s\\n' \"$EMBED_OUTPUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EMBED_OUTPUT", string(payload))
}

func hasEdge(edges []Edge, target, source string) bool {
	for _, edge := range edges {
		if edge.To == target && edge.Source == source {
			return true
		}
	}
	return false
}

func TestResolveWorkspaceUsesLiveListerAndLongestPrefix(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	nested := filepath.Join(root, "docs")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	api := &API{
		graph: BuildGraph(nil, nil),
		ws:    []WorkspaceConfig{{Alias: "stale", Path: parent}},
		lister: staticWorkspaceLister{items: []WorkspaceConfig{
			{Alias: "broad", Path: root},
			{Alias: "specific", Path: nested},
		}},
	}

	alias, wsPath := api.resolveWorkspace(filepath.Join(nested, "spec.md"))
	if alias != "specific" || wsPath != nested {
		t.Fatalf("resolveWorkspace = (%q, %q), want (%q, %q)", alias, wsPath, "specific", nested)
	}
}

func TestIncrementalSourceOwnershipRequiresFullRebuildOnlyForStandaloneSuperpowers(t *testing.T) {
	superpowersRoot := t.TempDir()
	superpowersPath := filepath.Join(superpowersRoot, "docs", "superpowers", "specs", "2026-07-20-cache-design.md")
	if err := os.MkdirAll(filepath.Dir(superpowersPath), 0o755); err != nil {
		t.Fatal(err)
	}
	api := &API{
		graph: BuildGraph(nil, nil),
		ws: []WorkspaceConfig{{
			Alias: "superpowers", Path: superpowersRoot, Type: source.KindSuperpowers,
		}},
	}
	if !api.sourceRequiresFullRebuild([]string{superpowersPath}) {
		t.Fatal("standalone Superpowers updates must rebuild convention edges and ownership")
	}
	knowledgePath := filepath.Join(superpowersRoot, "knowledge", "2026-07-20-cache-operations.md")
	if err := os.MkdirAll(filepath.Dir(knowledgePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if !api.sourceRequiresFullRebuild([]string{knowledgePath}) {
		t.Fatal("standalone Superpowers knowledge updates must rebuild the index")
	}

	openSpecRoot := t.TempDir()
	openSpecPath := filepath.Join(openSpecRoot, "docs", "superpowers", "specs", "2026-07-20-cache-design.md")
	for _, dir := range []string{filepath.Join(openSpecRoot, "openspec", "changes"), filepath.Dir(openSpecPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	api.ws = []WorkspaceConfig{{Alias: "openspec", Path: openSpecRoot}}
	if api.sourceRequiresFullRebuild([]string{openSpecPath}) {
		t.Fatal("Comet-owned Superpowers documents must retain OpenSpec incremental ownership")
	}
}

type staticWorkspaceLister struct {
	items []WorkspaceConfig
}

func (l staticWorkspaceLister) List() []WorkspaceConfig { return l.items }
