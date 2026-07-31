package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// transcript writes a JSONL transcript from raw lines and returns its path.
func transcript(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sessionLine(id, cwd, title, ts string) string {
	return fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":%q,"cwd":%q,"title":%q}`, id, ts, cwd, title)
}

func toolCallLine(ts, tool, intent string, args map[string]any) string {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"type":"message","timestamp":%q,"message":{"role":"assistant","content":[{"type":"toolCall","name":%q,"intent":%q,"arguments":%s}]}}`,
		ts, tool, intent, raw)
}

func userLine(ts, text string) string {
	return fmt.Sprintf(`{"type":"message","timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":%q}]}}`, ts, text)
}

func TestParseFileExtractsMetaTurnsAndPaths(t *testing.T) {
	dir := t.TempDir()
	cwd := "/repo"
	path := transcript(t, dir, "2026-07-30T00-00-00-000Z_abc.jsonl",
		`{"type":"title","v":1,"title":"early title"}`,
		sessionLine("sess-1", cwd, "Fix PCIe bring-up", "2026-07-30T01:00:00.000Z"),
		userLine("2026-07-30T01:00:05.000Z", "看下 pcie"),
		toolCallLine("2026-07-30T01:00:06.000Z", "read", "读取设计", map[string]any{"path": "docs/design.md:10-40"}),
		toolCallLine("2026-07-30T01:00:07.000Z", "edit", "改实现", map[string]any{"input": "[wiki/api.go#1A2B]\nSWAP 1.=1:\n+x\n"}),
		toolCallLine("2026-07-30T01:00:08.000Z", "write", "写报告", map[string]any{"path": "/abs/report.md"}),
		toolCallLine("2026-07-30T01:00:09.000Z", "bash", "跑测试", map[string]any{"command": "go test ./..."}),
		toolCallLine("2026-07-30T01:00:10.000Z", "grep", "搜索", map[string]any{"path": "wiki"}),
		userLine("2026-07-30T01:02:00.000Z", "继续"),
	)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if digest.ID != "sess-1" || digest.Cwd != cwd {
		t.Fatalf("meta = %q/%q", digest.ID, digest.Cwd)
	}
	if digest.Title != "Fix PCIe bring-up" {
		t.Fatalf("session header title must win over the rotated title line, got %q", digest.Title)
	}
	if digest.UserTurns != 2 {
		t.Fatalf("UserTurns = %d, want 2", digest.UserTurns)
	}
	if got, want := digest.ToolCalls["bash"], 1; got != want {
		t.Fatalf("bash count = %d, want %d", got, want)
	}
	if want := []string{"/repo/docs/design.md"}; !equal(digest.Reads, want) {
		t.Fatalf("Reads = %v, want %v (selector stripped, cwd-resolved)", digest.Reads, want)
	}
	if want := []string{"/repo/wiki/api.go"}; !equal(digest.Edits, want) {
		t.Fatalf("Edits = %v, want %v (edit patch headers only)", digest.Edits, want)
	}
	if want := []string{"/abs/report.md"}; !equal(digest.Writes, want) {
		t.Fatalf("Writes = %v, want %v (write creates or overwrites a file)", digest.Writes, want)
	}
	for _, path := range append(digest.Reads, digest.Edits...) {
		if path == "/repo/wiki" || path == "wiki" {
			t.Fatalf("grep/bash arguments must not become paths: %v", digest)
		}
	}
	if len(digest.Intents) != 5 || digest.Intents[0] != "读取设计" {
		t.Fatalf("Intents = %v", digest.Intents)
	}
	if !digest.StartedAt.Equal(time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("StartedAt = %v", digest.StartedAt)
	}
	if !digest.UpdatedAt.Equal(time.Date(2026, 7, 30, 1, 2, 0, 0, time.UTC)) {
		t.Fatalf("UpdatedAt = %v", digest.UpdatedAt)
	}
}

func TestParseFileRejectsInternalURIsAndSkipsPartialTail(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "s.jsonl",
		sessionLine("sess-2", "/repo", "t", "2026-07-30T01:00:00.000Z"),
		toolCallLine("2026-07-30T01:00:01.000Z", "read", "读工具文档", map[string]any{"path": "xd://mcp__comet_wiki_wiki_read"}),
		toolCallLine("2026-07-30T01:00:02.000Z", "read", "读产物", map[string]any{"path": "artifact://1489"}),
	)
	// A transcript being appended to right now ends mid-line.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"message","timestamp":"2026-07-30T01:00:03.000Z","mess`); err != nil {
		t.Fatal(err)
	}
	file.Close()

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(digest.Reads) != 0 {
		t.Fatalf("internal URIs must not become paths, got %v", digest.Reads)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if digest.Offset >= info.Size() {
		t.Fatalf("Offset = %d must stop before the partial tail (size %d)", digest.Offset, info.Size())
	}
	if digest.ToolCalls["read"] != 2 {
		t.Fatalf("read count = %d, want 2", digest.ToolCalls["read"])
	}
}

func TestParseFileResumesFromOffsetAndMergesNewActivity(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "s.jsonl",
		sessionLine("sess-3", "/repo", "t", "2026-07-30T01:00:00.000Z"),
		toolCallLine("2026-07-30T01:00:01.000Z", "read", "第一次读", map[string]any{"path": "a.md"}),
	)
	first, err := ParseFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(toolCallLine("2026-07-30T02:00:00.000Z", "edit", "第二次改", map[string]any{"input": "[b.md#0000]\nDEL 1\n"}) + "\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()

	second, err := ParseFile(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if second.Offset <= first.Offset {
		t.Fatalf("resume must advance the offset: %d -> %d", first.Offset, second.Offset)
	}
	if !equal(second.Reads, []string{"/repo/a.md"}) {
		t.Fatalf("earlier reads must survive the resume, got %v", second.Reads)
	}
	if !equal(second.Edits, []string{"/repo/b.md"}) {
		t.Fatalf("appended edits must be merged, got %v", second.Edits)
	}
	if second.ToolCalls["read"] != 1 || second.ToolCalls["edit"] != 1 {
		t.Fatalf("counts must accumulate across resume: %v", second.ToolCalls)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("UpdatedAt must advance: %v -> %v", first.UpdatedAt, second.UpdatedAt)
	}
	if first.Reads == nil || len(first.Reads) != 1 {
		t.Fatalf("resume must not mutate the previous digest: %v", first.Reads)
	}
}

func TestParseFileRebuildsWhenTranscriptShrinks(t *testing.T) {
	dir := t.TempDir()
	path := transcript(t, dir, "s.jsonl",
		sessionLine("sess-4", "/repo", "t", "2026-07-30T01:00:00.000Z"),
		toolCallLine("2026-07-30T01:00:01.000Z", "read", "读旧文件", map[string]any{"path": "old.md"}),
	)
	first, err := ParseFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	rotated := transcript(t, dir, "s.jsonl",
		sessionLine("sess-4", "/repo", "t2", "2026-07-30T03:00:00.000Z"),
	)
	if rotated != path {
		t.Fatalf("rewrite must reuse the path")
	}
	second, err := ParseFile(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Reads) != 0 {
		t.Fatalf("a shrunk transcript must be reparsed from scratch, got %v", second.Reads)
	}
	if second.Title != "t2" {
		t.Fatalf("Title = %q, want t2", second.Title)
	}
}

func TestDigestCapsIntentsAndPaths(t *testing.T) {
	dir := t.TempDir()
	lines := []string{sessionLine("sess-5", "/repo", "t", "2026-07-30T01:00:00.000Z")}
	for i := 0; i < MaxIntents+MaxPaths+20; i++ {
		lines = append(lines, toolCallLine("2026-07-30T01:00:00.000Z", "read",
			fmt.Sprintf("意图-%d", i), map[string]any{"path": fmt.Sprintf("f%d.md", i)}))
	}
	path := transcript(t, dir, "s.jsonl", lines...)

	digest, err := ParseFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.Intents) > MaxIntents || !digest.IntentsTruncated {
		t.Fatalf("intents = %d truncated=%v", len(digest.Intents), digest.IntentsTruncated)
	}
	if len(digest.Reads) > MaxPaths || !digest.PathsTruncated {
		t.Fatalf("reads = %d truncated=%v", len(digest.Reads), digest.PathsTruncated)
	}
}

func TestNormalizePath(t *testing.T) {
	for _, tc := range []struct {
		raw, cwd, want string
		ok             bool
	}{
		{"docs/a.md", "/repo", "/repo/docs/a.md", true},
		{"/abs/a.md", "/repo", "/abs/a.md", true},
		{"a.md:10-20", "/repo", "/repo/a.md", true},
		{"a.md:raw", "/repo", "/repo/a.md", true},
		{"a.go:2-4:raw", "/repo", "/repo/a.go", true},
		{"xd://tool", "/repo", "", false},
		{"https://example.com/a.md", "/repo", "", false},
		{"relative.md", "", "", false},
		{"   ", "/repo", "", false},
	} {
		got, ok := NormalizePath(tc.raw, tc.cwd)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("NormalizePath(%q, %q) = %q,%v want %q,%v", tc.raw, tc.cwd, got, ok, tc.want, tc.ok)
		}
	}
}

func TestStoreRefreshCachesAndDropsMissing(t *testing.T) {
	root := t.TempDir()
	bucket := filepath.Join(root, "-repo")
	first := transcript(t, bucket, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "A", "2026-07-30T01:00:00.000Z"),
		toolCallLine("2026-07-30T01:00:01.000Z", "read", "读 A", map[string]any{"path": "a.md"}),
	)
	transcript(t, bucket, "2026-07-30T00-00-00-000Z_b.jsonl",
		sessionLine("b", "/repo", "B", "2026-07-30T02:00:00.000Z"),
	)
	// A tool-artifact directory beside a transcript must not be descended into.
	if err := os.MkdirAll(filepath.Join(bucket, "2026-07-30T00-00-00-000Z_a", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	transcript(t, filepath.Join(bucket, "2026-07-30T00-00-00-000Z_a", "nested"), "deep.jsonl", `{"type":"session","id":"deep"}`)

	store := NewStore(filepath.Join(root, "sessions.json"))
	changed, total, err := store.Refresh(root)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(changed) != 2 || total != 2 {
		t.Fatalf("changed=%d total=%d, want 2/2 (nested transcripts are not discovered)", len(changed), total)
	}
	if list := store.List(); list[0].ID != "b" {
		t.Fatalf("List must sort by newest activity first, got %q", list[0].ID)
	}

	changed, total, err = store.Refresh(root)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(changed) != 0 || total != 2 {
		t.Fatalf("unchanged transcripts must not reparse: changed=%d total=%d", len(changed), total)
	}

	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if _, total, err = store.Refresh(root); err != nil || total != 1 {
		t.Fatalf("deleted transcript must drop from the store: total=%d err=%v", total, err)
	}
	if _, ok := store.Get(first); ok {
		t.Fatalf("Get must miss after the transcript disappears")
	}
}

func TestStoreSaveReloadRoundTrip(t *testing.T) {
	root := t.TempDir()
	transcript(t, filepath.Join(root, "-repo"), "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "A", "2026-07-30T01:00:00.000Z"),
		toolCallLine("2026-07-30T01:00:01.000Z", "read", "读 A", map[string]any{"path": "a.md"}),
	)
	cachePath := filepath.Join(root, "cache", "sessions.json")
	store := NewStore(cachePath)
	if _, _, err := store.Refresh(root); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := NewStore(cachePath)
	digest, ok := reloaded.Get(filepath.Join(root, "-repo", "2026-07-30T00-00-00-000Z_a.jsonl"))
	if !ok {
		t.Fatalf("reloaded store must serve the cached digest")
	}
	if !equal(digest.Reads, []string{"/repo/a.md"}) || digest.Offset == 0 {
		t.Fatalf("cached digest lost data: %+v", digest)
	}
	// A reloaded, unchanged transcript must not be reparsed.
	changed, _, err := reloaded.Refresh(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("cache must survive a restart, reparsed %d", len(changed))
	}
}

func TestNewStoreRejectsForeignSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":99,"sessions":{"/x":{"id":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if _, ok := store.Get("/x"); ok {
		t.Fatalf("a future schema version must not be loaded")
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
