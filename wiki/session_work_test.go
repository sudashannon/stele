package wiki

import (
	"testing"
	"time"
)

// A report's effort axis comes from this snapshot. It must count only what
// happened inside the window: a long-running session contributes the work it did
// that week, not its lifetime total.
func TestSnapshotSessionWorkCountsOnlyTheWindow(t *testing.T) {
	api, docPath, transcriptPath := sessionFixture(t)

	day := func(value string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	inside := api.SnapshotSessionWork(day("2026-07-30"), day("2026-07-31"), "")
	if len(inside) != 1 {
		t.Fatalf("sessions = %d, want the one active that day", len(inside))
	}
	item := inside[0]
	if item.Path != transcriptPath {
		t.Fatalf("path = %q, want the transcript", item.Path)
	}
	if item.ActiveDays != 1 || item.Events == 0 {
		t.Fatalf("effort not measured: %+v", item)
	}
	if item.Workspace != "proj" {
		t.Fatalf("workspace = %q", item.Workspace)
	}

	outside := api.SnapshotSessionWork(day("2026-08-10"), day("2026-08-17"), "")
	if len(outside) != 0 {
		t.Fatalf("a session with no activity in the window must not appear: %+v", outside)
	}
	_ = docPath
}

// Reading a document is not producing it. Attributing a report section by reads
// would credit whoever consulted a document most, so only writes and edits are
// returned.
func TestSnapshotSessionWorkSeparatesAuthorshipFromReads(t *testing.T) {
	api, docPath, _ := sessionFixture(t)

	items := api.SnapshotSessionWork(
		time.Date(2026, 7, 30, 0, 0, 0, 0, time.Local),
		time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local), "")
	if len(items) != 1 {
		t.Fatalf("sessions = %d", len(items))
	}
	produced := append(append([]string(nil), items[0].Produced...), items[0].Edited...)
	if len(produced) != 1 || produced[0] != docPath {
		t.Fatalf("authored documents = %v, want the written one", produced)
	}
}

// A transcript is never a document. If a session's own path could enter the
// authored list, a report would cite a derived artifact as evidence.
func TestSnapshotSessionWorkNeverReturnsTranscriptsAsDocuments(t *testing.T) {
	api, _, transcriptPath := sessionFixture(t)

	items := api.SnapshotSessionWork(
		time.Date(2026, 7, 30, 0, 0, 0, 0, time.Local),
		time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local), "")
	for _, item := range items {
		for _, path := range append(append([]string(nil), item.Produced...), item.Edited...) {
			if path == transcriptPath {
				t.Fatalf("transcript leaked into authored documents: %v", item)
			}
		}
	}
}

// A workspace-scoped report must not pull in another workspace's sessions.
func TestSnapshotSessionWorkRespectsWorkspaceFilter(t *testing.T) {
	api, _, _ := sessionFixture(t)

	if items := api.SnapshotSessionWork(
		time.Date(2026, 7, 30, 0, 0, 0, 0, time.Local),
		time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local), "other"); len(items) != 0 {
		t.Fatalf("foreign workspace returned sessions: %+v", items)
	}
	if items := api.SnapshotSessionWork(
		time.Date(2026, 7, 30, 0, 0, 0, 0, time.Local),
		time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local), "proj"); len(items) != 1 {
		t.Fatalf("own workspace returned %d sessions", len(items))
	}
}

// An empty or inverted window is a caller bug; returning everything would silently
// produce a report over all of history.
func TestSnapshotSessionWorkRejectsEmptyWindow(t *testing.T) {
	api, _, _ := sessionFixture(t)
	moment := time.Date(2026, 7, 30, 0, 0, 0, 0, time.Local)

	if items := api.SnapshotSessionWork(moment, moment, ""); items != nil {
		t.Fatalf("zero-width window returned %+v", items)
	}
	if items := api.SnapshotSessionWork(moment, moment.AddDate(0, 0, -1), ""); items != nil {
		t.Fatalf("inverted window returned %+v", items)
	}
}
