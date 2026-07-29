package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"comet-ui/wiki"
)

func TestExtractReportCorpusReadsStandaloneSuperpowersDesign(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "superpowers", "specs", "2026-07-26-cache-design.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Cache Design\n\n## Goal\n\nReduce cache misses.\n\n## Requirements\n\nKeep eviction deterministic."), 0o644); err != nil {
		t.Fatal(err)
	}
	activity := time.Date(2026, 7, 26, 9, 0, 0, 0, time.Local)
	snapshot := wiki.DocumentWindowSnapshot{Documents: []wiki.SnapshotDocument{{
		ID: path, Path: path, Title: "Cache Design", Type: wiki.TypeDesign, Workspace: "superpowers", UpdatedAt: activity,
	}}}
	corpus := extractReportCorpus(snapshot, activity, activity.AddDate(0, 0, 1), "superpowers")
	if len(corpus.Documents) != 1 {
		t.Fatalf("expected one evidence document, got %+v", corpus)
	}
	document := corpus.Documents[0]
	if document.Title != "Cache Design" || !strings.Contains(document.SemanticText, "Reduce cache misses") || !strings.Contains(document.SemanticText, "Keep eviction deterministic") {
		t.Fatalf("unexpected Superpowers evidence: %+v", document)
	}
	if document.ContentHash == "" || document.EvidenceID != "D1" {
		t.Fatalf("missing evidence identity: %+v", document)
	}
}
