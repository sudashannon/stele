package wiki

import (
	"os"
	"testing"

	"stele/chat"
)

// buildLinearGraph constructs A -> B -> C -> D (forward edges only), where
// each node's Component.Title is "Title-<id>", to exercise the 1-hop
// (forward+backlink) and 2-hop traversal in Neighborhood.
func buildLinearGraph() *Graph {
	components := []Component{
		{ID: "A", Title: "Title-A"},
		{ID: "B", Title: "Title-B"},
		{ID: "C", Title: "Title-C"},
		{ID: "D", Title: "Title-D"},
	}
	edges := []Edge{
		{From: "A", To: "B", Kind: "references"},
		{From: "B", To: "C", Kind: "implements"},
		{From: "C", To: "D", Kind: "generates"},
	}
	return BuildGraph(components, edges)
}

func TestNeighborhood_DirectIncludesForwardAndBacklinks(t *testing.T) {
	g := buildLinearGraph()
	api := NewAPI(g)

	// From B: forward neighbor is C, backlink neighbor is A.
	direct, _ := api.Neighborhood("B")
	if len(direct) != 2 {
		t.Fatalf("expected 2 direct neighbors, got %d: %+v", len(direct), direct)
	}
	byID := map[string]chat.NeighborInfo{}
	for _, n := range direct {
		byID[n.ID] = n
	}
	if n, ok := byID["C"]; !ok || n.Title != "Title-C" || n.Kind != "implements" {
		t.Errorf("expected forward neighbor C with title/kind, got %+v (ok=%v)", n, ok)
	}
	if n, ok := byID["A"]; !ok || n.Title != "Title-A" || n.Kind != "references" {
		t.Errorf("expected backlink neighbor A with title/kind, got %+v (ok=%v)", n, ok)
	}
}

func TestNeighborhood_SecondHopExcludesAlreadySeenNodes(t *testing.T) {
	g := buildLinearGraph()
	api := NewAPI(g)

	// From B: direct = {A, C}. 2-hop from C is D; 2-hop from A has no
	// forward edges. B itself and A/C (already direct) must not reappear.
	_, secondHop := api.Neighborhood("B")
	if len(secondHop) != 1 || secondHop[0] != "Title-D" {
		t.Fatalf("expected exactly [Title-D] as 2-hop, got %+v", secondHop)
	}
}

func TestNeighborhood_UnknownChangeReturnsEmpty(t *testing.T) {
	g := buildLinearGraph()
	api := NewAPI(g)

	direct, secondHop := api.Neighborhood("does-not-exist")
	if len(direct) != 0 || len(secondHop) != 0 {
		t.Fatalf("expected no neighbors for unknown id, got direct=%+v secondHop=%+v", direct, secondHop)
	}
}

func TestCommunityOverview_ReadsCachedFileWithoutGenerating(t *testing.T) {
	dir := t.TempDir()
	g := buildLinearGraph()
	g.SetCommunities(map[string]int{"A": 7, "B": 7, "C": 7, "D": 7})
	api := &API{graph: g, indexCacheDir: dir}

	members := []Component{{ID: "A", Title: "Title-A"}, {ID: "B", Title: "Title-B"}, {ID: "C", Title: "Title-C"}, {ID: "D", Title: "Title-D"}}
	key := overviewCacheKey(members)
	cacheDir := dir + "/overviews"
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	body := overviewPrefix + "seeded overview"
	if err := os.WriteFile(overviewCachePath(cacheDir, 7, key), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	got := api.CommunityOverview("A")
	if got != body {
		t.Errorf("expected cached overview body, got %q", got)
	}
}

func TestCommunityOverview_NoCacheFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	g := buildLinearGraph()
	g.SetCommunities(map[string]int{"A": 3, "B": 3, "C": 3, "D": 3})
	api := &API{graph: g, indexCacheDir: dir}

	if got := api.CommunityOverview("A"); got != "" {
		t.Errorf("expected empty string when no cache file exists, got %q", got)
	}
}

func TestCommunityOverview_NoCommunityReturnsEmpty(t *testing.T) {
	g := buildLinearGraph() // no communities set
	api := &API{graph: g, indexCacheDir: t.TempDir()}

	if got := api.CommunityOverview("A"); got != "" {
		t.Errorf("expected empty string when change has no community, got %q", got)
	}
}
