package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"stele/wiki"
)

const (
	worktreeStatusTimeout    = 15 * time.Second
	maxWorkflowArtifactBytes = 1 << 20
)

var errWorkflowArtifactTooLarge = errors.New("workflow artifact exceeds size limit")

type WorkflowArtifact struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Worktree string `json:"worktree"`
}

type WorkflowWork struct {
	Name       string             `json:"name"`
	Paths      []string           `json:"paths"`
	Branch     string             `json:"branch"`
	Kind       string             `json:"kind"`
	Worktrees  int                `json:"worktrees"`
	IdleDays   *int               `json:"idleDays"`
	Goal       string             `json:"goal"`
	Current    string             `json:"current"`
	Dirty      int                `json:"dirty"`
	Sessions   []string           `json:"sessions"`
	Notes      []string           `json:"notes"`
	State      string             `json:"state"`
	MergeState string             `json:"mergeState"`
	Merged     bool               `json:"merged"`
	Next       string             `json:"next"`
	Workspace  string             `json:"workspace"`
	Artifacts  []WorkflowArtifact `json:"artifacts"`
}

type WorkflowWorksResponse struct {
	Enabled bool           `json:"enabled"`
	Works   []WorkflowWork `json:"works"`
	Error   string         `json:"error,omitempty"`
}

func defaultWorktreeStatusScript() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "skills", "worktree-session", "scripts", "worktree-status.sh")
}

func loadWorkflowWorks(scriptPath string, request *http.Request) ([]WorkflowWork, string) {
	if scriptPath == "" {
		return nil, "workflow status script path is unavailable"
	}
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, "workflow status script is unavailable"
	}

	ctx, cancel := context.WithTimeout(request.Context(), worktreeStatusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", scriptPath, "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, "workflow work inventory is unavailable"
	}
	var works []WorkflowWork
	if err := json.Unmarshal(output, &works); err != nil {
		return nil, "workflow work inventory returned invalid JSON"
	}
	if works == nil {
		works = []WorkflowWork{}
	}
	return works, ""
}

func workflowArtifacts(worktree string) []WorkflowArtifact {
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	var artifacts []WorkflowArtifact
	add := func(relative, kind string) {
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || !pathWithinRoot(resolved, root) {
			return
		}
		info, err := os.Stat(resolved)
		if err == nil && info.Mode().IsRegular() {
			artifacts = append(artifacts, WorkflowArtifact{Path: relative, Kind: kind, Worktree: worktree})
		}
	}

	add("SESSION.md", "index")
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.Type()&os.ModeSymlink == 0 && entry.Type().IsRegular() &&
				strings.HasPrefix(name, "SESSION-") && strings.HasSuffix(name, ".md") {
				add(name, "session")
			}
		}
	}
	notesDir := filepath.Join(root, "notes")
	if notesInfo, err := os.Lstat(notesDir); err == nil && notesInfo.IsDir() {
		notesRoot, err := filepath.EvalSymlinks(notesDir)
		if err == nil && pathWithinRoot(notesRoot, root) {
			if notes, err := os.ReadDir(notesRoot); err == nil {
				for _, entry := range notes {
					if entry.Type()&os.ModeSymlink == 0 && entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") {
						add(filepath.ToSlash(filepath.Join("notes", entry.Name())), "note")
					}
				}
			}
		}
	}
	return artifacts
}

func handleWorkflowWorks(scriptPath string, workspaces func() []WorkspaceConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		works, message := loadWorkflowWorks(scriptPath, r)
		if message != "" {
			writeWorkflowWorks(w, WorkflowWorksResponse{Works: []WorkflowWork{}, Error: message})
			return
		}
		registered := []WorkspaceConfig(nil)
		if workspaces != nil {
			registered = workspaces()
		}
		for i := range works {
			works[i].Workspace = ""
			works[i].Artifacts = []WorkflowArtifact{}
			for _, path := range works[i].Paths {
				works[i].Artifacts = append(works[i].Artifacts, workflowArtifacts(path)...)
			}
			if len(works[i].Paths) > 0 {
				if workspace, ok := wiki.WorkspaceForPath(registered, works[i].Paths[0]); ok {
					works[i].Workspace = workspace.Alias
				}
			}
		}
		writeWorkflowWorks(w, WorkflowWorksResponse{Enabled: true, Works: works})
	}
}

func handleWorkflowArtifact(scriptPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		worktree := filepath.Clean(r.URL.Query().Get("worktree"))
		file := r.URL.Query().Get("file")
		if worktree == "." || file == "" {
			writeJSONError(w, "worktree and file are required", http.StatusBadRequest)
			return
		}
		works, message := loadWorkflowWorks(scriptPath, r)
		if message != "" {
			writeJSONError(w, message, http.StatusServiceUnavailable)
			return
		}
		var artifact *WorkflowArtifact
		for _, work := range works {
			for _, worktreePath := range work.Paths {
				if filepath.Clean(worktreePath) != worktree {
					continue
				}
				artifacts := workflowArtifacts(worktreePath)
				for i := range artifacts {
					if artifacts[i].Path == file {
						artifact = &artifacts[i]
						break
					}
				}
				if artifact != nil {
					break
				}
			}
			if artifact != nil {
				break
			}
		}
		if artifact == nil {
			writeJSONError(w, "workflow artifact not found", http.StatusNotFound)
			return
		}
		content, err := readWorkflowArtifact(*artifact)
		if errors.Is(err, errWorkflowArtifactTooLarge) {
			writeJSONError(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			writeJSONError(w, "workflow artifact is unavailable", http.StatusNotFound)
			return
		}
		if !utf8.Valid(content) {
			writeJSONError(w, "workflow artifact is not UTF-8 text", http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("ETag", artifactETag(content))
		if r.Method == http.MethodGet {
			_, _ = w.Write(content)
		}
	}
}

func readWorkflowArtifact(artifact WorkflowArtifact) ([]byte, error) {
	root, err := filepath.EvalSymlinks(artifact.Worktree)
	if err != nil {
		return nil, err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(artifact.Path)))
	if err != nil || !pathWithinRoot(path, root) {
		return nil, errors.New("workflow artifact path is outside worktree")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("workflow artifact is not a regular file")
	}
	if info.Size() > maxWorkflowArtifactBytes {
		return nil, errWorkflowArtifactTooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxWorkflowArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxWorkflowArtifactBytes {
		return nil, errWorkflowArtifactTooLarge
	}
	return content, nil
}

func writeWorkflowWorks(w http.ResponseWriter, response WorkflowWorksResponse) {
	_ = json.NewEncoder(w).Encode(response)
}

