package main

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Structural grouping rules for documents that no naming convention the graph
// knows about covers.
//
// Two defects in a measured week motivated both rules. A design and its plan were
// rendered as two separate sections ("十、...设计" and "十一、...实施计划"), and a
// BitNet/Qwen training pair from workspace `miao` was merged into an LZ100
// workstation section from workspace `lz100` because both mention LZ100 often
// enough to clear the 0.58 cosine threshold.
//
// Neither rule adds a threshold. Both are decided by structure a reader can check.

// reportSiblingDatePrefix matches the leading YYYY-MM-DD- that the knowledge
// convention puts on a filename; it is stripped so a design written one week and
// its plan written the next still pair.
var reportSiblingDatePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)

// reportSiblingMinSlug keeps the rule off short, generic stems ("note", "readme")
// where a shared name means nothing.
const reportSiblingMinSlug = 8

// reportSiblingDocumentPairs finds documents that are stages of one work item:
// same directory, same slug, differing only in the trailing suffix token
// (`...-integration-design.md` and `...-integration-plan.md`).
//
// The graph's must-link sources only fire for structured layouts - OpenSpec
// `changes/<name>/{proposal,design,tasks}.md` and superpowers slug records - so a
// flat `knowledge/YYYY-MM-DD-<slug>-{design,plan}.md` pair has no edge between it
// and previously clustered apart. There is no suffix whitelist on purpose: two
// files sharing a directory and a slug are about the same thing whatever the
// trailing word is, and a list would need maintaining every time someone invents
// a document kind.
func reportSiblingDocumentPairs(corpus *reportCorpus) [][2]string {
	type stem struct {
		directory string
		slug      string
	}
	groups := make(map[stem][]string)
	for _, document := range corpus.Documents {
		name := strings.TrimSuffix(filepath.Base(document.Path), filepath.Ext(document.Path))
		name = reportSiblingDatePrefix.ReplaceAllString(name, "")
		cut := strings.LastIndex(name, "-")
		if cut < reportSiblingMinSlug {
			continue
		}
		key := stem{directory: filepath.Dir(document.Path), slug: name[:cut]}
		groups[key] = append(groups[key], document.SourceID)
	}
	pairs := make([][2]string, 0)
	for _, sources := range groups {
		for i := 1; i < len(sources); i++ {
			pairs = append(pairs, [2]string{sources[0], sources[i]})
		}
	}
	return pairs
}

// reportMergeAllowed decides whether two clusters may merge on semantic
// similarity alone.
//
// A report section is one work item, and a work item lives in one workspace. Two
// clusters from different workspaces with no edge between them are a guess: they
// share vocabulary, not a subject. Requiring an edge (affinity > 0) keeps
// genuinely linked cross-workspace documents mergeable - a markdown link or a YAML
// reference still counts - while refusing the coincidence.
//
// The catch-all built by reportConsolidateOutliers is exempt by construction: it
// never calls this, because "独立事项" claims nothing about relatedness.
func reportMergeAllowed(a, b *reportClusterNode, affinity float64) bool {
	if affinity > 0 {
		return true
	}
	return reportWorkspacesOverlap(a.Workspaces, b.Workspaces)
}

// reportWorkspacesOverlap reports whether two clusters share any workspace. An
// unknown workspace ("" - a document outside every registered root) overlaps
// nothing, so it cannot pull unrelated clusters together.
func reportWorkspacesOverlap(left, right map[string]struct{}) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	for workspace := range left {
		if _, ok := right[workspace]; ok {
			return true
		}
	}
	return false
}
