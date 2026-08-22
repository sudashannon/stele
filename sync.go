package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"stele/internal/appdir"
	"stele/wiki"
)

type syncResult struct {
	Action       string `json:"action"` // pushed, pulled, merged, up-to-date, error
	FilesChanged int    `json:"filesChanged"`
	Message      string `json:"message"`
}

func handleSync(reg *WorkspaceRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}

		syncCfg := reg.Sync()
		if !syncCfg.Enabled || syncCfg.Remote == "" {
			writeJSONResp(w, syncResult{Action: "error", Message: "sync not configured (set sync.remote in workspaces.yaml)"})
			return
		}

		repoDir := appdir.Path("knowledge-repo")
		if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
			writeJSONResp(w, syncResult{Action: "error", Message: "knowledge-repo not initialized"})
			return
		}

		// Update origin in place. Removing it also deletes remote-tracking refs
		// and lets a damaged ref block every subsequent automatic push.
		if err := wiki.ConfigureMirrorRemote(repoDir, syncCfg.Remote); err != nil {
			writeJSONResp(w, syncResult{Action: "error", Message: "configure remote failed: " + err.Error()})
			return
		}

		// Fetch
		if err := gitRun(repoDir, "fetch", "origin", wiki.MirrorBranch); err != nil {
			writeJSONResp(w, syncResult{Action: "error", Message: "fetch failed: " + err.Error()})
			return
		}

		// Compare HEAD vs the mirror branch on the remote
		localHead := gitOutput(repoDir, "rev-parse", "HEAD")
		remoteHead := gitOutput(repoDir, "rev-parse", "origin/"+wiki.MirrorBranch)
		mergeBase := gitOutput(repoDir, "merge-base", "HEAD", "origin/"+wiki.MirrorBranch)

		var result syncResult

		switch {
		case localHead == remoteHead:
			result = syncResult{Action: "up-to-date", Message: "本地和远端已同步"}

		case mergeBase == remoteHead:
			// Local ahead → push
			if err := gitRun(repoDir, "push", "origin", "HEAD:"+wiki.MirrorBranch); err != nil {
				result = syncResult{Action: "error", Message: "push failed: " + err.Error()}
			} else {
				// Count commits pushed
				count := gitOutput(repoDir, "rev-list", "--count", remoteHead+"..HEAD")
				result = syncResult{Action: "pushed", Message: fmt.Sprintf("推送了 %s 个提交到远端", count)}
			}

		case mergeBase == localHead:
			// Remote ahead → pull + restore
			if err := gitRun(repoDir, "merge", "origin/"+wiki.MirrorBranch, "--ff-only"); err != nil {
				result = syncResult{Action: "error", Message: "pull failed: " + err.Error()}
			} else {
				n := restoreFiles(repoDir, reg)
				result = syncResult{Action: "pulled", FilesChanged: n, Message: fmt.Sprintf("拉取远端更新，还原了 %d 个文件", n)}
			}

		default:
			// Diverged → pull (merge) + restore + push
			if err := gitRun(repoDir, "merge", "origin/"+wiki.MirrorBranch, "--no-edit"); err != nil {
				result = syncResult{Action: "error", Message: "merge failed (conflict?): " + err.Error()}
			} else {
				n := restoreFiles(repoDir, reg)
				gitRun(repoDir, "push", "origin", "HEAD:"+wiki.MirrorBranch)
				result = syncResult{Action: "merged", FilesChanged: n, Message: fmt.Sprintf("合并远端更新并推送，还原了 %d 个文件", n)}
			}
		}

		writeJSONResp(w, result)
	}
}

// syncConfigResp is the wire shape for GET/PUT /api/sync/config: mirrors
// SyncConfig but kept distinct so the JSON contract doesn't silently
// change if SyncConfig's yaml-oriented fields evolve.
type syncConfigResp struct {
	Enabled bool   `json:"enabled"`
	Remote  string `json:"remote"`
}

// handleSyncConfig serves GET (read the current sync.remote/enabled from
// workspaces.yaml) and PUT (update sync.remote, persisting it back to
// workspaces.yaml via WorkspaceRegistry.SetSyncRemote).
func handleSyncConfig(reg *WorkspaceRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg := reg.Sync()
			writeJSONResp(w, syncConfigResp{Enabled: cfg.Enabled, Remote: cfg.Remote})

		case http.MethodPut:
			var body struct {
				Remote string `json:"remote"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSONError(w, "invalid body", 400)
				return
			}
			updated, err := reg.SetSyncRemote(strings.TrimSpace(body.Remote))
			if err != nil {
				writeJSONError(w, err.Error(), 500)
				return
			}
			writeJSONResp(w, syncConfigResp{Enabled: updated.Enabled, Remote: updated.Remote})

		default:
			writeJSONError(w, "method not allowed", 405)
		}
	}
}

// restoreFiles copies files from knowledge-repo/<alias>/<relpath> back to
// the original workspace path. Returns number of files restored.
func restoreFiles(repoDir string, reg *WorkspaceRegistry) int {
	workspaces := reg.List()
	// Build alias → workspace path map
	aliasToPath := make(map[string]string)
	for _, ws := range workspaces {
		aliasToPath[ws.Alias] = ws.Path
	}

	count := 0
	// Walk the repo dir (skip .git)
	filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Get relative path from repo root
		rel, _ := filepath.Rel(repoDir, path)
		// Split into alias + rest
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) < 2 {
			return nil
		}
		alias, relPath := parts[0], parts[1]
		wsPath, ok := aliasToPath[alias]
		if !ok {
			return nil
		}
		// Determine the actual destination
		// wsPath is the openspec path; parent is the project root
		// The file's original location is relative to parent
		parent := filepath.Dir(wsPath)
		destPath := filepath.Join(parent, relPath)

		// Copy file back
		if err := copyFileRestore(path, destPath); err != nil {
			log.Printf("sync restore: failed to copy %s -> %s: %v", rel, destPath, err)
			return nil
		}
		count++
		return nil
	})
	return count
}

func copyFileRestore(src, dst string) error {
	os.MkdirAll(filepath.Dir(dst), 0755)
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func gitRun(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
