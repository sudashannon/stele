package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"comet-ui/internal/source"
)

func TestIncrementalUpdateRemovesDeletedComponentAndPersistsEmbeddings(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	deletedPath := filepath.Join(root, "docs", "deleted.md")
	otherPath := filepath.Join(root, "docs", "other.md")

	g := BuildGraph(
		[]Component{
			{ID: deletedPath, Path: deletedPath, Workspace: "test"},
			{ID: otherPath, Path: otherPath, Workspace: "test"},
		},
		[]Edge{
			{From: deletedPath, To: otherPath, Kind: "references"},
			{From: otherPath, To: deletedPath, Kind: "references"},
		},
	)
	g.SetEmbeddingEntries(map[string]EmbeddingEntry{
		deletedPath: {ID: deletedPath, InputVersion: EmbeddingInputVersion, Vector: make([]float32, 384)},
		otherPath:   {ID: otherPath, InputVersion: EmbeddingInputVersion, Vector: make([]float32, 384)},
	})
	api := &API{
		graph:         g,
		ws:            []WorkspaceConfig{{Alias: "test", Path: root}},
		indexCacheDir: cacheDir,
	}

	if err := api.IncrementalUpdate([]string{deletedPath}); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}
	if _, ok := g.Component(deletedPath); ok {
		t.Fatal("deleted component remains in graph")
	}
	if len(g.Forward(deletedPath)) != 0 || len(g.Backlinks(deletedPath)) != 0 {
		t.Fatalf("deleted component edges remain: forward=%v backlinks=%v", g.Forward(deletedPath), g.Backlinks(deletedPath))
	}
	if _, ok := g.Embeddings()[deletedPath]; ok {
		t.Fatal("deleted component embedding remains")
	}
	if api.DirtyCount() < 1 {
		t.Fatalf("DirtyCount = %d, want structural change", api.DirtyCount())
	}

	cached, err := LoadEmbeddingCache(filepath.Join(cacheDir, "embeddings.bin"))
	if err != nil {
		t.Fatalf("LoadEmbeddingCache: %v", err)
	}
	if _, ok := cached[deletedPath]; ok {
		t.Fatal("persisted cache contains deleted component")
	}
	if _, ok := cached[otherPath]; !ok {
		t.Fatal("persisted cache lost unchanged component")
	}
}

func TestIncrementalUpdateAddsOneChangedComponentWithoutFullScan(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	specDir := filepath.Join(root, "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(specDir, "new.md")
	targetPath := filepath.Join(specDir, "target.md")
	if err := os.WriteFile(specPath, []byte("# New\n\n[target](target.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	installFakeBun(t, specPath, make([]float64, 384))

	api := &API{
		graph:         BuildGraph([]Component{{ID: targetPath, Path: targetPath}}, nil),
		ws:            []WorkspaceConfig{{Alias: "test", Path: root}},
		indexCacheDir: cacheDir,
	}

	start := time.Now()
	if err := api.IncrementalUpdate([]string{specPath, specPath}); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("single-file incremental update took %v, want <2s", elapsed)
	}
	component, ok := api.graph.Component(specPath)
	if !ok || component.Title != "New" || component.Workspace != "test" {
		t.Fatalf("component not incrementally added: %+v, ok=%v", component, ok)
	}
	if component.Frontmatter["_source"] != "openspec" {
		t.Fatalf("incremental component lost source marker: %+v", component.Frontmatter)
	}
	edges := api.graph.Forward(specPath)
	if len(edges) != 1 || edges[0].To != targetPath || edges[0].Source != "markdown-link" {
		t.Fatalf("incremental links = %+v, want markdown link to target", edges)
	}
	if len(api.graph.Embeddings()[specPath]) != 384 {
		t.Fatalf("embedding length = %d, want 384", len(api.graph.Embeddings()[specPath]))
	}
	if api.DirtyCount() != 2 {
		t.Fatalf("DirtyCount = %d, want one component plus one edge", api.DirtyCount())
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
