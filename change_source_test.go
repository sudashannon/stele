package main

import (
	"os"
	"path/filepath"
	"testing"

	"stele/internal/source"
)

// A docs workspace has no changes. Before this case existed the dispatch fell
// through to the OpenSpec reader, which opened <root>/changes; that directory
// does not exist in a plain documentation tree, so every dashboard refresh logged
// the workspace as unreadable and the frontend listed it under
// "以下 workspace 无法读取，已暂时跳过".
func TestChangeSourceForADocsWorkspaceListsNoChangesInsteadOfFailing(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "knowledge")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "note.md"), []byte("# Note\n\nreal content here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := WorkspaceConfig{Alias: "mdeploy", Path: root, Type: source.KindDocs}
	summaries, err := scanWorkspaceChanges(ws)
	if err != nil {
		t.Fatalf("scanWorkspaceChanges = %v, want no error: a docs tree has no changes, which is not a failure", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("summaries = %v, want none", summaries)
	}

	// And the aggregation must not report it as a failed workspace.
	all, failed := scanAllWorkspaces([]WorkspaceConfig{ws})
	if len(failed) != 0 {
		t.Fatalf("failedAliases = %v, want empty", failed)
	}
	if len(all) != 0 {
		t.Fatalf("changes = %v, want none", all)
	}
}
