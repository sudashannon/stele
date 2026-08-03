package sessions

import (
	"testing"
)

// A resumed session runs for days. Keeping the FIRST intents answered "what is
// this session doing" with its opening moves - one measured transcript kept a
// 0.9-hour window from four days before it ended - while every surface labelled
// them the latest activity.
func TestIntentsKeepTheMostRecentWindow(t *testing.T) {
	dir := t.TempDir()
	lines := []string{sessionLine("a", "/repo", "长会话", "2026-07-30T01:00:00.000Z")}
	for i := 0; i < MaxIntents+25; i++ {
		lines = append(lines, toolCallLine("2026-07-30T01:00:01.000Z", "read", fmtName(i), map[string]any{"path": "a.md"}))
	}
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl", lines...)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(digest.Intents) != MaxIntents || !digest.IntentsTruncated {
		t.Fatalf("intents = %d truncated=%v, want %d capped", len(digest.Intents), digest.IntentsTruncated, MaxIntents)
	}
	if got, want := digest.Intents[len(digest.Intents)-1], fmtName(MaxIntents+24); got != want {
		t.Fatalf("last intent = %q, want the newest %q", got, want)
	}
	if got, want := digest.Intents[0], fmtName(25); got != want {
		t.Fatalf("first intent = %q, want the window to start at %q", got, want)
	}
}

// The character budget is the same window seen from the other side: it must also
// drop the oldest, or a few long intents at the start would pin the window there.
func TestIntentCharBudgetDropsTheOldest(t *testing.T) {
	dir := t.TempDir()
	long := make([]byte, MaxIntentChars/2)
	for i := range long {
		long[i] = 'x'
	}
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "长意图", "2026-07-30T01:00:00.000Z"),
		toolCallLine("2026-07-30T01:00:01.000Z", "read", "第一条-"+string(long), map[string]any{"path": "a.md"}),
		toolCallLine("2026-07-30T01:00:02.000Z", "read", "第二条-"+string(long), map[string]any{"path": "a.md"}),
		toolCallLine("2026-07-30T01:00:03.000Z", "read", "最后一条", map[string]any{"path": "a.md"}),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if !digest.IntentsTruncated {
		t.Fatal("exceeding the character budget must be reported as truncation")
	}
	if last := digest.Intents[len(digest.Intents)-1]; last != "最后一条" {
		t.Fatalf("last intent = %q, want the newest kept", last)
	}
	for _, intent := range digest.Intents {
		if intent == "第一条-"+string(long) {
			t.Fatal("the oldest intent must be the one dropped")
		}
	}
}

// Resuming appends: the window has to slide, not freeze at what the first pass
// happened to see.
func TestIntentWindowSlidesAcrossAResume(t *testing.T) {
	dir := t.TempDir()
	lines := []string{sessionLine("a", "/repo", "续读", "2026-07-30T01:00:00.000Z")}
	for i := 0; i < MaxIntents; i++ {
		lines = append(lines, toolCallLine("2026-07-30T01:00:01.000Z", "read", fmtName(i), map[string]any{"path": "a.md"}))
	}
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl", lines...)
	first, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	appendLines(t, path, toolCallLine("2026-07-30T02:00:00.000Z", "read", "崭新意图", map[string]any{"path": "b.md"}))
	second, err := ParseFile(path, first)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if second.Offset <= first.Offset {
		t.Fatalf("resume must advance the offset: %d -> %d", first.Offset, second.Offset)
	}
	if last := second.Intents[len(second.Intents)-1]; last != "崭新意图" {
		t.Fatalf("last intent = %q, want the appended one", last)
	}
	if len(second.Intents) != MaxIntents {
		t.Fatalf("intents = %d, want the window held at %d", len(second.Intents), MaxIntents)
	}
	if second.Intents[0] != fmtName(1) {
		t.Fatalf("window start = %q, want the oldest dropped", second.Intents[0])
	}
}

// StartedAt→UpdatedAt is a range, not a duration: a session resumed over days
// needs per-day counts before any consumer can place its work in time.
func TestActivityCountsEachDaysTurnsAndToolCalls(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "跨天", "2026-07-30T04:00:00.000Z"),
		userLine("2026-07-30T04:00:01.000Z", "开始"),
		toolCallLine("2026-07-30T04:00:02.000Z", "read", "读一下", map[string]any{"path": "a.md"}),
		toolCallLine("2026-08-01T04:00:00.000Z", "read", "两天后继续", map[string]any{"path": "b.md"}),
		userLine("2026-08-01T04:00:01.000Z", "继续"),
		toolCallLine("2026-08-01T04:00:02.000Z", "write", "收尾", map[string]any{"path": "c.md"}),
		// An undated line cannot be placed on any day.
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"无时间戳"}]}}`,
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(digest.Activity) != 2 {
		t.Fatalf("Activity = %v, want two active days", digest.Activity)
	}
	total := 0
	for _, count := range digest.Activity {
		total += count
	}
	// The fixture records three user turns and three tool calls; the undated
	// turn counts as a turn but belongs to no day.
	if digest.UserTurns != 3 {
		t.Fatalf("UserTurns = %d, want 3", digest.UserTurns)
	}
	if total != 5 {
		t.Fatalf("activity total = %d, want 5 dated events", total)
	}
}

func TestActivityAccumulatesAcrossAResume(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "续读活跃度", "2026-07-30T04:00:00.000Z"),
		toolCallLine("2026-07-30T04:00:01.000Z", "read", "第一天", map[string]any{"path": "a.md"}),
	)
	first, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	appendLines(t, path, toolCallLine("2026-08-02T04:00:00.000Z", "read", "第四天", map[string]any{"path": "b.md"}))

	second, err := ParseFile(path, first)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(second.Activity) != 2 {
		t.Fatalf("Activity = %v, want both days after the resume", second.Activity)
	}
	// The first pass's day must survive: a resume adds, it does not restate.
	if len(first.Activity) != 1 {
		t.Fatalf("first pass Activity = %v, want one day", first.Activity)
	}
}

// A subagent's work happened on a real day and belongs to the session that
// dispatched it, so its days fold in like its tool calls do.
func TestMergeSumsActivityAcrossSubagents(t *testing.T) {
	primary := Digest{ID: "p", Path: "/s/p.jsonl", Activity: map[string]int{"2026-07-30": 4}}
	parts := []Digest{
		{Path: "/s/p/A.jsonl", Activity: map[string]int{"2026-07-30": 3, "2026-07-31": 5}},
		{Path: "/s/p/B.jsonl", Activity: map[string]int{"2026-07-31": 2}},
	}

	merged := Merge(primary, parts)

	if merged.Activity["2026-07-30"] != 7 || merged.Activity["2026-07-31"] != 7 {
		t.Fatalf("Activity = %v, want per-day sums", merged.Activity)
	}
	// The primary's own map must not be mutated by the merge.
	if primary.Activity["2026-07-30"] != 4 {
		t.Fatalf("primary mutated: %v", primary.Activity)
	}
}
