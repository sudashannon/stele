package main

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stele/wiki"
)

// The weekly report used to allocate its space by document count, and document
// count is not work. One measured week: a documentation import produced 189
// files in a single day and took six of the report's eight themes, while six
// engineering sessions worth 9262, 5357, 3420, 2472, 2307 and 1796 recorded
// events produced a dozen documents each and were merged into one catch-all
// theme. This file is the effort axis that fixes the allocation: sessions own the
// documents they authored, everything left over is grouped and labelled for what
// it is.

// reportNarratableDocuments is the most documents one prose section can honestly
// represent. It is derived, not picked: a theme's prose is capped at 8 claims and
// a claim cites at most a few documents, so past roughly two dozen documents a
// summary stops describing its own section. Measured case: a monthly section held
// 275 documents across 112 directories and 22 days while its claims cited 9 - a
// label on a pile, not a summary.
const reportNarratableDocuments = 24

// bulkImportMinDocuments is the smallest group that reads as an import rather
// than as authored work. Below it, a same-day directory of documents is more
// likely a small set of hand-written notes.
const bulkImportMinDocuments = 12

// reportSessionWork is one session's section input: the effort snapshot plus the
// corpus documents attributed to it.
type reportSessionWork struct {
	Item      wiki.SessionWorkItem `json:"item"`
	Documents []int                `json:"-"`
}

// reportBulkImport is a group of documents that arrived together and that no
// session claims: same directory subtree, same activity day, no author edge.
//
// Reporting these as themes was the visible half of the skew - six NVIDIA guide
// chapters became six "achievements" with 41 claims between them. They are
// rendered as one counted line instead, with no LLM involved: there is nothing to
// summarize about a mirror of someone else's documentation.
type reportBulkImport struct {
	Directory string `json:"directory"`
	Date      string `json:"date"`
	Documents []int  `json:"-"`
	Count     int    `json:"count"`
	// Kind distinguishes a same-day tree that arrived at once ("import") from
	// documents that merely changed across the window with no session record
	// ("churn"). Both are counted rather than narrated, but calling a month of
	// scattered edits an "import" would be wrong.
	Kind string `json:"kind,omitempty"`
}

// reportAttribution is the result of splitting a corpus by authorship.
type reportAttribution struct {
	Sessions    []reportSessionWork
	BulkImports []reportBulkImport
	// Unattributed are documents no session authored and no bulk group absorbed.
	// They still cluster and summarize as before: a document written outside a
	// tracked session is real work, just work without an effort record.
	Unattributed []int
}

// attributeReportCorpus assigns every source document to the session that
// authored it, to a bulk import, or to the unattributed remainder.
//
// Authorship means a `write` or `edit` edge - reads are excluded, because
// consulting a document is not producing it. A document claimed by two sessions
// goes to the one with more in-window effort, which is deterministic and matches
// how a reader would describe the week.
func attributeReportCorpus(corpus *reportCorpus, sessions []wiki.SessionWorkItem) reportAttribution {
	indexByPath := make(map[string]int, len(corpus.Documents))
	for index, document := range corpus.Documents {
		if document.ContextOnly {
			continue
		}
		indexByPath[document.Path] = index
	}

	claimed := make(map[int]int) // document index -> session position
	work := make([]reportSessionWork, 0, len(sessions))
	for position, item := range sessions {
		entry := reportSessionWork{Item: item}
		for _, path := range append(append([]string(nil), item.Produced...), item.Edited...) {
			index, ok := indexByPath[path]
			if !ok {
				continue
			}
			// Sessions arrive in effort order, so the first claim wins and a
			// later, smaller session does not steal a document.
			if _, taken := claimed[index]; taken {
				continue
			}
			claimed[index] = position
			entry.Documents = append(entry.Documents, index)
		}
		sort.Ints(entry.Documents)
		work = append(work, entry)
	}

	leftovers := make([]int, 0)
	for index, document := range corpus.Documents {
		if document.ContextOnly {
			continue
		}
		if _, taken := claimed[index]; taken {
			continue
		}
		leftovers = append(leftovers, index)
	}

	bulk, rest := detectReportBulkImports(corpus, leftovers)
	return reportAttribution{Sessions: work, BulkImports: bulk, Unattributed: rest}
}

// detectReportBulkImports groups leftover documents that share a directory
// subtree and an activity day. Grouping walks from the deepest shared directory
// upward so a mirrored documentation tree collapses into one entry rather than
// one per chapter folder.
func detectReportBulkImports(corpus *reportCorpus, candidates []int) ([]reportBulkImport, []int) {
	type key struct {
		directory string
		date      string
	}
	groups := make(map[key][]int)
	for _, index := range candidates {
		document := corpus.Documents[index]
		groups[key{directory: filepath.Dir(document.Path), date: document.ActivityAt.Local().Format("2006-01-02")}] = append(groups[key{
			directory: filepath.Dir(document.Path),
			date:      document.ActivityAt.Local().Format("2006-01-02"),
		}], index)
	}

	// Fold child directories into a parent that shares the same day, so a tree
	// mirrored one chapter per folder counts once.
	merged := make(map[key][]int, len(groups))
	for group, documents := range groups {
		root := group
		for candidate := range groups {
			if candidate.date != group.date || candidate.directory == group.directory {
				continue
			}
			if strings.HasPrefix(group.directory, candidate.directory+string(filepath.Separator)) &&
				len(candidate.directory) < len(root.directory) {
				root = candidate
			}
		}
		merged[root] = append(merged[root], documents...)
	}

	imports := make([]reportBulkImport, 0)
	remaining := make([]int, 0)
	for group, documents := range merged {
		if len(documents) < bulkImportMinDocuments {
			remaining = append(remaining, documents...)
			continue
		}
		sort.Ints(documents)
		imports = append(imports, reportBulkImport{
			Directory: group.directory,
			Date:      group.date,
			Documents: documents,
			Count:     len(documents),
			Kind:      "import",
		})
	}
	sort.Slice(imports, func(i, j int) bool {
		if imports[i].Count != imports[j].Count {
			return imports[i].Count > imports[j].Count
		}
		return imports[i].Directory < imports[j].Directory
	})
	sort.Ints(remaining)
	return imports, remaining
}

// subsetSessionWork narrows session effort to a sub-window. A monthly report
// generates the weeks that have no weekly report yet, and those slices must be
// grouped the same way a weekly report would group them - otherwise the same week
// reads differently depending on whether someone happened to run it before.
//
// Document lists are passed through unchanged: the slice's corpus is already
// date-filtered, so attribution only ever sees that slice's documents.
func subsetSessionWork(items []wiki.SessionWorkItem, start, end time.Time) []wiki.SessionWorkItem {
	out := make([]wiki.SessionWorkItem, 0, len(items))
	for _, item := range items {
		days, events := 0, 0
		for day, count := range item.Activity {
			parsed, err := time.ParseInLocation("2006-01-02", day, time.Local)
			if err != nil || parsed.Before(start) || !parsed.Before(end) {
				continue
			}
			days++
			events += count
		}
		if days == 0 {
			continue
		}
		narrowed := item
		narrowed.ActiveDays = days
		narrowed.Events = events
		out = append(out, narrowed)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Events != out[j].Events {
			return out[i].Events > out[j].Events
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// summarizeUnattributedChurn turns an oversized cluster of documents nobody's
// session authored into counted rows by directory, so the reader sees where the
// churn is instead of a paragraph that cites 9 of 275 documents.
//
// Directories holding only a couple of documents are folded into one scattered
// row: 112 one-line rows is the same failure as one 275-document paragraph.
func summarizeUnattributedChurn(corpus *reportCorpus, indexes []int) []reportBulkImport {
	byDirectory := make(map[string][]int)
	for _, index := range indexes {
		directory := filepath.Dir(corpus.Documents[index].Path)
		byDirectory[directory] = append(byDirectory[directory], index)
	}
	groups := make([]reportBulkImport, 0, len(byDirectory))
	scattered := make([]int, 0)
	for directory, documents := range byDirectory {
		if len(documents) < 5 {
			scattered = append(scattered, documents...)
			continue
		}
		sort.Ints(documents)
		groups = append(groups, reportBulkImport{
			Directory: directory,
			Date:      reportDocumentDateRange(corpus, documents),
			Documents: documents,
			Count:     len(documents),
			Kind:      "churn",
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Directory < groups[j].Directory
	})
	if len(scattered) > 0 {
		sort.Ints(scattered)
		groups = append(groups, reportBulkImport{
			Directory: "（分散于多个目录）",
			Date:      reportDocumentDateRange(corpus, scattered),
			Documents: scattered,
			Count:     len(scattered),
			Kind:      "churn",
		})
	}
	return groups
}

// reportDocumentDateRange renders the span a group of documents changed over,
// collapsing to a single date when they all landed on one day.
func reportDocumentDateRange(corpus *reportCorpus, indexes []int) string {
	first, last := "", ""
	for _, index := range indexes {
		day := corpus.Documents[index].ActivityAt.Local().Format("2006-01-02")
		if first == "" || day < first {
			first = day
		}
		if last == "" || day > last {
			last = day
		}
	}
	if first == last {
		return first
	}
	return first + "~" + last
}
