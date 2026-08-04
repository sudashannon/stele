package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"stele/wiki"
)

func TestHandleSync_NotConfiguredReturnsErrorAction(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	req := httptest.NewRequest("POST", "/api/sync", nil)
	w := httptest.NewRecorder()
	handleSync(reg)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got syncResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "error" {
		t.Fatalf("expected error action when sync unconfigured, got %+v", got)
	}
}

func TestHandleSync_MethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	req := httptest.NewRequest("GET", "/api/sync", nil)
	w := httptest.NewRecorder()
	handleSync(reg)(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// TestHandleSync_UpToDateWhenLocalMatchesRemote exercises the real git
// comparison path end-to-end against a local bare "remote" repo, verifying
// the up-to-date branch returns without erroring.
func TestHandleSync_UpToDateWhenLocalMatchesRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	remoteDir := filepath.Join(home, "remote.git")
	repoDir := filepath.Join(home, ".stele", "knowledge-repo")

	runGit(t, home, "init", "--bare", remoteDir)
	runGit(t, home, "init", repoDir)
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	// Follows the constant, so renaming the mirror branch cannot leave this
	// fixture testing a branch the code no longer uses.
	runGit(t, repoDir, "checkout", "-b", wiki.MirrorBranch)
	writeFileHelper(t, filepath.Join(repoDir, "note.md"), "hello")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "init")
	runGit(t, repoDir, "remote", "add", "origin", remoteDir)
	runGit(t, repoDir, "push", "origin", wiki.MirrorBranch)

	t.Setenv("HOME", home)

	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))
	if _, err := reg.SetSyncRemote(remoteDir); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/sync", nil)
	w := httptest.NewRecorder()
	handleSync(reg)(w, req)

	var got syncResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "up-to-date" {
		t.Fatalf("expected up-to-date, got %+v (body=%s)", got, w.Body.String())
	}
}

func TestHandleSyncConfig_GetReturnsCurrentConfig(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	req := httptest.NewRequest("GET", "/api/sync/config", nil)
	w := httptest.NewRecorder()
	handleSyncConfig(reg)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got syncConfigResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Remote != "" {
		t.Fatalf("expected empty/disabled default config, got %+v", got)
	}
}

func TestHandleSyncConfig_PutPersistsRemoteAndEnables(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workspaces.yaml")
	reg, _ := NewWorkspaceRegistry(cfgPath)

	body, _ := json.Marshal(map[string]string{"remote": "git@example.com:foo/bar.git"})
	req := httptest.NewRequest("PUT", "/api/sync/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleSyncConfig(reg)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got syncConfigResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Remote != "git@example.com:foo/bar.git" {
		t.Fatalf("expected enabled config with remote set, got %+v", got)
	}

	// Persisted to disk and re-loadable.
	reloaded, err := NewWorkspaceRegistry(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if s := reloaded.Sync(); !s.Enabled || s.Remote != "git@example.com:foo/bar.git" {
		t.Fatalf("expected reloaded sync config to persist, got %+v", s)
	}
}

func TestHandleSyncConfig_PutEmptyRemoteDisablesSync(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))
	reg.SetSyncRemote("git@example.com:foo/bar.git")

	body, _ := json.Marshal(map[string]string{"remote": ""})
	req := httptest.NewRequest("PUT", "/api/sync/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleSyncConfig(reg)(w, req)

	var got syncConfigResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Remote != "" {
		t.Fatalf("expected disabled config after clearing remote, got %+v", got)
	}
}

func TestHandleSyncConfig_MethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	req := httptest.NewRequest("DELETE", "/api/sync/config", nil)
	w := httptest.NewRecorder()
	handleSyncConfig(reg)(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
}

func writeFileHelper(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
