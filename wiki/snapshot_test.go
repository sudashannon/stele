package wiki

import (
	"errors"
	"testing"
	"time"
)

func TestSnapshotDocumentsFiltersWindowAndCopiesContextVectors(t *testing.T) {
	start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	primary := Component{ID: "/a/design.md", Path: "/a/design.md", Title: "Design", Type: TypeDesign, Workspace: "a", UpdatedAt: start.Add(12 * time.Hour)}
	context := Component{ID: "/a/plan.md", Path: "/a/plan.md", Title: "Plan", Type: TypePlan, Workspace: "a", UpdatedAt: start.AddDate(0, 0, -3)}
	future := Component{ID: "/a/future.md", Path: "/a/future.md", Title: "Future", Type: TypeArtifact, Workspace: "a", UpdatedAt: start.AddDate(0, 0, 8)}
	otherWorkspace := Component{ID: "/b/design.md", Path: "/b/design.md", Title: "Other", Type: TypeDesign, Workspace: "b", UpdatedAt: start}
	connector := Component{ID: "/a/change", Title: "Change", Type: TypeChange, Workspace: "a"}
	graph := BuildGraph([]Component{primary, context, future, otherWorkspace, connector}, []Edge{
		{From: primary.ID, To: connector.ID, Kind: "owned-by", Source: "convention-internal"},
		{From: connector.ID, To: context.ID, Kind: "generates", Source: "convention-internal"},
		{From: primary.ID, To: future.ID, Kind: "references", Source: "markdown-link"},
	})
	graph.SetEmbeddings(map[string][]float32{primary.ID: {1, 2}, context.ID: {3, 4}})
	graph.SetFailedWorkspaces([]string{"broken"})
	api := NewAPI(graph)

	snapshot, err := api.SnapshotDocuments(DocumentWindowFilter{
		Start: start, End: start.AddDate(0, 0, 7), Workspace: "a", IncludeContext: true, MaxContextDocuments: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Documents) != 2 {
		t.Fatalf("documents=%+v", snapshot.Documents)
	}
	if snapshot.Documents[0].ID != primary.ID || snapshot.Documents[0].ContextOnly {
		t.Fatalf("unexpected primary: %+v", snapshot.Documents[0])
	}
	if snapshot.Documents[1].ID != context.ID || !snapshot.Documents[1].ContextOnly {
		t.Fatalf("unexpected context: %+v", snapshot.Documents[1])
	}
	if len(snapshot.Connectors) != 1 || snapshot.Connectors[0].ID != connector.ID {
		t.Fatalf("connectors=%+v", snapshot.Connectors)
	}
	if len(snapshot.FailedWorkspaces) != 1 || snapshot.FailedWorkspaces[0] != "broken" {
		t.Fatalf("failed workspaces=%+v", snapshot.FailedWorkspaces)
	}

	snapshot.Embeddings[primary.ID][0] = 99
	snapshot.FailedWorkspaces[0] = "changed"
	if graph.Embeddings()[primary.ID][0] != 1 || graph.FailedWorkspaces()[0] != "broken" {
		t.Fatal("snapshot aliases live graph storage")
	}
}

func TestSnapshotDocumentsRejectsIndexBeforeInitialRebuild(t *testing.T) {
	api := NewAPIWithWorkspacesAsync(nil, "")
	_, err := api.SnapshotDocuments(DocumentWindowFilter{
		Start: time.Now(), End: time.Now().Add(24 * time.Hour),
	})
	if !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("want ErrIndexNotReady, got %v", err)
	}
}
