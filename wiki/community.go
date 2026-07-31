package wiki

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Edge weights by provenance. A hand-written yaml link and a statistical
// top-3 cosine neighbour are not equally strong evidence that two documents
// belong to the same theme, but the previous implementation counted both as
// exactly 1. Vector edges outnumber structural ones 4231:1262 in the live
// corpus, so unweighted detection was effectively clustering by kNN and
// ignoring the authored graph.
const (
	edgeWeightYAML         = 1.0
	edgeWeightConvention   = 0.9
	edgeWeightMarkdownLink = 0.7
	edgeWeightSlugMatch    = 0.4

	// Vector edges are already built top-3 above cosine 0.5 (see
	// ComputeVectorSimilarityEdges call sites), so their similarity spans
	// [0.5, 1.0]; map that onto a deliberately weak band.
	vectorEdgeFloorSim  = 0.5
	vectorEdgeMinWeight = 0.1
	vectorEdgeMaxWeight = 0.4
)

// CommunityResolution is the Louvain resolution γ. Below 1 it favours fewer,
// larger communities; above 1 it splits more aggressively. 1453 components
// over a sparse authored graph fragment badly at γ=1, and the point of the
// view is to name a handful of themes rather than to enumerate every cluster.
const CommunityResolution = 0.7

// edgeWeight scores one edge by how much evidence it carries. Positive
// explicit weights take precedence; legacy edges retain provenance defaults,
// while vector edges are graded by cosine from the graph embeddings.
func edgeWeight(e Edge, embeddings map[string][]float32) float64 {
	// Session edges record which agent session touched a file. That is
	// ambient tooling activity, not authored intent, and there are far more
	// of them than authored links: giving them any weight would re-cluster
	// the graph around access patterns. Checked before the explicit-weight
	// branch so no caller can opt them back in.
	if e.Source == SourceSession {
		return 0
	}
	if e.Weight > 0 {
		return e.Weight
	}
	if e.Source == "vector" || e.Kind == "similar" {
		sim := cosineSimilarity(embeddings[e.From], embeddings[e.To])
		if math.IsInf(sim, -1) || sim <= vectorEdgeFloorSim {
			return vectorEdgeMinWeight
		}
		span := (sim - vectorEdgeFloorSim) / (1 - vectorEdgeFloorSim)
		if span > 1 {
			span = 1
		}
		return vectorEdgeMinWeight + span*(vectorEdgeMaxWeight-vectorEdgeMinWeight)
	}
	switch e.Source {
	case "yaml":
		return edgeWeightYAML
	case "convention-internal":
		return edgeWeightConvention
	case "markdown-link":
		return edgeWeightMarkdownLink
	case "slug-match":
		return edgeWeightSlugMatch
	default:
		return edgeWeightMarkdownLink
	}
}

// weightedAdjacency is an undirected weighted view keyed by node id.
type weightedAdjacency struct {
	adj      map[string]map[string]float64
	selfLoop map[string]float64
	strength map[string]float64
	total2m  float64
}

func (w *weightedAdjacency) link(a, b string, weight float64) {
	if weight <= 0 {
		return
	}
	if a == b {
		w.selfLoop[a] += weight
		w.strength[a] += 2 * weight
		w.total2m += 2 * weight
		return
	}
	w.adj[a][b] += weight
	w.adj[b][a] += weight
	w.strength[a] += weight
	w.strength[b] += weight
	w.total2m += 2 * weight
}

func newWeightedAdjacency(nodes []string) *weightedAdjacency {
	w := &weightedAdjacency{
		adj:      make(map[string]map[string]float64, len(nodes)),
		selfLoop: make(map[string]float64, len(nodes)),
		strength: make(map[string]float64, len(nodes)),
	}
	for _, id := range nodes {
		w.adj[id] = map[string]float64{}
	}
	return w
}

// graphAdjacency collapses the directed multigraph into one weighted
// undirected edge per pair, taking the strongest provenance rather than
// summing: two documents linked by both a yaml edge and a cosine edge are one
// relationship with the confidence of the better source, not a double-strength
// one. Duplicate parallel edges of the same kind used to inflate weight.
func graphAdjacency(g *Graph) *weightedAdjacency {
	components := g.Components()
	ids := make([]string, 0, len(components))
	for id, component := range components {
		// Session components carry only zero-weight session edges, so
		// modularity has nothing to say about them. Leaving them out keeps
		// them from becoming singleton communities that renumber every real
		// community index.
		if component.Type == TypeSession {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	embeddings := g.Embeddings()
	best := make(map[[2]string]float64)
	for _, id := range ids {
		for _, e := range g.Forward(id) {
			// Dangling links (an edge to a path no component was indexed for)
			// are real in this corpus and lint reports them separately; they
			// carry no membership information, so drop them here.
			if e.From == e.To {
				continue
			}
			if _, ok := components[e.From]; !ok {
				continue
			}
			if _, ok := components[e.To]; !ok {
				continue
			}
			key := [2]string{e.From, e.To}
			if key[0] > key[1] {
				key[0], key[1] = key[1], key[0]
			}
			if weight := edgeWeight(e, embeddings); weight > best[key] {
				best[key] = weight
			}
		}
	}

	pairs := make([][2]string, 0, len(best))
	for key := range best {
		pairs = append(pairs, key)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})

	w := newWeightedAdjacency(ids)
	for _, key := range pairs {
		w.link(key[0], key[1], best[key])
	}
	return w
}

// localMoving is one Louvain pass: repeatedly move nodes to the neighbouring
// community that yields the best modularity gain at resolution γ, until no
// node moves. Returns node -> community and whether anything changed.
func localMoving(w *weightedAdjacency, order []string, resolution float64) (map[string]string, bool) {
	community := make(map[string]string, len(order))
	communityStrength := make(map[string]float64, len(order))
	for _, id := range order {
		community[id] = id
		communityStrength[id] = w.strength[id]
	}
	if w.total2m == 0 {
		return community, false
	}

	moved := false
	for improved := true; improved; {
		improved = false
		for _, id := range order {
			nodeStrength := w.strength[id]
			if nodeStrength == 0 {
				continue
			}
			current := community[id]

			toCommunity := make(map[string]float64, len(w.adj[id]))
			for neighbor, weight := range w.adj[id] {
				toCommunity[community[neighbor]] += weight
			}

			// Detach first so every candidate — including staying put — is
			// scored against the same baseline.
			communityStrength[current] -= nodeStrength

			bestComm := current
			bestGain := gainOfJoining(toCommunity[current], communityStrength[current], nodeStrength, resolution, w.total2m)
			candidates := make([]string, 0, len(toCommunity))
			for comm := range toCommunity {
				if comm != current {
					candidates = append(candidates, comm)
				}
			}
			sort.Strings(candidates) // deterministic ties
			for _, comm := range candidates {
				gain := gainOfJoining(toCommunity[comm], communityStrength[comm], nodeStrength, resolution, w.total2m)
				if gain > bestGain {
					bestGain = gain
					bestComm = comm
				}
			}

			communityStrength[bestComm] += nodeStrength
			if bestComm != current {
				community[id] = bestComm
				improved = true
				moved = true
			}
		}
	}
	return community, moved
}

// gainOfJoining is the resolution-scaled modularity contribution of putting a
// node of the given strength into a community it currently sits outside of.
func gainOfJoining(weightToComm, commStrength, nodeStrength, resolution, total2m float64) float64 {
	return weightToComm/total2m - resolution*commStrength*nodeStrength/(total2m*total2m)
}

// aggregate collapses each community into a single super-node, summing edge
// weights between communities and folding intra-community weight into self
// loops. This is the step the previous single-level implementation lacked:
// without it, detection stops at the first pass and 1453 components shatter
// into 276 clusters averaging 4.6 documents each.
func aggregate(w *weightedAdjacency, community map[string]string) (*weightedAdjacency, []string) {
	names := make(map[string]bool, len(community))
	for _, comm := range community {
		names[comm] = true
	}
	order := make([]string, 0, len(names))
	for comm := range names {
		order = append(order, comm)
	}
	sort.Strings(order)

	next := newWeightedAdjacency(order)
	for _, id := range order {
		next.selfLoop[id] = 0
	}
	// Fold existing self loops first, then every original edge exactly once.
	for id, loop := range w.selfLoop {
		if loop > 0 {
			next.link(community[id], community[id], loop)
		}
	}
	seen := make(map[[2]string]bool)
	for a, neighbors := range w.adj {
		for b, weight := range neighbors {
			key := [2]string{a, b}
			if key[0] > key[1] {
				key[0], key[1] = key[1], key[0]
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			next.link(community[a], community[b], weight)
		}
	}
	return next, order
}

// DetectCommunities runs weighted, multi-level Louvain over the undirected
// view of g (forward and backward edges merged) and returns a map from
// Component ID to community index.
//
// Components with no edge at all cannot be placed by modularity at all and are
// reported as -1 ("未归类"). Unlike the previous implementation, small but
// genuinely connected communities are kept: collapsing every cluster of ≤2
// into -1 hid the fragmentation instead of fixing it.
func DetectCommunities(g *Graph) map[string]int {
	components := g.Components()
	result := make(map[string]int, len(components))
	if len(components) == 0 {
		return result
	}

	level := graphAdjacency(g)
	order := make([]string, 0, len(level.adj))
	for id := range level.adj {
		order = append(order, id)
	}
	sort.Strings(order)

	// membership maps every original node id to its community at the current
	// level; each pass rewrites it through the freshly aggregated graph.
	membership := make(map[string]string, len(order))
	for _, id := range order {
		membership[id] = id
	}

	for pass := 0; pass < maxLouvainPasses; pass++ {
		community, moved := localMoving(level, order, CommunityResolution)
		if !moved {
			break
		}
		for id, comm := range membership {
			membership[id] = community[comm]
		}
		level, order = aggregate(level, community)
		if len(order) <= 1 {
			break
		}
	}

	members := make(map[string][]string)
	for id, comm := range membership {
		if level.strength[comm] == 0 && graphAdjacencyIsolated(g, id) {
			result[id] = -1
			continue
		}
		members[comm] = append(members[comm], id)
	}

	commOrder := make([]string, 0, len(members))
	for comm := range members {
		commOrder = append(commOrder, comm)
	}
	sort.Strings(commOrder)

	index := 0
	for _, comm := range commOrder {
		ids := members[comm]
		sort.Strings(ids)
		for _, id := range ids {
			result[id] = index
		}
		index++
	}
	return result
}

const maxLouvainPasses = 20

// graphAdjacencyIsolated reports whether a component has no edges at all, the
// only case modularity cannot express an opinion about.
func graphAdjacencyIsolated(g *Graph, id string) bool {
	return len(g.Forward(id)) == 0 && len(g.Backlinks(id)) == 0
}

// Modularity scores a partition of g at the given resolution, so a change to
// detection can be compared against the previous partition on real data
// instead of by eyeballing the graph view.
func Modularity(g *Graph, communities map[string]int, resolution float64) float64 {
	w := graphAdjacency(g)
	if w.total2m == 0 {
		return 0
	}
	internal := make(map[int]float64)
	total := make(map[int]float64)
	for id := range w.adj {
		comm, ok := communities[id]
		if !ok {
			comm = -1
		}
		key := comm
		if comm < 0 {
			// Unclassified nodes are singletons, not one shared community.
			key = -1000000 - len(internal)
		}
		total[key] += w.strength[id]
		internal[key] += w.selfLoop[id] * 2
		for neighbor, weight := range w.adj[id] {
			other, ok := communities[neighbor]
			if !ok {
				other = -1
			}
			if comm >= 0 && other == comm {
				internal[key] += weight
			}
		}
	}
	q := 0.0
	for key, in := range internal {
		q += in/w.total2m - resolution*math.Pow(total[key]/w.total2m, 2)
	}
	return q
}

// communityLabelStopwords lists terms too generic to serve as a useful
// community label (English function words, common Chinese particles, and
// generic engineering/workflow terms that appear across all communities).
var communityLabelStopwords = map[string]bool{
	// English function words
	"the": true, "a": true, "an": true, "and": true, "is": true, "of": true,
	"to": true, "in": true, "for": true, "on": true, "with": true, "from": true,
	// Chinese particles
	"的": true, "是": true, "了": true, "和": true, "在": true, "与": true,
	// Generic engineering terms (appear in almost every change)
	"fix": true, "spec": true, "plan": true, "proposal": true, "design": true,
	"tasks": true, "implementation": true, "v1": true, "v2": true,
	"docs": true, "test": true, "add": true, "update": true, "get": true,
}

// communityLabelTerms is how many distinctive terms a label combines.
const communityLabelTerms = 3

// CommunityLabels names each community by the most distinctive terms across
// its members' titles.
//
// It used to pick the title of the member closest to the community's embedding
// centroid, which produced labels like "Welcome" or
// "Task 05: _build_tos.sh Implementation Report" — the name of one document,
// not the theme its neighbours share. A TF-IDF keyword combination reads as a
// topic, and needs no embeddings at all.
func CommunityLabels(components []Component, communities map[string]int) map[int]string {
	return communityLabelsTFIDF(components, communities)
}

// cosineSimilarity returns the cosine similarity between a and b. Vectors of
// mismatched length compare only over their shared prefix. A zero-length
// vector on either side yields -Inf so it never wins a "closest" comparison.
func cosineSimilarity(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, normA, normB float64
	for i := range n {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return math.Inf(-1)
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// communityLabelsTFIDF labels each community with its most distinctive terms
// (highest TF-IDF) across the titles of its members.
//
// TF is the term's frequency within a community's titles; IDF is
// log(N / df) where N is the number of communities and df is the number of
// communities whose titles contain the term at least once. Terms shorter than
// 2 runes, pure numbers and common stopwords are excluded. The top
// communityLabelTerms terms are joined with " · ": one term alone is often
// ambiguous ("orin"), while three read as a theme.
func communityLabelsTFIDF(components []Component, communities map[string]int) map[int]string {
	// Group titles (as token lists) by community, skipping misc (-1).
	commTokens := make(map[int][]string)
	for _, c := range components {
		commID, ok := communities[c.ID]
		if !ok || commID == -1 {
			continue
		}
		commTokens[commID] = append(commTokens[commID], labelTokens(c.Title)...)
	}
	if len(commTokens) == 0 {
		return map[int]string{}
	}

	// termFreq[commID][term] = raw count within that community's titles.
	termFreq := make(map[int]map[string]int, len(commTokens))
	// docFreq[term] = number of communities containing the term at least once.
	docFreq := make(map[string]int)
	for commID, tokens := range commTokens {
		freq := make(map[string]int)
		for _, tok := range tokens {
			if isValidLabelTerm(tok) {
				freq[tok]++
			}
		}
		termFreq[commID] = freq
		for term := range freq {
			docFreq[term]++
		}
	}

	n := float64(len(commTokens))
	labels := make(map[int]string, len(commTokens))
	for commID, freq := range termFreq {
		terms := make([]string, 0, len(freq))
		for term := range freq {
			terms = append(terms, term)
		}
		sort.Strings(terms) // deterministic tie-breaking
		scored := make([]struct {
			term  string
			score float64
		}, 0, len(terms))
		for _, term := range terms {
			tf := float64(freq[term])
			idf := math.Log(n / float64(docFreq[term]))
			scored = append(scored, struct {
				term  string
				score float64
			}{term, tf * idf})
		}
		sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

		picked := make([]string, 0, communityLabelTerms)
		for _, entry := range scored {
			if len(picked) == communityLabelTerms {
				break
			}
			// Skip a term already contained in (or containing) a picked one, so
			// CJK bigram overlaps like "密钥"/"钥管" do not fill the label.
			redundant := false
			for _, chosen := range picked {
				if strings.Contains(chosen, entry.term) || strings.Contains(entry.term, chosen) {
					redundant = true
					break
				}
			}
			if !redundant {
				picked = append(picked, entry.term)
			}
		}
		if len(picked) > 0 {
			labels[commID] = strings.Join(picked, " · ")
		}
	}
	return labels
}

// isValidLabelTerm reports whether tok is eligible to be a community label:
// at least 2 runes long, not a common stopword, and not a pure numeric string
// (which typically comes from date fragments like "06", "2026").
func isValidLabelTerm(tok string) bool {
	if communityLabelStopwords[tok] {
		return false
	}
	if len([]rune(tok)) < 2 {
		return false
	}
	// Reject pure numeric tokens (date fragments like "06", "2026", "13")
	allDigit := true
	for _, r := range tok {
		if !unicode.IsDigit(r) {
			allDigit = false
			break
		}
	}
	return !allDigit
}

// labelTokens tokenizes text the same way tokenizeCorpus does for ASCII
// runs (kept as lowercased words), but emits CJK runs as overlapping
// 2-rune bigrams rather than single runes: single CJK characters are too
// generic to serve as a label (e.g. "设" from "设计"), while bigrams like
// "安全" or "编译" capture meaningful, distinctive terms.
func labelTokens(text string) []string {
	var tokens []string
	for _, field := range strings.Fields(text) {
		var asciiRun []rune
		var cjkRun []rune
		flushASCII := func() {
			if len(asciiRun) > 0 {
				tokens = append(tokens, strings.ToLower(string(asciiRun)))
				asciiRun = asciiRun[:0]
			}
		}
		flushCJK := func() {
			for i := 0; i+1 < len(cjkRun); i++ {
				tokens = append(tokens, string(cjkRun[i:i+2]))
			}
			cjkRun = cjkRun[:0]
		}
		for _, r := range field {
			if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				flushCJK()
				asciiRun = append(asciiRun, r)
				continue
			}
			flushASCII()
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				cjkRun = append(cjkRun, r)
				continue
			}
			// Punctuation and other symbols are separators.
			flushCJK()
		}
		flushASCII()
		flushCJK()
	}
	return tokens
}
