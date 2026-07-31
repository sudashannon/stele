package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stele/wiki"
)

func TestExtractReportCorpusReadsTrellisPRD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prd.md")
	body := "# Task\n\n## Goal\n\nShip document-driven reports.\n\n## Requirements\n\nInclude Trellis task context.\n\n- [x] Index PRD\n- [ ] Verify report"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	activity := time.Date(2026, 7, 26, 12, 0, 0, 0, time.Local)
	snapshot := wiki.DocumentWindowSnapshot{Documents: []wiki.SnapshotDocument{{
		ID: path, Path: path, Title: "Beta Task", Type: wiki.TypeArtifact, Workspace: "trellis", UpdatedAt: activity,
	}}}
	corpus := extractReportCorpus(snapshot, activity, activity.AddDate(0, 0, 1), "trellis")
	if len(corpus.Documents) != 1 {
		t.Fatalf("expected one Trellis evidence document, got %+v", corpus)
	}
	document := corpus.Documents[0]
	if !strings.Contains(document.SemanticText, "Ship document-driven reports") || !strings.Contains(document.SemanticText, "Include Trellis task context") {
		t.Fatalf("unexpected Trellis evidence: %+v", document)
	}
	if document.Metadata.ChecklistDone != 1 || document.Metadata.ChecklistOpen != 1 {
		t.Fatalf("unexpected checklist metadata: %+v", document.Metadata)
	}
}
