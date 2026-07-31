// main_transition_test.go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stele/internal/source"
)

func TestHandleTransition_RejectsInvalidChangeName(t *testing.T) {
	lock := NewTransitionLock()
	body, _ := json.Marshal(map[string]string{"targetPhase": "build"})
	req := httptest.NewRequest("POST", "/api/changes/../etc/passwd/transition", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleTransition(w, req, "../etc/passwd", ".", lock, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path-traversal change name, got %d", w.Code)
	}
}

func TestHandleTransition_RejectsInvalidTargetPhase(t *testing.T) {
	lock := NewTransitionLock()
	body, _ := json.Marshal(map[string]string{"targetPhase": "invalid-phase"})
	req := httptest.NewRequest("POST", "/api/changes/my-change/transition", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleTransition(w, req, "my-change", ".", lock, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid phase, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTransition_ReturnsPreflightErrorWhenGuardMissing(t *testing.T) {
	t.Setenv("COMET_GUARD", "")
	t.Setenv("HOME", t.TempDir()) // no guard script anywhere

	lock := NewTransitionLock()
	body, _ := json.Marshal(map[string]string{"targetPhase": "build"})
	req := httptest.NewRequest("POST", "/api/changes/my-change/transition", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleTransition(w, req, "my-change", ".", lock, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when guard can't be resolved, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTransition_ReturnsConflictWhenLockHeld(t *testing.T) {
	lock := NewTransitionLock()
	lock.TryAcquire("my-change") // simulate an in-flight transition

	body, _ := json.Marshal(map[string]string{"targetPhase": "build"})
	req := httptest.NewRequest("POST", "/api/changes/my-change/transition", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleTransition(w, req, "my-change", ".", lock, nil)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 when a transition is already in flight, got %d", w.Code)
	}
}

func TestHandleTransition_StreamsSuccessExitMarker(t *testing.T) {
	fakeGuard := filepath.Join(t.TempDir(), "fake-guard.sh")
	os.WriteFile(fakeGuard, []byte("#!/bin/bash\necho ok\nexit 0\n"), 0755)
	t.Setenv("COMET_GUARD", fakeGuard)

	lock := NewTransitionLock()
	body, _ := json.Marshal(map[string]string{"targetPhase": "build"})
	req := httptest.NewRequest("POST", "/api/changes/my-change/transition", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleTransition(w, req, "my-change", ".", lock, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "__GUARD_EXIT__:0") {
		t.Fatalf("expected a success exit marker in the stream, got: %s", w.Body.String())
	}
}

func TestHandleTransition_StreamsFailureExitMarker(t *testing.T) {
	fakeGuard := filepath.Join(t.TempDir(), "fake-guard.sh")
	os.WriteFile(fakeGuard, []byte("#!/bin/bash\necho failing\nexit 1\n"), 0755)
	t.Setenv("COMET_GUARD", fakeGuard)

	lock := NewTransitionLock()
	body, _ := json.Marshal(map[string]string{"targetPhase": "build"})
	req := httptest.NewRequest("POST", "/api/changes/my-change/transition", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleTransition(w, req, "my-change", ".", lock, nil)

	if !strings.Contains(w.Body.String(), "__GUARD_EXIT__:1") {
		t.Fatalf("expected a failure exit marker in the stream, got: %s", w.Body.String())
	}
}

func TestHandleTransition_RunsAgainstAliasedWorkspacePath(t *testing.T) {
	fakeGuard := filepath.Join(t.TempDir(), "fake-guard.sh")
	os.WriteFile(fakeGuard, []byte("#!/bin/bash\npwd\nexit 0\n"), 0755)
	t.Setenv("COMET_GUARD", fakeGuard)

	wsDir := t.TempDir()
	openspecDir := filepath.Join(wsDir, "openspec")
	os.MkdirAll(filepath.Join(openspecDir, "changes"), 0755)

	regDir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(regDir, "workspaces.yaml"))
	reg.Add(WorkspaceConfig{Alias: "aliased", Path: openspecDir, Color: "#000"})

	lock := NewTransitionLock()
	body, _ := json.Marshal(map[string]string{"targetPhase": "build"})
	req := httptest.NewRequest("POST", "/api/changes/my-change/transition?workspace=aliased", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleTransition(w, req, "my-change", ".", lock, reg)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resolved, err := filepath.EvalSymlinks(openspecDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), resolved) {
		t.Fatalf("expected guard to run in aliased workspace dir %q, got: %s", resolved, w.Body.String())
	}
}

func TestHandleTransitionRejectsReadOnlySuperpowersWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "ideas", Path: root, Type: source.KindSuperpowers}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"targetPhase": "build"})
	req := httptest.NewRequest(http.MethodPost, "/api/changes/cache-redesign/transition?workspace=ideas", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	handleTransition(recorder, req, "cache-redesign", ".", NewTransitionLock(), reg)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(strings.ToLower(recorder.Body.String()), "read-only") {
		t.Fatalf("expected read-only 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}
