package main

import (
	"sort"
)

// Monthly grouping and effort aggregation.
//
// The month used to cluster weekly themes by prose similarity. Measured result on
// July: 40-odd weekly themes collapsed into 3 monthly themes, one holding 317
// documents, while 225 documents of the corpus belonged to no theme at all. Every
// weekly theme reads like engineering prose, so their embeddings sit close
// together and the 0.58 merge threshold swallows the month.
//
// The fix is the same principle the weekly already uses, one level up: a work item
// is identified by the session that did it, not by how its summary reads. The same
// session working across four weeks is one monthly section with its four weeks of
// effort summed. Prose clustering survives only for themes no session authored.

// groupMonthlyWorkItems turns the ordered weekly themes into monthly themes.
// Returned DocumentIndexes point into `ordered`, matching what
// summarizeMonthlyTheme expects.
func groupMonthlyWorkItems(ordered []monthlySourceTheme, macroCorpus *reportCorpus) []reportTheme {
	sessionOrder := make([]string, 0)
	bySession := make(map[string][]int)
	loose := make([]int, 0)
	for index, source := range ordered {
		id := source.Theme.SessionID
		if id == "" {
			loose = append(loose, index)
			continue
		}
		if _, seen := bySession[id]; !seen {
			sessionOrder = append(sessionOrder, id)
		}
		bySession[id] = append(bySession[id], index)
	}

	items := make([]reportTheme, 0, len(sessionOrder)+4)
	for _, id := range sessionOrder {
		indexes := bySession[id]
		item := reportTheme{SessionID: id, DocumentIndexes: indexes}
		best := -1
		for _, index := range indexes {
			theme := ordered[index].Theme
			item.Effort.Events += theme.Effort.Events
			item.Effort.ActiveDays += theme.Effort.ActiveDays
			item.Effort.UserTurns += theme.Effort.UserTurns
			item.Effort.Subagents += theme.Effort.Subagents
			if item.Effort.Workspace == "" {
				item.Effort.Workspace = theme.Effort.Workspace
			}
			if item.SessionPath == "" {
				item.SessionPath = theme.SessionPath
			}
			// The heaviest week names the month: a session's label is its own
			// title, and the week it did the most work in carries the version of
			// that title worth showing.
			if best < 0 || theme.Effort.Events > ordered[best].Theme.Effort.Events {
				best = index
			}
		}
		if best >= 0 {
			item.Label = ordered[best].Theme.Label
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Effort.Events != items[j].Effort.Events {
			return items[i].Effort.Events > items[j].Effort.Events
		}
		return items[i].Label < items[j].Label
	})

	// Sessions past the narration cap share one section, exactly as in the weekly
	// report: a month with thirty active sessions is normal, and thirty prose
	// sections is not a report anyone reads.
	narrated := items
	var tail []reportTheme
	if len(items) > reportSessionThemeLimit {
		narrated = items[:reportSessionThemeLimit]
		tail = items[reportSessionThemeLimit:]
	}

	themes := make([]reportTheme, 0, len(narrated)+2)
	number := 1
	for _, item := range narrated {
		item.ID = "T" + strconvItoa(number)
		themes = append(themes, item)
		number++
	}
	if len(tail) > 0 {
		merged := reportTheme{ID: "T" + strconvItoa(number), Label: "其他工作项", Independent: true}
		for _, item := range tail {
			merged.DocumentIndexes = append(merged.DocumentIndexes, item.DocumentIndexes...)
			merged.Effort.Events += item.Effort.Events
			merged.Effort.ActiveDays += item.Effort.ActiveDays
			merged.Effort.UserTurns += item.Effort.UserTurns
		}
		sort.Ints(merged.DocumentIndexes)
		themes = append(themes, merged)
		number++
	}

	// A weekly catch-all ("独立事项") is a bag, not a subject. Letting it into the
	// semantic merge is how a month ended up with one 304-document section: two
	// weeks' catch-alls pulled named themes in after them. Catch-alls merge only
	// with catch-alls; named themes cluster among themselves.
	named := make([]int, 0, len(loose))
	catchAll := make([]int, 0)
	for _, index := range loose {
		if ordered[index].Theme.Independent {
			catchAll = append(catchAll, index)
			continue
		}
		named = append(named, index)
	}
	if len(catchAll) > 0 {
		themes = append(themes, reportTheme{
			ID:              "T" + strconvItoa(number),
			Label:           "独立事项",
			Independent:     true,
			Unattributed:    true,
			DocumentIndexes: catchAll,
		})
		number++
	}

	// Weekly themes no session authored keep the old treatment: cluster them by
	// content, because a document written outside a tracked session is real work.
	if len(named) > 0 {
		subset := subsetCorpusDocuments(macroCorpus, named)
		for _, clustered := range clusterReportCorpus(&subset) {
			members := make([]int, 0, len(clustered.DocumentIndexes))
			for _, index := range clustered.DocumentIndexes {
				members = append(members, named[index])
			}
			sort.Ints(members)
			// Pack rather than merge wholesale. Each weekly theme is small enough to
			// narrate, but ten of them are not: the macro clustering produced a
			// monthly section holding 192 documents whose claims cited a handful.
			// Related weeks still land together, just not past what one section can
			// represent.
			for _, group := range packMonthlyThemeMembers(ordered, members) {
				mapped := reportTheme{
					ID:              "T" + strconvItoa(number),
					Label:           ordered[group[0]].Theme.Label,
					Unattributed:    true,
					DocumentIndexes: group,
				}
				themes = append(themes, mapped)
				number++
			}
		}
		macroCorpus.Coverage.ClusteringMode = subset.Coverage.ClusteringMode
	}
	return themes
}

// attachMonthlyEffort merges the effort axis of every source period onto the
// monthly digest: one row per session for the whole month, and bulk imports folded
// by directory.
//
// Without this the month showed `sessions: []` and `bulkImports: []` while still
// counting those documents in its totals - so a 189-file import inflated the
// document count with nothing on the page to explain it.
func attachMonthlyEffort(digest *reportPeriodDigest, sources []reportPeriodDigest) {
	order := make([]string, 0)
	merged := make(map[string]*reportDigestSession)
	for _, source := range sources {
		for _, session := range source.Sessions {
			existing, ok := merged[session.ID]
			if !ok {
				copied := session
				copied.OpenTodos = append([]reportDigestOpenTodo(nil), session.OpenTodos...)
				merged[session.ID] = &copied
				order = append(order, session.ID)
				continue
			}
			existing.Days = mergeSortedDays(existing.Days, session.Days)
			if len(existing.Days) > 0 {
				existing.ActiveDays = len(existing.Days)
			} else {
				existing.ActiveDays += session.ActiveDays
			}
			existing.Events += session.Events
			existing.UserTurns += session.UserTurns
			existing.Documents += session.Documents
			// The latest period's open tasks are the ones still open at month end;
			// carrying every week's snapshot would list tasks that were finished
			// two weeks later as if they were outstanding.
			if len(session.OpenTodos) > 0 {
				existing.OpenTodos = append([]reportDigestOpenTodo(nil), session.OpenTodos...)
			}
			if session.Title != "" {
				existing.Title = session.Title
			}
		}
	}
	digest.Sessions = make([]reportDigestSession, 0, len(order))
	for _, id := range order {
		digest.Sessions = append(digest.Sessions, *merged[id])
	}
	sort.SliceStable(digest.Sessions, func(i, j int) bool {
		if digest.Sessions[i].Events != digest.Sessions[j].Events {
			return digest.Sessions[i].Events > digest.Sessions[j].Events
		}
		return digest.Sessions[i].Path < digest.Sessions[j].Path
	})

	importOrder := make([]string, 0)
	importsByDirectory := make(map[string]*reportDigestBulkImport)
	for _, source := range sources {
		for _, group := range source.BulkImports {
			existing, ok := importsByDirectory[group.Directory]
			if !ok {
				copied := group
				importsByDirectory[group.Directory] = &copied
				importOrder = append(importOrder, group.Directory)
				continue
			}
			existing.Count += group.Count
			// Keep the earliest date: it is when the tree landed, and a later week
			// only added to it.
			if group.Date < existing.Date {
				existing.Date = group.Date
			}
		}
	}
	digest.BulkImports = make([]reportDigestBulkImport, 0, len(importOrder))
	for _, directory := range importOrder {
		digest.BulkImports = append(digest.BulkImports, *importsByDirectory[directory])
	}
	sort.SliceStable(digest.BulkImports, func(i, j int) bool {
		if digest.BulkImports[i].Count != digest.BulkImports[j].Count {
			return digest.BulkImports[i].Count > digest.BulkImports[j].Count
		}
		return digest.BulkImports[i].Directory < digest.BulkImports[j].Directory
	})

	digest.Counts.Sessions = len(digest.Sessions)
	digest.Counts.BulkImportDocuments = 0
	for _, group := range digest.BulkImports {
		digest.Counts.BulkImportDocuments += group.Count
	}
	digest.Counts.UnattributedDocuments = 0
	for _, source := range sources {
		digest.Counts.UnattributedDocuments += source.Counts.UnattributedDocuments
	}
	// Work items are the month's sessions, not the sum of every week's count - a
	// session active in four weeks was counted four times before.
	if len(digest.Sessions) > 0 {
		digest.Counts.WorkItems = len(digest.Sessions)
	}
}

// monthlyThemeWorkspace names the workspace a weekly theme belongs to, so the
// cross-workspace merge guard can see it. A session theme knows its own; a
// document cluster is named by where most of its evidence lives.
//
// Every macro pseudo-document used to inherit the report's workspace *filter*,
// which is empty for an all-workspace report - the guard then treated every pair
// as overlapping and merged four workspaces of loose themes into one section.
func monthlyThemeWorkspace(theme reportThemeDigest, documentWorkspace map[string]string) string {
	if theme.Effort.Workspace != "" {
		return theme.Effort.Workspace
	}
	counts := make(map[string]int)
	for _, id := range theme.EvidenceIDs {
		if workspace := documentWorkspace[id]; workspace != "" {
			counts[workspace]++
		}
	}
	best, bestCount := "", 0
	for workspace, count := range counts {
		if count > bestCount || (count == bestCount && workspace < best) {
			best, bestCount = workspace, count
		}
	}
	return best
}

// packMonthlyThemeMembers splits a cluster of weekly themes into sections that
// each stay within what prose can represent, measured by the documents they carry.
//
// A single weekly theme larger than the limit still gets its own section: it was
// already checked when its own report was built, and splitting one theme's evidence
// across two monthly sections would put one subject in two places.
func packMonthlyThemeMembers(ordered []monthlySourceTheme, members []int) [][]int {
	groups := make([][]int, 0, 1)
	current := make([]int, 0, len(members))
	size := 0
	for _, index := range members {
		count := len(ordered[index].Theme.EvidenceIDs)
		if len(current) > 0 && size+count > reportNarratableDocuments {
			groups = append(groups, current)
			current = make([]int, 0, len(members))
			size = 0
		}
		current = append(current, index)
		size += count
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

// mergeSortedDays unions two date lists. A session spanning several weekly
// periods must not count a day twice, and its month total is the days it actually
// worked, not the sum of per-period counts.
func mergeSortedDays(left, right []string) []string {
	if len(right) == 0 {
		return left
	}
	seen := make(map[string]struct{}, len(left)+len(right))
	out := make([]string, 0, len(left)+len(right))
	for _, day := range append(append([]string(nil), left...), right...) {
		if _, duplicate := seen[day]; duplicate {
			continue
		}
		seen[day] = struct{}{}
		out = append(out, day)
	}
	sort.Strings(out)
	return out
}
