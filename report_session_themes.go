package main

import (
	"sort"

	"stele/wiki"
)

// reportSessionThemeLimit caps the sections that get prose. The effort table
// still lists every session, so nothing disappears - this only bounds how many
// model calls one report makes and how long the narrative runs.
const reportSessionThemeLimit = 8

// buildSessionThemes turns an attribution into the report's theme list.
//
// The skeleton is the effort axis: one theme per session that did work in the
// window, ordered by that work, so the week reads as what it cost rather than as
// what it happened to write down. A session that produced many documents still
// gets internal structure - its documents run through the same clustering as
// before, one sub-theme per coherent group - but that structure now lives inside
// the session instead of replacing it.
//
// Documents nobody authored keep their old treatment (clustered and summarized),
// because a document written outside a tracked session is real work; only bulk
// imports are pulled out, and those are rendered as counts, not achievements.
func buildSessionThemes(corpus *reportCorpus, attribution *reportAttribution) []reportTheme {
	themes := make([]reportTheme, 0, len(attribution.Sessions)+4)
	// Oversized unattributed clusters are appended to the attribution's counted
	// groups rather than narrated; the caller renders them beside bulk imports.
	churn := make([]reportBulkImport, 0)
	number := 1
	narrated := 0
	tail := make([]reportSessionWork, 0)

	for _, session := range attribution.Sessions {
		// A session with no citable document cannot carry claims - the evidence
		// contract is that every claim points at a document hash. Its effort is
		// not lost: it still appears in the report's effort table and in any
		// blocker it recorded. Inventing a theme with no evidence would be the
		// opposite failure from the one this file fixes.
		if !sessionHasPrimaryDocument(corpus, session.Documents) {
			continue
		}
		// Narrated sections are capped by effort, not by document count. Every
		// session still appears in the effort table with its own numbers; past the
		// cap they share one section instead of each costing a model call. A week
		// with fourteen active sessions is normal, and fourteen prose sections is
		// not a report anyone reads.
		if narrated >= reportSessionThemeLimit {
			tail = append(tail, session)
			continue
		}
		narrated++
		theme := reportTheme{
			ID:              "T" + strconvItoa(number),
			Label:           session.Item.Title,
			DocumentIndexes: append([]int(nil), session.Documents...),
			SessionID:       session.Item.ID,
			SessionPath:     session.Item.Path,
			OpenTasks:       sessionOpenTaskLines(session.Item.OpenTodos),
			Effort: reportThemeEffort{
				Workspace:  session.Item.Workspace,
				ActiveDays: session.Item.ActiveDays,
				Events:     session.Item.Events,
				UserTurns:  session.Item.UserTurns,
				Subagents:  session.Item.Subagents,
			},
		}
		for _, index := range session.Documents {
			document := corpus.Documents[index]
			if document.ContextOnly {
				theme.ContextEvidenceIDs = append(theme.ContextEvidenceIDs, document.EvidenceID)
			} else {
				theme.EvidenceIDs = append(theme.EvidenceIDs, document.EvidenceID)
			}
		}
		theme.RepresentativeIDs = reportRepresentativeEvidence(corpus, session.Documents, 3)
		themes = append(themes, theme)
		number++
		corpus.Counts.WorkItems++
	}

	// Sessions past the cap keep their documents visible under one honest label.
	if len(tail) > 0 {
		merged := reportTheme{
			ID:          "T" + strconvItoa(number),
			Label:       "其他工作项",
			Independent: true,
		}
		for _, session := range tail {
			merged.DocumentIndexes = append(merged.DocumentIndexes, session.Documents...)
			merged.Effort.ActiveDays += session.Item.ActiveDays
			merged.Effort.Events += session.Item.Events
			merged.Effort.UserTurns += session.Item.UserTurns
			merged.OpenTasks = append(merged.OpenTasks, sessionOpenTaskLines(session.Item.OpenTodos)...)
		}
		sort.Ints(merged.DocumentIndexes)
		for _, index := range merged.DocumentIndexes {
			document := corpus.Documents[index]
			if document.ContextOnly {
				merged.ContextEvidenceIDs = append(merged.ContextEvidenceIDs, document.EvidenceID)
			} else {
				merged.EvidenceIDs = append(merged.EvidenceIDs, document.EvidenceID)
			}
		}
		merged.RepresentativeIDs = reportRepresentativeEvidence(corpus, merged.DocumentIndexes, 3)
		themes = append(themes, merged)
		number++
	}

	// Leftover authored documents cluster among themselves, so an untracked but
	// genuinely written document is still summarized rather than counted.
	if len(attribution.Unattributed) > 0 {
		leftover := subsetCorpusDocuments(corpus, attribution.Unattributed)
		for _, theme := range clusterReportCorpus(&leftover) {
			mapped := reportTheme{
				ID:                 "T" + strconvItoa(number),
				Label:              theme.Label,
				Independent:        theme.Independent,
				Unattributed:       true,
				EvidenceIDs:        theme.EvidenceIDs,
				ContextEvidenceIDs: theme.ContextEvidenceIDs,
				RepresentativeIDs:  theme.RepresentativeIDs,
			}
			for _, index := range theme.DocumentIndexes {
				mapped.DocumentIndexes = append(mapped.DocumentIndexes, attribution.Unattributed[index])
			}
			// A cluster too large to represent in prose is counted instead. Over a
			// long window most documents have no session record, and clustering
			// hundreds of unrelated ones into a handful of groups produces sections
			// that cite a tenth of their own evidence.
			if len(mapped.DocumentIndexes) > reportNarratableDocuments {
				churn = append(churn, summarizeUnattributedChurn(corpus, mapped.DocumentIndexes)...)
				continue
			}
			themes = append(themes, mapped)
			number++
		}
		corpus.Coverage.ClusteringMode = leftover.Coverage.ClusteringMode
	}
	attribution.BulkImports = append(attribution.BulkImports, churn...)

	return themes
}

// subsetCorpusDocuments builds a corpus over a document subset, keeping the
// edges and vectors the clustering needs. Indexes in the result are positions in
// the subset, which the caller maps back.
func subsetCorpusDocuments(corpus *reportCorpus, indexes []int) reportCorpus {
	subset := reportCorpus{
		Start:      corpus.Start,
		End:        corpus.End,
		Workspace:  corpus.Workspace,
		Edges:      corpus.Edges,
		Connectors: corpus.Connectors,
		Counts:     documentReportCounts{Types: map[string]int{}},
		Coverage:   reportCoverage{},
	}
	for _, index := range indexes {
		document := corpus.Documents[index]
		subset.Documents = append(subset.Documents, document)
		if document.Vector == nil {
			subset.Coverage.MissingEmbeddings++
		}
	}
	return subset
}

// reportRepresentativeEvidence picks the documents that best name a session's
// output: produced or edited documents with the most structure, longest first, so
// the label and the summary lead with the substantial artifact rather than a
// one-line note.
func reportRepresentativeEvidence(corpus *reportCorpus, indexes []int, limit int) []string {
	ordered := append([]int(nil), indexes...)
	sort.Slice(ordered, func(i, j int) bool {
		left, right := corpus.Documents[ordered[i]], corpus.Documents[ordered[j]]
		leftWeight := len(left.Metadata.Headings) + len(left.Metadata.KeyParagraphs)
		rightWeight := len(right.Metadata.Headings) + len(right.Metadata.KeyParagraphs)
		if leftWeight != rightWeight {
			return leftWeight > rightWeight
		}
		return left.EvidenceID < right.EvidenceID
	})
	out := make([]string, 0, limit)
	for _, index := range ordered {
		if corpus.Documents[index].ContextOnly {
			continue
		}
		out = append(out, corpus.Documents[index].EvidenceID)
		if len(out) == limit {
			break
		}
	}
	return out
}

// sessionWorkTotals summarises the effort axis for the report's overview.
func sessionWorkTotals(items []wiki.SessionWorkItem) (sessions int, events int, days int) {
	seenDays := 0
	for _, item := range items {
		sessions++
		events += item.Events
		seenDays += item.ActiveDays
	}
	return sessions, events, seenDays
}

// sessionHasPrimaryDocument reports whether any attributed document is in-window
// output rather than context carried in for continuity.
func sessionHasPrimaryDocument(corpus *reportCorpus, indexes []int) bool {
	for _, index := range indexes {
		if !corpus.Documents[index].ContextOnly {
			return true
		}
	}
	return false
}

// sessionOpenTaskLines renders a session's unfinished tasks as one line each,
// carrying the blocker reason when the session recorded one: "waiting on X" is
// the part of an unfinished task that is worth reading.
func sessionOpenTaskLines(todos []wiki.SessionTodoSnapshot) []string {
	lines := make([]string, 0, len(todos))
	for _, todo := range todos {
		line := todo.Content
		if todo.Phase != "" {
			line = todo.Phase + " / " + line
		}
		if todo.Blocker != "" {
			line += "(阻塞:" + todo.Blocker + ")"
		} else if todo.Status != "" {
			line += "(" + todo.Status + ")"
		}
		lines = append(lines, line)
	}
	return lines
}
