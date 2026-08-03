package main

import (
	"strings"
	"testing"
)

func effortDigest() reportPeriodDigest {
	return reportPeriodDigest{
		Type: "weekly", Start: "2026-07-27", End: "2026-08-02", Workspace: "",
		Counts: documentReportCounts{Documents: 225, Workspaces: 3, BulkImportDocuments: 189},
		Sessions: []reportDigestSession{
			{ID: "s1", Path: "/s/1.jsonl", Title: "安全鉴权", Workspace: "lz100", ActiveDays: 6, Events: 9262, UserTurns: 120, Documents: 20,
				OpenTodos: []reportDigestOpenTodo{
					{Phase: "验证", Content: "等待硬件回归", Status: "blocked", Blocker: "板子未到"},
					{Content: "补充签名文档", Status: "pending"},
				}},
			{ID: "s2", Path: "/s/2026-07-29T19-01-19-064Z_019faf40.jsonl", Title: "2026-07-29T19-01-19-064Z_019faf40", Workspace: "rx101", ActiveDays: 1, Events: 40, Documents: 0},
		},
		BulkImports: []reportDigestBulkImport{{Directory: "/repo/knowledge/jetson-r39.2", Date: "2026-07-30", Count: 189, Kind: "import"}},
		Themes: []reportThemeDigest{{
			ID: "T1", Label: "安全鉴权", SessionID: "s1",
			Effort:  reportThemeEffort{Workspace: "lz100", ActiveDays: 6, Events: 9262, UserTurns: 120, Subagents: 2},
			Summary: reportEvidenceClaim{Kind: "summary", Text: "完成鉴权链路。", EvidenceIDs: []string{"D1"}},
		}},
		Documents: []reportDigestDocument{{EvidenceID: "D1", Title: "鉴权设计", Type: "design", Workspace: "lz100", ActivityAt: "2026-07-28T10:00:00+08:00"}},
	}
}

// The effort table is the section that answers "where did the time go". A
// document list cannot: one import outweighs six engineering sessions on count.
func TestWeeklyReportLeadsWithEffort(t *testing.T) {
	body := string(renderWeeklyDocumentReport(effortDigest()))

	for _, expected := range []string{
		"## 本周投入",
		"| 安全鉴权 | lz100 | 6 | 9262 | 120 | 20 |",
		"本周有 2 个工作项在推进，累计 9302 次记录事件",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q:\n%s", expected, body)
		}
	}
	effortIndex := strings.Index(body, "## 本周投入")
	themeIndex := strings.Index(body, "## 一、安全鉴权")
	if effortIndex < 0 || themeIndex < 0 || effortIndex > themeIndex {
		t.Fatalf("effort table must precede the themes (effort=%d theme=%d)", effortIndex, themeIndex)
	}
}

// A session with effort but no documents still has to appear, or the report loses
// a whole workstream. An untitled one is named by when it ran - a uuid in a
// report table is noise a reader cannot place.
func TestWeeklyReportKeepsDocumentlessSessionInEffortTable(t *testing.T) {
	body := string(renderWeeklyDocumentReport(effortDigest()))

	if !strings.Contains(body, "| 未命名会话（07-29 19:01） | rx101 | 1 | 40 | 0 | 0 |") {
		t.Fatalf("documentless session missing from the effort table:\n%s", body)
	}
	if strings.Contains(body, "019faf40") {
		t.Fatalf("a transcript uuid leaked into the report:\n%s", body)
	}
}

// The import that used to take six themes is one counted row with its share
// stated, so a reader is not misled by the document total.
func TestWeeklyReportCollapsesBulkImportsWithShare(t *testing.T) {
	body := string(renderWeeklyDocumentReport(effortDigest()))

	for _, expected := range []string{
		"## 无会话归属的文档",
		"### 批量资料导入",
		"| `/repo/knowledge/jetson-r39.2` | 2026-07-30 | 189 |",
		"其中 189 份文档（占 84%）无会话产出记录，已单列计数而不计入工作项，其中 189 份为同日批量导入。",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "## 二、") {
		t.Fatalf("a bulk import must not become a theme:\n%s", body)
	}
}

// A blocker with its recorded reason is the most actionable line in a weekly
// report and the only one documents cannot reconstruct.
func TestWeeklyReportRendersBlockersWithReasons(t *testing.T) {
	body := string(renderWeeklyDocumentReport(effortDigest()))

	for _, expected := range []string{
		"## 未完成与阻塞",
		"**安全鉴权**：验证 / 等待硬件回归 — 阻塞原因：板子未到",
		"安全鉴权：补充签名文档",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q:\n%s", expected, body)
		}
	}
	blocked := strings.Index(body, "阻塞原因：板子未到")
	pending := strings.Index(body, "补充签名文档")
	if blocked > pending {
		t.Fatal("blocked items must lead the section")
	}
}

// Each theme states its weight next to its claims, so a section's size on the
// page is justified by the record rather than by document count.
func TestWeeklyReportStatesPerThemeEffort(t *testing.T) {
	body := string(renderWeeklyDocumentReport(effortDigest()))

	if !strings.Contains(body, "> 投入：6 个活跃日、9262 次记录事件、120 轮对话、2 个子代理；Workspace lz100。") {
		t.Fatalf("theme effort line missing:\n%s", body)
	}
}

// A document nobody's session authored is still real work; the report says how it
// was grouped instead of implying an author.
func TestWeeklyReportMarksUnattributedThemes(t *testing.T) {
	digest := effortDigest()
	digest.Sessions = nil
	digest.BulkImports = nil
	digest.Counts.BulkImportDocuments = 0
	digest.Themes = []reportThemeDigest{{
		ID: "T1", Label: "外部资料", Unattributed: true,
		Summary: reportEvidenceClaim{Kind: "summary", Text: "整理资料。", EvidenceIDs: []string{"D1"}},
	}}

	body := string(renderWeeklyDocumentReport(digest))

	if !strings.Contains(body, "> 本节文档没有对应的会话记录，仅按内容归并。") {
		t.Fatalf("unattributed marker missing:\n%s", body)
	}
	if strings.Contains(body, "## 本周投入") {
		t.Fatalf("no sessions means no effort table:\n%s", body)
	}
}

// Sections are no longer capped at eight, so the numerals must keep counting
// instead of switching to digits partway down the document.
func TestReportThemeOrdinalsStayChineseBeyondEight(t *testing.T) {
	for index, want := range map[int]string{0: "一", 7: "八", 8: "九", 9: "十", 10: "十一", 18: "十九", 19: "二十", 20: "二十一"} {
		if got := reportThemeOrdinal(index); got != want {
			t.Fatalf("ordinal(%d) = %q, want %q", index, got, want)
		}
	}
}

// One reason routinely blocks several tasks. A real report printed the same
// 90-character CMD53 explanation twice under two task names; the reason is the
// information, repeating it is noise.
func TestOpenWorkGroupsTasksSharingABlocker(t *testing.T) {
	sessions := []reportDigestSession{{
		ID: "s1", Path: "/s/1.jsonl", Title: "AIC8800",
		OpenTodos: []reportDigestOpenTodo{
			{Phase: "根因修复", Content: "定位首包超时根因", Status: "blocked", Blocker: "需要板端 A/B 对比"},
			{Phase: "根因修复", Content: "实施最小 WiFi 修复", Status: "blocked", Blocker: "需要板端 A/B 对比"},
			{Content: "补充文档", Status: "pending"},
		},
	}}

	groups := groupReportOpenWork(sessions, 0)

	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want one blocked group plus the pending task", groups)
	}
	if groups[0].Blocker != "需要板端 A/B 对比" || len(groups[0].Tasks) != 2 {
		t.Fatalf("blocked group = %+v, want both tasks under one reason", groups[0])
	}
	if groups[1].Blocker != "" || len(groups[1].Tasks) != 1 {
		t.Fatalf("pending group = %+v", groups[1])
	}

	var out strings.Builder
	renderReportOpenWork(&out, reportPeriodDigest{Sessions: sessions})
	body := out.String()
	if count := strings.Count(body, "需要板端 A/B 对比"); count != 1 {
		t.Fatalf("blocker reason rendered %d times, want once:\n%s", count, body)
	}
	if !strings.Contains(body, "定位首包超时根因；根因修复 / 实施最小 WiFi 修复") {
		t.Fatalf("both tasks must share the line:\n%s", body)
	}
}

// The cap counts groups, so a long tail of tasks cannot push the report into a
// task dump - but a blocked group is never split across the cap.
func TestOpenWorkCapCountsGroups(t *testing.T) {
	sessions := make([]reportDigestSession, 0)
	for i := 0; i < 15; i++ {
		sessions = append(sessions, reportDigestSession{
			ID: "s" + string(rune('a'+i)), Path: "/s/x.jsonl", Title: "会话" + string(rune('a'+i)),
			OpenTodos: []reportDigestOpenTodo{{Content: "任务", Status: "blocked", Blocker: "原因" + string(rune('a'+i))}},
		})
	}

	if groups := groupReportOpenWork(sessions, 10); len(groups) != 10 {
		t.Fatalf("groups = %d, want the cap", len(groups))
	}
	if groups := groupReportOpenWork(sessions, 0); len(groups) != 15 {
		t.Fatalf("groups = %d, want no cap when limit is zero", len(groups))
	}
}

// Two different stories share the counted bucket: a tree that landed in one day,
// and documents that merely changed with no session record. Calling the second an
// import would misdescribe a month of scattered edits.
func TestWeeklyReportSeparatesImportsFromChurn(t *testing.T) {
	digest := effortDigest()
	digest.BulkImports = []reportDigestBulkImport{
		{Directory: "/repo/knowledge/jetson", Date: "2026-07-30", Count: 100, Kind: "import"},
		{Directory: "/repo/docs/superpowers/plans", Date: "2026-07-01~2026-07-21", Count: 60, Kind: "churn"},
	}
	digest.Counts.BulkImportDocuments = 160

	body := string(renderWeeklyDocumentReport(digest))

	for _, expected := range []string{
		"### 批量资料导入",
		"### 期间变更（无会话记录）",
		"| `/repo/docs/superpowers/plans` | 2026-07-01~2026-07-21 | 60 |",
		"其中 100 份为同日批量导入",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q:\n%s", expected, body)
		}
	}
	// The churn row must not be described as an import.
	churnSection := body[strings.Index(body, "### 期间变更"):]
	if strings.Contains(churnSection[:strings.Index(churnSection, "---")], "批量") {
		t.Fatalf("churn section must not call itself an import:\n%s", churnSection)
	}
}

// Sessions run in parallel, so their active days overlap. Summing them produced
// "40 个活跃工作日" inside a 7-day week, which reads like a month-long report.
func TestEffortNoteCountsDistinctDaysNotTheSum(t *testing.T) {
	digest := reportPeriodDigest{
		Counts: documentReportCounts{Documents: 10},
		Sessions: []reportDigestSession{
			{ID: "a", Path: "/s/a.jsonl", Title: "甲", Days: []string{"2026-07-27", "2026-07-28", "2026-07-29"}, ActiveDays: 3, Events: 100},
			{ID: "b", Path: "/s/b.jsonl", Title: "乙", Days: []string{"2026-07-28", "2026-07-29", "2026-07-30"}, ActiveDays: 3, Events: 50},
		},
	}

	var out strings.Builder
	renderReportEffortNote(&out, digest)
	body := out.String()

	if !strings.Contains(body, "覆盖 4 个活跃日") {
		t.Fatalf("want the union of days (4), got:\n%s", body)
	}
	if strings.Contains(body, "6 个") {
		t.Fatalf("day counts must not be summed:\n%s", body)
	}
	if !strings.Contains(body, "累计 150 次记录事件") {
		t.Fatalf("events do sum across sessions:\n%s", body)
	}
}

// A digest written before day lists existed must not report an impossible total.
func TestEffortNoteFallsBackToTheLongestSession(t *testing.T) {
	digest := reportPeriodDigest{
		Counts: documentReportCounts{Documents: 5},
		Sessions: []reportDigestSession{
			{ID: "a", Path: "/s/a.jsonl", Title: "甲", ActiveDays: 6, Events: 100},
			{ID: "b", Path: "/s/b.jsonl", Title: "乙", ActiveDays: 4, Events: 50},
		},
	}

	var out strings.Builder
	renderReportEffortNote(&out, digest)

	if !strings.Contains(out.String(), "覆盖 6 个活跃日") {
		t.Fatalf("want the lower bound (6), got:\n%s", out.String())
	}
}
