package wiki

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stele/internal/appdir"
	"stele/internal/source"
)

// Deleting a document from the panel removes a real file from a real workspace,
// and the quality rule that surfaces candidates is a heuristic - while
// calibrating it, three separate versions of one signal flagged the six
// best-written documents in the corpus. So this moves files to a trash directory
// instead of unlinking them, and records where each one came from.

// trashDir is <data dir>/trash. It lives beside the index rather than inside a
// workspace so a restore is never mistaken for an edit by the watcher.
func trashDir() string {
	return appdir.Path("trash")
}

// TrashEntry records one moved file so it can be put back.
type TrashEntry struct {
	Original  string    `json:"original"`
	Stored    string    `json:"stored"`
	Workspace string    `json:"workspace"`
	DeletedAt time.Time `json:"deletedAt"`
}

type deleteRequest struct {
	Paths []string `json:"paths"`
}

type deleteResponse struct {
	Deleted []TrashEntry `json:"deleted"`
	Failed  []deleteFail `json:"failed"`
	Trash   string       `json:"trash"`
}

type deleteFail struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// HandleDeleteDocuments moves indexed documents to the trash directory.
//
// Two guards, both refusing rather than reporting a partial success: the path
// must be an indexed component, and it must resolve inside a registered
// workspace. The first stops the endpoint from becoming a general file remover;
// the second stops a symlink or a ".." from reaching outside, which matters
// because this handler deletes.
func (a *API) HandleDeleteDocuments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req deleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Paths) == 0 {
		http.Error(w, "no paths given", http.StatusBadRequest)
		return
	}

	a.mu.RLock()
	graph := a.graph
	workspaces := append([]WorkspaceConfig(nil), a.ws...)
	a.mu.RUnlock()

	root := trashDir()
	resp := deleteResponse{Trash: root}
	for _, p := range req.Paths {
		entry, err := a.trashOne(graph, workspaces, root, p)
		if err != nil {
			resp.Failed = append(resp.Failed, deleteFail{Path: p, Reason: err.Error()})
			continue
		}
		resp.Deleted = append(resp.Deleted, *entry)
	}

	if len(resp.Deleted) > 0 {
		if err := appendTrashManifest(root, resp.Deleted); err != nil {
			log.Printf("wiki delete: trash manifest: %v", err)
		}
		// The graph still holds the moved components; rebuild so the panel and
		// every downstream reader stop referring to files that are gone.
		go func() {
			if err := a.Rebuild(); err != nil {
				log.Printf("wiki delete: rebuild after delete: %v", err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (a *API) trashOne(graph *Graph, workspaces []WorkspaceConfig, root, path string) (*TrashEntry, error) {
	if graph == nil {
		return nil, fmt.Errorf("index not ready")
	}
	component, ok := graph.components[path]
	if !ok {
		return nil, fmt.Errorf("not an indexed document")
	}
	// Only prose is deletable here. A change's .comet.yaml is generated metadata
	// that other tooling owns.
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		return nil, fmt.Errorf("only markdown documents can be deleted")
	}
	if component.Type == TypeSession {
		return nil, fmt.Errorf("session transcripts are read-only")
	}

	owner, rel, err := resolveWorkspaceOwner(workspaces, path)
	if err != nil {
		return nil, err
	}

	stored := filepath.Join(root, owner.Alias, rel)
	if err := os.MkdirAll(filepath.Dir(stored), 0o755); err != nil {
		return nil, fmt.Errorf("preparing trash: %w", err)
	}
	stored = uniqueTrashPath(stored)
	if err := moveFile(path, stored); err != nil {
		return nil, err
	}
	return &TrashEntry{
		Original:  path,
		Stored:    stored,
		Workspace: owner.Alias,
		DeletedAt: time.Now(),
	}, nil
}

// resolveWorkspaceOwner finds the registered workspace a path belongs to, after
// symlink resolution so a link inside a workspace cannot point out of it.
func resolveWorkspaceOwner(workspaces []WorkspaceConfig, path string) (WorkspaceConfig, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return WorkspaceConfig{}, "", fmt.Errorf("invalid path")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return WorkspaceConfig{}, "", fmt.Errorf("unreadable path")
	}
	for _, ws := range workspaces {
		projectRoot := source.ProjectRoot(source.Workspace{Alias: ws.Alias, Path: ws.Path, Type: ws.Type})
		rootAbs, err := filepath.EvalSymlinks(projectRoot)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootAbs, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return ws, rel, nil
	}
	return WorkspaceConfig{}, "", fmt.Errorf("path is outside every registered workspace")
}

// uniqueTrashPath keeps an earlier deletion of the same document instead of
// overwriting it: a file can be recreated and deleted again.
func uniqueTrashPath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	stamp := time.Now().Format("20060102-150405")
	candidate := fmt.Sprintf("%s.%s%s", base, stamp, ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s.%s-%d%s", base, stamp, i, ext)
	}
}

// moveFile prefers a rename and falls back to copy+remove, because the data
// directory and a workspace can sit on different filesystems.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Base(from), err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		return fmt.Errorf("writing to trash: %w", err)
	}
	if err := os.Remove(from); err != nil {
		return fmt.Errorf("removing original: %w", err)
	}
	return nil
}

// appendTrashManifest records deletions as JSON lines so a restore does not
// depend on reading the directory layout back.
func appendTrashManifest(root string, entries []TrashEntry) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, "deleted.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}
