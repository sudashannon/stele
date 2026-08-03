package sessions

import (
	"encoding/json"
	"fmt"
	"testing"
)

// todoLine renders one tracker tool call.
func todoLine(ts string, args map[string]any) string {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"type":"message","timestamp":%q,"message":{"role":"assistant","content":[{"type":"toolCall","name":"todo","intent":"跟踪","arguments":%s}]}}`, ts, raw)
}

func phased(phase string, items ...string) map[string]any {
	return map[string]any{"phase": phase, "items": items}
}

func TestTodoReplayBuildsThePhasedListWithStatuses(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "带清单的会话", "2026-07-30T01:00:00.000Z"),
		todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "list": []any{
			phased("Core", "写解析器", "接上索引"),
			phased("Verify", "跑门禁"),
		}}),
		todoLine("2026-07-30T01:00:02.000Z", map[string]any{"op": "done", "task": "写解析器"}),
		todoLine("2026-07-30T01:00:03.000Z", map[string]any{"op": "start", "task": "接上索引"}),
		todoLine("2026-07-30T01:00:04.000Z", map[string]any{"op": "block", "task": "跑门禁", "reason": "等构建"}),
		todoLine("2026-07-30T01:00:05.000Z", map[string]any{"op": "view"}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	want := []TodoItem{
		{Phase: "Core", Content: "写解析器", Status: TodoCompleted},
		{Phase: "Core", Content: "接上索引", Status: TodoInProgress},
		// A block carries its reason; "blocked" on its own is not actionable.
		{Phase: "Verify", Content: "跑门禁", Status: TodoBlocked, Blocker: "等构建"},
	}
	if len(digest.Todos) != len(want) {
		t.Fatalf("Todos = %+v, want %d items", digest.Todos, len(want))
	}
	for i := range want {
		if digest.Todos[i] != want[i] {
			t.Fatalf("Todos[%d] = %+v, want %+v", i, digest.Todos[i], want[i])
		}
	}
	if digest.TodoReplans != 0 {
		t.Fatalf("TodoReplans = %d, want 0", digest.TodoReplans)
	}
	// The tracker itself is a tool call like any other.
	if digest.ToolCalls["todo"] != 5 {
		t.Fatalf("todo tool calls = %d, want 5", digest.ToolCalls["todo"])
	}
}

// The blocker text is the only actionable part of a stuck task, and it must not
// outlive the block: a task that got unblocked and finished is not blocked by
// anything.
func TestTodoReplayKeepsBlockerReasonUntilTheTaskMovesOn(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "阻塞原因", "2026-07-30T01:00:00.000Z"),
		todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "items": []string{"等外部输入", "等评审"}}),
		todoLine("2026-07-30T01:00:02.000Z", map[string]any{"op": "block", "task": "等外部输入", "reason": "飞书文档需要登录"}),
		todoLine("2026-07-30T01:00:03.000Z", map[string]any{"op": "block", "task": "等评审", "reason": "需 SI 复核"}),
		todoLine("2026-07-30T01:00:04.000Z", map[string]any{"op": "unblock", "task": "等外部输入"}),
		todoLine("2026-07-30T01:00:05.000Z", map[string]any{"op": "done", "task": "等外部输入"}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	byContent := map[string]TodoItem{}
	for _, item := range digest.Todos {
		byContent[item.Content] = item
	}
	if got := byContent["等评审"]; got.Status != TodoBlocked || got.Blocker != "需 SI 复核" {
		t.Fatalf("blocked task = %+v, want the reason recorded", got)
	}
	if got := byContent["等外部输入"]; got.Status != TodoCompleted || got.Blocker != "" {
		t.Fatalf("finished task = %+v, want no stale blocker", got)
	}
}

// A phase-wide block applies one reason to every task in the phase.
func TestTodoReplayBlocksAWholePhaseWithOneReason(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "整段阻塞", "2026-07-30T01:00:00.000Z"),
		todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "list": []any{phased("Verify", "跑门禁", "写报告")}}),
		todoLine("2026-07-30T01:00:02.000Z", map[string]any{"op": "block", "phase": "Verify", "reason": "等硬件回归"}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, item := range digest.Todos {
		if item.Status != TodoBlocked || item.Blocker != "等硬件回归" {
			t.Fatalf("item = %+v, want the phase's reason on every task", item)
		}
	}
}

// A session that re-plans loses its earlier lists from the tracker, so the
// finished work has to survive the replacement or the record is a lie: a
// six-hour session would show four open tasks and nothing done.
func TestTodoReplayCarriesFinishedWorkAcrossReplans(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "反复重规划", "2026-07-30T01:00:00.000Z"),
		todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "list": []any{phased("Recon", "读设计", "读实现")}}),
		todoLine("2026-07-30T01:00:02.000Z", map[string]any{"op": "done", "task": "读设计"}),
		todoLine("2026-07-30T01:00:03.000Z", map[string]any{"op": "drop", "task": "读实现"}),
		todoLine("2026-07-30T01:00:04.000Z", map[string]any{"op": "init", "list": []any{phased("Build", "改代码")}}),
		todoLine("2026-07-30T01:00:05.000Z", map[string]any{"op": "done", "task": "改代码"}),
		todoLine("2026-07-30T01:00:06.000Z", map[string]any{"op": "init", "items": []string{"收尾"}}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if digest.TodoReplans != 2 {
		t.Fatalf("TodoReplans = %d, want 2", digest.TodoReplans)
	}
	if !equal(digest.TodosCompleted, []string{"读设计", "改代码"}) {
		t.Fatalf("TodosCompleted = %v, want the finished tasks from both earlier lists", digest.TodosCompleted)
	}
	// A dropped task was abandoned, not finished: it must not appear as done.
	for _, done := range digest.TodosCompleted {
		if done == "读实现" {
			t.Fatal("a dropped task must not count as completed")
		}
	}
	// The flattened form of init has no phase.
	if len(digest.Todos) != 1 || digest.Todos[0].Content != "收尾" || digest.Todos[0].Phase != "" {
		t.Fatalf("Todos = %+v, want the flattened final list", digest.Todos)
	}
}

func TestTodoReplayHandlesPhaseWideOpsAppendAndRemoval(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "整段操作", "2026-07-30T01:00:00.000Z"),
		todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "list": []any{
			phased("A", "a1", "a2"),
			phased("B", "b1"),
		}}),
		// A phase-wide op addresses every task in that phase.
		todoLine("2026-07-30T01:00:02.000Z", map[string]any{"op": "done", "phase": "A"}),
		todoLine("2026-07-30T01:00:03.000Z", map[string]any{"op": "append", "phase": "B", "items": []string{"b2"}}),
		todoLine("2026-07-30T01:00:04.000Z", map[string]any{"op": "append", "phase": "C", "items": []string{"c1"}}),
		todoLine("2026-07-30T01:00:05.000Z", map[string]any{"op": "rm", "task": "b1"}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	statuses := map[string]string{}
	phases := map[string]string{}
	for _, item := range digest.Todos {
		statuses[item.Content] = item.Status
		phases[item.Content] = item.Phase
	}
	if statuses["a1"] != TodoCompleted || statuses["a2"] != TodoCompleted {
		t.Fatalf("a phase-wide done must apply to every task: %v", statuses)
	}
	if _, present := statuses["b1"]; present {
		t.Fatalf("rm must remove the task: %v", statuses)
	}
	// append lazily creates the phase it names.
	if phases["c1"] != "C" || statuses["c1"] != TodoPending {
		t.Fatalf("appended task = %v / %v", phases["c1"], statuses["c1"])
	}
	if phases["b2"] != "B" {
		t.Fatalf("appended task landed in phase %q, want B", phases["b2"])
	}
	// A removal is a clearing too, so anything finished first is retired.
	if !equal(digest.TodosCompleted, nil) && len(digest.TodosCompleted) == 0 {
		t.Fatalf("TodosCompleted = %v", digest.TodosCompleted)
	}
}

func TestTodoReplayUnblockReturnsToPendingAndBareRemovalClears(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "阻塞与清空", "2026-07-30T01:00:00.000Z"),
		todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "items": []string{"等外部输入", "另一件事"}}),
		todoLine("2026-07-30T01:00:02.000Z", map[string]any{"op": "block", "task": "等外部输入"}),
		todoLine("2026-07-30T01:00:03.000Z", map[string]any{"op": "unblock", "task": "等外部输入"}),
		todoLine("2026-07-30T01:00:04.000Z", map[string]any{"op": "done", "task": "另一件事"}),
		todoLine("2026-07-30T01:00:05.000Z", map[string]any{"op": "rm"}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(digest.Todos) != 0 {
		t.Fatalf("a bare rm must clear the list, got %+v", digest.Todos)
	}
	if !equal(digest.TodosCompleted, []string{"另一件事"}) {
		t.Fatalf("TodosCompleted = %v, want the finished task retired before clearing", digest.TodosCompleted)
	}
}

func TestTodoReplayCapsTheRecord(t *testing.T) {
	dir := t.TempDir()
	lines := []string{sessionLine("a", "/repo", "超长清单", "2026-07-30T01:00:00.000Z")}
	items := make([]string, 0, MaxTodos+20)
	for i := 0; i < MaxTodos+20; i++ {
		items = append(items, fmtName(i))
	}
	lines = append(lines, todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "items": items}))
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl", lines...)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(digest.Todos) != MaxTodos || !digest.TodosTruncated {
		t.Fatalf("Todos = %d truncated=%v, want %d and a truncation flag", len(digest.Todos), digest.TodosTruncated, MaxTodos)
	}
}

// A malformed or unknown operation must not corrupt the list.
func TestTodoReplayIgnoresUnknownAndMalformedOps(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "怪操作", "2026-07-30T01:00:00.000Z"),
		todoLine("2026-07-30T01:00:01.000Z", map[string]any{"op": "init", "items": []string{"活着"}}),
		todoLine("2026-07-30T01:00:02.000Z", map[string]any{"op": "teleport", "task": "活着"}),
		`{"type":"message","timestamp":"2026-07-30T01:00:03.000Z","message":{"role":"assistant","content":[{"type":"toolCall","name":"todo","arguments":"not-an-object"}]}}`,
		todoLine("2026-07-30T01:00:04.000Z", map[string]any{"op": "done", "task": "活着"}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(digest.Todos) != 1 || digest.Todos[0].Status != TodoCompleted {
		t.Fatalf("Todos = %+v, want the one task completed", digest.Todos)
	}
}

// The tracker's own record belongs to the session that wrote it; a subagent's
// breakdown of one delegated slice must not splice into it.
func TestMergeKeepsThePrimaryTaskRecord(t *testing.T) {
	primary := Digest{
		ID: "p", Path: "/s/p.jsonl",
		Todos:          []TodoItem{{Phase: "Core", Content: "父任务", Status: TodoInProgress}},
		TodosCompleted: []string{"父已完成"},
		TodoReplans:    2,
	}
	parts := []Digest{{
		Path:           "/s/p/Worker.jsonl",
		Todos:          []TodoItem{{Content: "子任务", Status: TodoCompleted}},
		TodosCompleted: []string{"子已完成"},
		TodoReplans:    7,
	}}

	merged := Merge(primary, parts)

	if len(merged.Todos) != 1 || merged.Todos[0].Content != "父任务" {
		t.Fatalf("Todos = %+v, want only the primary's list", merged.Todos)
	}
	if !equal(merged.TodosCompleted, []string{"父已完成"}) {
		t.Fatalf("TodosCompleted = %v, want only the primary's history", merged.TodosCompleted)
	}
	if merged.TodoReplans != 2 {
		t.Fatalf("TodoReplans = %d, want the primary's count", merged.TodoReplans)
	}
}
