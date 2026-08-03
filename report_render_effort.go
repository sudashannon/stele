package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// The report's deterministic sections. Nothing here goes through the model:
// effort, blockers and import counts are measured facts, and a summary of a
// measurement is a worse version of the measurement.

// renderReportEffortTable writes the week ordered by what it cost. This is the
// section that answers "where did the time go", which a document list cannot: one
// import can outweigh six engineering sessions on document count alone.
func renderReportEffortTable(out *strings.Builder, digest reportPeriodDigest) {
	if len(digest.Sessions) == 0 {
		return
	}
	out.WriteString("## 本周投入\n\n")
	out.WriteString("| 工作项 | Workspace | 活跃天 | 记录事件 | 用户轮次 | 产出文档 |\n")
	out.WriteString("|---|---|---|---:|---:|---:|\n")
	for _, session := range digest.Sessions {
		fmt.Fprintf(out, "| %s | %s | %d | %d | %d | %d |\n",
			escapeMarkdownCell(reportSessionTitle(session)),
			escapeMarkdownCell(session.Workspace),
			session.ActiveDays, session.Events, session.UserTurns, session.Documents)
	}
	out.WriteString("\n> 投入口径：会话记录中落在本周的用户轮次与工具调用次数；活跃天为有记录的日期数，非连续天数。\n\n")
}

// renderReportEffortNote frames the overview with the effort split, and says so
// plainly when most of the corpus is an import rather than authored work.
func renderReportEffortNote(out *strings.Builder, digest reportPeriodDigest) {
	if len(digest.Sessions) == 0 && digest.Counts.BulkImportDocuments == 0 {
		return
	}
	events := 0
	for _, session := range digest.Sessions {
		events += session.Events
	}
	if len(digest.Sessions) > 0 {
		fmt.Fprintf(out, "本周有 %d 个工作项在推进，累计 %d 次记录事件，覆盖 %d 个活跃日。\n\n",
			len(digest.Sessions), events, reportActiveDayCount(digest.Sessions))
	}
	if digest.Counts.BulkImportDocuments > 0 {
		share := 0
		if digest.Counts.Documents > 0 {
			share = digest.Counts.BulkImportDocuments * 100 / digest.Counts.Documents
		}
		imported := 0
		for _, group := range digest.BulkImports {
			if group.Kind != "churn" {
				imported += group.Count
			}
		}
		// Same distinction as the section below: an import that landed in one day is
		// not the same story as documents that merely changed with no session record.
		fmt.Fprintf(out, "> 其中 %d 份文档（占 %d%%）无会话产出记录，已单列计数而不计入工作项",
			digest.Counts.BulkImportDocuments, share)
		if imported > 0 {
			fmt.Fprintf(out, "，其中 %d 份为同日批量导入", imported)
		}
		out.WriteString("。\n\n")
	}
}

// renderReportSessionEffort writes one theme's effort line, so a section's weight
// is visible next to its claims.
func renderReportSessionEffort(out *strings.Builder, theme reportThemeDigest) {
	if theme.SessionID == "" {
		if theme.Unattributed {
			out.WriteString("> 本节文档没有对应的会话记录，仅按内容归并。\n\n")
		}
		return
	}
	fmt.Fprintf(out, "> 投入：%d 个活跃日、%d 次记录事件", theme.Effort.ActiveDays, theme.Effort.Events)
	if theme.Effort.UserTurns > 0 {
		fmt.Fprintf(out, "、%d 轮对话", theme.Effort.UserTurns)
	}
	if theme.Effort.Subagents > 0 {
		fmt.Fprintf(out, "、%d 个子代理", theme.Effort.Subagents)
	}
	if theme.Effort.Workspace != "" {
		fmt.Fprintf(out, "；Workspace %s", escapeMarkdownCell(theme.Effort.Workspace))
	}
	out.WriteString("。\n\n")
}

// renderReportBulkImports lists the documents no session authored. They get a
// count and a path, not a summary: a mirror of someone else's documentation has no
// achievements to report, and a month of scattered edits with no session record is
// inventory, not a story. Rendering either as themes with claims is exactly the
// distortion this section exists to remove.
func renderReportBulkImports(out *strings.Builder, digest reportPeriodDigest) {
	if len(digest.BulkImports) == 0 {
		return
	}
	imports := make([]reportDigestBulkImport, 0, len(digest.BulkImports))
	churn := make([]reportDigestBulkImport, 0, len(digest.BulkImports))
	for _, group := range digest.BulkImports {
		if group.Kind == "churn" {
			churn = append(churn, group)
			continue
		}
		imports = append(imports, group)
	}
	out.WriteString("## 无会话归属的文档\n\n")
	if len(imports) > 0 {
		out.WriteString("### 批量资料导入\n\n| 目录 | 日期 | 文档数 |\n|---|---|---:|\n")
		for _, group := range imports {
			fmt.Fprintf(out, "| `%s` | %s | %d |\n", escapeMarkdownCell(group.Directory), group.Date, group.Count)
		}
		out.WriteString("\n> 判定口径：同一目录子树、同一天入库，且没有任何会话的写入或编辑记录。\n\n")
	}
	if len(churn) > 0 {
		out.WriteString("### 期间变更（无会话记录）\n\n| 目录 | 日期跨度 | 文档数 |\n|---|---|---:|\n")
		for _, group := range churn {
			fmt.Fprintf(out, "| `%s` | %s | %d |\n", escapeMarkdownCell(group.Directory), group.Date, group.Count)
		}
		out.WriteString("\n> 这些文档在本期有更新但没有会话产出记录，数量已超过一节散文能代表的范围，因此只计数、不生成结论。\n\n")
	}
	out.WriteString("---\n\n")
}

// reportOpenWorkGroup is one blocker (or the unblocked remainder) of a work item,
// with every task it holds up.
//
// Grouping matters because one reason routinely blocks several tasks: a real
// report printed the same 90-character CMD53 A/B explanation twice in a row under
// two different task names. The reason is the information; repeating it is noise.
type reportOpenWorkGroup struct {
	Session string
	Blocker string
	Tasks   []string
}

// groupReportOpenWork collects unfinished work, blocked groups first, capped at
// `limit` groups so a report does not turn into a task dump.
func groupReportOpenWork(sessions []reportDigestSession, limit int) []reportOpenWorkGroup {
	type key struct{ session, blocker string }
	index := make(map[key]int)
	blocked := make([]reportOpenWorkGroup, 0)
	pending := make([]reportOpenWorkGroup, 0)
	for _, session := range sessions {
		name := reportSessionTitle(session)
		for _, todo := range session.OpenTodos {
			task := reportTodoLine(todo)
			if todo.Blocker == "" {
				pending = append(pending, reportOpenWorkGroup{Session: name, Tasks: []string{task}})
				continue
			}
			at, ok := index[key{name, todo.Blocker}]
			if !ok {
				index[key{name, todo.Blocker}] = len(blocked)
				blocked = append(blocked, reportOpenWorkGroup{Session: name, Blocker: todo.Blocker, Tasks: []string{task}})
				continue
			}
			blocked[at].Tasks = append(blocked[at].Tasks, task)
		}
	}
	sort.SliceStable(blocked, func(i, j int) bool { return blocked[i].Session < blocked[j].Session })
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].Session < pending[j].Session })
	groups := append(blocked, pending...)
	if limit > 0 && len(groups) > limit {
		groups = groups[:limit]
	}
	return groups
}

// renderReportOpenWork writes the unfinished tasks the sessions recorded. A
// blocker with its reason is the most actionable line in a weekly report and the
// only one that cannot be reconstructed from documents.
func renderReportOpenWork(out *strings.Builder, digest reportPeriodDigest) {
	groups := groupReportOpenWork(digest.Sessions, 0)
	if len(groups) == 0 {
		return
	}
	out.WriteString("## 未完成与阻塞\n\n")
	for _, group := range groups {
		tasks := escapeMarkdownCell(strings.Join(group.Tasks, "；"))
		if group.Blocker == "" {
			fmt.Fprintf(out, "- %s：%s\n", escapeMarkdownCell(group.Session), tasks)
			continue
		}
		fmt.Fprintf(out, "- **%s**：%s — 阻塞原因：%s\n",
			escapeMarkdownCell(group.Session), tasks, escapeMarkdownCell(group.Blocker))
	}
	out.WriteString("\n> 来源：会话自身的任务记录，未经模型改写。\n\n---\n\n")
}

// reportTodoLine renders a task with the phase that gives it context.
func reportTodoLine(todo reportDigestOpenTodo) string {
	if todo.Phase == "" {
		return todo.Content
	}
	return todo.Phase + " / " + todo.Content
}

// reportSessionTitle names a session for a report row. An untitled session falls
// back to when it ran rather than to its transcript filename: a uuid in a report
// table is noise, and the timestamp is the only part a reader can place.
func reportSessionTitle(session reportDigestSession) string {
	title := strings.TrimSpace(session.Title)
	// The session index fills Title with the transcript's own name when a session
	// never got one, so an untitled session arrives here looking titled. Compare
	// against the file name to tell the two apart.
	untitled := title == "" || title == strings.TrimSuffix(filepath.Base(session.Path), ".jsonl")
	if !untitled {
		return title
	}
	if stamp := reportSessionStamp(session.Path); stamp != "" {
		return "未命名会话（" + stamp + "）"
	}
	return "未命名会话"
}

// reportSessionStamp pulls "07-29 19:01" out of a transcript name shaped like
// 2026-07-29T19-01-19-064Z_<uuid>.jsonl.
func reportSessionStamp(path string) string {
	name := filepath.Base(path)
	if len(name) < 16 || name[4] != '-' || name[7] != '-' || name[10] != 'T' {
		return ""
	}
	return name[5:7] + "-" + name[8:10] + " " + name[11:13] + ":" + name[14:16]
}
