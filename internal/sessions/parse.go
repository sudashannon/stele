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
// payload. A session that exceeds them is still indexed; only the tail of the
// overflowing list is dropped, and Digest records that it was truncated.
const (
	MaxIntents     = 60
	MaxIntentChars = 4000
	MaxPaths       = 400
)

// maxLineBytes bounds a single JSONL line. Tool results embed whole file
// contents, so lines of several megabytes are normal; anything beyond this is
// skipped rather than buffered.
const maxLineBytes = 8 << 20

// Digest is everything the wiki layer needs about one session. Field values
// are derived, never transcript bytes.
//
// Writes and Edits are kept apart because they answer different questions:
// `write` creates or overwrites a file (the session produced it), while `edit`
// patches one that already existed (the session changed it).
type Digest struct {
	ID        string         `json:"id"`
	Path      string         `json:"path"`
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

	IntentsTruncated bool `json:"intentsTruncated,omitempty"`
	PathsTruncated   bool `json:"pathsTruncated,omitempty"`

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
	if ts, ok := parseTime(ev.Ts); ok {
		if d.StartedAt.IsZero() || ts.Before(d.StartedAt) {
			d.StartedAt = ts
		}
		if ts.After(d.UpdatedAt) {
			d.UpdatedAt = ts
		}
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
		d.applyMessage(ev)
	}
}

func (d *Digest) applyMessage(ev event) {
	if ev.Message == nil {
		return
	}
	if ev.Message.Role == "user" {
		d.UserTurns++
	}
	for _, part := range ev.Message.Content {
		if part.Type != "toolCall" || part.Name == "" {
			continue
		}
		d.ToolCalls[part.Name]++
		d.addIntent(part.Intent)
		d.addToolPaths(part.Name, part.Arguments)
	}
}

func (d *Digest) addIntent(intent string) {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return
	}
	if len(d.Intents) > 0 && d.Intents[len(d.Intents)-1] == intent {
		return // collapse a repeated intent across retries
	}
	if len(d.Intents) >= MaxIntents {
		d.IntentsTruncated = true
		return
	}
	d.Intents = append(d.Intents, intent)
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
	total := 0
	for index, intent := range d.Intents {
		total += len(intent)
		if total > MaxIntentChars {
			d.Intents = d.Intents[:index]
			d.IntentsTruncated = true
			break
		}
	}
	if d.StartedAt.IsZero() {
		d.StartedAt = d.UpdatedAt
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
