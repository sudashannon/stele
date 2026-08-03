package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"stele/wiki"
)

// The skeleton is the effort axis: sessions in effort order, so the report reads
// as what the week cost. This is the whole point of the change - the previous
// ordering was document count, under which one import outranked six sessions.
func TestSessionThemesFollowEffortOrder(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		reportDoc("/repo/small.md", "小工作", "2026-07-28", false),
		reportDoc("/repo/big.md", "大工作", "2026-07-28", false),
	}}
	attribution := attributeReportCorpus(&corpus, []wiki.SessionWorkItem{
		sessionItem("大投入", 9000, []string{"/repo/big.md"}, nil),
		sessionItem("小投入", 30, []string{"/repo/small.md"}, nil),
	})

	themes := buildSessionThemes(&corpus, &attribution)

	if len(themes) != 2 {
		t.Fatalf("themes = %d, want one per session", len(themes))
	}
	if themes[0].Label != "大投入" || themes[1].Label != "小投入" {
		t.Fatalf("themes out of effort order: %s, %s", themes[0].Label, themes[1].Label)
	}
	if themes[0].SessionID == "" || themes[0].Effort.Events != 9000 {
		t.Fatalf("session theme lost its effort: %+v", themes[0])
	}
}

// A session's own title is what the person called the work. Keeping it is what
// makes a section traceable back to the transcript.
func TestSessionThemeKeepsSessionTitle(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{reportDoc("/repo/a.md", "某个文档标题", "2026-07-28", false)}}
	attribution := attributeReportCorpus(&corpus, []wiki.SessionWorkItem{sessionItem("安全鉴权", 100, []string{"/repo/a.md"}, nil)})

	themes := buildSessionThemes(&corpus, &attribution)

	if themes[0].Label != "安全鉴权" {
		t.Fatalf("label = %q, want the session title", themes[0].Label)
	}
}

// Claims must cite documents, never sessions: a transcript is derived data, and
// the report's credibility rests on content hashes of authored prose.
func TestSessionThemeEvidenceIsDocumentsOnly(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{reportDoc("/repo/a.md", "A", "2026-07-28", false)}}
	attribution := attributeReportCorpus(&corpus, []wiki.SessionWorkItem{
		{ID: "s1", Path: "/s/1.jsonl", Title: "会话", Workspace: "rx101", ActiveDays: 1, Events: 10, Produced: []string{"/repo/a.md"}},
	})

	themes := buildSessionThemes(&corpus, &attribution)

	if len(themes[0].EvidenceIDs) != 1 {
		t.Fatalf("evidence = %v", themes[0].EvidenceIDs)
	}
	for _, id := range themes[0].EvidenceIDs {
		if id == "/s/1.jsonl" || id == "s1" {
			t.Fatalf("a session leaked into evidence: %v", themes[0].EvidenceIDs)
		}
	}
	prompt := buildReportThemePrompt(&corpus, themes[0])
	if prompt.Session == nil || prompt.Session.ActiveDays != 1 {
		t.Fatalf("session framing missing from the prompt: %+v", prompt.Session)
	}
	for _, document := range prompt.Documents {
		if document.EvidenceID == "" {
			t.Fatal("prompt documents must keep their evidence ids")
		}
	}
}

// Documents outside any session still cluster and summarize - work written
// without a tracked session is real work, just work without an effort record.
func TestUnattributedDocumentsKeepClustering(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		reportDoc("/repo/one.md", "独立文档一", "2026-07-28", false),
		reportDoc("/repo/two.md", "独立文档二", "2026-07-29", false),
	}}
	attribution := attributeReportCorpus(&corpus, nil)

	themes := buildSessionThemes(&corpus, &attribution)

	if len(themes) == 0 {
		t.Fatal("unattributed documents must still produce themes")
	}
	for _, theme := range themes {
		if !theme.Unattributed || theme.SessionID != "" {
			t.Fatalf("theme should be marked unattributed: %+v", theme)
		}
		for _, index := range theme.DocumentIndexes {
			if index < 0 || index >= len(corpus.Documents) {
				t.Fatalf("document index %d escaped the corpus", index)
			}
		}
	}
}

// Indexes are remapped from the leftover subset back to the corpus. Off-by-one
// here would cite the wrong document, which is the one failure mode a report
// cannot have.
func TestUnattributedThemeIndexesMapBackToCorpus(t *testing.T) {
	corpus := reportCorpus{Documents: []reportDocument{
		reportDoc("/repo/owned.md", "会话产出", "2026-07-28", false),
		reportDoc("/repo/loose.md", "散落文档", "2026-07-29", false),
	}}
	attribution := attributeReportCorpus(&corpus, []wiki.SessionWorkItem{sessionItem("会话", 100, []string{"/repo/owned.md"}, nil)})

	themes := buildSessionThemes(&corpus, &attribution)

	var unattributed *reportTheme
	for index := range themes {
		if themes[index].Unattributed {
			unattributed = &themes[index]
		}
	}
	if unattributed == nil {
		t.Fatal("expected an unattributed theme")
	}
	for _, index := range unattributed.DocumentIndexes {
		if corpus.Documents[index].Path != "/repo/loose.md" {
			t.Fatalf("index %d resolves to %q, want the loose document", index, corpus.Documents[index].Path)
		}
	}
	for _, id := range unattributed.EvidenceIDs {
		if id != corpus.Documents[1].EvidenceID {
			t.Fatalf("evidence %q does not belong to the loose document", id)
		}
	}
}

// A pre-effort weekly digest must not be reused by a monthly rollup. Measured:
// one reused v1 week put 296 documents into a single monthly section, because its
// themes are grouped by document count and still list bulk-import documents as
// evidence. Nothing is lost - the month regenerates that week instead.
func TestMonthlyReuseRefusesPreEffortDigests(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, version int, start, end string) {
		digest := reportPeriodDigest{
			SchemaVersion: version, Type: "weekly", Start: start, End: end,
			DigestID: name, ReportFile: name + ".md",
			Themes:    []reportThemeDigest{{ID: "T1", Label: "旧主题", Summary: reportEvidenceClaim{Kind: "summary", Text: "内容。", EvidenceIDs: []string{"D1"}}}},
			Documents: []reportDigestDocument{{EvidenceID: "D1", SourceID: "/repo/a.md", Path: "/repo/a.md", Title: "A", Type: "design", ActivityAt: start + "T10:00:00+08:00"}},
		}
		payload, err := json.Marshal(digest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".manifest.json"), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("weekly-old", 1, "2026-07-06", "2026-07-12")
	write("weekly-new", reportManifestSchemaVersion, "2026-07-13", "2026-07-19")

	digests, err := loadContainedWeeklyDigests(dir, "2026-07-01", "2026-07-31", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(digests) != 1 || digests[0].DigestID != "weekly-new" {
		t.Fatalf("digests = %+v, want only the current-schema week", digests)
	}

	// The refused week must come back as a period the month regenerates, so its
	// documents still reach the report.
	periods, err := missingReportPeriods("2026-07-06", "2026-07-19", digests)
	if err != nil {
		t.Fatal(err)
	}
	var covered bool
	for _, period := range periods {
		if period.Start <= "2026-07-06" && period.End >= "2026-07-12" {
			covered = true
		}
	}
	if !covered {
		t.Fatalf("the refused week must be regenerated, got periods %+v", periods)
	}
}

// A week with fourteen active sessions is normal; fourteen prose sections is not
// a report anyone reads, and each one costs a model call. Past the cap sessions
// share a section - but the effort table still lists every one of them, so the
// cap bounds the narrative without hiding work.
func TestSessionThemesCapNarratedSectionsWithoutLosingDocuments(t *testing.T) {
	documents := []reportDocument{}
	items := []wiki.SessionWorkItem{}
	for i := 0; i < reportSessionThemeLimit+3; i++ {
		path := "/repo/s" + string(rune('a'+i)) + ".md"
		documents = append(documents, reportDoc(path, "文档"+string(rune('a'+i)), "2026-07-28", false))
		items = append(items, sessionItem("会话"+string(rune('a'+i)), 1000-i, []string{path}, nil))
	}
	corpus := reportCorpus{Documents: documents}
	attribution := attributeReportCorpus(&corpus, items)

	themes := buildSessionThemes(&corpus, &attribution)

	if len(themes) != reportSessionThemeLimit+1 {
		t.Fatalf("themes = %d, want the cap plus one shared section", len(themes))
	}
	shared := themes[len(themes)-1]
	if shared.Label != "其他工作项" || !shared.Independent {
		t.Fatalf("tail section = %+v", shared)
	}
	if len(shared.DocumentIndexes) != 3 {
		t.Fatalf("tail documents = %v, want the three past the cap", shared.DocumentIndexes)
	}
	if shared.Effort.Events == 0 {
		t.Fatal("the shared section must carry the summed effort")
	}
	// Every document still belongs to exactly one section.
	seen := map[int]bool{}
	for _, theme := range themes {
		for _, index := range theme.DocumentIndexes {
			if seen[index] {
				t.Fatalf("document %d appears twice", index)
			}
			seen[index] = true
		}
	}
	if len(seen) != len(documents) {
		t.Fatalf("documents covered = %d, want all %d", len(seen), len(documents))
	}
}

// The catch-all for sessions past the narration cap is named by what it collects.
// Hardcoding "独立事项" for every grouped section previously overwrote it, which
// mislabelled real session work as unattributed.
func TestTailSectionKeepsItsOwnLabel(t *testing.T) {
	documents := []reportDocument{}
	items := []wiki.SessionWorkItem{}
	for i := 0; i < reportSessionThemeLimit+1; i++ {
		path := "/repo/s" + string(rune('a'+i)) + ".md"
		documents = append(documents, reportDoc(path, "文档", "2026-07-28", false))
		items = append(items, sessionItem("会话"+string(rune('a'+i)), 1000-i, []string{path}, nil))
	}
	corpus := reportCorpus{Documents: documents}
	themes := buildSessionThemesFromItems(&corpus, items)
	tail := themes[len(themes)-1]

	digest, err := validateReportThemeResponse(reportThemeResponse{
		Label:   "模型自己起的名字",
		Summary: reportPromptSummary{Text: "概述。", EvidenceIDs: tail.EvidenceIDs[:1]},
	}, tail, &corpus)
	if err != nil {
		t.Fatal(err)
	}
	if digest.Label != "其他工作项" {
		t.Fatalf("label = %q, want the section's own name", digest.Label)
	}
}

// buildSessionThemesFromItems keeps the older tests readable now that the builder
// takes a mutable attribution (it appends counted groups to it).
func buildSessionThemesFromItems(corpus *reportCorpus, items []wiki.SessionWorkItem) []reportTheme {
	attribution := attributeReportCorpus(corpus, items)
	return buildSessionThemes(corpus, &attribution)
}
