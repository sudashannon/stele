package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// monthlyRenderFixture builds a monthly digest with deliberate repetition and
// enough sessions to exercise the aggregation and edge cases.
func monthlyRenderFixture() reportPeriodDigest {
	// 14 sessions — enough to trigger the 12-row cap.
	sessions := make([]reportDigestSession, 14)
	for i := range 14 {
		sessions[i] = reportDigestSession{
			ID:        fmt.Sprintf("s%d", i+1),
			Path:      fmt.Sprintf("/s/%d.jsonl", i+1),
			Title:     fmt.Sprintf("WorkItem-%d", i+1),
			Workspace: "ws",
			// Highest-effort session.
			Events:     (14 - i) * 100,
			ActiveDays: 14 - i,
			Documents:  i,
		}
	}
	// Session 7 is untitled (will render as 未命名会话).
	sessions[6].Title = ""
	sessions[6].Path = "/s/2026-07-29T19-01-19-064Z_019faf40.jsonl"
	// Session 14 has a blocked todo.
	sessions[13].OpenTodos = []reportDigestOpenTodo{
		{Phase: "验证", Content: "等待硬件回归", Status: "blocked", Blocker: "板子未到"},
		{Content: "补充签名文档", Status: "pending"},
	}
	// Session 13 has 0 documents and an unblocked todo.
	sessions[12].Documents = 0
	sessions[12].OpenTodos = []reportDigestOpenTodo{
		{Content: "编写 README", Status: "pending"},
	}

	// Claim text that repeats across themes — must dedupe.
	repeatedText := "完成了核心鉴权模块的开发和测试"
	uniqueText := "优化了内存占用，减少 30% 峰值"

	return reportPeriodDigest{
		SchemaVersion: 2,
		Type:          "monthly",
		Start:         "2026-07-01",
		End:           "2026-07-31",
		Workspace:     "",
		Sessions:      sessions,
		BulkImports: []reportDigestBulkImport{
			{Directory: "/repo/knowledge/jetson-r39.2", Date: "2026-07-15", Count: 189},
		},
		Themes: []reportThemeDigest{
			// Theme A: highest effort, has the repeated text plus enough
			// next-kind claims to survive Theme Items (max 6) and reach Focus.
			{
				ID:        "T1",
				Label:     "安全鉴权",
				SessionID: "s1",
				Effort:    reportThemeEffort{Workspace: "ws", ActiveDays: 14, Events: 1400, UserTurns: 100, Subagents: 3},
				Summary:   reportEvidenceClaim{Kind: "outcome", Text: repeatedText, EvidenceIDs: []string{"D1", "D2"}},
				Claims: []reportEvidenceClaim{
					{Kind: "outcome", Text: repeatedText, EvidenceIDs: []string{"D1"}, Date: "2026-07-05"},
					{Kind: "progress", Text: uniqueText, EvidenceIDs: []string{"D2"}},
					{Kind: "outcome", Text: "通过了全部 142 个测试用例", EvidenceIDs: []string{"D3"}, Date: "2026-07-10"},
					{Kind: "next", Text: "鉴权模块后续计划：集成性能压测", EvidenceIDs: []string{"D100"}},
					{Kind: "next", Text: "鉴权模块后续计划：扩展 OAuth2 支持", EvidenceIDs: []string{"D101"}},
					{Kind: "next", Text: "鉴权模块后续计划：补充安全审计文档", EvidenceIDs: []string{"D102"}},
					{Kind: "next", Text: "鉴权模块后续计划：实现 RBAC 权限模型", EvidenceIDs: []string{"D103"}},
					{Kind: "next", Text: "鉴权模块后续计划：新增 LDAP 集成", EvidenceIDs: []string{"D104"}},
					{Kind: "next", Text: "鉴权模块后续计划：优化 JWT 刷新策略", EvidenceIDs: []string{"D105"}},
					{Kind: "next", Text: "鉴权模块后续计划：完成全链路 tracing", EvidenceIDs: []string{"D106"}},
				},
				EvidenceIDs:       []string{"D1", "D2", "D3"},
				RepresentativeIDs: []string{"D1"},
			},
			// Theme B: medium effort, fewer documents, also has next-kind claims.
			{
				ID:        "T2",
				Label:     "模型优化",
				SessionID: "s2",
				Effort:    reportThemeEffort{Workspace: "ws", ActiveDays: 10, Events: 500, UserTurns: 80, Subagents: 1},
				Summary:   reportEvidenceClaim{Kind: "outcome", Text: "优化推理延迟至 5ms 以内", EvidenceIDs: []string{"D4", "D5", "D6", "D7"}},
				Claims: []reportEvidenceClaim{
					{Kind: "outcome", Text: "优化推理延迟至 5ms 以内", EvidenceIDs: []string{"D4"}, Date: "2026-07-12"},
					{Kind: "progress", Text: "完成了核心鉴权模块的开发和测试", EvidenceIDs: []string{"D5"}}, // same text as T1, should dedupe
					{Kind: "next", Text: "模型优化后续计划：尝试 ONNX 导出", EvidenceIDs: []string{"D200"}},
					{Kind: "next", Text: "模型优化后续计划：评估 TensorRT 加速", EvidenceIDs: []string{"D201"}},
					{Kind: "next", Text: "模型优化后续计划：对比 INT8 量化精度", EvidenceIDs: []string{"D202"}},
					{Kind: "next", Text: "模型优化后续计划：分析 bandwidth 瓶颈", EvidenceIDs: []string{"D203"}},
					{Kind: "next", Text: "模型优化后续计划：优化内存分配策略", EvidenceIDs: []string{"D204"}},
					{Kind: "next", Text: "模型优化后续计划：添加 CUDA Graph 支持", EvidenceIDs: []string{"D205"}},
					{Kind: "next", Text: "模型优化后续计划：实现动态 batch", EvidenceIDs: []string{"D206"}},
				},
				EvidenceIDs:       []string{"D4", "D5", "D6", "D7"},
				RepresentativeIDs: []string{"D4"},
			},
			// Theme C: unattributed, many documents, low effort.
			{
				ID:      "T3",
				Label:   "资料归档",
				Summary: reportEvidenceClaim{Kind: "background", Text: "历史资料批量导入", EvidenceIDs: []string{"D8", "D9", "D10", "D11", "D12", "D13", "D14", "D15", "D16", "D17"}},
				Claims: []reportEvidenceClaim{
					{Kind: "background", Text: "历史资料批量导入", EvidenceIDs: []string{"D8"}},
					{Kind: "background", Text: "完成了核心鉴权模块的开发和测试", EvidenceIDs: []string{"D9"}}, // deduped
				},
				EvidenceIDs:       []string{"D8", "D9", "D10", "D11", "D12", "D13", "D14", "D15", "D16", "D17"},
				RepresentativeIDs: []string{"D8"},
				Unattributed:      true,
			},
		},
		Documents: []reportDigestDocument{
			{EvidenceID: "D1", Title: "鉴权设计", Type: "design", Workspace: "ws", ActivityAt: "2026-07-05T10:00:00+08:00"},
			{EvidenceID: "D2", Title: "内存优化方案", Type: "design", Workspace: "ws", ActivityAt: "2026-07-08T10:00:00+08:00"},
			{EvidenceID: "D3", Title: "测试报告", Type: "report", Workspace: "ws", ActivityAt: "2026-07-10T10:00:00+08:00"},
			{EvidenceID: "D4", Title: "推理优化", Type: "design", Workspace: "ws", ActivityAt: "2026-07-12T10:00:00+08:00"},
			{EvidenceID: "D5", Title: "鉴权验证", Type: "report", Workspace: "ws", ActivityAt: "2026-07-15T10:00:00+08:00"},
			{EvidenceID: "D6", Title: "延迟基准", Type: "report", Workspace: "ws", ActivityAt: "2026-07-18T10:00:00+08:00"},
			{EvidenceID: "D7", Title: "性能对比", Type: "report", Workspace: "ws", ActivityAt: "2026-07-20T10:00:00+08:00"},
			{EvidenceID: "D8", Title: "归档 1", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-01T10:00:00+08:00"},
			{EvidenceID: "D9", Title: "归档 2", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-02T10:00:00+08:00"},
			{EvidenceID: "D10", Title: "归档 3", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-03T10:00:00+08:00"},
			{EvidenceID: "D11", Title: "归档 4", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-04T10:00:00+08:00"},
			{EvidenceID: "D12", Title: "归档 5", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-05T10:00:00+08:00"},
			{EvidenceID: "D13", Title: "归档 6", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-06T10:00:00+08:00"},
			{EvidenceID: "D14", Title: "归档 7", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-07T10:00:00+08:00"},
			{EvidenceID: "D15", Title: "归档 8", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-08T10:00:00+08:00"},
			{EvidenceID: "D16", Title: "归档 9", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-09T10:00:00+08:00"},
			{EvidenceID: "D17", Title: "归档 10", Type: "doc", Workspace: "ws", ActivityAt: "2026-07-10T10:00:00+08:00"},
		},
		Counts: documentReportCounts{
			Documents:           17,
			WorkItems:           3,
			Themes:              3,
			Workspaces:          1,
			Reports:             5,
			Sessions:            14,
			BulkImportDocuments: 0, // deliberately 0 for the full-data fixture
		},
	}
}

func TestMonthlyMainlineDiffersFromOverview(t *testing.T) {
	digest := monthlyRenderFixture()
	jsonData := monthlyJSONFromDigest(digest)
	raw, err := json.Marshal(jsonData)
	if err != nil {
		t.Fatal(err)
	}
	html, err := renderMonthlyFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := string(html)

	// Mainline and Overview must be different.
	if jsonData.Mainline == jsonData.Overview {
		t.Fatalf("Mainline and Overview must differ; both are %q", jsonData.Mainline)
	}
	// Neither contains the other as a substring.
	if jsonData.Overview != "" && strings.Contains(jsonData.Overview, jsonData.Mainline) {
		t.Fatalf("Overview must not contain Mainline:\nOverview: %s\nMainline: %s", jsonData.Overview, jsonData.Mainline)
	}
	// Mainline should contain effort info, not the repeated claim text.
	if strings.Contains(jsonData.Mainline, "核心鉴权模块") {
		t.Fatalf("Mainline must not contain claim text; got %q", jsonData.Mainline)
	}
	_ = body
}

func TestMonthlySectionsAreNotReprojectionsOfEachOther(t *testing.T) {
	digest := monthlyRenderFixture()
	jsonData := monthlyJSONFromDigest(digest)

	// The defect this layout replaced: 主线 copied 概述, theme cards copied their
	// own summary into 关键成果, and 重点主题 copied both - one sentence rendered
	// four times. Each section now draws from a disjoint source: cards carry
	// summaries, focus carries the top work items' claims, milestones carry the
	// remaining dated claims.
	for _, card := range jsonData.ThemesDetail {
		if len(card.Items) != 1 {
			t.Fatalf("a theme card carries exactly its summary, got %d items", len(card.Items))
		}
		for _, project := range jsonData.FocusProjects {
			for _, point := range project.Points {
				if point == card.Items[0] {
					t.Fatalf("focus repeats a theme card's summary: %q", point)
				}
			}
		}
		for _, milestone := range jsonData.Milestones {
			if milestone.Text == card.Items[0] {
				t.Fatalf("milestone repeats a theme card's summary: %q", milestone.Text)
			}
		}
	}
	// Within a section nothing repeats.
	for _, project := range jsonData.FocusProjects {
		seen := map[string]bool{}
		for _, point := range project.Points {
			if seen[point] {
				t.Fatalf("focus project %q repeats a point", project.Name)
			}
			seen[point] = true
		}
	}
	seen := map[string]bool{}
	for _, milestone := range jsonData.Milestones {
		if seen[milestone.Text] {
			t.Fatalf("milestones repeat an entry: %q", milestone.Text)
		}
		seen[milestone.Text] = true
	}
}

// A heading with nothing under it is what the reader saw when 重点主题 and
// 关键成果 were starved. The invariant is not "every section has data" - a month
// may genuinely have no milestones - it is that a section without data does not
// render its heading at all.
func TestMonthlyRendersNoEmptyHeadings(t *testing.T) {
	check := func(name string, digest reportPeriodDigest) {
		payload, err := json.Marshal(monthlyJSONFromDigest(digest))
		if err != nil {
			t.Fatal(err)
		}
		body, err := renderMonthlyFromJSON(payload)
		if err != nil {
			t.Fatal(err)
		}
		html := string(body)
		for index := strings.Index(html, "<h2>"); index >= 0; {
			close := strings.Index(html[index:], "</section>")
			if close < 0 {
				t.Fatalf("%s: <h2> at %d is not inside a section", name, index)
			}
			block := html[index : index+close]
			after := block[strings.Index(block, "</h2>")+len("</h2>"):]
			stripped := strings.TrimSpace(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(after, ""))
			if stripped == "" {
				heading := block[len("<h2>"):strings.Index(block, "</h2>")]
				t.Fatalf("%s: section %q rendered a heading with no content", name, heading)
			}
			next := strings.Index(html[index+close:], "<h2>")
			if next < 0 {
				break
			}
			index = index + close + next
		}
	}

	check("full fixture", monthlyRenderFixture())

	// A month whose only work items are all focus themes has no milestones left.
	sparse := monthlyRenderFixture()
	sparse.Themes = sparse.Themes[:1]
	check("single theme", sparse)

	// And a digest with no themes at all.
	empty := monthlyRenderFixture()
	empty.Themes = nil
	check("no themes", empty)
}

func TestMonthlyEffortTableOrderAndAggregation(t *testing.T) {
	digest := monthlyRenderFixture()
	jsonData := monthlyJSONFromDigest(digest)
	// SessionsHTML is pre-rendered; verify it directly.
	sessionsHTML := jsonData.SessionsHTML

	if sessionsHTML == "" {
		t.Fatal("SessionsHTML must not be empty when Sessions are present")
	}
	// Highest-effort session (s1, 1400 events) must appear before s2 (1300).
	firstIdx := strings.Index(sessionsHTML, "WorkItem-1")
	secondIdx := strings.Index(sessionsHTML, "WorkItem-2")
	if firstIdx < 0 || secondIdx < 0 || firstIdx > secondIdx {
		t.Fatalf("effort table must sort by Events desc; WorkItem-1 before WorkItem-2:\n%s", sessionsHTML)
	}
	// We have 14 sessions > 12 cap; must see an aggregate row.
	if !strings.Contains(sessionsHTML, "其余 2 个工作项") {
		t.Fatalf("effort table must aggregate remaining sessions:\n%s", sessionsHTML)
	}
	// The aggregate row must appear after the 12th session.
	aggIdx := strings.Index(sessionsHTML, "其余 2 个工作项")
	lastSessionIdx := strings.Index(sessionsHTML, "WorkItem-12")
	if aggIdx < lastSessionIdx {
		t.Fatalf("aggregate row must appear after the last explicit session:\n%s", sessionsHTML)
	}
}

func TestMonthlyBlockedTodoAndBulkImport(t *testing.T) {
	digest := monthlyRenderFixture()
	jsonData := monthlyJSONFromDigest(digest)
	openWorkHTML := jsonData.OpenWorkHTML

	if openWorkHTML == "" {
		t.Fatal("OpenWorkHTML must not be empty when todos and bulk imports are present")
	}
	// Blocked todo renders with its reason.
	if !strings.Contains(openWorkHTML, "等待硬件回归") {
		t.Fatalf("blocked todo content missing:\n%s", openWorkHTML)
	}
	if !strings.Contains(openWorkHTML, "板子未到") {
		t.Fatalf("blocked todo blocker reason missing:\n%s", openWorkHTML)
	}
	// Bulk import renders its count and directory.
	if !strings.Contains(openWorkHTML, "/repo/knowledge/jetson-r39.2") {
		t.Fatalf("bulk import directory missing:\n%s", openWorkHTML)
	}
	if !strings.Contains(openWorkHTML, "189") {
		t.Fatalf("bulk import count missing:\n%s", openWorkHTML)
	}
}

func TestMonthlyAbsentSectionsWhenEmpty(t *testing.T) {
	// Build a digest with no Sessions and no BulkImports.
	digest := reportPeriodDigest{
		SchemaVersion: 2,
		Type:          "monthly",
		Start:         "2026-07-01",
		End:           "2026-07-31",
		Themes: []reportThemeDigest{{
			ID: "T1", Label: "单主题",
			Summary:           reportEvidenceClaim{Kind: "outcome", Text: "完成", EvidenceIDs: []string{"D1"}},
			EvidenceIDs:       []string{"D1"},
			RepresentativeIDs: []string{"D1"},
		}},
		Documents: []reportDigestDocument{
			{EvidenceID: "D1", Title: "文档", Type: "design", Workspace: "ws", ActivityAt: "2026-07-01T10:00:00+08:00"},
		},
		Counts: documentReportCounts{
			Documents:  1,
			WorkItems:  1,
			Themes:     1,
			Workspaces: 1,
			Reports:    0,
		},
	}
	jsonData := monthlyJSONFromDigest(digest)
	raw, err := json.Marshal(jsonData)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderMonthlyFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := string(rendered)

	// Neither new section heading appears.
	for _, heading := range []string{"投入概览", "待办与未归入"} {
		if strings.Contains(body, heading) {
			t.Fatalf("section %q must not appear when data is absent:\n%s", heading, body)
		}
	}
	// SessionsHTML and OpenWorkHTML must be empty strings.
	if jsonData.SessionsHTML != "" {
		t.Fatalf("SessionsHTML must be empty when no sessions: %q", jsonData.SessionsHTML)
	}
	if jsonData.OpenWorkHTML != "" {
		t.Fatalf("OpenWorkHTML must be empty when no todos/imports: %q", jsonData.OpenWorkHTML)
	}
}

func TestMonthlyFocusProjectsRankByEffort(t *testing.T) {
	digest := monthlyRenderFixture()
	jsonData := monthlyJSONFromDigest(digest)

	// T3 (资料归档) has 10 documents — more than T1 (3) and T2 (4) — but
	// 0 effort events. FocusProjects must pick the two highest-effort themes
	// (T1: 1400, T2: 500), not the highest-document-count theme.
	if len(jsonData.FocusProjects) != 2 {
		t.Fatalf("expected 2 focus projects, got %d", len(jsonData.FocusProjects))
	}
	if jsonData.FocusProjects[0].Name != "安全鉴权" {
		t.Fatalf("first focus project must be highest-effort theme (安全鉴权), got %q", jsonData.FocusProjects[0].Name)
	}
	if jsonData.FocusProjects[1].Name != "模型优化" {
		t.Fatalf("second focus project must be second-highest-effort theme (模型优化), got %q", jsonData.FocusProjects[1].Name)
	}
	// T3 (资料归档) must not appear in FocusProjects.
	for _, fp := range jsonData.FocusProjects {
		if fp.Name == "资料归档" {
			t.Fatalf("unattributed / zero-effort theme must not be a focus project")
		}
	}
}

// The monthly page rendered completely blank in the panel: the template's <style>
// block lost its closing tag, so a browser parsed the entire document as CSS text.
// Reading the tag-stripped text hid it - the content was all there, just never
// rendered. This test parses the output the way a browser would.
func TestMonthlyHTMLIsStructurallyRenderable(t *testing.T) {
	digest := reportPeriodDigest{
		Type: "monthly", Start: "2026-07-01", End: "2026-07-31",
		Counts: documentReportCounts{Documents: 100, WorkItems: 2, Workspaces: 1, Reports: 3, Sessions: 2},
		Sessions: []reportDigestSession{
			{ID: "s1", Path: "/s/1.jsonl", Title: "甲", Days: []string{"2026-07-06"}, ActiveDays: 1, Events: 900, Documents: 2,
				OpenTodos: []reportDigestOpenTodo{{Content: "待办一", Status: "blocked", Blocker: "等硬件"}}},
			{ID: "s2", Path: "/s/2.jsonl", Title: "乙", Days: []string{"2026-07-07"}, ActiveDays: 1, Events: 100, Documents: 1},
		},
		BulkImports: []reportDigestBulkImport{{Directory: "/repo/tree", Date: "2026-07-30", Count: 50, Kind: "import"}},
		Themes: []reportThemeDigest{{
			ID: "T1", Label: "甲", SessionID: "s1", Effort: reportThemeEffort{ActiveDays: 1, Events: 900},
			Summary:     reportEvidenceClaim{Kind: "summary", Text: "本月完成甲。", EvidenceIDs: []string{"D1"}},
			Claims:      []reportEvidenceClaim{{Kind: "outcome", Text: "甲的成果。", EvidenceIDs: []string{"D1"}, Date: "2026-07-06"}},
			EvidenceIDs: []string{"D1"},
		}},
		Documents: []reportDigestDocument{{EvidenceID: "D1", Title: "文档甲", Type: "design", Workspace: "miao", ActivityAt: "2026-07-06T10:00:00+08:00"}},
	}

	payload, err := json.Marshal(monthlyJSONFromDigest(digest))
	if err != nil {
		t.Fatal(err)
	}
	body, err := renderMonthlyFromJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	// Every element the template opens must close, or the browser swallows the page.
	for _, tag := range []string{"html", "head", "style", "body", "section", "div", "table", "ul"} {
		open := strings.Count(html, "<"+tag+">") + strings.Count(html, "<"+tag+" ")
		closed := strings.Count(html, "</"+tag+">")
		if open != closed {
			t.Fatalf("<%s>: %d opened, %d closed", tag, open, closed)
		}
	}
	// The style block must terminate before the body, otherwise everything after it
	// is CSS and the page shows nothing.
	styleEnd := strings.Index(html, "</style>")
	bodyStart := strings.Index(html, "<body>")
	if styleEnd < 0 || bodyStart < 0 || styleEnd > bodyStart {
		t.Fatalf("style block must close before <body> (styleEnd=%d bodyStart=%d)", styleEnd, bodyStart)
	}
	// There must be visible text outside <style>, which is what "renders blank"
	// actually means.
	visible := html[bodyStart:]
	for _, expected := range []string{"本月完成甲", "甲的成果", "投入概览", "2026-07-01"} {
		if !strings.Contains(visible, expected) {
			t.Fatalf("body is missing %q", expected)
		}
	}
	// No placeholder may survive into the output.
	if index := strings.Index(html, "{{"); index >= 0 {
		t.Fatalf("unreplaced placeholder at %d: %s", index, html[index:index+24])
	}
}
