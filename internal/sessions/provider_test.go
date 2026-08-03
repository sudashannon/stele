package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderRegistryResolvesByNameAndRejectsUnknown(t *testing.T) {
	provider, ok := ProviderByName("OMP")
	if !ok || provider.Name() != "omp" {
		t.Fatalf("ProviderByName must resolve case-insensitively, got %v ok=%v", provider, ok)
	}
	if _, ok := ProviderByName("no-such-runtime"); ok {
		t.Fatal("an unregistered runtime must not resolve")
	}
	// Every registered runtime must be reachable by its own name: that pairing
	// is what a --sessions-source value resolves against.
	names := ProviderNames()
	if len(names) == 0 {
		t.Fatal("ProviderNames must not be empty")
	}
	for _, name := range names {
		registered, ok := ProviderByName(name)
		if !ok || registered.Name() != name {
			t.Fatalf("ProviderNames listed %q but ProviderByName returned %v/%v", name, registered, ok)
		}
	}
}

// Discovery expresses layout: which files are one session. A subagent's
// transcript is a part of the session that dispatched it, at any nesting depth,
// while the artifacts beside it are not transcripts at all.
func TestOMPDiscoverGroupsSubagentsIntoTheDispatchingSession(t *testing.T) {
	root := t.TempDir()
	bucket := filepath.Join(root, "-repo")
	primary := transcript(t, bucket, "2026-07-30T00-00-00-000Z_a.jsonl", sessionLine("a", "/repo", "A", "2026-07-30T01:00:00.000Z"))
	loose := transcript(t, root, "2026-07-30T00-00-00-000Z_loose.jsonl", sessionLine("loose", "/repo", "L", "2026-07-30T01:00:00.000Z"))
	artifacts := filepath.Join(bucket, "2026-07-30T00-00-00-000Z_a")
	child := transcript(t, artifacts, "Reviewer.jsonl", sessionLine("sub", "/repo", "", "2026-07-30T01:10:00.000Z"))
	grandchild := transcript(t, filepath.Join(artifacts, "Reviewer"), "Nested.jsonl", sessionLine("sub2", "/repo", "", "2026-07-30T01:20:00.000Z"))
	if err := os.WriteFile(filepath.Join(artifacts, "7.bash.log"), []byte("log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "report.md"), []byte("# report\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	units, err := ompProvider{}.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("units = %d, want 2 (one per dispatching session)", len(units))
	}
	byPrimary := map[string][]string{}
	for _, unit := range units {
		byPrimary[unit.Primary] = unit.Parts
	}
	if !equal(byPrimary[primary], []string{child, grandchild}) {
		t.Fatalf("parts = %v, want both nested transcripts", byPrimary[primary])
	}
	if parts, ok := byPrimary[loose]; !ok || len(parts) != 0 {
		t.Fatalf("a loose transcript is its own session with no parts, got %v ok=%v", parts, ok)
	}
}

func TestOMPDiscoverTreatsAMissingRootAsEmpty(t *testing.T) {
	units, err := ompProvider{}.Discover(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(units) != 0 {
		t.Fatalf("a missing root must be silent: %d units, err=%v", len(units), err)
	}
}

func TestMergeFoldsWorkAndKeepsIdentityAndHumanTurns(t *testing.T) {
	primary := Digest{
		ID: "parent", Path: "/s/parent.jsonl", Source: "omp", Cwd: "/repo", Title: "父会话",
		StartedAt: mustTime(t, "2026-07-30T01:00:00Z"), UpdatedAt: mustTime(t, "2026-07-30T02:00:00Z"),
		UserTurns: 3, ToolCalls: map[string]int{"read": 2}, Reads: []string{"/repo/a.md"}, Intents: []string{"父意图"},
		Offset: 512,
	}
	parts := []Digest{
		{
			ID: "late", Path: "/s/parent/Late.jsonl", Cwd: "/other", Title: "子",
			StartedAt: mustTime(t, "2026-07-30T03:00:00Z"), UpdatedAt: mustTime(t, "2026-07-30T04:00:00Z"),
			UserTurns: 9, ToolCalls: map[string]int{"read": 1, "edit": 4},
			Edits: []string{"/repo/b.md"}, Reads: []string{"/repo/a.md"}, Intents: []string{"晚意图"},
		},
		{
			ID: "early", Path: "/s/parent/Early.jsonl",
			StartedAt: mustTime(t, "2026-07-30T00:30:00Z"), UpdatedAt: mustTime(t, "2026-07-30T00:45:00Z"),
			UserTurns: 2, ToolCalls: map[string]int{"write": 1}, Writes: []string{"/repo/c.md"},
			Intents: []string{"早意图"}, PathsTruncated: true,
		},
	}

	merged := Merge(primary, parts)

	if merged.ID != "parent" || merged.Cwd != "/repo" || merged.Title != "父会话" || merged.Source != "omp" {
		t.Fatalf("identity must stay the primary's: %+v", merged)
	}
	if merged.UserTurns != 3 {
		t.Fatalf("UserTurns = %d, want 3: a subagent prompt is not a human turn", merged.UserTurns)
	}
	if merged.ToolCalls["read"] != 3 || merged.ToolCalls["edit"] != 4 || merged.ToolCalls["write"] != 1 {
		t.Fatalf("tool calls must sum: %v", merged.ToolCalls)
	}
	if !equal(merged.Reads, []string{"/repo/a.md"}) {
		t.Fatalf("paths must union without duplicates: %v", merged.Reads)
	}
	if !equal(merged.Edits, []string{"/repo/b.md"}) || !equal(merged.Writes, []string{"/repo/c.md"}) {
		t.Fatalf("a subagent's produced and patched documents must fold in: %+v", merged)
	}
	if !equal(merged.Subagents, []string{"Early", "Late"}) {
		t.Fatalf("Subagents = %v, want both names sorted", merged.Subagents)
	}
	// Intents read in the order the work happened, so the earliest subagent's
	// intent precedes the later one.
	if !equal(merged.Intents, []string{"父意图", "早意图", "晚意图"}) {
		t.Fatalf("Intents = %v", merged.Intents)
	}
	if !merged.PathsTruncated {
		t.Fatal("a truncated part must mark the merged digest truncated")
	}
	// The span covers every part: a subagent can start before the primary's
	// first event and finish after its last.
	if got := merged.StartedAt; !got.Equal(mustTime(t, "2026-07-30T00:30:00Z")) {
		t.Fatalf("StartedAt = %v", got)
	}
	if got := merged.UpdatedAt; !got.Equal(mustTime(t, "2026-07-30T04:00:00Z")) {
		t.Fatalf("UpdatedAt = %v", got)
	}
	if merged.Offset != 512 {
		t.Fatalf("Offset = %d: resume state belongs to the primary file", merged.Offset)
	}
}

func TestMergeWithoutPartsIsThePrimary(t *testing.T) {
	primary := Digest{ID: "solo", Path: "/s/solo.jsonl", ToolCalls: map[string]int{"read": 1}, Reads: []string{"/repo/a.md"}}
	merged := Merge(primary, nil)
	if merged.ID != "solo" || len(merged.Subagents) != 0 || merged.ToolCalls["read"] != 1 {
		t.Fatalf("a session without subagents must be unchanged: %+v", merged)
	}
}

func TestMergeCapsAPayloadGrownByManySubagents(t *testing.T) {
	primary := Digest{ID: "p", Path: "/s/p.jsonl", ToolCalls: map[string]int{}}
	var parts []Digest
	for i := 0; i < MaxIntents+10; i++ {
		parts = append(parts, Digest{
			Path:      filepath.Join("/s/p", fmtName(i)),
			StartedAt: mustTime(t, "2026-07-30T00:00:00Z").Add(minute(i)),
			Intents:   []string{fmtName(i)},
		})
	}
	merged := Merge(primary, parts)
	if len(merged.Intents) > MaxIntents || !merged.IntentsTruncated {
		t.Fatalf("intents = %d, truncated=%v; want capped at %d", len(merged.Intents), merged.IntentsTruncated, MaxIntents)
	}
}

// A part appended to after the last refresh must resume from its own offset,
// leaving the other transcripts in the unit untouched.
func TestStoreRefreshResumesOnlyTheChangedPart(t *testing.T) {
	root := t.TempDir()
	bucket := filepath.Join(root, "-repo")
	primary := transcript(t, bucket, "2026-07-30T00-00-00-000Z_a.jsonl",
		sessionLine("a", "/repo", "A", "2026-07-30T01:00:00.000Z"),
		toolCallLine("2026-07-30T01:00:01.000Z", "read", "读 A", map[string]any{"path": "a.md"}),
	)
	childPath := transcript(t, filepath.Join(bucket, "2026-07-30T00-00-00-000Z_a"), "Worker.jsonl",
		sessionLine("sub", "/repo", "", "2026-07-30T01:10:00.000Z"),
		toolCallLine("2026-07-30T01:10:01.000Z", "read", "读 B", map[string]any{"path": "b.md"}),
	)

	store := NewStore(filepath.Join(root, "sessions.json"))
	if _, _, err := store.Refresh(ompSources(root)); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Get(primary)
	if !equal(before.Reads, []string{"/repo/a.md", "/repo/b.md"}) {
		t.Fatalf("Reads = %v, want both transcripts' documents", before.Reads)
	}
	primaryOffset := storedPart(t, store, primary, primary).Offset

	appendLines(t, childPath, toolCallLine("2026-07-30T01:20:00.000Z", "write", "写 C", map[string]any{"path": "c.md"}))
	changed, _, err := store.Refresh(ompSources(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("changed = %d, want the one session whose part grew", len(changed))
	}
	after, _ := store.Get(primary)
	if !equal(after.Writes, []string{"/repo/c.md"}) {
		t.Fatalf("Writes = %v, want the appended document", after.Writes)
	}
	if got := storedPart(t, store, primary, primary).Offset; got != primaryOffset {
		t.Fatalf("primary re-read: offset %d -> %d", primaryOffset, got)
	}
	if got := storedPart(t, store, primary, childPath).Offset; got <= 0 {
		t.Fatalf("part offset = %d, want a resume position", got)
	}
}

// Two runtimes pointed at different roots stay separate entries, each tagged
// with the provider that produced it.
func TestStoreRefreshKeepsSourcesApart(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	transcript(t, filepath.Join(first, "-repo"), "2026-07-30T00-00-00-000Z_a.jsonl", sessionLine("a", "/repo", "A", "2026-07-30T01:00:00.000Z"))
	transcript(t, filepath.Join(second, "-repo"), "2026-07-30T00-00-00-000Z_b.jsonl", sessionLine("b", "/repo", "B", "2026-07-30T02:00:00.000Z"))

	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	other := renamedProvider{Provider: ompProvider{}, name: "other"}
	_, total, err := store.Refresh([]Source{{Provider: ompProvider{}, Root: first}, {Provider: other, Root: second}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want one session per source", total)
	}
	sourcesByID := map[string]string{}
	for _, digest := range store.List() {
		sourcesByID[digest.ID] = digest.Source
	}
	if sourcesByID["a"] != "omp" || sourcesByID["b"] != "other" {
		t.Fatalf("sources = %v", sourcesByID)
	}
}

func TestStoreRefreshSkipsUnusableSources(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	_, total, err := store.Refresh([]Source{{Provider: nil, Root: "/x"}, {Provider: ompProvider{}, Root: ""}})
	if err != nil || total != 0 {
		t.Fatalf("an unusable source must be a no-op: total=%d err=%v", total, err)
	}
}

// renamedProvider stands in for a second runtime: same format, different name.
type renamedProvider struct {
	Provider
	name string
}

func (p renamedProvider) Name() string { return p.name }
