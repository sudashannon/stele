package main

import (
	"testing"
	"time"

	"stele/wiki"
)

func reportDoc(path, title string, day string, contextOnly bool) reportDocument {
	at, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		panic(err)
	}
	return reportDocument{
		EvidenceID:  "D" + path,
		SourceID:    path,
		Path:        path,
		Title:       title,
		Type:        "knowledge",
		Workspace:   "rx101",
		ActivityAt:  at,
		ContextOnly: contextOnly,
	}
}

func sessionItem(title string, events int, produced []string, edited []string) wiki.SessionWorkItem {
	return wiki.SessionWorkItem{
		ID: title, Path: "/s/" + title + ".jsonl", Title: title, Workspace: "rx101",
		ActiveDays: 1, Events: events, Produced: produced, Edited: edited,
	}
}

// Authorship is a write or an edit. Reads are excluded on purpose: crediting the
// session that merely consulted a document would put a week's work under whoever
// read the most.
func TestAttributionGivesEachDocumentToItsAuthoringSession(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		reportDoc("/repo/a.md", "A", "2026-07-28", false),
		reportDoc("/repo/b.md", "B", "2026-07-28", false),
		reportDoc("/repo/c.md", "C", "2026-07-29", false),
	}}
	items := []wiki.SessionWorkItem{
		sessionItem("大会话", 9000, []string{"/repo/a.md"}, []string{"/repo/b.md"}),
		sessionItem("小会话", 100, nil, []string{"/repo/c.md"}),
	}

	attribution := attributeReportCorpus(&corpus, items)

	if len(attribution.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(attribution.Sessions))
	}
	if got := attribution.Sessions[0].Documents; len(got) != 2 {
		t.Fatalf("first session documents = %v, want both authored ones", got)
	}
	if got := attribution.Sessions[1].Documents; len(got) != 1 || corpus.Documents[got[0]].Path != "/repo/c.md" {
		t.Fatalf("second session documents = %v", got)
	}
	if len(attribution.Unattributed) != 0 || len(attribution.BulkImports) != 0 {
		t.Fatalf("nothing should be left over: %+v", attribution)
	}
}

// Two sessions can touch the same document in one week. Effort order decides, so
// the section a reader sees matches where the work actually went.
func TestAttributionBreaksTiesByEffort(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{reportDoc("/repo/shared.md", "Shared", "2026-07-28", false)}}
	items := []wiki.SessionWorkItem{
		sessionItem("投入大", 5000, nil, []string{"/repo/shared.md"}),
		sessionItem("投入小", 50, []string{"/repo/shared.md"}, nil),
	}

	attribution := attributeReportCorpus(&corpus, items)

	if len(attribution.Sessions[0].Documents) != 1 {
		t.Fatalf("the higher-effort session must own the shared document")
	}
	if len(attribution.Sessions[1].Documents) != 0 {
		t.Fatalf("the smaller session must not also claim it")
	}
}

// Context documents are pulled in for continuity, not as this week's output, so
// they must not be attributed or counted as a bulk import.
func TestAttributionIgnoresContextDocuments(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		reportDoc("/repo/ctx.md", "Older context", "2026-06-01", true),
		reportDoc("/repo/new.md", "This week", "2026-07-28", false),
	}}
	items := []wiki.SessionWorkItem{sessionItem("会话", 10, []string{"/repo/ctx.md", "/repo/new.md"}, nil)}

	attribution := attributeReportCorpus(&corpus, items)

	if got := attribution.Sessions[0].Documents; len(got) != 1 || corpus.Documents[got[0]].Path != "/repo/new.md" {
		t.Fatalf("documents = %v, want only the in-window one", got)
	}
}

// A mirrored documentation tree arrives in one day under one subtree and no
// session authors it. Reporting its chapters as achievements is what buried the
// week's real work, so it collapses into one counted group.
func TestBulkImportCollapsesASameDayTree(t *testing.T) {
	documents := []reportDocument{}
	for _, name := range []string{"AR/Boot.md", "AR/Boot/Flow.md", "AR/Boot/Thor.md", "AR/Arch.md", "SIPL/Intro.md", "SIPL/Api.md"} {
		documents = append(documents, reportDoc("/repo/knowledge/jetson/"+name, name, "2026-07-30", false))
	}
	for i := 0; i < 8; i++ {
		documents = append(documents, reportDoc("/repo/knowledge/jetson/extra"+string(rune('a'+i))+".md", "extra", "2026-07-30", false))
	}
	// Authored work the same week, in another tree: must stay out of the group.
	documents = append(documents, reportDoc("/repo/knowledge/design.md", "Design", "2026-07-28", false))
	corpus := reportCorpus{Documents: documents}

	attribution := attributeReportCorpus(&corpus, nil)

	if len(attribution.BulkImports) != 1 {
		t.Fatalf("bulk imports = %+v, want one collapsed group", attribution.BulkImports)
	}
	group := attribution.BulkImports[0]
	if group.Count != 14 {
		t.Fatalf("count = %d, want the whole tree", group.Count)
	}
	if group.Directory != "/repo/knowledge/jetson" && group.Directory != "/repo/knowledge/jetson/AR" {
		t.Fatalf("directory = %q, want the shared subtree", group.Directory)
	}
	if group.Date != "2026-07-30" {
		t.Fatalf("date = %q", group.Date)
	}
	if len(attribution.Unattributed) != 1 || corpus.Documents[attribution.Unattributed[0]].Title != "Design" {
		t.Fatalf("unattributed = %v, want the separately authored document", attribution.Unattributed)
	}
}

// A handful of same-day notes is authored work, not an import: collapsing them
// would hide real output behind a count.
func TestBulkImportIgnoresSmallSameDayGroups(t *testing.T) {
	documents := []reportDocument{}
	for i := 0; i < bulkImportMinDocuments-1; i++ {
		documents = append(documents, reportDoc("/repo/knowledge/note"+string(rune('a'+i))+".md", "note", "2026-07-30", false))
	}
	corpus := reportCorpus{Documents: documents}

	attribution := attributeReportCorpus(&corpus, nil)

	if len(attribution.BulkImports) != 0 {
		t.Fatalf("bulk imports = %+v, want none below the threshold", attribution.BulkImports)
	}
	if len(attribution.Unattributed) != bulkImportMinDocuments-1 {
		t.Fatalf("unattributed = %d, want all of them", len(attribution.Unattributed))
	}
}

// A session with in-window effort but no documents still has to appear: "worked
// six days, produced no document" is information, and dropping it is how the
// old report lost entire workstreams.
func TestAttributionKeepsSessionsWithoutDocuments(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{reportDoc("/repo/a.md", "A", "2026-07-28", false)}}
	items := []wiki.SessionWorkItem{
		sessionItem("有产出", 100, []string{"/repo/a.md"}, nil),
		sessionItem("无产出但投入大", 8000, nil, nil),
	}

	attribution := attributeReportCorpus(&corpus, items)

	if len(attribution.Sessions) != 2 {
		t.Fatalf("sessions = %d, want both", len(attribution.Sessions))
	}
	var found bool
	for _, session := range attribution.Sessions {
		if session.Item.Title == "无产出但投入大" && len(session.Documents) == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("a session without documents must survive attribution")
	}
}

// A monthly report generates the weeks that have no weekly report yet. Those
// slices must be narrowed to their own window, or a week would inherit the whole
// month's effort and outrank everything.
func TestSubsetSessionWorkNarrowsToTheSliceWindow(t *testing.T) {
	day := func(value string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	items := []wiki.SessionWorkItem{{
		ID: "s1", Path: "/s/1.jsonl", Title: "跨周会话", Workspace: "rx101",
		ActiveDays: 3, Events: 600,
		Activity: map[string]int{"2026-07-06": 100, "2026-07-13": 200, "2026-07-14": 300},
		Produced: []string{"/repo/a.md"},
	}}

	first := subsetSessionWork(items, day("2026-07-06"), day("2026-07-13"))
	if len(first) != 1 || first[0].ActiveDays != 1 || first[0].Events != 100 {
		t.Fatalf("first week = %+v, want only that week's effort", first)
	}
	second := subsetSessionWork(items, day("2026-07-13"), day("2026-07-20"))
	if len(second) != 1 || second[0].ActiveDays != 2 || second[0].Events != 500 {
		t.Fatalf("second week = %+v", second)
	}
	if len(first[0].Produced) != 1 {
		t.Fatal("document lists pass through: the slice corpus is already date-filtered")
	}
	empty := subsetSessionWork(items, day("2026-07-20"), day("2026-07-27"))
	if len(empty) != 0 {
		t.Fatalf("a week with no activity must drop the session: %+v", empty)
	}
}

// A cluster of hundreds of documents cannot be represented by a paragraph that
// cites nine of them. Past the narratable size the cluster is counted by directory
// instead, with a scattered row so 112 one-line rows do not replace one bad
// paragraph.
func TestUnattributedChurnIsCountedByDirectory(t *testing.T) {
	documents := make([]reportDocument, 0)
	add := func(dir string, n int, day string) {
		for i := 0; i < n; i++ {
			documents = append(documents, reportDoc(dir+"/doc"+string(rune('a'+i))+".md", "d", day, false))
		}
	}
	add("/w/miao/knowledge", 8, "2026-07-02")
	add("/w/miao/docs/superpowers/plans", 6, "2026-07-15")
	add("/w/miao/odd/one", 2, "2026-07-20")
	add("/w/miao/odd/two", 2, "2026-07-31")
	corpus := reportCorpus{Documents: documents}

	groups := summarizeUnattributedChurn(&corpus, func() []int {
		all := make([]int, len(documents))
		for i := range documents {
			all[i] = i
		}
		return all
	}())

	if len(groups) != 3 {
		t.Fatalf("groups = %+v, want two directories plus a scattered row", groups)
	}
	if groups[0].Count != 8 || groups[0].Kind != "churn" {
		t.Fatalf("largest group = %+v", groups[0])
	}
	if groups[0].Date != "2026-07-02" {
		t.Fatalf("single-day group date = %q", groups[0].Date)
	}
	scattered := groups[len(groups)-1]
	if scattered.Count != 4 || scattered.Directory != "（分散于多个目录）" {
		t.Fatalf("scattered row = %+v", scattered)
	}
	if scattered.Date != "2026-07-20~2026-07-31" {
		t.Fatalf("scattered date span = %q, want a range", scattered.Date)
	}
}

// The whole point: an oversized unattributed cluster must not become a narrated
// theme, and its documents must still be accounted for.
func TestOversizedUnattributedClusterIsNotNarrated(t *testing.T) {
	documents := make([]reportDocument, 0)
	for i := 0; i < reportNarratableDocuments+10; i++ {
		documents = append(documents, reportDoc("/w/miao/knowledge/doc"+strconvItoa(i)+".md", "散落文档", "2026-07-0"+strconvItoa(i%9+1), false))
	}
	corpus := reportCorpus{Documents: documents, Counts: documentReportCounts{Types: map[string]int{}}}
	attribution := attributeReportCorpus(&corpus, nil)

	themes := buildSessionThemes(&corpus, &attribution)

	for _, theme := range themes {
		if len(theme.DocumentIndexes) > reportNarratableDocuments {
			t.Fatalf("theme %s narrates %d documents, over the limit", theme.ID, len(theme.DocumentIndexes))
		}
	}
	counted := 0
	for _, group := range attribution.BulkImports {
		counted += group.Count
	}
	narrated := 0
	for _, theme := range themes {
		narrated += len(theme.DocumentIndexes)
	}
	if counted+narrated != len(documents) {
		t.Fatalf("counted %d + narrated %d != %d documents", counted, narrated, len(documents))
	}
}
