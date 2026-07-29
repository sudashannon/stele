package trellis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeTaskFixture(t *testing.T, dir string, task map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanProgressAndContextReferences(t *testing.T) {
	root := t.TempDir()
	tasks := filepath.Join(root, ".trellis", "tasks")
	parentDir := filepath.Join(tasks, "07-26-parent")
	writeTaskFixture(t, parentDir, map[string]any{
		"id": "parent", "title": "Parent", "status": StatusInProgress,
		"createdAt": "2026-07-26", "subtasks": []string{"07-25-child"},
	})
	childDir := filepath.Join(tasks, "archive", "2026-07", "07-25-child")
	writeTaskFixture(t, childDir, map[string]any{
		"id": "child", "title": "Child", "status": StatusCompleted,
		"createdAt": "2026-07-25", "completedAt": "2026-07-26",
	})
	leafDir := filepath.Join(tasks, "07-26-leaf")
	writeTaskFixture(t, leafDir, map[string]any{
		"id": "leaf", "title": "Leaf", "status": StatusPlanning,
		"createdAt": "2026-07-26",
	})
	if err := os.WriteFile(filepath.Join(leafDir, "prd.md"), []byte("- [x] done\n- [ ] pending\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	context := "{\"file\":\".trellis/spec/backend/index.md\",\"reason\":\"contract\"}\n" +
		"{\"example\":\"seed row\"}\n"
	if err := os.WriteFile(filepath.Join(leafDir, "implement.jsonl"), []byte(context), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("expected three tasks, got %d", len(records))
	}
	parent, err := Find(root, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.Task.Children) != 1 || parent.Task.Children[0] != "07-25-child" {
		t.Fatalf("legacy subtasks were not normalized: %+v", parent.Task.Children)
	}
	completed, total := Progress(parent, records)
	if completed != 1 || total != 1 {
		t.Fatalf("parent progress mismatch: %d/%d", completed, total)
	}
	leaf, err := Find(root, "07-26-leaf")
	if err != nil {
		t.Fatal(err)
	}
	completed, total = Progress(leaf, records)
	if completed != 1 || total != 2 {
		t.Fatalf("leaf PRD progress mismatch: %d/%d", completed, total)
	}
	refs, err := ContextReferences(leaf)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != ".trellis/spec/backend/index.md" {
		t.Fatalf("unexpected context refs: %v", refs)
	}
	archived, err := Find(root, "child")
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.Task.CompletedAt != "2026-07-26" {
		t.Fatalf("archived task metadata mismatch: %+v", archived)
	}
	if ProjectRoot(parent) != root || ProjectRoot(archived) != root {
		t.Fatalf("project root must survive archive movement: active=%q archived=%q", ProjectRoot(parent), ProjectRoot(archived))
	}
}

func TestReadTaskRejectsUnknownStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.json")
	if err := os.WriteFile(path, []byte(`{"id":"x","status":"unknown"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadTask(path); err == nil {
		t.Fatal("expected unknown Trellis status to be rejected")
	}
}

func TestResolveReferenceRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, ".trellis", "spec", "index.md")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("# Spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := ResolveReference(root, ".trellis/spec/index.md"); !ok || got != inside {
		t.Fatalf("expected in-root reference, got %q ok=%v", got, ok)
	}
	if got, ok := ResolveReference(root, "../outside.md"); ok || got != "" {
		t.Fatalf("traversal must be rejected, got %q ok=%v", got, ok)
	}
}

func TestScanWithoutTasksDirectoryReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".trellis", "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected empty task set, got %+v", records)
	}
}
