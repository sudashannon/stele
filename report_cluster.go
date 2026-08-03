package main

import (
	"container/heap"
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	reportClusterMergeThreshold = 0.58
	reportThemeLimit            = 8
)

type reportTheme struct {
	ID                 string   `json:"id"`
	Label              string   `json:"label"`
	EvidenceIDs        []string `json:"evidenceIds"`
	ContextEvidenceIDs []string `json:"contextEvidenceIds,omitempty"`
	RepresentativeIDs  []string `json:"representativeIds"`
	Independent        bool     `json:"independent,omitempty"`
	// SessionID / SessionPath / Effort are set when the theme is a session's
	// work rather than a document cluster. They are the effort axis: document
	// count says how much was written, not how much was done.
	SessionID   string            `json:"sessionId,omitempty"`
	SessionPath string            `json:"sessionPath,omitempty"`
	Effort      reportThemeEffort `json:"effort,omitempty"`
	// OpenTasks are the session's unfinished items, used as prompt framing and
	// rendered deterministically; they never become claims.
	OpenTasks []string `json:"-"`
	// Unattributed marks a cluster of documents no session authored.
	Unattributed    bool  `json:"unattributed,omitempty"`
	DocumentIndexes []int `json:"-"`
}

// reportThemeEffort is what a session cost inside the report window.
type reportThemeEffort struct {
	Workspace  string `json:"workspace,omitempty"`
	ActiveDays int    `json:"activeDays,omitempty"`
	Events     int    `json:"events,omitempty"`
	UserTurns  int    `json:"userTurns,omitempty"`
	Subagents  int    `json:"subagents,omitempty"`
}

type reportClusterNode struct {
	ID          int
	Key         string
	Workspaces  map[string]struct{}
	Documents   []int
	Vector      []float64
	VectorDocs  int
	Lexical     map[string]float64
	LexicalDocs int
	PrimaryDocs int
	Version     int
	Active      bool
}

type reportClusterPair struct {
	A, B               int
	VersionA, VersionB int
	Score              float64
	KeyA, KeyB         string
}

type reportClusterQueue []reportClusterPair

func (q reportClusterQueue) Len() int { return len(q) }
func (q reportClusterQueue) Less(i, j int) bool {
	if q[i].Score != q[j].Score {
		return q[i].Score > q[j].Score
	}
	if q[i].KeyA != q[j].KeyA {
		return q[i].KeyA < q[j].KeyA
	}
	return q[i].KeyB < q[j].KeyB
}
func (q reportClusterQueue) Swap(i, j int)   { q[i], q[j] = q[j], q[i] }
func (q *reportClusterQueue) Push(value any) { *q = append(*q, value.(reportClusterPair)) }
func (q *reportClusterQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

type reportDisjointSet struct {
	parent map[string]string
}

func newReportDisjointSet() *reportDisjointSet {
	return &reportDisjointSet{parent: make(map[string]string)}
}

func (set *reportDisjointSet) add(value string) {
	if _, ok := set.parent[value]; !ok {
		set.parent[value] = value
	}
}

func (set *reportDisjointSet) find(value string) string {
	parent, ok := set.parent[value]
	if !ok {
		set.add(value)
		return value
	}
	if parent != value {
		set.parent[value] = set.find(parent)
	}
	return set.parent[value]
}

func (set *reportDisjointSet) union(a, b string) {
	rootA, rootB := set.find(a), set.find(b)
	if rootA == rootB {
		return
	}
	if rootB < rootA {
		rootA, rootB = rootB, rootA
	}
	set.parent[rootB] = rootA
}

// clusterReportCorpus first preserves lifecycle ownership groups, then performs
// deterministic centroid-linkage agglomeration. Context-only groups are
// attached to a primary group before semantic merges and never become KPIs.
func clusterReportCorpus(corpus *reportCorpus) []reportTheme {
	if len(corpus.Documents) == 0 {
		corpus.Coverage.ClusteringMode = "lexical"
		return nil
	}
	if corpus.Coverage.MissingEmbeddings == 0 {
		corpus.Coverage.ClusteringMode = "vector"
	} else if corpus.Coverage.MissingEmbeddings == len(corpus.Documents) {
		corpus.Coverage.ClusteringMode = "lexical"
	} else {
		corpus.Coverage.ClusteringMode = "hybrid"
	}

	set := newReportDisjointSet()
	for _, document := range corpus.Documents {
		set.add(document.SourceID)
	}
	for _, connector := range corpus.Connectors {
		set.add(connector.ID)
	}
	for _, edge := range corpus.Edges {
		if reportMustLinkSource(edge.Source) {
			set.union(edge.From, edge.To)
		}
	}
	for _, pair := range reportSiblingDocumentPairs(corpus) {
		set.union(pair[0], pair[1])
	}

	groupDocuments := make(map[string][]int)
	for index, document := range corpus.Documents {
		root := set.find(document.SourceID)
		groupDocuments[root] = append(groupDocuments[root], index)
	}
	groupRoots := make([]string, 0, len(groupDocuments))
	for root := range groupDocuments {
		groupRoots = append(groupRoots, root)
	}
	sort.Slice(groupRoots, func(i, j int) bool {
		return corpus.Documents[groupDocuments[groupRoots[i]][0]].SourceID < corpus.Documents[groupDocuments[groupRoots[j]][0]].SourceID
	})

	lexicalVectors := reportLexicalVectors(corpus, groupRoots, groupDocuments)
	nodes := make(map[int]*reportClusterNode, len(groupRoots))
	rootNode := make(map[string]int, len(groupRoots))
	for id, root := range groupRoots {
		documents := append([]int(nil), groupDocuments[root]...)
		sort.Ints(documents)
		node := &reportClusterNode{
			ID:          id,
			Key:         corpus.Documents[documents[0]].SourceID,
			Workspaces:  make(map[string]struct{}),
			Documents:   documents,
			Lexical:     lexicalVectors[root],
			LexicalDocs: len(documents),
			Active:      true,
		}
		for _, index := range documents {
			node.Workspaces[corpus.Documents[index].Workspace] = struct{}{}
		}
		node.Vector, node.VectorDocs = reportDocumentCentroid(corpus, documents)
		for _, index := range documents {
			if !corpus.Documents[index].ContextOnly {
				node.PrimaryDocs++
			}
		}
		if node.PrimaryDocs > 0 {
			corpus.Counts.WorkItems++
		}
		nodes[id] = node
		rootNode[root] = id
	}

	affinity := make(map[int]map[int]float64)
	for _, edge := range corpus.Edges {
		if reportMustLinkSource(edge.Source) {
			continue
		}
		rootA, rootB := set.find(edge.From), set.find(edge.To)
		a, okA := rootNode[rootA]
		b, okB := rootNode[rootB]
		if !okA || !okB || a == b {
			continue
		}
		reportSetAffinity(affinity, a, b, reportRelationAffinity(edge.Source))
	}

	// Context-only evidence was selected because it is connected to an in-window
	// source. Attach it deterministically; it may explain but cannot create a
	// standalone theme or work-item count.
	contextNodeIDs := make([]int, 0)
	for id, node := range nodes {
		if node.PrimaryDocs == 0 {
			contextNodeIDs = append(contextNodeIDs, id)
		}
	}
	sort.Ints(contextNodeIDs)
	for _, contextID := range contextNodeIDs {
		contextNode := nodes[contextID]
		if !contextNode.Active {
			continue
		}
		targetID := -1
		bestScore := -1.0
		for id, candidate := range nodes {
			if !candidate.Active || candidate.PrimaryDocs == 0 || id == contextID {
				continue
			}
			candidateAffinity := reportGetAffinity(affinity, contextID, id)
			if !reportMergeAllowed(contextNode, candidate, candidateAffinity) {
				continue
			}
			score := reportClusterScore(contextNode, candidate, candidateAffinity)
			if score > bestScore || (score == bestScore && (targetID < 0 || candidate.Key < nodes[targetID].Key)) {
				bestScore, targetID = score, id
			}
		}
		if targetID >= 0 {
			reportMergeClusterNodes(nodes[targetID], contextNode)
			reportMergeAffinity(affinity, targetID, contextID, nodes)
		}
	}

	queue := make(reportClusterQueue, 0)
	activeIDs := reportActiveNodeIDs(nodes)
	for i, a := range activeIDs {
		for _, b := range activeIDs[i+1:] {
			reportPushClusterPair(&queue, nodes[a], nodes[b], affinity)
		}
	}
	heap.Init(&queue)
	for queue.Len() > 0 {
		pair := heap.Pop(&queue).(reportClusterPair)
		a, b := nodes[pair.A], nodes[pair.B]
		if a == nil || b == nil || !a.Active || !b.Active || a.Version != pair.VersionA || b.Version != pair.VersionB {
			continue
		}
		if pair.Score < reportClusterMergeThreshold {
			break
		}
		keep, remove := a, b
		if remove.Key < keep.Key {
			keep, remove = remove, keep
		}
		reportMergeClusterNodes(keep, remove)
		reportMergeAffinity(affinity, keep.ID, remove.ID, nodes)
		for id, other := range nodes {
			if id == keep.ID || !other.Active {
				continue
			}
			reportPushClusterPair(&queue, keep, other, affinity)
		}
	}

	finalNodes := make([]*reportClusterNode, 0)
	for _, node := range nodes {
		if node.Active && node.PrimaryDocs > 0 {
			finalNodes = append(finalNodes, node)
		}
	}
	if len(finalNodes) > reportThemeLimit {
		finalNodes = reportConsolidateOutliers(finalNodes, nodes)
	}
	sort.Slice(finalNodes, func(i, j int) bool {
		return reportFirstPrimaryDocument(finalNodes[i], corpus) < reportFirstPrimaryDocument(finalNodes[j], corpus)
	})

	themes := make([]reportTheme, 0, len(finalNodes))
	for i, node := range finalNodes {
		theme := reportBuildTheme(i+1, node, corpus)
		themes = append(themes, theme)
	}
	corpus.Counts.Themes = len(themes)
	return themes
}

func reportMustLinkSource(source string) bool {
	return source == "convention-internal" || source == "superpowers-convention"
}

func reportRelationAffinity(source string) float64 {
	switch source {
	case "yaml":
		return 0.18
	case "task-json":
		return 0.14
	case "task-context":
		return 0.12
	case "markdown-link":
		return 0.08
	default:
		return 0.04
	}
}

func reportDocumentCentroid(corpus *reportCorpus, documents []int) ([]float64, int) {
	var centroid []float64
	count := 0
	for _, index := range documents {
		vector := corpus.Documents[index].Vector
		if len(vector) == 0 {
			continue
		}
		if len(centroid) == 0 {
			centroid = make([]float64, len(vector))
		}
		if len(vector) != len(centroid) {
			continue
		}
		for i, value := range vector {
			centroid[i] += float64(value)
		}
		count++
	}
	return reportNormalizeDense(centroid), count
}

func reportClusterScore(a, b *reportClusterNode, affinity float64) float64 {
	semantic := 0.0
	if a.VectorDocs > 0 && b.VectorDocs > 0 && len(a.Vector) == len(b.Vector) {
		semantic = reportDenseCosine(a.Vector, b.Vector)
	} else {
		lexical := reportSparseCosine(a.Lexical, b.Lexical)
		if lexical > 0 {
			semantic = 0.35 + 0.65*lexical
		}
	}
	return math.Min(1, semantic+affinity)
}

func reportPushClusterPair(queue *reportClusterQueue, a, b *reportClusterNode, affinity map[int]map[int]float64) {
	if b.Key < a.Key {
		a, b = b, a
	}
	if !reportMergeAllowed(a, b, reportGetAffinity(affinity, a.ID, b.ID)) {
		return
	}
	heap.Push(queue, reportClusterPair{
		A: a.ID, B: b.ID,
		VersionA: a.Version, VersionB: b.Version,
		Score: reportClusterScore(a, b, reportGetAffinity(affinity, a.ID, b.ID)),
		KeyA:  a.Key, KeyB: b.Key,
	})
}

func reportMergeClusterNodes(keep, remove *reportClusterNode) {
	if keep.Workspaces == nil {
		keep.Workspaces = make(map[string]struct{}, len(remove.Workspaces))
	}
	for workspace := range remove.Workspaces {
		keep.Workspaces[workspace] = struct{}{}
	}
	keep.Documents = append(keep.Documents, remove.Documents...)
	sort.Ints(keep.Documents)
	keep.Vector = reportWeightedDenseMerge(keep.Vector, keep.VectorDocs, remove.Vector, remove.VectorDocs)
	keep.VectorDocs += remove.VectorDocs
	keep.Lexical = reportWeightedSparseMerge(keep.Lexical, keep.LexicalDocs, remove.Lexical, remove.LexicalDocs)
	keep.LexicalDocs += remove.LexicalDocs
	keep.PrimaryDocs += remove.PrimaryDocs
	if remove.Key < keep.Key {
		keep.Key = remove.Key
	}
	keep.Version++
	remove.Active = false
	remove.Version++
}

func reportSetAffinity(affinity map[int]map[int]float64, a, b int, value float64) {
	if a == b || value == 0 {
		return
	}
	if b < a {
		a, b = b, a
	}
	if affinity[a] == nil {
		affinity[a] = make(map[int]float64)
	}
	if value > affinity[a][b] {
		affinity[a][b] = value
	}
}

func reportGetAffinity(affinity map[int]map[int]float64, a, b int) float64 {
	if b < a {
		a, b = b, a
	}
	return affinity[a][b]
}

func reportMergeAffinity(affinity map[int]map[int]float64, keep, remove int, nodes map[int]*reportClusterNode) {
	for id, node := range nodes {
		if id == keep || id == remove || !node.Active {
			continue
		}
		value := math.Max(reportGetAffinity(affinity, keep, id), reportGetAffinity(affinity, remove, id))
		reportSetAffinity(affinity, keep, id, value)
	}
	for key, values := range affinity {
		delete(values, remove)
		if key == remove {
			delete(affinity, key)
		}
	}
}

func reportActiveNodeIDs(nodes map[int]*reportClusterNode) []int {
	ids := make([]int, 0, len(nodes))
	for id, node := range nodes {
		if node.Active {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func reportConsolidateOutliers(nodes []*reportClusterNode, all map[int]*reportClusterNode) []*reportClusterNode {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].PrimaryDocs != nodes[j].PrimaryDocs {
			return nodes[i].PrimaryDocs > nodes[j].PrimaryDocs
		}
		if len(nodes[i].Documents) != len(nodes[j].Documents) {
			return len(nodes[i].Documents) > len(nodes[j].Documents)
		}
		return nodes[i].Key < nodes[j].Key
	})
	kept := append([]*reportClusterNode(nil), nodes[:reportThemeLimit-1]...)
	misc := &reportClusterNode{ID: len(all) + 1, Key: "~misc", Active: true}
	for _, node := range nodes[reportThemeLimit-1:] {
		reportMergeClusterNodes(misc, node)
		misc.Active = true
	}
	misc.Key = "~misc"
	kept = append(kept, misc)
	return kept
}

func reportFirstPrimaryDocument(node *reportClusterNode, corpus *reportCorpus) int {
	for _, index := range node.Documents {
		if !corpus.Documents[index].ContextOnly {
			return index
		}
	}
	return len(corpus.Documents)
}

func reportBuildTheme(number int, node *reportClusterNode, corpus *reportCorpus) reportTheme {
	theme := reportTheme{
		ID:              "T" + strconvItoa(number),
		DocumentIndexes: append([]int(nil), node.Documents...),
	}
	for _, index := range node.Documents {
		document := corpus.Documents[index]
		if document.ContextOnly {
			theme.ContextEvidenceIDs = append(theme.ContextEvidenceIDs, document.EvidenceID)
		} else {
			theme.EvidenceIDs = append(theme.EvidenceIDs, document.EvidenceID)
		}
	}
	representatives := reportRepresentativeDocuments(node, corpus, 3)
	for _, index := range representatives {
		theme.RepresentativeIDs = append(theme.RepresentativeIDs, corpus.Documents[index].EvidenceID)
	}
	if node.Key == "~misc" {
		theme.Label = "独立事项"
		theme.Independent = true
	} else if len(representatives) > 0 {
		theme.Label = corpus.Documents[representatives[0]].Title
	} else {
		theme.Label = "未命名主题"
	}
	return theme
}

func reportRepresentativeDocuments(node *reportClusterNode, corpus *reportCorpus, limit int) []int {
	candidates := make([]int, 0, len(node.Documents))
	for _, index := range node.Documents {
		if !corpus.Documents[index].ContextOnly {
			candidates = append(candidates, index)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		scoreA, scoreB := 0.0, 0.0
		if len(node.Vector) > 0 {
			scoreA = reportFloat32DenseCosine(corpus.Documents[a].Vector, node.Vector)
			scoreB = reportFloat32DenseCosine(corpus.Documents[b].Vector, node.Vector)
		}
		if scoreA != scoreB {
			return scoreA > scoreB
		}
		return corpus.Documents[a].SourceID < corpus.Documents[b].SourceID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func reportLexicalVectors(corpus *reportCorpus, roots []string, groups map[string][]int) map[string]map[string]float64 {
	termCounts := make(map[string]map[string]int, len(roots))
	documentFrequency := make(map[string]int)
	for _, root := range roots {
		counts := make(map[string]int)
		for _, index := range groups[root] {
			for _, term := range reportTerms(corpus.Documents[index].Title + "\n" + corpus.Documents[index].SemanticText) {
				counts[term]++
			}
		}
		termCounts[root] = counts
		for term := range counts {
			documentFrequency[term]++
		}
	}
	vectors := make(map[string]map[string]float64, len(roots))
	n := float64(len(roots))
	for _, root := range roots {
		vector := make(map[string]float64, len(termCounts[root]))
		for term, count := range termCounts[root] {
			idf := math.Log((1+n)/(1+float64(documentFrequency[term]))) + 1
			vector[term] = (1 + math.Log(float64(count))) * idf
		}
		vectors[root] = reportNormalizeSparse(vector)
	}
	return vectors
}

func reportTerms(text string) []string {
	text = strings.ToLower(text)
	terms := make([]string, 0)
	var word []rune
	var previousHan rune
	flush := func() {
		if len(word) >= 2 {
			terms = append(terms, string(word))
		}
		word = word[:0]
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			flush()
			terms = append(terms, string(r))
			if previousHan != 0 {
				terms = append(terms, string([]rune{previousHan, r}))
			}
			previousHan = r
			continue
		}
		previousHan = 0
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return terms
}

func reportWeightedDenseMerge(a []float64, countA int, b []float64, countB int) []float64 {
	if countA == 0 {
		return append([]float64(nil), b...)
	}
	if countB == 0 || len(a) != len(b) {
		return append([]float64(nil), a...)
	}
	merged := make([]float64, len(a))
	for i := range a {
		merged[i] = a[i]*float64(countA) + b[i]*float64(countB)
	}
	return reportNormalizeDense(merged)
}

func reportWeightedSparseMerge(a map[string]float64, countA int, b map[string]float64, countB int) map[string]float64 {
	merged := make(map[string]float64, len(a)+len(b))
	for term, value := range a {
		merged[term] += value * float64(countA)
	}
	for term, value := range b {
		merged[term] += value * float64(countB)
	}
	return reportNormalizeSparse(merged)
}

func reportNormalizeDense(vector []float64) []float64 {
	norm := 0.0
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return vector
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] /= norm
	}
	return vector
}

func reportNormalizeSparse(vector map[string]float64) map[string]float64 {
	norm := 0.0
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return vector
	}
	norm = math.Sqrt(norm)
	for term := range vector {
		vector[term] /= norm
	}
	return vector
}

func reportDenseCosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	score := 0.0
	for i := range a {
		score += a[i] * b[i]
	}
	return score
}

func reportFloat32DenseCosine(a []float32, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	norm := 0.0
	score := 0.0
	for i, value := range a {
		v := float64(value)
		score += v * b[i]
		norm += v * v
	}
	if norm == 0 {
		return 0
	}
	return score / math.Sqrt(norm)
}

func reportSparseCosine(a, b map[string]float64) float64 {
	if len(a) > len(b) {
		a, b = b, a
	}
	score := 0.0
	for term, value := range a {
		score += value * b[term]
	}
	return score
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
