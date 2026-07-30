package wiki

import (
	"fmt"
	"strings"
	"testing"
)

// buildClusteredGraph builds two fully-connected triangles (A-B-C and D-E-F)
// joined by a single weak bridge edge A-D.
func buildClusteredGraph() *Graph {
	components := []Component{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
		{ID: "d", Title: "D"},
		{ID: "e", Title: "E"},
		{ID: "f", Title: "F"},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: "references"},
		{From: "b", To: "c", Kind: "references"},
		{From: "c", To: "a", Kind: "references"},
		{From: "d", To: "e", Kind: "references"},
		{From: "e", To: "f", Kind: "references"},
		{From: "f", To: "d", Kind: "references"},
		{From: "a", To: "d", Kind: "references"},
	}
	return BuildGraph(components, edges)
}

func TestDetectCommunities_TwoClusters(t *testing.T) {
	g := buildClusteredGraph()
	got := DetectCommunities(g)

	if len(got) != 6 {
		t.Fatalf("expected 6 entries, got %d: %+v", len(got), got)
	}

	// All members within a cluster must share the same community.
	if got["a"] != got["b"] || got["b"] != got["c"] {
		t.Fatalf("expected a,b,c to share a community, got %+v", got)
	}
	if got["d"] != got["e"] || got["e"] != got["f"] {
		t.Fatalf("expected d,e,f to share a community, got %+v", got)
	}
	if got["a"] == got["d"] {
		t.Fatalf("expected the two clusters to be in different communities, got %+v", got)
	}

	// Both clusters have 3 members each, so neither should be reassigned to misc.
	if got["a"] == -1 {
		t.Fatalf("expected cluster a,b,c to not be misc, got %+v", got)
	}
	if got["d"] == -1 {
		t.Fatalf("expected cluster d,e,f to not be misc, got %+v", got)
	}
}

func TestDetectCommunities_DisconnectedNodeIsMisc(t *testing.T) {
	components := []Component{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "lonely", Title: "Lonely"},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: "references"},
	}
	g := BuildGraph(components, edges)

	got := DetectCommunities(g)

	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(got), got)
	}
	if got["lonely"] != -1 {
		t.Fatalf("expected lonely node to be misc (-1), got %d", got["lonely"])
	}
	// A connected pair is a real community: collapsing every cluster of <= 2
	// into misc hid fragmentation instead of reporting it.
	if got["a"] < 0 || got["a"] != got["b"] {
		t.Fatalf("expected connected pair a,b to form one real community, got %+v", got)
	}
}

func TestDetectCommunities_EmptyGraph(t *testing.T) {
	g := BuildGraph(nil, nil)
	got := DetectCommunities(g)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %+v", got)
	}
}

func TestDetectCommunities_MiscCommunitiesAllShareNegativeOne(t *testing.T) {
	components := []Component{
		{ID: "x", Title: "X"},
		{ID: "y", Title: "Y"},
	}
	// No edges at all: two isolated singleton communities, both size 1 <= 2.
	g := BuildGraph(components, nil)

	got := DetectCommunities(g)
	if got["x"] != -1 || got["y"] != -1 {
		t.Fatalf("expected both isolated nodes to be misc, got %+v", got)
	}
}

func TestCommunityLabels_PicksDistinctiveTerm(t *testing.T) {
	components := []Component{
		{ID: "a", Title: "安全编译设计"},
		{ID: "b", Title: "安全编译实施"},
		{ID: "c", Title: "前端界面设计"},
		{ID: "d", Title: "前端界面实施"},
	}
	communities := map[string]int{
		"a": 0, "b": 0,
		"c": 1, "d": 1,
	}

	labels := CommunityLabels(components, communities)

	label, ok := labels[0]
	if !ok {
		t.Fatalf("expected a label for community 0, got %+v", labels)
	}
	if !strings.Contains(label, "安") && !strings.Contains(label, "全") &&
		!strings.Contains(label, "编") && !strings.Contains(label, "译") {
		t.Fatalf("expected label for community 0 to contain 安/全/编/译, got %q", label)
	}
}

func TestCommunityLabels_SkipsMiscCommunity(t *testing.T) {
	components := []Component{
		{ID: "a", Title: "安全编译设计"},
		{ID: "b", Title: "安全编译实施"},
		{ID: "c", Title: "孤立节点"},
	}
	communities := map[string]int{
		"a": 0, "b": 0, "c": -1,
	}

	labels := CommunityLabels(components, communities)

	if _, ok := labels[-1]; ok {
		t.Fatalf("expected no label for misc community -1, got %+v", labels)
	}
	if len(labels) != 1 {
		t.Fatalf("expected exactly one labeled community, got %+v", labels)
	}
}

func TestCommunityLabels_CombinesSeveralTermsNotOneTitle(t *testing.T) {
	comps := []Component{
		{ID: "a", Title: "PKI 密钥管理设计"},
		{ID: "b", Title: "PKI 证书签发实施"},
		{ID: "c", Title: "OTA 升级回滚"},
		{ID: "d", Title: "OTA 升级校验"},
	}
	communities := map[string]int{"a": 0, "b": 0, "c": 1, "d": 1}

	labels := CommunityLabels(comps, communities)

	for commID, label := range labels {
		if !strings.Contains(label, " · ") {
			t.Errorf("community %d: expected a multi-term label, got %q", commID, label)
		}
		for _, c := range comps {
			if label == c.Title {
				t.Errorf("community %d: label is a raw member title %q, not a theme", commID, label)
			}
		}
	}
	if labels[0] == labels[1] {
		t.Errorf("distinct communities must not share a label, both got %q", labels[0])
	}
}

// TestDetectCommunities_StrongEdgeBeatsSeveralWeakOnes pins the weighting: one
// authored yaml link outranks two statistical cosine neighbours, which the
// previous unweighted implementation could not express.
func TestDetectCommunities_StrongEdgeBeatsSeveralWeakOnes(t *testing.T) {
	components := []Component{
		{ID: "hub", Title: "Hub"},
		{ID: "a1", Title: "A1"}, {ID: "a2", Title: "A2"},
		{ID: "b1", Title: "B1"}, {ID: "b2", Title: "B2"},
		{ID: "x", Title: "X"},
	}
	edges := []Edge{
		// Cluster A, authored.
		{From: "hub", To: "a1", Kind: "implements", Source: "yaml"},
		{From: "hub", To: "a2", Kind: "implements", Source: "yaml"},
		{From: "a1", To: "a2", Kind: "references", Source: "yaml"},
		// Cluster B, statistical only.
		{From: "b1", To: "b2", Kind: "similar", Source: "vector"},
		// x has one authored edge into A and two cosine edges into B.
		{From: "x", To: "hub", Kind: "references", Source: "yaml"},
		{From: "x", To: "b1", Kind: "similar", Source: "vector"},
		{From: "x", To: "b2", Kind: "similar", Source: "vector"},
	}
	g := BuildGraph(components, edges)

	got := DetectCommunities(g)

	if got["x"] != got["hub"] {
		t.Fatalf("expected x to follow its authored yaml edge into the hub community, got %+v", got)
	}
	if got["x"] == got["b1"] {
		t.Fatalf("expected x not to be pulled into the vector-only cluster, got %+v", got)
	}
}

func TestEdgeWeightExplicitAndLegacyBehavior(t *testing.T) {
	embeddings := map[string][]float32{
		"a": {1, 0},
		"b": {1, 0},
	}
	tests := []struct {
		name string
		edge Edge
		want float64
	}{
		{
			name: "explicit tag weight",
			edge: Edge{From: "a", To: "b", Kind: "shares-tag:orin", Source: "tag", Weight: 0.37},
			want: 0.37,
		},
		{
			name: "explicit weight precedes vector cosine",
			edge: Edge{From: "a", To: "b", Kind: "similar", Source: "vector", Weight: 0.23},
			want: 0.23,
		},
		{
			name: "zero weight yaml retains provenance default",
			edge: Edge{From: "a", To: "b", Kind: "references", Source: "yaml"},
			want: edgeWeightYAML,
		},
		{
			name: "zero weight vector retains cosine behavior",
			edge: Edge{From: "a", To: "b", Kind: "similar", Source: "vector"},
			want: vectorEdgeMaxWeight,
		},
		{
			name: "zero weight markdown retains provenance default",
			edge: Edge{From: "a", To: "b", Kind: "references", Source: "markdown-link"},
			want: edgeWeightMarkdownLink,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := edgeWeight(test.edge, embeddings); got != test.want {
				t.Fatalf("edgeWeight(%+v) = %v, want %v", test.edge, got, test.want)
			}
		})
	}
}

func TestGraphAdjacencyCollapsesParallelEvidenceToStrongestWeight(t *testing.T) {
	components := []Component{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	graph := BuildGraph(components, []Edge{
		{From: "a", To: "b", Kind: "shares-tag:orin", Source: "tag", Weight: 0.4},
		{From: "b", To: "a", Kind: "references", Source: "yaml"},
		{From: "a", To: "b", Kind: "similar", Source: "vector", Weight: 0.2},
		{From: "b", To: "c", Kind: "similar", Source: "vector", Weight: 0.1},
		{From: "c", To: "b", Kind: "shares-tag:pcie", Source: "tag", Weight: 0.3},
	})

	adjacency := graphAdjacency(graph)
	if got := adjacency.adj["a"]["b"]; got != edgeWeightYAML {
		t.Fatalf("authored YAML must beat weaker tag and vector evidence, got %v", got)
	}
	if got := adjacency.adj["b"]["c"]; got != 0.3 {
		t.Fatalf("tag evidence must beat weaker vector evidence, got %v", got)
	}
	if got := adjacency.total2m; got != 2*(edgeWeightYAML+0.3) {
		t.Fatalf("parallel evidence was summed instead of collapsed: total2m=%v", got)
	}
}

// TestDetectCommunities_AggregatesAcrossLevels pins the multi-level pass: a
// chain of triangles must not survive as one community per triangle, which is
// exactly what the previous single-level implementation produced.
func TestDetectCommunities_AggregatesAcrossLevels(t *testing.T) {
	var components []Component
	var edges []Edge
	const triangles = 6
	for i := range triangles {
		ids := [3]string{
			fmt.Sprintf("t%d-a", i), fmt.Sprintf("t%d-b", i), fmt.Sprintf("t%d-c", i),
		}
		for _, id := range ids {
			components = append(components, Component{ID: id, Title: strings.ToUpper(id)})
		}
		edges = append(edges,
			Edge{From: ids[0], To: ids[1], Kind: "references", Source: "yaml"},
			Edge{From: ids[1], To: ids[2], Kind: "references", Source: "yaml"},
			Edge{From: ids[2], To: ids[0], Kind: "references", Source: "yaml"},
		)
		if i > 0 {
			edges = append(edges, Edge{
				From: fmt.Sprintf("t%d-a", i-1), To: ids[0], Kind: "references", Source: "yaml",
			})
		}
	}
	g := BuildGraph(components, edges)

	got := DetectCommunities(g)

	distinct := map[int]bool{}
	for _, comm := range got {
		distinct[comm] = true
	}
	if len(distinct) >= triangles {
		t.Fatalf("expected triangles to merge across levels, got %d communities for %d triangles: %+v",
			len(distinct), triangles, got)
	}
	if len(distinct) < 2 {
		t.Fatalf("expected more than one community for a %d-triangle chain, got %+v", triangles, got)
	}
}

func TestModularity_ScoresCorrectPartitionHigher(t *testing.T) {
	g := buildClusteredGraph()

	correct := map[string]int{"a": 0, "b": 0, "c": 0, "d": 1, "e": 1, "f": 1}
	scrambled := map[string]int{"a": 0, "b": 1, "c": 0, "d": 1, "e": 0, "f": 1}

	good := Modularity(g, correct, CommunityResolution)
	bad := Modularity(g, scrambled, CommunityResolution)
	if good <= bad {
		t.Fatalf("expected the correct partition to score higher, got %.4f vs %.4f", good, bad)
	}
	if detected := Modularity(g, DetectCommunities(g), CommunityResolution); detected < good {
		t.Fatalf("detection scored %.4f below the hand-written partition %.4f", detected, good)
	}
}
