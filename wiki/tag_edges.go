package wiki

import (
	"math"
	"sort"
)

const maxTagEdgeDegree = 6

// ComputeTagEdges builds sparse, coverage-pruned edges from controlled tags.
// Each eligible tag contributes only cycle candidates over its sorted member
// IDs; the global retained tag-degree cap may greedily prune individual
// candidates when overlapping tags would otherwise create dense hubs.
//
// Only workspace documents count as corpus: session components would inflate
// the corpus size that drives both the coverage ceiling and every tag's IDF
// weight, so agent activity must not shift document tag edges.
func ComputeTagEdges(all []Component, taxonomy *Taxonomy) []Edge {
	components := documentComponents(all)
	if taxonomy == nil || len(components) == 0 {
		return nil
	}

	postings := make(map[string]map[string]struct{})
	for _, component := range components {
		for _, rawTag := range EffectiveComponentTags(component, taxonomy) {
			canonical, controlled := taxonomy.Canonical(rawTag)
			facet, hasFacet := taxonomy.facetOf[canonical]
			if !controlled || !hasFacet || !facet.Edges {
				continue
			}
			members := postings[canonical]
			if members == nil {
				members = make(map[string]struct{})
				postings[canonical] = members
			}
			members[component.ID] = struct{}{}
		}
	}

	maxDocs := int(math.Floor(float64(len(components)) * taxonomy.maxShare))
	if maxDocs < taxonomy.minDocs {
		return nil
	}

	tags := make([]string, 0, len(postings))
	for tag, members := range postings {
		if taxonomy.EdgeEligible(tag, len(members), len(components)) {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)

	byPair := make(map[tagEdgePair]Edge)
	for _, tag := range tags {
		members := postings[tag]
		ids := make([]string, 0, len(members))
		for id := range members {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) < 2 {
			continue
		}

		weight := tagEdgeWeight(len(components), len(ids), taxonomy.minDocs, maxDocs)
		kind := "shares-tag:" + tag
		addCandidate := func(left, right string) {
			if right < left {
				left, right = right, left
			}
			pair := tagEdgePair{from: left, to: right}
			candidate := Edge{From: left, To: right, Kind: kind, Source: "tag", Weight: weight}
			if prior, exists := byPair[pair]; !exists || strongerTagEdge(candidate, prior) {
				byPair[pair] = candidate
			}
		}
		for i := 1; i < len(ids); i++ {
			addCandidate(ids[i-1], ids[i])
		}
		if len(ids) >= 3 {
			addCandidate(ids[len(ids)-1], ids[0])
		}
	}

	candidates := make([]Edge, 0, len(byPair))
	for _, edge := range byPair {
		candidates = append(candidates, edge)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Weight != right.Weight {
			return left.Weight > right.Weight
		}
		if left.From != right.From {
			return left.From < right.From
		}
		if left.To != right.To {
			return left.To < right.To
		}
		return left.Kind < right.Kind
	})

	degree := make(map[string]int)
	retained := make([]Edge, 0, len(candidates))
	for _, edge := range candidates {
		if degree[edge.From] >= maxTagEdgeDegree || degree[edge.To] >= maxTagEdgeDegree {
			continue
		}
		retained = append(retained, edge)
		degree[edge.From]++
		degree[edge.To]++
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

type tagEdgePair struct {
	from string
	to   string
}

func strongerTagEdge(candidate, prior Edge) bool {
	return candidate.Weight > prior.Weight || (candidate.Weight == prior.Weight && candidate.Kind < prior.Kind)
}

func tagEdgeWeight(corpusSize, docFrequency, minDocs, maxDocs int) float64 {
	x := 1.0
	if minDocs != maxDocs {
		n := float64(corpusSize)
		numerator := math.Log(n/float64(docFrequency)) - math.Log(n/float64(maxDocs))
		denominator := math.Log(n/float64(minDocs)) - math.Log(n/float64(maxDocs))
		x = numerator / denominator
		if x < 0 {
			x = 0
		} else if x > 1 {
			x = 1
		}
	}
	return 0.20 + 0.20*x
}
