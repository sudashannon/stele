package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ---- helpers ---------------------------------------------------------------

// ccSessionLine builds a system event that carries session metadata.
// All Claude Code events carry sessionId and cwd; for a fixture we use a
// system event so it does not count as a human turn.
func ccSessionLine(id, cwd, ts string) string {
	return `{"type":"system","timestamp":"` + ts + `","sessionId":"` + id + `","cwd":"` + cwd + `"}`
}

// ccAssistantLine builds an assistant event with tool_use blocks.
func ccAssistantLine(ts string, blocks ...string) string {
	body := `{"type":"assistant","timestamp":"` + ts + `","message":{"role":"assistant","content":[`
	for i, b := range blocks {
		if i > 0 {
			body += ","
		}
		body += b
	}
	body += `]}}`
	return body
}

// ccUserLine builds a user event with text content.
func ccUserLine(ts, text string) string {
	return `{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":"` + text + `"}}`
}

// ccToolResultLine builds a user event carrying a tool_result block.
func ccToolResultLine(ts, toolUseID string) string {
	return `{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + toolUseID + `","content":"ok","is_error":false}]}}`
}

// ccSidechainLine builds a user event that is a sidechain (subagent prompt).
func ccSidechainLine(ts, text string) string {
	return `{"type":"user","timestamp":"` + ts + `","isSidechain":true,"message":{"role":"user","content":"` + text + `"}}`
}

// ccToolBlock builds one tool_use content block.
func ccToolBlock(name, filePath string) string {
	if filePath != "" {
		return `{"type":"tool_use","name":"` + name + `","input":{"file_path":"` + filePath + `"}}`
	}
	return `{"type":"tool_use","name":"` + name + `","input":{}}`
}

// ccTitleLine builds a custom-title event.
func ccTitleLine(ts, title string) string {
	return `{"type":"custom-title","timestamp":"` + ts + `","title":"` + title + `"}`
}

// ccTodoBlock builds a TodoWrite tool_use block with raw input.
func ccTodoBlock(input string) string {
	return `{"type":"tool_use","name":"TodoWrite","input":` + input + `}`
}

// ---- discover --------------------------------------------------------------

// Discover returns one Unit per .jsonl file, ignoring non-.jsonl files and
// handling missing roots without error.
func TestCCDiscoverGroupsPerTranscript(t *testing.T) {
	dir := t.TempDir()
	// Project directory with two sessions.
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	transcript(t, proj, "aaa.jsonl", ccSessionLine("aaa", "/home/user/myapp", "2026-08-01T10:00:00Z"))
	transcript(t, proj, "bbb.jsonl", ccSessionLine("bbb", "/home/user/myapp", "2026-08-01T11:00:00Z"))
	// Non-.jsonl file that must be ignored.
	if err := os.WriteFile(filepath.Join(proj, "README.md"), []byte("not a transcript"), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := claudeCodeProvider{}
	units, err := provider.Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	// Order is sorted by path.
	if units[0].Primary != filepath.Join(proj, "aaa.jsonl") {
		t.Fatalf("first unit = %s", units[0].Primary)
	}
	if units[1].Primary != filepath.Join(proj, "bbb.jsonl") {
		t.Fatalf("second unit = %s", units[1].Primary)
	}
	// No Parts for claude-code (subagents are separate Units).
	for _, u := range units {
		if len(u.Parts) > 0 {
			t.Fatalf("unit %s has %d parts, want 0", u.Primary, len(u.Parts))
		}
	}
}

// A missing root returns nil with no error.
func TestCCDiscoverMissingRoot(t *testing.T) {
	provider := claudeCodeProvider{}
	units, err := provider.Discover("/nonexistent/path")
	if err != nil || units != nil {
		t.Fatalf("Discover(nonexistent) = (%v, %v), want (nil, nil)", units, err)
	}
}

// Transcripts in subdirectories (subagents/) are also discovered as their
// own Units.
func TestCCDiscoverWalksSubdirectories(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	subagents := filepath.Join(proj, "subagents")
	mkdir(t, subagents)

	transcript(t, proj, "main.jsonl", ccSessionLine("main", "/home/user/myapp", "2026-08-01T10:00:00Z"))
	transcript(t, subagents, "agent-1.jsonl", ccSessionLine("agent-1", "/home/user/myapp", "2026-08-01T10:01:00Z"))

	provider := claudeCodeProvider{}
	units, err := provider.Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2 (main + subagent)", len(units))
	}
}

// ---- parse: identity and metadata -----------------------------------------

// Parse extracts cwd, session id, title, and timestamps from the transcript.
func TestCCParseExtractsIdentityAndTimestamps(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "abc-def-123.jsonl",
		ccSessionLine("abc-def-123", "/home/user/myapp", "2026-08-01T10:00:00Z"),
		ccTitleLine("2026-08-01T10:00:01Z", "Fix the login bug"),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if digest.Source != "claude-code" {
		t.Fatalf("Source = %q, want claude-code", digest.Source)
	}
	if digest.ID != "abc-def-123" {
		t.Fatalf("ID = %q, want abc-def-123", digest.ID)
	}
	if digest.Cwd != "/home/user/myapp" {
		t.Fatalf("Cwd = %q", digest.Cwd)
	}
	if digest.Title != "Fix the login bug" {
		t.Fatalf("Title = %q, want Fix the login bug", digest.Title)
	}
	if digest.StartedAt.IsZero() {
		t.Fatal("StartedAt is zero")
	}
}

// When no custom-title event exists, Title falls back to the filename stem.
func TestCCParseTitleFallsBackToFilename(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "my-session-id.jsonl",
		ccSessionLine("my-session-id", "/cwd", "2026-08-01T10:00:00Z"),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if digest.Title != "my-session-id" {
		t.Fatalf("Title = %q, want my-session-id (filename stem)", digest.Title)
	}
}

// ---- parse: tool calls ----------------------------------------------------

// Tool calls are counted by their Claude Code runtime names. Read/Write/Edit
// contribute paths; Bash/Grep/Glob do not.
func TestCCParseCountsToolCallsAndPaths(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		// Turn 1: Read and Bash (Bash should NOT contribute paths).
		ccAssistantLine("2026-08-01T10:00:01Z",
			ccToolBlock("Read", "src/main.go"),
			ccToolBlock("Bash", ""),
		),
		// Turn 2: Write and Grep (Grep should NOT contribute paths).
		ccAssistantLine("2026-08-01T10:00:02Z",
			ccToolBlock("Write", "out/report.txt"),
			ccToolBlock("Grep", ""),
		),
		// Turn 3: Edit with relative path (normalized against cwd).
		ccAssistantLine("2026-08-01T10:00:03Z",
			ccToolBlock("Edit", "pkg/util.go"),
		),
		// Turn 4: Glob (no path).
		ccAssistantLine("2026-08-01T10:00:04Z",
			ccToolBlock("Glob", ""),
		),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Tool counts by runtime name.
	if digest.ToolCalls["Read"] != 1 {
		t.Fatalf("Read calls = %d, want 1", digest.ToolCalls["Read"])
	}
	if digest.ToolCalls["Write"] != 1 {
		t.Fatalf("Write calls = %d, want 1", digest.ToolCalls["Write"])
	}
	if digest.ToolCalls["Edit"] != 1 {
		t.Fatalf("Edit calls = %d, want 1", digest.ToolCalls["Edit"])
	}
	if digest.ToolCalls["Bash"] != 1 {
		t.Fatalf("Bash calls = %d, want 1", digest.ToolCalls["Bash"])
	}
	if digest.ToolCalls["Grep"] != 1 {
		t.Fatalf("Grep calls = %d, want 1", digest.ToolCalls["Grep"])
	}
	if digest.ToolCalls["Glob"] != 1 {
		t.Fatalf("Glob calls = %d, want 1", digest.ToolCalls["Glob"])
	}

	// Path classification.
	cwd := "/repo"
	if !containsPath(digest.Reads, filepath.Join(cwd, "src/main.go")) {
		t.Fatalf("Reads missing src/main.go: %v", digest.Reads)
	}
	if !containsPath(digest.Writes, filepath.Join(cwd, "out/report.txt")) {
		t.Fatalf("Writes missing out/report.txt: %v", digest.Writes)
	}
	if !containsPath(digest.Edits, filepath.Join(cwd, "pkg/util.go")) {
		t.Fatalf("Edits missing pkg/util.go: %v", digest.Edits)
	}

	// Bash, Grep, Glob must contribute zero paths.
	if len(digest.Reads) != 1 {
		t.Fatalf("Reads has %d entries, want exactly 1 (Bash/Grep/Glob did not contribute)", len(digest.Reads))
	}
	if len(digest.Writes) != 1 {
		t.Fatalf("Writes has %d entries, want exactly 1", len(digest.Writes))
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// ---- parse: user turns ----------------------------------------------------

// Human turns are counted; tool results and sidechain entries are not.
// This is the subtle test: if someone counts all user-role entries, this
// fails with userTurns=4 instead of 2.
func TestCCParseUserTurnsExcludesToolResultsAndSidechains(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		// Real human turn.
		ccUserLine("2026-08-01T10:00:01Z", "fix the bug"),
		// Assistant turn with a tool call.
		ccAssistantLine("2026-08-01T10:00:02Z",
			ccToolBlock("Read", "foo.go"),
		),
		// Tool result — NOT a human turn.
		ccToolResultLine("2026-08-01T10:00:03Z", "toolu_01"),
		// Sidechain user entry — NOT a human turn.
		ccSidechainLine("2026-08-01T10:00:04Z", "subagent prompt"),
		// Another real human turn.
		ccUserLine("2026-08-01T10:00:05Z", "also check the tests"),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if digest.UserTurns != 2 {
		t.Fatalf("UserTurns = %d, want 2 (tool result and sidechain excluded)", digest.UserTurns)
	}
}

// Noise user entries (local-command-stdout, system-reminder, etc.) must not
// count as human turns.
func TestCCParseUserTurnsExcludesNoiseEntries(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		ccUserLine("2026-08-01T10:00:01Z", "real prompt"),
		// Each of these should be excluded.
		ccUserLine("2026-08-01T10:00:02Z", "<local-command-stdout> output"),
		ccUserLine("2026-08-01T10:00:03Z", "<local-command-caveat> warning"),
		ccUserLine("2026-08-01T10:00:04Z", "<system-reminder> reminder text"),
		ccUserLine("2026-08-01T10:00:05Z", "[Request interrupted by user]"),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if digest.UserTurns != 1 {
		t.Fatalf("UserTurns = %d, want 1 (noise entries excluded)", digest.UserTurns)
	}
}

// ---- parse: todo handling -------------------------------------------------

// TodoWrite replaces the entire list on each call. The last call wins.
func TestCCParseTodoWriteLastCallWins(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		// First TodoWrite — three items.
		ccAssistantLine("2026-08-01T10:00:01Z",
			ccTodoBlock(`{"newTodos":[{"content":"task A","status":"pending"},{"content":"task B","status":"in_progress"},{"content":"task C","status":"completed"}]}`),
		),
		// Second TodoWrite — replaces with two items, cancelling the rest.
		ccAssistantLine("2026-08-01T10:00:02Z",
			ccTodoBlock(`{"newTodos":[{"content":"task X","status":"pending"},{"content":"task Y","status":"cancelled"}]}`),
		),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(digest.Todos) != 2 {
		t.Fatalf("Todos = %d items, want 2 (last call replaced first)", len(digest.Todos))
	}
	if digest.Todos[0].Content != "task X" || digest.Todos[0].Status != TodoPending {
		t.Fatalf("Todos[0] = {%q %q}, want {task X pending}", digest.Todos[0].Content, digest.Todos[0].Status)
	}
	if digest.Todos[1].Content != "task Y" || digest.Todos[1].Status != TodoDropped {
		t.Fatalf("Todos[1] = {%q %q}, want {task Y dropped}", digest.Todos[1].Content, digest.Todos[1].Status)
	}

	// TodoCompleted and TodoReplans stay zero — nothing was lost on replacement.
	if len(digest.TodosCompleted) != 0 {
		t.Fatalf("TodosCompleted = %v, want empty", digest.TodosCompleted)
	}
}

// Statuses map to the shared constants.
func TestCCParseTodoWriteMapsStatuses(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		ccAssistantLine("2026-08-01T10:00:01Z",
			ccTodoBlock(`{"newTodos":[{"content":"pending task","status":"pending"},{"content":"active task","status":"in_progress"},{"content":"done task","status":"completed"},{"content":"cancelled task","status":"cancelled"}]}`),
		),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []struct {
		content string
		status  string
	}{
		{"pending task", TodoPending},
		{"active task", TodoInProgress},
		{"done task", TodoCompleted},
		{"cancelled task", TodoDropped},
	}
	if len(digest.Todos) != len(want) {
		t.Fatalf("got %d todos, want %d", len(digest.Todos), len(want))
	}
	for i, w := range want {
		if digest.Todos[i].Content != w.content || digest.Todos[i].Status != w.status {
			t.Fatalf("Todos[%d] = {%q %q}, want {%q %q}", i, digest.Todos[i].Content, digest.Todos[i].Status, w.content, w.status)
		}
		if digest.Todos[i].Phase != "" {
			t.Fatalf("Todos[%d].Phase = %q, want empty (no phases in Claude Code)", i, digest.Todos[i].Phase)
		}
		if digest.Todos[i].Blocker != "" {
			t.Fatalf("Todos[%d].Blocker = %q, want empty (no blocker in Claude Code)", i, digest.Todos[i].Blocker)
		}
	}
}

// A session with no TodoWrite calls has no todos.
func TestCCParseNoTodoWriteNoTodos(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		ccUserLine("2026-08-01T10:00:01Z", "just chatting"),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(digest.Todos) != 0 {
		t.Fatalf("Todos = %v, want empty", digest.Todos)
	}
}

// ---- parse: resume --------------------------------------------------------

// Appending to a transcript resumes from the offset without re-reading.
func TestCCParseResumesFromOffset(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		ccUserLine("2026-08-01T10:00:01Z", "first turn"),
	)

	provider := claudeCodeProvider{}
	first, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("first Parse: %v", err)
	}
	if first.UserTurns != 1 {
		t.Fatalf("first pass UserTurns = %d, want 1", first.UserTurns)
	}
	firstOffset := first.Offset

	// Append more lines.
	appendLines(t, path,
		ccUserLine("2026-08-01T10:00:02Z", "second turn"),
		ccUserLine("2026-08-01T10:00:03Z", "third turn"),
	)

	// Resuming from prev should only parse the new lines.
	second, err := provider.Parse(path, first)
	if err != nil {
		t.Fatalf("second Parse: %v", err)
	}
	if second.UserTurns != 3 {
		t.Fatalf("resumed UserTurns = %d, want 3 (1 original + 2 new)", second.UserTurns)
	}
	// Offset must have advanced beyond the first parse.
	if second.Offset <= firstOffset {
		t.Fatalf("Offset = %d, must be > %d", second.Offset, firstOffset)
	}
}

// A shrunk file is re-parsed from the start.
func TestCCParseRebuildsWhenShrunk(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		ccUserLine("2026-08-01T10:00:01Z", "one"),
		ccUserLine("2026-08-01T10:00:02Z", "two"),
	)

	provider := claudeCodeProvider{}
	first, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("first Parse: %v", err)
	}
	if first.UserTurns != 2 {
		t.Fatalf("first pass UserTurns = %d, want 2", first.UserTurns)
	}

	// Replace file with fewer lines (simulating rotation/rewrite).
	if err := os.WriteFile(path, []byte(
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z")+"\n"+
			ccUserLine("2026-08-01T10:00:01Z", "only one now")+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := provider.Parse(path, first)
	if err != nil {
		t.Fatalf("second Parse: %v", err)
	}
	if second.UserTurns != 1 {
		t.Fatalf("shrunk UserTurns = %d, want 1 (reparsed from start)", second.UserTurns)
	}
}

// ---- parse: caps ----------------------------------------------------------

// Exceeding MaxPaths sets PathsTruncated.
func TestCCParseCapsPaths(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)

	var lines []string
	lines = append(lines, ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"))
	for i := range MaxPaths + 10 {
		// Use the numeric index to guarantee uniqueness across all 410 paths.
		// i%26 would recycle after 26 and the dedup would skip repeats.
		file := "-repo-src-file" + fmt.Sprintf("%d", i) + ".go"
		lines = append(lines, ccAssistantLine("2026-08-01T10:00:01Z",
			ccToolBlock("Read", file),
		))
	}
	path := transcript(t, proj, "s.jsonl", lines...)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !digest.PathsTruncated {
		t.Fatal("PathsTruncated must be true when exceeding MaxPaths")
	}
	if len(digest.Reads) > MaxPaths {
		t.Fatalf("Reads = %d items, must be capped at %d", len(digest.Reads), MaxPaths)
	}
}

// Exceeding MaxTodos sets TodosTruncated.
func TestCCParseCapsTodos(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-user-myapp")
	mkdir(t, proj)

	// Build a TodoWrite with more items than MaxTodos.
	var items string
	for i := range MaxTodos + 10 {
		if i > 0 {
			items += ","
		}
		items += `{"content":"task ` + fmt.Sprintf("%d", i) + `","status":"pending"}`
	}
	path := transcript(t, proj, "s.jsonl",
		ccSessionLine("s", "/repo", "2026-08-01T10:00:00Z"),
		ccAssistantLine("2026-08-01T10:00:01Z",
			ccTodoBlock(`{"newTodos":[`+items+`]}`),
		),
	)

	provider := claudeCodeProvider{}
	digest, err := provider.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !digest.TodosTruncated {
		t.Fatal("TodosTruncated must be true when exceeding MaxTodos")
	}
	if len(digest.Todos) > MaxTodos {
		t.Fatalf("Todos = %d items, must be capped at %d", len(digest.Todos), MaxTodos)
	}
}

// ---- registry --------------------------------------------------------------

// ProviderByName resolves "claude-code" and ProviderNames includes it.
func TestCCProviderRegistryResolves(t *testing.T) {
	provider, ok := ProviderByName("claude-code")
	if !ok {
		t.Fatal("ProviderByName(claude-code) must resolve")
	}
	if provider.Name() != "claude-code" {
		t.Fatalf("Name = %q, want claude-code", provider.Name())
	}

	names := ProviderNames()
	found := false
	for _, n := range names {
		if n == "claude-code" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ProviderNames = %v, must include claude-code", names)
	}
}

// Activity is runtime-agnostic: a Claude Code session spans days exactly like an
// OMP one, and every consumer (panel grouping, calendar) reads the same field.
func TestCCParseCountsDailyActivity(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "11111111-2222-3333-4444-555555555555.jsonl",
		ccSessionLine("sess-cc", "/repo", "2026-07-30T04:00:00.000Z"),
		ccUserLine("2026-07-30T04:00:00.000Z", "看一下"),
		ccAssistantLine("2026-07-30T04:00:01.000Z", ccToolBlock("Read", "/repo/a.md")),
		ccAssistantLine("2026-08-02T04:00:00.000Z", ccToolBlock("Read", "/repo/b.md")),
	)

	digest, err := claudeCodeProvider{}.Parse(path, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(digest.Activity) != 2 {
		t.Fatalf("Activity = %v, want two active days", digest.Activity)
	}
	total := 0
	for _, count := range digest.Activity {
		total += count
	}
	if total != 3 {
		t.Fatalf("activity total = %d, want one turn plus two tool calls", total)
	}
}
