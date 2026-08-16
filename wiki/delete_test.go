package wiki

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// deleteFixture builds a workspace with one indexed document and an API whose
// graph knows it. STELE_DATA_DIR is redirected so trash lands in a temp dir.
func deleteFixture(t *testing.T) (*API, string, string) {
	t.Helper()
	t.Setenv("STELE_DATA_DIR", t.TempDir())
	wsRoot := t.TempDir()
	docsDir := filepath.Join(wsRoot, "knowledge")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(docsDir, "thin.md")
	if err := os.WriteFile(doc, []byte("# Thin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	graph := BuildGraph(
		[]Component{{ID: doc, Path: doc, Type: TypeKnowledge, Title: "Thin", Workspace: "ws", UpdatedAt: time.Now()}},
		nil,
	)
	api := &API{graph: graph, ws: []WorkspaceConfig{{Alias: "ws", Path: wsRoot, Type: "docs"}}}
	return api, wsRoot, doc
}

func postDelete(t *testing.T, api *API, paths ...string) deleteResponse {
	t.Helper()
	body, _ := json.Marshal(deleteRequest{Paths: paths})
	rec := httptest.NewRecorder()
	api.HandleDeleteDocuments(rec, httptest.NewRequest("POST", "/api/wiki/delete", bytes.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp deleteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// Deletion moves rather than unlinks, because the rule that surfaces candidates
// is a heuristic and a wrong verdict must be recoverable.
func TestDeleteMovesTheFileToTrashAndKeepsItReadable(t *testing.T) {
	api, _, doc := deleteFixture(t)

	resp := postDelete(t, api, doc)

	if len(resp.Deleted) != 1 || len(resp.Failed) != 0 {
		t.Fatalf("deleted=%d failed=%v, want exactly one deletion", len(resp.Deleted), resp.Failed)
	}
	if _, err := os.Stat(doc); !os.IsNotExist(err) {
		t.Fatal("the original file is still in the workspace")
	}
	stored := resp.Deleted[0].Stored
	content, err := os.ReadFile(stored)
	if err != nil {
		t.Fatalf("the trashed copy is not readable: %v", err)
	}
	if string(content) != "# Thin\n" {
		t.Fatalf("trashed content = %q, want the original bytes", content)
	}
	// The workspace-relative layout is preserved so a restore knows where it went.
	if !strings.HasSuffix(filepath.ToSlash(stored), "ws/knowledge/thin.md") {
		t.Fatalf("stored at %q, want it under <trash>/ws/knowledge/", stored)
	}
	// And the move is recorded, so a restore does not depend on the layout.
	manifest, err := os.ReadFile(filepath.Join(resp.Trash, "deleted.jsonl"))
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if !strings.Contains(string(manifest), doc) {
		t.Fatalf("manifest does not record the original path: %s", manifest)
	}
}

// The endpoint deletes, so every refusal matters more than any success.
func TestDeleteRefusesAnythingOutsideTheIndexedWorkspaces(t *testing.T) {
	t.Setenv("STELE_DATA_DIR", t.TempDir())
	wsRoot := t.TempDir()
	elsewhere := t.TempDir()

	inside := filepath.Join(wsRoot, "knowledge", "keep.md")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("# Keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(elsewhere, "outside.md")
	if err := os.WriteFile(outside, []byte("# Outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := filepath.Join(wsRoot, ".comet.yaml")
	if err := os.WriteFile(yaml, []byte("phase: build\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(wsRoot, "knowledge", "gone.md")

	// Every path below is INDEXED, so the "not an indexed document" check cannot
	// be what refuses them. That check masked the others in an earlier version of
	// this test: removing the workspace-boundary guard still passed.
	graph := BuildGraph([]Component{
		{ID: inside, Path: inside, Type: TypeKnowledge, Title: "Keep", Workspace: "ws"},
		{ID: outside, Path: outside, Type: TypeKnowledge, Title: "Outside", Workspace: "ws"},
		{ID: yaml, Path: yaml, Type: TypeChange, Title: "Change", Workspace: "ws"},
		{ID: missing, Path: missing, Type: TypeKnowledge, Title: "Gone", Workspace: "ws"},
	}, nil)
	api := &API{graph: graph, ws: []WorkspaceConfig{{Alias: "ws", Path: wsRoot, Type: "docs"}}}

	for name, path := range map[string]string{
		"outside every workspace": outside,
		"generated metadata":      yaml,
		"file already gone":       missing,
		"empty path":              "",
	} {
		t.Run(name, func(t *testing.T) {
			resp := postDelete(t, api, path)
			if len(resp.Deleted) != 0 {
				t.Fatalf("deleted %v, want a refusal", resp.Deleted)
			}
			if len(resp.Failed) != 1 {
				t.Fatalf("failed = %v, want one entry explaining the refusal", resp.Failed)
			}
		})
	}

	// Nothing the refusals touched may have moved.
	for _, p := range []string{inside, outside, yaml} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a refused request removed %s: %v", filepath.Base(p), err)
		}
	}
}

// A session transcript is read-only everywhere else in this codebase; deletion
// must not be the one exception.
func TestDeleteRefusesSessionTranscripts(t *testing.T) {
	t.Setenv("STELE_DATA_DIR", t.TempDir())
	wsRoot := t.TempDir()
	transcript := filepath.Join(wsRoot, "session.md")
	if err := os.WriteFile(transcript, []byte("# s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	graph := BuildGraph([]Component{{
		ID: transcript, Path: transcript, Type: TypeSession, Title: "s", Workspace: "ws",
	}}, nil)
	api := &API{graph: graph, ws: []WorkspaceConfig{{Alias: "ws", Path: wsRoot, Type: "docs"}}}

	resp := postDelete(t, api, transcript)
	if len(resp.Deleted) != 0 {
		t.Fatal("deleted a session transcript")
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Fatalf("the transcript was removed: %v", err)
	}
}

// Deleting the same relative path twice must keep both copies: a document can be
// recreated and deleted again, and the first version is the one worth keeping.
func TestDeleteKeepsAnEarlierTrashedCopy(t *testing.T) {
	api, _, doc := deleteFixture(t)

	first := postDelete(t, api, doc)
	if len(first.Deleted) != 1 {
		t.Fatalf("first delete failed: %v", first.Failed)
	}
	if err := os.WriteFile(doc, []byte("# Thin again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := postDelete(t, api, doc)
	if len(second.Deleted) != 1 {
		t.Fatalf("second delete failed: %v", second.Failed)
	}
	if first.Deleted[0].Stored == second.Deleted[0].Stored {
		t.Fatal("the second deletion overwrote the first trashed copy")
	}
	original, err := os.ReadFile(first.Deleted[0].Stored)
	if err != nil || string(original) != "# Thin\n" {
		t.Fatalf("the first trashed copy was lost: %v %q", err, original)
	}
}

// A batch reports per-path outcomes instead of failing whole; the panel deletes
// several documents at once and needs to know which ones went.
func TestDeleteReportsPerPathOutcomesInABatch(t *testing.T) {
	api, wsRoot, doc := deleteFixture(t)

	resp := postDelete(t, api, doc, filepath.Join(wsRoot, "knowledge", "absent.md"))
	if len(resp.Deleted) != 1 {
		t.Fatalf("deleted = %v, want the valid path to succeed", resp.Deleted)
	}
	if len(resp.Failed) != 1 || resp.Failed[0].Reason == "" {
		t.Fatalf("failed = %v, want one entry with a reason", resp.Failed)
	}
}
