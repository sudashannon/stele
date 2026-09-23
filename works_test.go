package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func createLinkedWorktree(t *testing.T) (repo, worktree string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "workspace", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit(repo, "init", "-q")
	runGit(repo, "config", "user.email", "t@t")
	runGit(repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(repo, "add", ".")
	runGit(repo, "commit", "-q", "-m", "init")
	worktree = filepath.Join(root, "develop", "work")
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-q", "--detach", worktree, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	return repo, worktree
}

func writeWorkScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worktree-status.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWorkflowWorksHandlerMapsWorktreeToWorkspace(t *testing.T) {
	repo, worktree := createLinkedWorktree(t)
	payload, err := json.Marshal([]WorkflowWork{{Name: "work", Paths: []string{worktree}, Branch: "feature/test", State: "active"}})
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(t.TempDir(), "works.json")
	if err := os.WriteFile(jsonPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	script := writeWorkScript(t, "cat '"+jsonPath+"'\n")
	handler := handleWorkflowWorks(script, func() []WorkspaceConfig {
		return []WorkspaceConfig{{Alias: "proj", Path: repo}}
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/works", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var response WorkflowWorksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Enabled || len(response.Works) != 1 || response.Works[0].Workspace != "proj" {
		t.Fatalf("response = %+v; want enabled work attributed to proj", response)
	}
}

func TestWorkflowWorksHandlerKeepsUnregisteredWorksVisible(t *testing.T) {
	payload := `[{"name":"unregistered","paths":["/tmp/worktree"],"state":"-","workspace":"miao"}]`
	jsonPath := filepath.Join(t.TempDir(), "works.json")
	if err := os.WriteFile(jsonPath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := handleWorkflowWorks(writeWorkScript(t, "cat '"+jsonPath+"'\n"), func() []WorkspaceConfig {
		return []WorkspaceConfig{{Alias: "other", Path: t.TempDir()}}
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/works", nil))
	var response WorkflowWorksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Enabled || len(response.Works) != 1 || response.Works[0].Workspace != "" || response.Works[0].State != "-" {
		t.Fatalf("response = %+v; want unregistered work retained with undeclared state", response)
	}
}

func TestWorkflowWorksHandlerDegradesWhenScriptUnavailable(t *testing.T) {
	handler := handleWorkflowWorks(filepath.Join(t.TempDir(), "missing.sh"), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/works", nil))
	var response WorkflowWorksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Enabled || len(response.Works) != 0 || response.Error == "" {
		t.Fatalf("response = %+v; want disabled, empty response with reason", response)
	}
}

func TestWorkflowWorksHandlerOnlyAllowsGET(t *testing.T) {
	handler := handleWorkflowWorks("", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/api/works", nil))
	if rec.Code != 405 {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func workflowArtifactURL(worktree, file string) string {
	query := url.Values{"worktree": {worktree}, "file": {file}}
	return "/api/workflow/artifact?" + query.Encode()
}

func TestWorkflowArtifactHandlerReadsOnlyInventoryMarkdown(t *testing.T) {
	_, worktree := createLinkedWorktree(t)
	notes := filepath.Join(worktree, "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"SESSION.md":      "# Work\n",
		"SESSION-test.md": "# Session log\n",
		"notes/topic.md":  "# Shared note\n",
		"notes/large.md":  "",
	}
	for name, content := range files {
		data := []byte(content)
		if name == "notes/large.md" {
			data = make([]byte, maxWorkflowArtifactBytes+1)
		}
		if err := os.WriteFile(filepath.Join(worktree, filepath.FromSlash(name)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(filepath.Dir(worktree), "private.md")
	if err := os.WriteFile(outside, []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{
		filepath.Join(worktree, "SESSION-external.md"),
		filepath.Join(notes, "external.md"),
	} {
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
	}
	payload, err := json.Marshal([]WorkflowWork{{Name: "work", Paths: []string{worktree}}})
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(t.TempDir(), "works.json")
	if err := os.WriteFile(jsonPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	script := writeWorkScript(t, "cat '"+jsonPath+"'\n")
	var response WorkflowWorksResponse
	list := httptest.NewRecorder()
	handleWorkflowWorks(script, nil).ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/works", nil))
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Works) != 1 {
		t.Fatalf("works = %d, want 1", len(response.Works))
	}
	artifacts := map[string]string{}
	for _, artifact := range response.Works[0].Artifacts {
		artifacts[artifact.Path] = artifact.Kind
	}
	for name, kind := range map[string]string{
		"SESSION.md":      "index",
		"SESSION-test.md": "session",
		"notes/topic.md":  "note",
		"notes/large.md":  "note",
	} {
		if artifacts[name] != kind {
			t.Errorf("artifact %q kind = %q, want %q", name, artifacts[name], kind)
		}
	}
	if _, ok := artifacts["SESSION-external.md"]; ok {
		t.Fatal("listed a symlinked session artifact")
	}
	if _, ok := artifacts["notes/external.md"]; ok {
		t.Fatal("listed a symlinked note")
	}

	handler := handleWorkflowArtifact(script)
	for _, test := range []struct {
		name   string
		url    string
		method string
		status int
		body   string
	}{
		{"indexed session", workflowArtifactURL(worktree, "SESSION.md"), http.MethodGet, http.StatusOK, "# Work\n"},
		{"shared note", workflowArtifactURL(worktree, "notes/topic.md"), http.MethodGet, http.StatusOK, "# Shared note\n"},
		{"unlisted file", workflowArtifactURL(worktree, "private.md"), http.MethodGet, http.StatusNotFound, "{\"error\":\"workflow artifact not found\"}\n"},
		{"traversal", workflowArtifactURL(worktree, "../private.md"), http.MethodGet, http.StatusNotFound, "{\"error\":\"workflow artifact not found\"}\n"},
		{"unknown worktree", workflowArtifactURL(filepath.Join(worktree, "..", "other"), "SESSION.md"), http.MethodGet, http.StatusNotFound, "{\"error\":\"workflow artifact not found\"}\n"},
		{"oversized file", workflowArtifactURL(worktree, "notes/large.md"), http.MethodGet, http.StatusRequestEntityTooLarge, "{\"error\":\"workflow artifact exceeds size limit\"}\n"},
		{"write denied", workflowArtifactURL(worktree, "SESSION.md"), http.MethodPost, http.StatusMethodNotAllowed, "{\"error\":\"method not allowed\"}\n"},
		{"head", workflowArtifactURL(worktree, "SESSION.md"), http.MethodHead, http.StatusOK, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(test.method, test.url, nil))
			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, test.status, rec.Body.String())
			}
			if rec.Body.String() != test.body {
				t.Fatalf("body = %q, want %q", rec.Body.String(), test.body)
			}
		})
	}
}
