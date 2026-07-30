package wiki

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func tagEdgeTaxonomy(minDocs int, maxShare float64, edgeTags, filterTags []string) *Taxonomy {
	taxonomy := &Taxonomy{
		facetOf:   make(map[string]Facet),
		canonical: make(map[string]string),
		minDocs:   minDocs,
		maxShare:  maxShare,
	}
	for _, tag := range edgeTags {
		taxonomy.facetOf[tag] = Facet{Name: "edge", Edges: true}
		taxonomy.canonical[tag] = tag
	}
	for _, tag := range filterTags {
		taxonomy.facetOf[tag] = Facet{Name: "filter", Edges: false}
		taxonomy.canonical[tag] = tag
	}
	return taxonomy
}

func taggedComponent(id string, tags ...string) Component {
	values := make([]any, len(tags))
	for i, tag := range tags {
		values[i] = tag
	}
	return Component{ID: id, Frontmatter: map[string]any{"tags": values}}
}

func edgeByKind(t *testing.T, edges []Edge, kind string) Edge {
	t.Helper()
	for _, edge := range edges {
		if edge.Kind == kind {
			return edge
		}
	}
	t.Fatalf("missing edge kind %q in %#v", kind, edges)
	return Edge{}
}

func TestComputeTagEdgesExcludesIneligibleTags(t *testing.T) {
	taxonomy := tagEdgeTaxonomy(2, 0.5, []string{"eligible", "below", "above"}, []string{"filter"})
	taxonomy.canonical["missing-facet"] = "missing-facet"
	components := []Component{
		taggedComponent("a", "eligible", "below", "above", "filter", "unknown", "missing-facet"),
		taggedComponent("b", "eligible", "above", "filter", "unknown", "missing-facet"),
		taggedComponent("c", "above"), taggedComponent("d", "above"), taggedComponent("e", "above"), taggedComponent("f", "above"),
		taggedComponent("g"), taggedComponent("h"), taggedComponent("i"), taggedComponent("j"),
	}

	got := ComputeTagEdges(components, taxonomy)
	want := []Edge{{From: "a", To: "b", Kind: "shares-tag:eligible", Source: "tag", Weight: 0.4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComputeTagEdges = %#v; want %#v", got, want)
	}
}

func TestComputeTagEdgesBuildsCycleNotClique(t *testing.T) {
	taxonomy := tagEdgeTaxonomy(2, 1, []string{"cycle"}, nil)
	components := []Component{taggedComponent("d", "cycle"), taggedComponent("b", "cycle"), taggedComponent("a", "cycle"), taggedComponent("c", "cycle")}

	got := ComputeTagEdges(components, taxonomy)
	wantPairs := []string{"a-b", "a-d", "b-c", "c-d"}
	gotPairs := make([]string, len(got))
	for i, edge := range got {
		if edge.Source != "tag" || edge.Kind != "shares-tag:cycle" || edge.Weight != 0.2 {
			t.Errorf("edge = %#v; want exact tag source/kind/weight", edge)
		}
		gotPairs[i] = edge.From + "-" + edge.To
	}
	sort.Strings(gotPairs)
	if !reflect.DeepEqual(gotPairs, wantPairs) {
		t.Fatalf("cycle pairs = %v; want %v", gotPairs, wantPairs)
	}
}

func TestComputeTagEdgesNormalizesWeightByCoverage(t *testing.T) {
	taxonomy := tagEdgeTaxonomy(2, 0.5, []string{"rare", "middle", "common"}, nil)
	components := []Component{
		taggedComponent("a", "rare"), taggedComponent("b", "rare"),
		taggedComponent("c", "middle"), taggedComponent("d", "middle"), taggedComponent("e", "middle"),
		taggedComponent("f", "common"), taggedComponent("g", "common"), taggedComponent("h", "common"), taggedComponent("i", "common"), taggedComponent("j", "common"),
	}

	edges := ComputeTagEdges(components, taxonomy)
	rare := edgeByKind(t, edges, "shares-tag:rare").Weight
	middle := edgeByKind(t, edges, "shares-tag:middle").Weight
	common := edgeByKind(t, edges, "shares-tag:common").Weight
	if rare != 0.4 || common != 0.2 {
		t.Fatalf("endpoint weights rare=%v common=%v; want 0.4 and 0.2", rare, common)
	}
	if !(rare > middle && middle > common) {
		t.Fatalf("weights are not monotonic: rare=%v middle=%v common=%v", rare, middle, common)
	}
	wantMiddle := 0.20 + 0.20*((math.Log(10.0/3.0)-math.Log(10.0/5.0))/(math.Log(10.0/2.0)-math.Log(10.0/5.0)))
	if middle != wantMiddle {
		t.Fatalf("middle weight = %.17g; want %.17g", middle, wantMiddle)
	}
	if degenerate := tagEdgeWeight(4, 4, 4, 4); degenerate != 0.4 {
		t.Fatalf("degenerate coverage weight = %v; want 0.4", degenerate)
	}
}

func TestComputeTagEdgesPairDedupeUsesStrongestThenLexicalKind(t *testing.T) {
	taxonomy := tagEdgeTaxonomy(2, 1, []string{"alpha", "beta", "zeta", "broad"}, nil)
	components := []Component{
		taggedComponent("a", "alpha", "beta", "zeta", "broad"),
		taggedComponent("b", "alpha", "beta", "zeta", "broad"),
		taggedComponent("c", "alpha", "beta", "zeta", "broad"),
		taggedComponent("d", "broad"),
	}

	edges := ComputeTagEdges(components, taxonomy)
	var ab Edge
	for _, edge := range edges {
		if edge.From == "a" && edge.To == "b" {
			ab = edge
		}
	}
	if ab.Kind != "shares-tag:alpha" || ab.Weight != tagEdgeWeight(4, 3, 2, 4) {
		t.Fatalf("a-b = %#v; want stronger df=3 edge and lexical alpha tie winner", ab)
	}
}

func TestComputeTagEdgesCapsEndpointDegree(t *testing.T) {
	tags := []string{"t1", "t2", "t3", "t4"}
	taxonomy := tagEdgeTaxonomy(3, 1, tags, nil)
	components := []Component{taggedComponent("hub", tags...)}
	for i, tag := range tags {
		components = append(components,
			taggedComponent(string(rune('a'+i*2)), tag),
			taggedComponent(string(rune('b'+i*2)), tag),
		)
	}

	edges := ComputeTagEdges(components, taxonomy)
	degree := make(map[string]int)
	for _, edge := range edges {
		degree[edge.From]++
		degree[edge.To]++
	}
	if degree["hub"] != maxTagEdgeDegree {
		t.Fatalf("hub degree = %d; want cap %d", degree["hub"], maxTagEdgeDegree)
	}
	for id, count := range degree {
		if count > maxTagEdgeDegree {
			t.Fatalf("%s degree = %d; exceeds cap", id, count)
		}
	}
}

func TestComputeTagEdgesDeterministicUnderShuffledInputs(t *testing.T) {
	taxonomy := tagEdgeTaxonomy(2, 1, []string{"alpha", "beta"}, nil)
	forward := []Component{
		taggedComponent("a", "beta", "alpha"),
		taggedComponent("b", "alpha", "beta"),
		taggedComponent("c", "beta", "alpha"),
		taggedComponent("d", "alpha"),
	}
	reversed := []Component{
		taggedComponent("d", "alpha"),
		taggedComponent("c", "alpha", "beta"),
		taggedComponent("b", "beta", "alpha"),
		taggedComponent("a", "alpha", "beta"),
	}
	if got, want := ComputeTagEdges(reversed, taxonomy), ComputeTagEdges(forward, taxonomy); !reflect.DeepEqual(got, want) {
		t.Fatalf("shuffled result = %#v; want %#v", got, want)
	}
}

func TestEdgeWeightJSONAndIdentity(t *testing.T) {
	structural := Edge{From: "a", To: "b", Kind: "references", Source: "yaml"}
	encodedStructural, err := json.Marshal(structural)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedStructural), "weight") {
		t.Fatalf("zero-weight JSON = %s; weight must be omitted", encodedStructural)
	}
	tag := Edge{From: "a", To: "b", Kind: "shares-tag:x", Source: "tag", Weight: 0.4}
	encodedTag, err := json.Marshal(tag)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encodedTag), `"weight":0.4`) {
		t.Fatalf("tag JSON = %s; want weight", encodedTag)
	}

	weightedStructural := structural
	weightedStructural.Weight = 0.3
	if !sameEdge(structural, weightedStructural) {
		t.Fatal("sameEdge must ignore weight")
	}
	if len(edgeSet([]Edge{structural, weightedStructural})) != 1 {
		t.Fatal("edgeSet identity must ignore weight")
	}
	deduped := deduplicateEdges([]Edge{structural, weightedStructural})
	if len(deduped) != 1 || deduped[0].Weight != 0.3 {
		t.Fatalf("deduplicateEdges = %#v; want strongest weight", deduped)
	}
}
