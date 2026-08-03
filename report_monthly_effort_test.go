package main

import (
	"testing"
)

func monthlySource(period string, theme reportThemeDigest) monthlySourceTheme {
	return monthlySourceTheme{DigestID: "dg-" + period, PeriodStart: period, PeriodEnd: period, Theme: theme}
}

func sessionSourceTheme(id, label string, events, days int, docs ...string) reportThemeDigest {
	return reportThemeDigest{
		ID: "T1", Label: label, SessionID: id, SessionPath: "/s/" + id + ".jsonl",
		Effort:      reportThemeEffort{Workspace: "rx101", ActiveDays: days, Events: events, UserTurns: days},
		EvidenceIDs: docs,
		Summary:     reportEvidenceClaim{Kind: "summary", Text: label + " 概述。", EvidenceIDs: docs},
	}
}

// A session working across four weeks is one work item, not four themes. Prose
// clustering could not see that: it grouped by how summaries read, which collapsed
// a whole month into three themes with one holding 317 documents.
func TestMonthlyGroupsAWorkItemAcrossWeeks(t *testing.T) {
	ordered := []monthlySourceTheme{
		monthlySource("2026-07-05", sessionSourceTheme("s1", "安全鉴权", 3000, 4, "D1")),
		monthlySource("2026-07-12", sessionSourceTheme("s1", "安全鉴权", 5000, 5, "D2")),
		monthlySource("2026-07-19", sessionSourceTheme("s2", "rx101 pcie", 1000, 2, "D3")),
	}
	macro := reportCorpus{Documents: []reportDocument{
		reportDoc("/m/1", "w1", "2026-07-05", false),
		reportDoc("/m/2", "w2", "2026-07-12", false),
		reportDoc("/m/3", "w3", "2026-07-19", false),
	}}

	themes := groupMonthlyWorkItems(ordered, &macro)

	if len(themes) != 2 {
		t.Fatalf("themes = %d, want one per session", len(themes))
	}
	first := themes[0]
	if first.SessionID != "s1" || len(first.DocumentIndexes) != 2 {
		t.Fatalf("the cross-week session must be one theme: %+v", first)
	}
	if first.Effort.Events != 8000 || first.Effort.ActiveDays != 9 {
		t.Fatalf("effort must sum across weeks: %+v", first.Effort)
	}
	if themes[1].SessionID != "s2" {
		t.Fatalf("second theme = %+v, want the smaller session", themes[1])
	}
}

// The month is ordered by what it cost, like the week.
func TestMonthlyWorkItemsFollowEffortOrder(t *testing.T) {
	ordered := []monthlySourceTheme{
		monthlySource("2026-07-05", sessionSourceTheme("small", "小工作", 100, 1, "D1")),
		monthlySource("2026-07-12", sessionSourceTheme("big", "大工作", 9000, 6, "D2")),
	}
	macro := reportCorpus{Documents: []reportDocument{
		reportDoc("/m/1", "w1", "2026-07-05", false),
		reportDoc("/m/2", "w2", "2026-07-12", false),
	}}

	themes := groupMonthlyWorkItems(ordered, &macro)

	if themes[0].Label != "大工作" {
		t.Fatalf("labels out of effort order: %s then %s", themes[0].Label, themes[1].Label)
	}
}

// A month with thirty active sessions is normal; thirty prose sections is not a
// report. Past the cap they share a section, and nothing is dropped.
func TestMonthlyCapsNarratedSections(t *testing.T) {
	ordered := make([]monthlySourceTheme, 0)
	documents := make([]reportDocument, 0)
	for i := 0; i < reportSessionThemeLimit+4; i++ {
		id := "s" + string(rune('a'+i))
		ordered = append(ordered, monthlySource("2026-07-05", sessionSourceTheme(id, "会话"+id, 1000-i, 1, "D"+id)))
		documents = append(documents, reportDoc("/m/"+id, "w", "2026-07-05", false))
	}
	macro := reportCorpus{Documents: documents}

	themes := groupMonthlyWorkItems(ordered, &macro)

	if len(themes) != reportSessionThemeLimit+1 {
		t.Fatalf("themes = %d, want the cap plus a shared section", len(themes))
	}
	tail := themes[len(themes)-1]
	if tail.Label != "其他工作项" || len(tail.DocumentIndexes) != 4 {
		t.Fatalf("tail section = %+v", tail)
	}
	covered := map[int]bool{}
	for _, theme := range themes {
		for _, index := range theme.DocumentIndexes {
			if covered[index] {
				t.Fatalf("weekly theme %d landed in two sections", index)
			}
			covered[index] = true
		}
	}
	if len(covered) != len(ordered) {
		t.Fatalf("covered %d of %d weekly themes", len(covered), len(ordered))
	}
}

// Weekly themes no session authored still cluster by content and are marked, so
// a document written outside a tracked session is not silently dropped.
func TestMonthlyKeepsUnattributedThemes(t *testing.T) {
	ordered := []monthlySourceTheme{
		monthlySource("2026-07-05", sessionSourceTheme("s1", "有会话", 500, 2, "D1")),
		monthlySource("2026-07-12", reportThemeDigest{ID: "T2", Label: "无会话文档", Unattributed: true, EvidenceIDs: []string{"D2"}}),
	}
	macro := reportCorpus{Documents: []reportDocument{
		reportDoc("/m/1", "w1", "2026-07-05", false),
		reportDoc("/m/2", "w2", "2026-07-12", false),
	}}

	themes := groupMonthlyWorkItems(ordered, &macro)

	var found bool
	for _, theme := range themes {
		if theme.Unattributed {
			found = true
			if theme.SessionID != "" {
				t.Fatalf("unattributed theme must not claim a session: %+v", theme)
			}
			if len(theme.DocumentIndexes) != 1 || theme.DocumentIndexes[0] != 1 {
				t.Fatalf("index must map back to the loose weekly theme: %+v", theme.DocumentIndexes)
			}
		}
	}
	if !found {
		t.Fatal("the loose weekly theme must still produce a section")
	}
}

// The month's effort table must be one row per session for the whole month. A
// session active in four weeks was previously counted as four work items.
func TestMonthlyEffortMergesSessionsAcrossPeriods(t *testing.T) {
	digest := reportPeriodDigest{Counts: documentReportCounts{Documents: 500, WorkItems: 42}}
	sources := []reportPeriodDigest{
		{Sessions: []reportDigestSession{
			{ID: "s1", Path: "/s/1.jsonl", Title: "安全鉴权", ActiveDays: 4, Events: 3000, UserTurns: 40, Documents: 5,
				OpenTodos: []reportDigestOpenTodo{{Content: "第一周留下的", Status: "pending"}}},
			{ID: "s2", Path: "/s/2.jsonl", Title: "小事", ActiveDays: 1, Events: 50, Documents: 0},
		}, Counts: documentReportCounts{UnattributedDocuments: 3}},
		{Sessions: []reportDigestSession{
			{ID: "s1", Path: "/s/1.jsonl", Title: "安全鉴权", ActiveDays: 5, Events: 5000, UserTurns: 60, Documents: 7,
				OpenTodos: []reportDigestOpenTodo{{Content: "月末仍阻塞", Status: "blocked", Blocker: "等板子"}}},
		}, Counts: documentReportCounts{UnattributedDocuments: 4}},
	}

	attachMonthlyEffort(&digest, sources)

	if len(digest.Sessions) != 2 {
		t.Fatalf("sessions = %d, want one row per session for the month", len(digest.Sessions))
	}
	top := digest.Sessions[0]
	if top.ID != "s1" || top.Events != 8000 || top.ActiveDays != 9 || top.Documents != 12 {
		t.Fatalf("cross-period merge wrong: %+v", top)
	}
	// Only the latest snapshot of open work survives: a task finished in week two
	// must not be reported as still open at month end.
	if len(top.OpenTodos) != 1 || top.OpenTodos[0].Content != "月末仍阻塞" {
		t.Fatalf("open todos = %+v, want the latest period's", top.OpenTodos)
	}
	if digest.Counts.WorkItems != 2 {
		t.Fatalf("work items = %d, want the month's session count", digest.Counts.WorkItems)
	}
	if digest.Counts.UnattributedDocuments != 7 {
		t.Fatalf("unattributed = %d, want the sum", digest.Counts.UnattributedDocuments)
	}
}

// A bulk import that spans two weekly reports is one tree, not two.
func TestMonthlyEffortFoldsBulkImportsByDirectory(t *testing.T) {
	digest := reportPeriodDigest{Counts: documentReportCounts{Documents: 500}}
	sources := []reportPeriodDigest{
		{BulkImports: []reportDigestBulkImport{{Directory: "/repo/jetson", Date: "2026-07-30", Count: 189}}},
		{BulkImports: []reportDigestBulkImport{
			{Directory: "/repo/jetson", Date: "2026-07-28", Count: 11},
			{Directory: "/repo/other", Date: "2026-07-14", Count: 20},
		}},
	}

	attachMonthlyEffort(&digest, sources)

	if len(digest.BulkImports) != 2 {
		t.Fatalf("imports = %+v, want one per directory", digest.BulkImports)
	}
	first := digest.BulkImports[0]
	if first.Directory != "/repo/jetson" || first.Count != 200 {
		t.Fatalf("counts must fold: %+v", first)
	}
	if first.Date != "2026-07-28" {
		t.Fatalf("date = %q, want when the tree first landed", first.Date)
	}
	if digest.Counts.BulkImportDocuments != 220 {
		t.Fatalf("bulk documents = %d", digest.Counts.BulkImportDocuments)
	}
}

// Older digests carry no effort axis at all; the month must not invent one or
// overwrite the counts it does have.
func TestMonthlyEffortToleratesSourcesWithoutSessions(t *testing.T) {
	digest := reportPeriodDigest{Counts: documentReportCounts{Documents: 100, WorkItems: 7}}

	attachMonthlyEffort(&digest, []reportPeriodDigest{{Counts: documentReportCounts{}}})

	if len(digest.Sessions) != 0 || len(digest.BulkImports) != 0 {
		t.Fatalf("nothing to merge: %+v / %+v", digest.Sessions, digest.BulkImports)
	}
	if digest.Counts.WorkItems != 7 {
		t.Fatalf("work items = %d, want the existing count preserved", digest.Counts.WorkItems)
	}
}

// A weekly catch-all is a bag, not a subject. Letting it into the semantic merge
// is how one monthly section grew to 304 documents: the catch-alls pulled named
// themes in behind them.
func TestMonthlyKeepsCatchAllsOutOfNamedClusters(t *testing.T) {
	ordered := []monthlySourceTheme{
		monthlySource("2026-07-05", reportThemeDigest{ID: "T1", Label: "独立事项", Independent: true, Unattributed: true, EvidenceIDs: []string{"D1"}}),
		monthlySource("2026-07-12", reportThemeDigest{ID: "T2", Label: "独立事项", Independent: true, Unattributed: true, EvidenceIDs: []string{"D2"}}),
		monthlySource("2026-07-19", reportThemeDigest{ID: "T3", Label: "RX101 工站接入", Unattributed: true, EvidenceIDs: []string{"D3"}}),
	}
	macro := reportCorpus{Documents: []reportDocument{
		reportDoc("/m/1", "catch a", "2026-07-05", false),
		reportDoc("/m/2", "catch b", "2026-07-12", false),
		reportDoc("/m/3", "named", "2026-07-19", false),
	}}

	themes := groupMonthlyWorkItems(ordered, &macro)

	var bag, named *reportTheme
	for i := range themes {
		if themes[i].Independent {
			bag = &themes[i]
		} else if themes[i].Unattributed {
			named = &themes[i]
		}
	}
	if bag == nil || len(bag.DocumentIndexes) != 2 {
		t.Fatalf("both weekly catch-alls must share one section: %+v", bag)
	}
	if named == nil || len(named.DocumentIndexes) != 1 || named.DocumentIndexes[0] != 2 {
		t.Fatalf("the named theme must stay separate: %+v", named)
	}
}

// A macro pseudo-document must carry its own theme's workspace. Stamping the
// report's (possibly empty) workspace filter made the cross-workspace guard a
// no-op, which is what let four workspaces of loose themes merge.
func TestMonthlyThemeWorkspaceComesFromTheThemeNotTheFilter(t *testing.T) {
	documentWorkspace := map[string]string{"D1": "lz100", "D2": "lz100", "D3": "miao"}

	session := reportThemeDigest{Effort: reportThemeEffort{Workspace: "rx101"}, EvidenceIDs: []string{"D1"}}
	if got := monthlyThemeWorkspace(session, documentWorkspace); got != "rx101" {
		t.Fatalf("session theme workspace = %q, want its own effort workspace", got)
	}
	cluster := reportThemeDigest{EvidenceIDs: []string{"D1", "D2", "D3"}}
	if got := monthlyThemeWorkspace(cluster, documentWorkspace); got != "lz100" {
		t.Fatalf("cluster workspace = %q, want where most evidence lives", got)
	}
	if got := monthlyThemeWorkspace(reportThemeDigest{}, documentWorkspace); got != "" {
		t.Fatalf("empty theme workspace = %q, want empty", got)
	}
}

// Ten narratable weekly themes are not one narratable monthly theme. The macro
// clustering produced a 192-document section whose claims cited a handful, so
// members are packed up to what one section can represent.
func TestMonthlyPacksClusteredThemesWithinNarratableSize(t *testing.T) {
	ordered := make([]monthlySourceTheme, 0)
	for i := 0; i < 10; i++ {
		ids := make([]string, 0, 10)
		for j := 0; j < 10; j++ {
			ids = append(ids, "D"+strconvItoa(i*10+j))
		}
		ordered = append(ordered, monthlySource("2026-07-05", reportThemeDigest{
			ID: "T" + strconvItoa(i+1), Label: "文档主题" + strconvItoa(i), Unattributed: true, EvidenceIDs: ids,
		}))
	}

	// All ten in one cluster: packing must break them into sections of <= 24 docs.
	members := make([]int, 10)
	for i := range members {
		members[i] = i
	}
	groups := packMonthlyThemeMembers(ordered, members)

	if len(groups) < 4 {
		t.Fatalf("groups = %d, want the cluster split into representable sections", len(groups))
	}
	for _, group := range groups {
		size := 0
		for _, index := range group {
			size += len(ordered[index].Theme.EvidenceIDs)
		}
		if size > reportNarratableDocuments {
			t.Fatalf("group holds %d documents, over the narratable limit", size)
		}
	}
	seen := map[int]bool{}
	for _, group := range groups {
		for _, index := range group {
			if seen[index] {
				t.Fatalf("weekly theme %d packed twice", index)
			}
			seen[index] = true
		}
	}
	if len(seen) != len(members) {
		t.Fatalf("packed %d of %d weekly themes", len(seen), len(members))
	}
}

// One oversized weekly theme keeps its own section rather than being split: its
// evidence is one subject, and splitting it would file that subject twice.
func TestMonthlyPackingKeepsAnOversizedThemeWhole(t *testing.T) {
	big := make([]string, reportNarratableDocuments+20)
	for i := range big {
		big[i] = "D" + strconvItoa(i)
	}
	ordered := []monthlySourceTheme{
		monthlySource("2026-07-05", reportThemeDigest{ID: "T1", Label: "大主题", Unattributed: true, EvidenceIDs: big}),
		monthlySource("2026-07-12", reportThemeDigest{ID: "T2", Label: "小主题", Unattributed: true, EvidenceIDs: []string{"Dx"}}),
	}

	groups := packMonthlyThemeMembers(ordered, []int{0, 1})

	if len(groups) != 2 || len(groups[0]) != 1 || groups[0][0] != 0 {
		t.Fatalf("groups = %+v, want the oversized theme alone in its own section", groups)
	}
}

// A gap must be generated as weeks, not as one long window. Over 31 days the
// documents no session authored pile into one pool far past what a section can
// narrate, which pushed 490 of 583 documents into counted-only rows.
func TestMissingPeriodsAreGeneratedAsWeeks(t *testing.T) {
	periods, err := missingReportPeriods("2026-07-01", "2026-07-31", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(periods) != 5 {
		t.Fatalf("periods = %+v, want week-sized slices", periods)
	}
	if periods[0].Start != "2026-07-01" || periods[0].End != "2026-07-07" {
		t.Fatalf("first period = %+v", periods[0])
	}
	last := periods[len(periods)-1]
	if last.Start != "2026-07-29" || last.End != "2026-07-31" {
		t.Fatalf("last period = %+v, want the short tail", last)
	}
	// Every day of the month is covered exactly once.
	days := map[string]int{}
	for _, period := range periods {
		start, end, parseErr := parseInclusiveReportDates(period.Start, period.End)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
			days[day.Format("2006-01-02")]++
		}
	}
	if len(days) != 31 {
		t.Fatalf("covered %d days, want 31", len(days))
	}
	for day, count := range days {
		if count != 1 {
			t.Fatalf("%s covered %d times", day, count)
		}
	}
}

// A gap beside an existing weekly report is still chunked, and never overlaps the
// reused week.
func TestMissingPeriodsChunkAroundReusedWeeks(t *testing.T) {
	reused := []reportPeriodDigest{{Start: "2026-07-06", End: "2026-07-12"}}

	periods, err := missingReportPeriods("2026-07-01", "2026-07-31", reused)
	if err != nil {
		t.Fatal(err)
	}

	for _, period := range periods {
		if period.Start <= "2026-07-12" && period.End >= "2026-07-06" {
			t.Fatalf("period %+v overlaps the reused week", period)
		}
	}
	tail := 0
	for _, period := range periods {
		start, end, _ := parseInclusiveReportDates(period.Start, period.End)
		if days := int(end.Sub(start).Hours()/24) + 1; days > 7 {
			t.Fatalf("period %+v spans %d days", period, days)
		}
		tail++
	}
	if tail < 4 {
		t.Fatalf("periods = %+v, want the remaining days chunked into weeks", periods)
	}
}

// A session spanning four weekly periods worked the days it worked; adding the
// per-period counts would inflate a month past its own length.
func TestMonthlyEffortUnionsSessionDays(t *testing.T) {
	digest := reportPeriodDigest{Counts: documentReportCounts{Documents: 10}}
	sources := []reportPeriodDigest{
		{Sessions: []reportDigestSession{{ID: "s1", Path: "/s/1.jsonl", Title: "甲",
			Days: []string{"2026-07-06", "2026-07-07"}, ActiveDays: 2, Events: 100}}},
		{Sessions: []reportDigestSession{{ID: "s1", Path: "/s/1.jsonl", Title: "甲",
			Days: []string{"2026-07-07", "2026-07-13"}, ActiveDays: 2, Events: 200}}},
	}

	attachMonthlyEffort(&digest, sources)

	session := digest.Sessions[0]
	if session.ActiveDays != 3 {
		t.Fatalf("active days = %d, want the union (07-06, 07-07, 07-13)", session.ActiveDays)
	}
	if session.Events != 300 {
		t.Fatalf("events = %d, events do sum", session.Events)
	}
	if reportActiveDayCount(digest.Sessions) != 3 {
		t.Fatalf("aggregate day count = %d", reportActiveDayCount(digest.Sessions))
	}
}
