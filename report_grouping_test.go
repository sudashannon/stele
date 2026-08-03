package main

import "testing"

func groupingDoc(path, workspace string) reportDocument {
	return reportDocument{
		EvidenceID: "D" + path, SourceID: path, Path: path, Title: path,
		Type: "knowledge", Workspace: workspace,
	}
}

// A design and its plan are two stages of one work item. The graph's must-link
// sources only cover structured layouts, so a flat knowledge pair had no edge and
// rendered as two separate report sections.
func TestSiblingPairsLinkDesignAndPlan(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		groupingDoc("/k/2026-07-18-rx101-workstation-integration-design.md", "lz100"),
		groupingDoc("/k/2026-07-18-rx101-workstation-integration-plan.md", "lz100"),
	}}

	pairs := reportSiblingDocumentPairs(&corpus)

	if len(pairs) != 1 {
		t.Fatalf("pairs = %+v, want the design/plan pair", pairs)
	}
}

// The date prefix is stripped, so a design written one week and its plan the next
// still pair - waiting a week to write the plan is normal.
func TestSiblingPairsIgnoreTheDatePrefix(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		groupingDoc("/k/2026-07-18-rx101-workstation-integration-design.md", "lz100"),
		groupingDoc("/k/2026-07-25-rx101-workstation-integration-plan.md", "lz100"),
	}}

	if pairs := reportSiblingDocumentPairs(&corpus); len(pairs) != 1 {
		t.Fatalf("pairs = %+v, want the pair across dates", pairs)
	}
}

// Different slugs are different work items. The BitNet training documents share a
// directory and a date with nothing else here, and must not be dragged together.
func TestSiblingPairsRequireTheSameSlug(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		groupingDoc("/k/2026-07-30-bitnet-158bit-qat-training-technical.md", "miao"),
		groupingDoc("/k/2026-07-30-lz100-bitnet-robot-training-data-plan.md", "miao"),
	}}

	if pairs := reportSiblingDocumentPairs(&corpus); len(pairs) != 0 {
		t.Fatalf("pairs = %+v, want none: the slugs differ", pairs)
	}
}

// Same slug in different directories is not the same work item - two workspaces
// can both have a "-design" note about the same subject.
func TestSiblingPairsStayWithinADirectory(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		groupingDoc("/a/2026-07-18-station-integration-design.md", "lz100"),
		groupingDoc("/b/2026-07-18-station-integration-plan.md", "rx101"),
	}}

	if pairs := reportSiblingDocumentPairs(&corpus); len(pairs) != 0 {
		t.Fatalf("pairs = %+v, want none across directories", pairs)
	}
}

// A short stem carries no meaning; pairing on it would merge unrelated notes.
func TestSiblingPairsIgnoreShortStems(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		groupingDoc("/k/api-design.md", "lz100"),
		groupingDoc("/k/api-plan.md", "lz100"),
	}}

	if pairs := reportSiblingDocumentPairs(&corpus); len(pairs) != 0 {
		t.Fatalf("pairs = %+v, want none for a stem under the minimum", pairs)
	}
}

// Three stages of one work item all land together, not just the first two.
func TestSiblingPairsChainEveryStage(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		groupingDoc("/k/2026-07-18-workstation-integration-design.md", "lz100"),
		groupingDoc("/k/2026-07-18-workstation-integration-plan.md", "lz100"),
		groupingDoc("/k/2026-07-18-workstation-integration-verify.md", "lz100"),
	}}

	pairs := reportSiblingDocumentPairs(&corpus)

	set := newReportDisjointSet()
	for _, document := range corpus.Documents {
		set.add(document.SourceID)
	}
	for _, pair := range pairs {
		set.union(pair[0], pair[1])
	}
	root := set.find(corpus.Documents[0].SourceID)
	for _, document := range corpus.Documents[1:] {
		if set.find(document.SourceID) != root {
			t.Fatalf("%s did not join the work item", document.Path)
		}
	}
}

// Vocabulary is not a subject. Two clusters in different workspaces with no edge
// between them share words, and merging them put a Qwen/BitNet training pair
// inside an LZ100 workstation section.
func TestMergeRefusesCrossWorkspaceWithoutAnEdge(t *testing.T) {
	left := &reportClusterNode{Workspaces: map[string]struct{}{"miao": {}}}
	right := &reportClusterNode{Workspaces: map[string]struct{}{"lz100": {}}}

	if reportMergeAllowed(left, right, 0) {
		t.Fatal("a cross-workspace merge with no edge must be refused")
	}
	if !reportMergeAllowed(left, right, 0.08) {
		t.Fatal("a real edge still permits a cross-workspace merge")
	}
	same := &reportClusterNode{Workspaces: map[string]struct{}{"miao": {}}}
	if !reportMergeAllowed(left, same, 0) {
		t.Fatal("same-workspace merges are unaffected")
	}
}

// A cluster that already spans two workspaces (because an edge linked it) can
// still merge with either side.
func TestMergeAllowsOverlapAfterAMixedCluster(t *testing.T) {
	mixed := &reportClusterNode{Workspaces: map[string]struct{}{"miao": {}, "lz100": {}}}
	single := &reportClusterNode{Workspaces: map[string]struct{}{"lz100": {}}}

	if !reportMergeAllowed(mixed, single, 0) {
		t.Fatal("overlapping workspace sets must merge")
	}
}

// End to end: the two defects from the real report, on the same seven documents.
func TestClusteringSplitsWorkspacesAndJoinsSiblings(t *testing.T) {
	documents := []reportDocument{
		groupingDoc("/w/miao/knowledge/2026-07-30-bitnet-158bit-qat-training-technical.md", "miao"),
		groupingDoc("/w/miao/knowledge/2026-07-30-lz100-bitnet-robot-training-data-plan.md", "miao"),
		groupingDoc("/w/lz100/knowledge/2026-07-18-rx101-workstation-integration-design.md", "lz100"),
		groupingDoc("/w/lz100/knowledge/2026-07-18-rx101-workstation-integration-plan.md", "lz100"),
		groupingDoc("/w/lz100/knowledge/2026-07-22-rx101-controlboard-test-station-design.md", "lz100"),
		groupingDoc("/w/lz100/knowledge/2026-07-22-rx101-controlboard-test-station-plan.md", "lz100"),
	}
	corpus := reportCorpus{Documents: documents, Counts: documentReportCounts{Types: map[string]int{}}}

	themes := clusterReportCorpus(&corpus)

	themeOf := make(map[string]string, len(documents))
	for _, theme := range themes {
		for _, index := range theme.DocumentIndexes {
			themeOf[documents[index].Path] = theme.ID
		}
	}
	design := themeOf["/w/lz100/knowledge/2026-07-18-rx101-workstation-integration-design.md"]
	plan := themeOf["/w/lz100/knowledge/2026-07-18-rx101-workstation-integration-plan.md"]
	if design == "" || design != plan {
		t.Fatalf("design %q and plan %q must share a section", design, plan)
	}
	station := themeOf["/w/lz100/knowledge/2026-07-22-rx101-controlboard-test-station-design.md"]
	stationPlan := themeOf["/w/lz100/knowledge/2026-07-22-rx101-controlboard-test-station-plan.md"]
	if station == "" || station != stationPlan {
		t.Fatalf("the second design/plan pair must share a section (%q vs %q)", station, stationPlan)
	}
	bitnet := themeOf["/w/miao/knowledge/2026-07-30-bitnet-158bit-qat-training-technical.md"]
	if bitnet == design || bitnet == station {
		t.Fatalf("the miao training documents must not join an lz100 section (%q)", bitnet)
	}
}
