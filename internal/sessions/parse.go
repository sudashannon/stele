// Package sessions distills OMP agent session transcripts into small,
// queryable digests.
//
// A transcript is a JSONL file where every line is one event; in production
// these reach hundreds of megabytes, and measurement on a real 30 MB session
// found only 0.21% of the bytes to be human-authored prose (2481 messages, of
// which 1204 were tool results). Nothing here may therefore hold a whole file
// in memory, and nothing may hand transcript bytes to a downstream consumer:
// the parser streams line by line and keeps only counts, tool intents, and the
// file paths a session read or edited.
package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Caps keep one pathological session from dominating memory or the API
// payload. A session that exceeds them is still indexed and Digest records the
// truncation.
//
// Intents keep the most RECENT calls, not the first ones. A resumed session runs
// for days - one measured transcript made 2573 intent-bearing tool calls over
// 109.5 hours - so a head-biased cap answered "what was this session doing" with
// a 0.9-hour window from four days before it ended, while every surface labelled
// it the latest activity. Paths and todos are sets rather than a timeline, so
// they still drop the overflowing tail.
const (
	MaxIntents     = 60
	MaxIntentChars = 4000
	MaxPaths       = 400
)

// maxLineBytes bounds a single JSONL line. Tool results embed whole file
// contents, so lines of several megabytes are normal; anything beyond this is
// skipped rather than buffered.
const maxLineBytes = 8 << 20

// activityDateLayout keys the per-day activity histogram.
const activityDateLayout = "2006-01-02"

// Digest is everything the wiki layer needs about one session. Field values
// are derived, never transcript bytes.
//
// Writes and Edits are kept apart because they answer different questions:
// `write` creates or overwrites a file (the session produced it), while `edit`
// patches one that already existed (the session changed it).
type Digest struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	// Source names the provider that produced this digest ("omp"). It travels
	// with the digest so one cache can hold several runtimes and callers can
	// filter by runtime without consulting configuration.
	Source    string         `json:"source,omitempty"`
	Cwd       string         `json:"cwd"`
	Title     string         `json:"title"`
	StartedAt time.Time      `json:"startedAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	UserTurns int            `json:"userTurns"`
	ToolCalls map[string]int `json:"toolCalls,omitempty"`
	Writes    []string       `json:"writes,omitempty"`
	Edits     []string       `json:"edits,omitempty"`
	Reads     []string       `json:"reads,omitempty"`
	Intents   []string       `json:"intents,omitempty"`
	// Subagents names the nested transcripts folded into this digest. A
	// subagent's work is part of the session that dispatched it, so its tool
	// calls and touched paths merge into these totals (see Merge).
	Subagents []string `json:"subagents,omitempty"`
	// Todos is the session's own task tracker as it stood when the transcript
	// ended, replayed from the tracker operations the session issued.
	Todos []TodoItem `json:"todos,omitempty"`
	// TodosCompleted holds tasks the session finished under an *earlier* list
	// and are therefore absent from Todos. A long session re-plans repeatedly
	// (`init` replaces the list), so the final list alone hides most of what it
	// actually got done; together the two answer "what is it tracking now" and
	// "what did it finish".
	TodosCompleted []string `json:"todosCompleted,omitempty"`
	// TodoReplans counts how many times the session threw its list away and
	// started a new one, which is why a long session can end with a short list.
	TodoReplans int `json:"todoReplans,omitempty"`
	// Activity counts a day's user turns and tool calls, keyed by local date
	// (YYYY-MM-DD). A resumed session runs across days - one measured transcript
	// spans 109.5 hours - so StartedAt→UpdatedAt is a range, not a duration, and
	// a single "last active" day hides that the work happened on five of them.
	// Local dates because every consumer (panel grouping, calendar) renders in
	// the viewer's day, and the panel and binary are the same machine.
	Activity map[string]int `json:"activity,omitempty"`

	IntentsTruncated bool `json:"intentsTruncated,omitempty"`
	PathsTruncated   bool `json:"pathsTruncated,omitempty"`
	TodosTruncated   bool `json:"todosTruncated,omitempty"`

	// Resume state. Offset is the byte position just past the last complete
	// line consumed, so an appended-to transcript is parsed from there instead
	// of re-read from the start.
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Offset  int64     `json:"offset"`
}

// event is the subset of a transcript line this package understands. Unknown
// types and unknown fields are ignored by design: the transcript format is
// owned by another product and gains event types over time.
type event struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
	ID      string `json:"id"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title"`
	Ts      string `json:"timestamp"`

	Message *struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string          `json:"type"`
			Name      string          `json:"name"`
			Intent    string          `json:"intent"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"content"`
	} `json:"message"`
}

// toolArgs covers the argument shapes that name a file. `input` is the edit
// tool's patch body, which carries its own `[path#tag]` section headers.
type toolArgs struct {
	Path  string `json:"path"`
	Input string `json:"input"`
}

// editHeaderRE matches an edit-tool section header, e.g. `[wiki/api.go#1A2B]`.
var editHeaderRE = regexp.MustCompile(`(?m)^\[([^\]\n]+)#[0-9A-Fa-f]{4}\]\s*$`)

// selectorRE strips a read selector from a path: `foo.go:50-200`, `foo.go:raw`,
// `foo.go:2-4:raw`, `db.sqlite:users:42`.
var selectorRE = regexp.MustCompile(`:(?:raw|conflicts|[0-9][0-9,\-+]*)$`)

// TodoItem is one entry of a session's task tracker after replay.
type TodoItem struct {
	Phase   string `json:"phase,omitempty"`
	Content string `json:"content"`
	// Status uses the tracker's own vocabulary: pending, in_progress,
	// completed, dropped or blocked.
	Status string `json:"status"`
	// Blocker is why the task is stuck, as the session recorded it. For a
	// blocked task this is the only actionable part - "blocked" alone says
	// nothing a reader can act on.
	Blocker string `json:"blocker,omitempty"`
}

// Tracker statuses, in the tracker's vocabulary.
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoCompleted  = "completed"
	TodoDropped    = "dropped"
	TodoBlocked    = "blocked"
)

// MaxTodos bounds a replayed list. A session that tracks more than this is
// still indexed; the tail is dropped and Digest records the truncation.
const MaxTodos = 200

// todoArgs is the tracker tool's argument shape. Operation names are OMP's, so
// this replay lives with the OMP parser: another runtime records its task list
// differently (a whole-state array rather than operations) and its provider
// fills Digest.Todos its own way.
type todoArgs struct {
	Op     string   `json:"op"`
	Task   string   `json:"task"`
	Phase  string   `json:"phase"`
	Items  []string `json:"items"`
	Reason string   `json:"reason"`
	List   []struct {
		Phase string   `json:"phase"`
		Items []string `json:"items"`
	} `json:"list"`
}

// ParseFile builds a digest for path. When prev describes the same file and
// the file only grew, parsing resumes from prev.Offset and merges into a copy
// of prev; otherwise the file is parsed from the start.
func ParseFile(path string, prev *Digest) (*Digest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sessions: %s is not a regular file", path)
	}

	digest := &Digest{Path: path, ToolCalls: map[string]int{}}
	var start int64
	if resumable(prev, info) {
		digest = prev.clone()
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

	consumed, err := digest.consume(file)
	if err != nil {
		return nil, err
	}
	digest.Offset = start + consumed
	digest.Size = info.Size()
	digest.ModTime = info.ModTime()
	if digest.ID == "" {
		digest.ID = sessionIDFromName(path)
	}
	if digest.Title == "" {
		digest.Title = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	if digest.UpdatedAt.IsZero() {
		digest.UpdatedAt = info.ModTime()
	}
	digest.finalize()
	return digest, nil
}

// resumable reports whether prev can be extended instead of rebuilt. A file
// that shrank was rotated or rewritten, so its digest is no longer valid.
func resumable(prev *Digest, info os.FileInfo) bool {
	return prev != nil && prev.Offset > 0 && info.Size() >= prev.Offset
}

func (d *Digest) clone() *Digest {
	out := *d
	out.ToolCalls = make(map[string]int, len(d.ToolCalls))
	for name, count := range d.ToolCalls {
		out.ToolCalls[name] = count
	}
	out.Writes = append([]string(nil), d.Writes...)
	out.Reads = append([]string(nil), d.Reads...)
	out.Edits = append([]string(nil), d.Edits...)
	out.Intents = append([]string(nil), d.Intents...)
	out.Subagents = append([]string(nil), d.Subagents...)
	out.Todos = append([]TodoItem(nil), d.Todos...)
	out.TodosCompleted = append([]string(nil), d.TodosCompleted...)
	if d.Activity != nil {
		out.Activity = make(map[string]int, len(d.Activity))
		for day, count := range d.Activity {
			out.Activity[day] = count
		}
	}
	return &out
}

// consume reads complete lines and returns the number of bytes consumed. A
// trailing partial line (a transcript being appended to right now) is left
// unconsumed so the next parse sees it whole.
func (d *Digest) consume(file *os.File) (int64, error) {
	reader := bufio.NewReaderSize(file, 256<<10)
	var consumed int64
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			consumed += int64(len(line))
			if len(line) <= maxLineBytes {
				d.apply(line)
			}
		}
		if err != nil {
			return consumed, nil
		}
	}
}

func (d *Digest) apply(line []byte) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || trimmed[0] != '{' {
		return
	}
	var ev event
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return
	}
	day := ""
	if ts, ok := parseTime(ev.Ts); ok {
		if d.StartedAt.IsZero() || ts.Before(d.StartedAt) {
			d.StartedAt = ts
		}
		if ts.After(d.UpdatedAt) {
			d.UpdatedAt = ts
		}
		day = ts.Local().Format(activityDateLayout)
	}

	switch ev.Type {
	case "session":
		if ev.ID != "" {
			d.ID = ev.ID
		}
		if ev.Cwd != "" {
			d.Cwd = ev.Cwd
		}
		if ev.Title != "" {
			d.Title = ev.Title
		}
	case "title", "title_change":
		if ev.Title != "" {
			d.Title = ev.Title
		}
	case "message":
		d.applyMessage(ev, day)
	}
}

// countActivity attributes one unit of work to a day. Undated events are
// skipped rather than bucketed under "today": a transcript line without a
// timestamp cannot be placed, and guessing would smear old work into the
// present, which is exactly the confusion the histogram exists to fix.
func (d *Digest) countActivity(day string) {
	if day == "" {
		return
	}
	if d.Activity == nil {
		d.Activity = map[string]int{}
	}
	d.Activity[day]++
}

func (d *Digest) applyMessage(ev event, day string) {
	if ev.Message == nil {
		return
	}
	if ev.Message.Role == "user" {
		d.UserTurns++
		d.countActivity(day)
	}
	for _, part := range ev.Message.Content {
		if part.Type != "toolCall" || part.Name == "" {
			continue
		}
		d.ToolCalls[part.Name]++
		d.countActivity(day)
		d.addIntent(part.Intent)
		d.addToolPaths(part.Name, part.Arguments)
		if part.Name == "todo" {
			d.applyTodo(part.Arguments)
		}
	}
}

// applyTodo replays one tracker operation onto the session's list.
//
// The list is state, not a log: `init` replaces it wholesale. Replacing it would
// erase everything the session had already finished, so completed tasks are
// carried into TodosCompleted first - that history is the point of the record.
//
// Auto-promotion (the tracker moving the next open task to in_progress on each
// completion) is deliberately not modelled: it is a display rule of the tool,
// and inferring it here would invent status the transcript never recorded.
func (d *Digest) applyTodo(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var args todoArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return
	}
	switch args.Op {
	case "init":
		if len(d.Todos) > 0 {
			d.TodoReplans++
			d.retireCompleted()
		}
		d.Todos = nil
		for _, group := range args.List {
			for _, item := range group.Items {
				d.addTodo(group.Phase, item)
			}
		}
		for _, item := range args.Items {
			d.addTodo("", item)
		}
	case "append":
		for _, item := range args.Items {
			d.addTodo(args.Phase, item)
		}
	case "start":
		d.setTodoStatus(args, TodoInProgress)
	case "done":
		d.setTodoStatus(args, TodoCompleted)
	case "drop":
		d.setTodoStatus(args, TodoDropped)
	case "block":
		d.setTodoStatus(args, TodoBlocked)
	case "unblock":
		d.setTodoStatus(args, TodoPending)
	case "rm":
		d.retireCompleted()
		d.removeTodo(args)
	}
}

// retireCompleted moves finished tasks into the historical record before the
// current list is replaced or cleared. Contents are deduplicated: a session that
// re-plans the same task repeatedly finished it once.
func (d *Digest) retireCompleted() {
	for _, item := range d.Todos {
		if item.Status != TodoCompleted {
			continue
		}
		if len(d.TodosCompleted) >= MaxTodos {
			d.TodosTruncated = true
			return
		}
		d.TodosCompleted = appendUnique(d.TodosCompleted, item.Content)
	}
}

func (d *Digest) addTodo(phase, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	for _, existing := range d.Todos {
		if existing.Content == content && existing.Phase == phase {
			return
		}
	}
	if len(d.Todos) >= MaxTodos {
		d.TodosTruncated = true
		return
	}
	d.Todos = append(d.Todos, TodoItem{Phase: phase, Content: content, Status: TodoPending})
}

// setTodoStatus applies a status to one task or to every task in a phase, which
// is how the tracker addresses work. A block carries its reason: for a stuck
// task that reason is the only thing a reader can act on, and leaving it in the
// transcript makes the record say "blocked" and nothing more. Any other status
// clears it, because a task that moved on is no longer blocked by it.
func (d *Digest) setTodoStatus(args todoArgs, status string) {
	task := strings.TrimSpace(args.Task)
	phase := strings.TrimSpace(args.Phase)
	blocker := ""
	if status == TodoBlocked {
		blocker = strings.TrimSpace(args.Reason)
	}
	for index := range d.Todos {
		if task != "" && d.Todos[index].Content != task {
			continue
		}
		if task == "" && (phase == "" || d.Todos[index].Phase != phase) {
			continue
		}
		d.Todos[index].Status = status
		d.Todos[index].Blocker = blocker
	}
}

// removeTodo drops one task, a whole phase, or - with neither named - the list.
func (d *Digest) removeTodo(args todoArgs) {
	task := strings.TrimSpace(args.Task)
	phase := strings.TrimSpace(args.Phase)
	if task == "" && phase == "" {
		d.Todos = nil
		d.TodosTruncated = false
		return
	}
	kept := d.Todos[:0]
	for _, item := range d.Todos {
		if (task != "" && item.Content == task) || (task == "" && phase != "" && item.Phase == phase) {
			continue
		}
		kept = append(kept, item)
	}
	d.Todos = kept
}

// addIntent appends to a sliding window of the most recent intents. Dropping the
// oldest entry rather than refusing the newest is what makes "what was this
// session doing" answerable for a session that ran for days.
func (d *Digest) addIntent(intent string) {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return
	}
	if len(d.Intents) > 0 && d.Intents[len(d.Intents)-1] == intent {
		return // collapse a repeated intent across retries
	}
	d.Intents = append(d.Intents, intent)
	if len(d.Intents) > MaxIntents {
		d.Intents = d.Intents[len(d.Intents)-MaxIntents:]
		d.IntentsTruncated = true
	}
}

// addToolPaths records file paths from the tools whose arguments name a
// concrete file. Search and shell tools are deliberately excluded: their
// arguments are patterns and command lines, and guessing paths from them
// would attach a session to files it never opened.
func (d *Digest) addToolPaths(tool string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var args toolArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return
	}
	switch tool {
	case "read":
		d.addPath(&d.Reads, args.Path)
	case "write":
		d.addPath(&d.Writes, args.Path)
	case "edit":
		for _, match := range editHeaderRE.FindAllStringSubmatch(args.Input, -1) {
			d.addPath(&d.Edits, match[1])
		}
	}
}

func (d *Digest) addPath(list *[]string, raw string) {
	candidate, ok := NormalizePath(raw, d.Cwd)
	if !ok {
		return
	}
	for _, existing := range *list {
		if existing == candidate {
			return
		}
	}
	if len(*list) >= MaxPaths {
		d.PathsTruncated = true
		return
	}
	*list = append(*list, candidate)
}

// NormalizePath turns a tool argument into an absolute filesystem path.
// Internal URIs (xd://, artifact://, memory://, http://, …) are rejected, read
// selectors are stripped, and relative paths resolve against the session cwd.
func NormalizePath(raw, cwd string) (string, bool) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" || strings.Contains(candidate, "://") {
		return "", false
	}
	// A read may carry stacked selectors (`foo.go:2-4:raw`); strip repeatedly.
	for range 3 {
		stripped := selectorRE.ReplaceAllString(candidate, "")
		if stripped == candidate {
			break
		}
		candidate = stripped
	}
	if candidate == "" {
		return "", false
	}
	if !filepath.IsAbs(candidate) {
		if cwd == "" {
			return "", false
		}
		candidate = filepath.Join(cwd, candidate)
	}
	return filepath.Clean(candidate), true
}

// finalize enforces the payload caps and a stable order.
func (d *Digest) finalize() {
	if len(d.ToolCalls) == 0 {
		d.ToolCalls = nil
	}
	sort.Strings(d.Writes)
	sort.Strings(d.Edits)
	sort.Strings(d.Reads)
	d.trimIntentChars()
	if d.StartedAt.IsZero() {
		d.StartedAt = d.UpdatedAt
	}
}

// trimIntentChars enforces the character budget by dropping the OLDEST intents,
// keeping the window aligned with MaxIntents: both caps answer "what happened
// most recently", so trimming from the front would reintroduce the head bias.
func (d *Digest) trimIntentChars() {
	total := 0
	for index := len(d.Intents) - 1; index >= 0; index-- {
		total += len(d.Intents[index])
		if total > MaxIntentChars {
			d.Intents = d.Intents[index+1:]
			d.IntentsTruncated = true
			return
		}
	}
}

// sessionIDFromName recovers the session id from `<ISO>_<uuid>.jsonl`, the
// only place it appears when a transcript's header line was rotated away.
func sessionIDFromName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if index := strings.LastIndex(base, "_"); index >= 0 && index+1 < len(base) {
		return base[index+1:]
	}
	return base
}

func parseTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
