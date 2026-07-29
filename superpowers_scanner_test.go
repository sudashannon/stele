package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comet-ui/internal/source"
)

func writeSuperpowersFile(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanSuperpowersWorkspaceChangesAndDetail(t *testing.T) {
	root := t.TempDir()
	designPath := writeSuperpowersFile(t, root, "docs/superpowers/specs/2026-07-20-cache-redesign-design.md", "# Cache Redesign\n")
	writeSuperpowersFile(t, root, "docs/superpowers/plans/2026-07-21-cache-redesign.md", "# Plan\n\n- [x] First\n- [ ] Second\n")
	artifactPath := writeSuperpowersFile(t, root, "docs/superpowers/artifacts/cache-redesign/review.md", "# Review\n")

	workspace := WorkspaceConfig{Alias: "superpowers", Path: root, Type: source.KindSuperpowers}
	changes, err := scanSuperpowersWorkspaceChanges(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one Superpowers work item, got %+v", changes)
	}
	change := changes[0]
	if change.Name != "cache-redesign" || change.Title != "Cache Redesign" || change.SourceType != source.KindSuperpowers {
		t.Fatalf("unexpected Superpowers identity: %+v", change)
	}
	if change.Workspace != "superpowers" || change.Workflow != "superpowers" || change.Phase != "build" {
		t.Fatalf("unexpected workspace lifecycle: %+v", change)
	}
	if change.TasksCompleted != 1 || change.TasksTotal != 2 || change.ComponentID != designPath {
		t.Fatalf("unexpected progress or graph anchor: %+v", change)
	}
	if change.NextTransition != nil || len(change.Lifecycle) != 5 || change.ProposalPath != designPath {
		t.Fatalf("Superpowers must be read-only with a five-step lifecycle: %+v", change)
	}

	detail, err := scanSuperpowersChangeDetail(workspace, "cache-redesign")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Phases) != 5 {
		t.Fatalf("expected five detail phases, got %+v", detail.Phases)
	}
	var foundArtifact bool
	for _, phase := range detail.Phases {
		for _, artifact := range phase.Artifacts {
			if artifact.Path == artifactPath && artifact.Exists {
				foundArtifact = true
			}
		}
	}
	if !foundArtifact {
		t.Fatalf("detail did not expose execution artifact %q: %+v", artifactPath, detail.Phases)
	}
}

func TestSuperpowersChangeAndTransitionFactoriesStayReadOnly(t *testing.T) {
	root := t.TempDir()
	writeSuperpowersFile(t, root, "docs/superpowers/specs/2026-07-20-read-only-design.md", "# Read Only\n")
	workspace := WorkspaceConfig{Alias: "superpowers", Path: root, Type: source.KindSuperpowers}

	adapter, resolved, err := changeSourceFor(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(superpowersChangeSource); !ok || resolved.Type != source.KindSuperpowers {
		t.Fatalf("unexpected change source factory result: %T %+v", adapter, resolved)
	}

	transition, resolved, err := transitionSourceFor(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transition.(superpowersTransitionSource); !ok || resolved.Type != source.KindSuperpowers {
		t.Fatalf("unexpected transition source factory result: %T %+v", transition, resolved)
	}
	if err := transition.ValidateTarget("build"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "read-only") {
		t.Fatalf("expected explicit read-only transition rejection, got %v", err)
	}
}
