package sessions

// claudeCodeProvider adapts Anthropic's Claude Code agent runtime transcripts
// to the digest pipeline.
//
// # Format references (all consulted 2026-08-03)
//
// Schema confirmed against these sources:
//   - https://claude-dev.tools/docs/jsonl-format
//     Primary field reference: type, uuid, parentUuid, timestamp, sessionId,
//     cwd, gitBranch, version, isSidechain. Content block types: text,
//     thinking, tool_use, tool_result.
//   - https://github.com/d1ll0n/parse-cc (TypeScript parser, v0.2.0)
//     Confirmed entry type guards (user/assistant/system/summary/custom-title
//     etc.), content block guards (text/thinking/tool_use/tool_result),
//     TodoWrite shape (oldTodos + newTodos arrays of {content, status,
//     priority}), and subagent layout (subagents/ subdirectory).
//   - https://www.adityabawankule.io/blog/claude-code-session-jsonl-format
//     Detailed field documentation: confirmed isSidechain behaviour (modern
//     versions write subagents to separate files), noise-prefix patterns,
//     user-entry discrimination (string = prompt, array with tool_result =
//     tool result), and the "Request interrupted" guard.
//   - https://github.com/ryoppippi/ccusage (Clojure/Rust usage analyser)
//     Confirmed file layout and the file-history-snapshot exclusion issue.
//   - https://github.com/daaain/claude-code-log (Python HTML exporter)
//     Confirmed event types across hundreds of real sessions.
//
// # Layout
//
//	<root>/<project-slug>/<session-uuid>.jsonl
//
// The project-slug is the cwd with slashes replaced by hyphens
// (e.g. /Users/alice/my-app → -Users-alice-my-app). Subagents live at
// <project-slug>/subagents/<agent-uuid>.jsonl in newer versions; older
// versions nested them under <session-uuid>/subagents/. Each .jsonl is
// returned as its own Unit without Parts, because subagent files carry
// their own sessionId distinct from the parent and are not guaranteed to
// be co-located (the project-level subagents/ layout has no per-session
// grouping).
//
// # Event shape (per line)
//
// Top-level: type, uuid, parentUuid, timestamp, sessionId, cwd, gitBranch,
// version, isSidechain (bool, absent = false).
//
// User entries (type:"user"): message.role="user", message.content is a
// string (plain text prompt) or an array of blocks. Real human turns are
// entries whose content is a non-empty string or array of text blocks
// with no tool_result blocks, AND isSidechain is absent/false. Tool results
// are user entries with tool_result blocks.
//
// Assistant entries (type:"assistant"): message.content is an array of
// blocks: text, thinking, tool_use. Each tool_use block has id, name
// (Read/Write/Edit/MultiEdit/Bash/Grep/Glob/TodoWrite/Task/...), and
// input whose shape is tool-specific.
//
// TodoWrite: input.newTodos is the whole current list; the last call wins
// (whole-state replacement, not operation stream). Statuses observed:
// pending, in_progress, completed, cancelled. Claude Code v2.1.142+
// migrated to TaskCreate/TaskUpdate tools; sessions using those will have
// no todos (documented degradation — Intents is nil for the same reason).
//
// custom-title (type:"custom-title"): carries a title field naming the
// session; used as Digest.Title when present, falling back to the
// filename stem.
//
// # Inferred fields
//
//   - TodoItem.Blocker: the Claude Code todo schema has no blocker/reason
//     field. It is always empty.
//   - TodoItem.Phase: Claude Code has no phase concept; always empty.
//   - TodoCompleted, TodoReplans: zero — the last TodoWrite replaces the
//     entire list so nothing is lost that a historical record would need
//     to carry forward.
//   - The priority field on todo items is observed but has no Digest slot.
//   - file_path is the canonical key for Read/Write/Edit tool arguments;
//     "path" is accepted as a fallback ONLY for Read/Write/Edit (Grep and
//     Glob use "path" for a directory and are excluded from path tracking).
//   - Inferred noise prefixes beyond the four confirmed in source #3
//     (<local-command-stdout>, <local-command-caveat>, <system-reminder>,
//     [Request interrupted): <command-message> and <command-name> from
//     parse-cc extractFirstUserMessage.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudeCodeProvider reads Claude Code transcripts.
type claudeCodeProvider struct{}

func (claudeCodeProvider) Name() string { return "claude-code" }

// Discover returns one Unit per .jsonl file under root's project directories.
// A missing root returns (nil, nil) with no error, matching the OMP provider.
func (p claudeCodeProvider) Discover(root string) ([]Unit, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sessions: read %s: %w", root, err)
	}

	var units []Unit
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(root, entry.Name())
		units = append(units, unitsInCC(projectDir)...)
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Primary < units[j].Primary })
	return units, nil
}

// unitsInCC returns one Unit per .jsonl in dir. Subagent directories are
// walked recursively so subagent transcripts are also discoverable, each as
// its own Unit.
func unitsInCC(dir string) []Unit {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("sessions: skipping %s: %v", dir, err)
		return nil
	}
	var units []Unit
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			// Walk subdirectories (subagents/, session artifacts) for
			// nested .jsonl files.
			units = append(units, unitsInCC(filepath.Join(dir, name))...)
			continue
		}
		if !isTranscript(name) {
			continue
		}
		units = append(units, Unit{Primary: filepath.Join(dir, name)})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Primary < units[j].Primary })
	return units
}

// Parse builds a Digest from a Claude Code transcript at path, resuming
// from prev when the file only grew.
func (p claudeCodeProvider) Parse(path string, prev *Digest) (*Digest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sessions: %s is not a regular file", path)
	}

	d := &Digest{Path: path, ToolCalls: map[string]int{}, Source: p.Name()}
	var start int64
	if resumable(prev, info) {
		d = prev.clone()
		start = prev.Offset
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if start > 0 {
		if _, err := file.Seek(start, 0); err != nil {
			return nil, err
		}
	}

	parser := &ccParser{d: d}
	consumed, err := parser.consume(file)
	if err != nil {
		return nil, err
	}
	d.Offset = start + consumed
	d.Size = info.Size()
	d.ModTime = info.ModTime()
	if d.ID == "" {
		// Claude Code session files are named <uuid>.jsonl.
		d.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	if d.Title == "" {
		d.Title = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = info.ModTime()
	}
	d.finalize()
	return d, nil
}

// ---- event types -----------------------------------------------------------

// ccEvent is the subset of a Claude Code transcript line we need. Unknown
// types and fields are ignored by design — the format evolves with Claude
// Code versions.
type ccEvent struct {
	Type        string     `json:"type"`
	Timestamp   string     `json:"timestamp"`
	SessionID   string     `json:"sessionId"`
	Cwd         string     `json:"cwd"`
	Title       string     `json:"title"`
	IsSidechain *bool      `json:"isSidechain"`
	Message     *ccMessage `json:"message"`
}

type ccMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []ccContentBlock
}

type ccContentBlock struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
}

// ccToolInput covers the tool-argument shapes that name a file or carry a
// todo list.
type ccToolInput struct {
	FilePath string       `json:"file_path"`
	Path     string       `json:"path"` // used by Grep/Glob (directory, not a concrete file)
	NewTodos []ccTodoItem `json:"newTodos"`
	OldTodos []ccTodoItem `json:"oldTodos"`
}

type ccTodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// ccNoisePrefixes match non-human user entries in Claude Code (source:
// parse-cc extractFirstUserMessage, confirmed by adityabawankule.io).
var ccNoisePrefixes = []string{
	"<local-command-stdout>",
	"<local-command-caveat>",
	"<system-reminder>",
	"[Request interrupted",
	"<command-message>",
	"<command-name>",
}

// ---- line-level streaming --------------------------------------------------

// ccParser streams a Claude Code transcript line by line, updating the
// digest in place. It mirrors ParseFile's consume loop but applies ccEvent
// semantics instead of the OMP event struct.
type ccParser struct {
	d *Digest
}

func (p *ccParser) consume(file *os.File) (int64, error) {
	reader := bufio.NewReaderSize(file, 256<<10)
	var consumed int64
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			consumed += int64(len(line))
			if len(line) <= maxLineBytes {
				p.apply(line)
			}
		}
		if err != nil {
			if err == io.EOF {
				return consumed, nil
			}
			return consumed, nil
		}
	}
}

func (p *ccParser) apply(line []byte) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || trimmed[0] != '{' {
		return
	}
	var ev ccEvent
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return
	}
	// Timestamps. The day is carried into the counters so a Claude Code session
	// gets the same per-day histogram as an OMP one: the field is about dated
	// work, not about either runtime's event shape.
	day := ""
	if ts, ok := parseTime(ev.Timestamp); ok {
		if p.d.StartedAt.IsZero() || ts.Before(p.d.StartedAt) {
			p.d.StartedAt = ts
		}
		if ts.After(p.d.UpdatedAt) {
			p.d.UpdatedAt = ts
		}
		day = ts.Local().Format(activityDateLayout)
	}

	// Session metadata.
	if ev.SessionID != "" && p.d.ID == "" {
		p.d.ID = ev.SessionID
	}
	if ev.Cwd != "" {
		p.d.Cwd = ev.Cwd
	}
	if ev.Type == "custom-title" && ev.Title != "" {
		p.d.Title = ev.Title
	}

	// User turns.
	if ev.Type == "user" && ev.Message != nil {
		if !isCCSidechain(ev.IsSidechain) && isRealCCTurn(ev.Message.Content) {
			p.d.UserTurns++
			p.d.countActivity(day)
		}
	}

	// Assistant entries carry tool calls.
	if ev.Type == "assistant" && ev.Message != nil {
		p.countToolCalls(ev.Message.Content, day)
	}
}

// ---- user-turn classification ----------------------------------------------

func isCCSidechain(sc *bool) bool {
	return sc != nil && *sc
}

// isRealCCTurn decides whether a user-role message is a human turn or an
// injected tool result / sidechain prompt.
//
// Claude Code records tool results as user-role messages whose content is
// an array containing tool_result blocks. Sidechain prompts (isSidechain:true
// user entries) are system-generated context for subagents and must not count
// as human turns. Noise entries (local-command-stdout, system-reminder, etc.)
// are also excluded.
//
// Source: parse-cc extractFirstUserMessage, adityabawankule.io.
func isRealCCTurn(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	// Plain string = typed prompt.
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return false
		}
		for _, prefix := range ccNoisePrefixes {
			if strings.HasPrefix(text, prefix) {
				return false
			}
		}
		return true
	}

	// Array of content blocks.
	var blocks []ccContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return false
	}
	hasText := false
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false // tool results are not human turns
		}
		if b.Type == "text" {
			hasText = true
		}
	}
	if !hasText {
		return false
	}
	// Check text blocks for noise.
	for _, b := range blocks {
		if b.Type == "text" {
			for _, prefix := range ccNoisePrefixes {
				if strings.HasPrefix(b.Text, prefix) {
					return false
				}
			}
		}
	}
	return true
}

// ---- tool-call counting and path classification ----------------------------

func (p *ccParser) countToolCalls(raw json.RawMessage, day string) {
	if len(raw) == 0 {
		return
	}
	var blocks []ccContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "tool_use" || b.Name == "" {
			continue
		}
		p.d.ToolCalls[b.Name]++
		p.d.countActivity(day)
		p.addPaths(b.Name, b.Input)
	}
}

func (p *ccParser) addPaths(tool string, input json.RawMessage) {
	if len(input) == 0 {
		return
	}
	var args ccToolInput
	if err := json.Unmarshal(input, &args); err != nil {
		return
	}

	// TodoWrite carries no file path; handle it before the filePath guard.
	if tool == "TodoWrite" {
		p.applyTodoWrite(args)
		return
	}

	// Claude Code uses "file_path" as the canonical key for file-targeting
	// tools. "path" is a fallback for older transcripts, but ONLY for tools
	// that target files — Grep and Glob use "path" for a directory.
	filePath := args.FilePath
	if filePath == "" {
		filePath = args.Path
	}
	if filePath == "" {
		return
	}

	switch tool {
	case "Read":
		p.d.addPath(&p.d.Reads, filePath)
	case "Write":
		p.d.addPath(&p.d.Writes, filePath)
	case "Edit", "MultiEdit":
		p.d.addPath(&p.d.Edits, filePath)
		// Bash, Grep, Glob: deliberately excluded — their arguments are
		// commands and patterns, not concrete file paths.
	}
}

// ---- todo handling ---------------------------------------------------------

// applyTodoWrite sets the digest's todo list from the last TodoWrite call.
// TodoWrite carries the whole list state, not an operation stream; the last
// call replaces any prior list. TodoCompleted and TodoReplans stay zero
// because nothing is lost on replacement — there is no earlier-list history
// to carry forward.
func (p *ccParser) applyTodoWrite(args ccToolInput) {
	if len(args.NewTodos) == 0 {
		p.d.Todos = nil
		return
	}
	p.d.Todos = make([]TodoItem, 0, len(args.NewTodos))
	for _, t := range args.NewTodos {
		if len(p.d.Todos) >= MaxTodos {
			p.d.TodosTruncated = true
			return
		}
		p.d.Todos = append(p.d.Todos, TodoItem{
			Content: t.Content,
			Status:  mapCCStatus(t.Status),
			// Phase is empty: Claude Code has no phases.
			// Blocker is empty: Claude Code todo items carry no reason field.
		})
	}
}

// mapCCStatus translates Claude Code todo statuses to the shared constants.
// Statuses confirmed by parse-cc and the Anthropic agent SDK docs.
// Unknown future statuses are treated as pending.
func mapCCStatus(status string) string {
	switch status {
	case "pending":
		return TodoPending
	case "in_progress":
		return TodoInProgress
	case "completed":
		return TodoCompleted
	case "cancelled":
		return TodoDropped
	default:
		return TodoPending
	}
}
