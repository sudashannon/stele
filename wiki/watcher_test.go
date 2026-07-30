package wiki

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsWikiFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"changes/x/design.md", true},
		{"changes/x/.comet.yaml", true},
		{"changes/x/image.png", false},
		{"/absolute/path/to/proposal.md", true},
		{"changes/x/comet.yaml", false},
		{".trellis/tasks/07-26-beta/task.json", true},
		{".trellis/tasks/07-26-beta/implement.jsonl", true},
		{"changes/x/notes.markdown", false},
	}
	for _, c := range cases {
		if got := isWikiFile(c.path); got != c.want {
			t.Errorf("isWikiFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestNewWatcherDefaults(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))
	w := NewWatcher(api, "scripts/embed.ts")
	if w.debounce != 5e9 {
		t.Errorf("debounce = %v, want 5s", w.debounce)
	}
	if w.communityDelay != 30e9 {
		t.Errorf("communityDelay = %v, want 30s", w.communityDelay)
	}
}

func TestCommunityRedetectionRequiresDirtyThreshold(t *testing.T) {
	graph := BuildGraph([]Component{{ID: "a"}}, nil)
	graph.SetCommunities(map[string]int{"sentinel": 7})
	api := NewAPI(graph)
	watcher := NewWatcher(api, "")

	api.AddDirty(communityDirtyThreshold)
	watcher.redetectCommunities()
	if api.DirtyCount() != communityDirtyThreshold {
		t.Fatalf("dirty count below trigger was reset: %d", api.DirtyCount())
	}
	if got := graph.Communities()["sentinel"]; got != 7 {
		t.Fatalf("communities changed below threshold: sentinel=%d", got)
	}

	api.AddDirty(1)
	watcher.redetectCommunities()
	if api.DirtyCount() != 0 {
		t.Fatalf("dirty count after re-detection = %d, want 0", api.DirtyCount())
	}
	if _, stale := graph.Communities()["sentinel"]; stale {
		t.Fatal("community detection did not replace stale communities")
	}
}

func TestWatcherPrunesTrellisRuntime(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".trellis")
	tasks := filepath.Join(root, "tasks")
	runtimeDir := filepath.Join(root, ".runtime", "sessions")
	for _, dir := range []string{tasks, runtimeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	watcher := NewWatcher(NewAPI(BuildGraph(nil, nil)), "")
	if err := watcher.Start([]string{root}); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()
	watched := map[string]bool{}
	for _, path := range watcher.watcher.WatchList() {
		watched[filepath.Clean(path)] = true
	}
	if !watched[root] || !watched[tasks] {
		t.Fatalf("expected .trellis and tasks watches, got %v", watched)
	}
	if watched[runtimeDir] || watched[filepath.Dir(runtimeDir)] {
		t.Fatalf(".trellis runtime must not be watched: %v", watched)
	}
}

func TestWatcherResetPathsReplacesTreesAndPreservesOverlappingRoot(t *testing.T) {
	oldRoot := t.TempDir()
	oldOnly := filepath.Join(oldRoot, "old-only")
	retainedRoot := filepath.Join(oldRoot, "retained")
	retainedChild := filepath.Join(retainedRoot, "child")
	newRoot := t.TempDir()
	newChild := filepath.Join(newRoot, "new-child")
	for _, dir := range []string{oldOnly, retainedChild, newChild} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	watcher := NewWatcher(NewAPI(BuildGraph(nil, nil)), "")
	if err := watcher.Start([]string{oldRoot}); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	watcher.ResetPaths([]string{retainedRoot, newRoot})

	watched := map[string]bool{}
	for _, path := range watcher.watcher.WatchList() {
		watched[filepath.Clean(path)] = true
	}
	for _, stale := range []string{oldRoot, oldOnly} {
		if watched[stale] {
			t.Fatalf("stale tree remains watched after roots reset: %s", stale)
		}
	}
	for _, desired := range []string{retainedRoot, retainedChild, newRoot, newChild} {
		if !watched[desired] {
			t.Fatalf("desired tree is not watched after roots reset: %s (watches: %v)", desired, watched)
		}
	}
}

func TestWatcherRelativeRootWatchesCreatedSubtree(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	relativeRoot, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}

	watcher := NewWatcher(NewAPI(BuildGraph(nil, nil)), "")
	watcher.debounce = time.Hour
	if err := watcher.Start([]string{relativeRoot}); err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	absoluteRoot, err := filepath.Abs(relativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	if _, ok := watcher.roots[absoluteRoot]; !ok {
		t.Fatalf("relative watch root was not normalized to %q: %v", absoluteRoot, watcher.roots)
	}

	child := filepath.Join(absoluteRoot, "created", "subtree")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		watched := false
		for _, path := range watcher.watcher.WatchList() {
			if filepath.Clean(path) == child {
				watched = true
				break
			}
		}
		if watched {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("created subtree %q was not watched: %v", child, watcher.watcher.WatchList())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatcherStopAndResetAreSafeAcrossLifecycle(t *testing.T) {
	notStarted := NewWatcher(NewAPI(BuildGraph(nil, nil)), "")
	notStarted.ResetPaths([]string{t.TempDir()})
	notStarted.Stop()
	notStarted.Stop()

	if err := notStarted.Start([]string{t.TempDir()}); err == nil {
		t.Fatal("Start after Stop unexpectedly succeeded")
	}
	notStarted.ResetPaths([]string{t.TempDir()})
	notStarted.Stop()

	started := NewWatcher(NewAPI(BuildGraph(nil, nil)), "")
	if err := started.Start([]string{t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	started.Stop()
	started.ResetPaths([]string{t.TempDir()})
	started.Stop()
}

func TestRequiresFullRebuildForSuperpowersArtifacts(t *testing.T) {
	path := filepath.Join("/repo", "docs", "superpowers", "plans", "2026-07-21-cache.md")
	if !requiresFullRebuild([]string{path}) {
		t.Fatalf("Superpowers convention changes require a full rebuild: %q", path)
	}
	if requiresFullRebuild([]string{filepath.Join("/repo", "docs", "notes.md")}) {
		t.Fatal("unrelated project docs must retain incremental updates")
	}
}
