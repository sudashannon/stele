package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comet-ui/internal/source"
	"comet-ui/internal/trellis"
)

func writeMainTrellisTask(t *testing.T, dir, status string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"id": "beta", "title": "Beta Task", "description": "Trellis fixture",
		"status": status, "createdAt": "2026-07-26",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTrellisScannerMapsSummaryDetailAndLifecycle(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".trellis", "tasks", "07-26-beta")
	writeMainTrellisTask(t, dir, trellis.StatusInProgress)
	if err := os.WriteFile(filepath.Join(dir, "prd.md"), []byte("# Beta\n\n- [x] first\n- [ ] second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "design.md"), []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := WorkspaceConfig{Alias: "trellis", Path: root, Type: source.KindTrellis}

	changes, err := scanTrellisWorkspaceChanges(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one change, got %d", len(changes))
	}
	got := changes[0]
	if got.Name != "07-26-beta" || got.Title != "Beta Task" || got.SourceType != source.KindTrellis {
		t.Fatalf("unexpected summary identity: %+v", got)
	}
	if got.TasksCompleted != 1 || got.TasksTotal != 2 {
		t.Fatalf("unexpected progress: %d/%d", got.TasksCompleted, got.TasksTotal)
	}
	if got.NextTransition == nil || got.NextTransition.Target != trellis.StatusCompleted || got.NextTransition.BlockedReason == "" {
		t.Fatalf("expected blocked completion transition, got %+v", got.NextTransition)
	}
	if got.ComponentID != filepath.Join(dir, "task.json") || !got.Artifacts["design"] {
		t.Fatalf("unexpected graph/artifact mapping: %+v", got)
	}

	detail, err := scanTrellisChangeDetail(ws, got.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Phases) != 3 || detail.Phases[1].Status != "current" {
		t.Fatalf("unexpected lifecycle phases: %+v", detail.Phases)
	}
	if detail.Phases[2].Artifacts == nil {
		t.Fatal("empty phase artifacts must encode as [] rather than null")
	}
}

func TestTrellisTransitionSourceInvokesProjectCLI(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".trellis", "tasks", "07-26-beta")
	writeMainTrellisTask(t, dir, trellis.StatusPlanning)
	if err := os.WriteFile(filepath.Join(dir, "prd.md"), []byte("- [x] accepted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(root, ".trellis", "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "from pathlib import Path\nimport sys\nPath('.trellis/invocation.txt').write_text('\\n'.join(sys.argv[1:]))\n"
	if err := os.WriteFile(filepath.Join(scripts, "task.py"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := WorkspaceConfig{Alias: "trellis", Path: root, Type: source.KindTrellis}
	runner := trellisTransitionSource{}

	if err := runner.Preflight(ws, "07-26-beta", trellis.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	output, err := runner.Trigger(context.Background(), ws, "07-26-beta", trellis.StatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(output); err != nil {
		t.Fatal(err)
	}
	output.Close()
	invocation, err := os.ReadFile(filepath.Join(root, ".trellis", "invocation.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(invocation) != "start\n07-26-beta" {
		t.Fatalf("unexpected start invocation: %q", invocation)
	}

	writeMainTrellisTask(t, dir, trellis.StatusInProgress)
	if err := runner.Preflight(ws, "07-26-beta", trellis.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	output, err = runner.Trigger(context.Background(), ws, "07-26-beta", trellis.StatusCompleted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(output); err != nil {
		t.Fatal(err)
	}
	output.Close()
	invocation, err = os.ReadFile(filepath.Join(root, ".trellis", "invocation.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(invocation)) != "archive\n07-26-beta\n--no-commit" {
		t.Fatalf("unexpected archive invocation: %q", invocation)
	}
}
