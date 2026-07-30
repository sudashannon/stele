package wiki

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// slowLister simulates a large workspace tree whose scan would take a long
// time (the real-world ~38s cold-start scenario), by blocking List() until
// unblocked is closed. It is only ever consulted by (*API).Rebuild, never
// by NewAPIWithWorkspacesAsync itself.
type slowLister struct {
	workspaces []WorkspaceConfig
	unblocked  chan struct{}
}

func (l *slowLister) List() []WorkspaceConfig {
	<-l.unblocked
	return l.workspaces
}

type serialLister struct {
	mu            sync.Mutex
	calls         int
	first         []WorkspaceConfig
	latest        []WorkspaceConfig
	firstEntered  chan struct{}
	secondEntered chan struct{}
	releaseFirst  chan struct{}
}

func (l *serialLister) List() []WorkspaceConfig {
	l.mu.Lock()
	l.calls++
	call := l.calls
	l.mu.Unlock()
	if call == 1 {
		close(l.firstEntered)
		<-l.releaseFirst
		return l.first
	}
	if call == 2 {
		close(l.secondEntered)
	}
	return l.latest
}

// TestNewAPIWithWorkspacesAsync_DoesNotBlockOnSlowWorkspace proves the
// cold-start fix: constructing the API never waits on a workspace scan,
// even one that would take an arbitrarily long time (here, forever, since
// unblocked is never closed during this test). This is what lets main.go
// call http.ListenAndServe immediately after construction instead of
// leaving the dashboard unreachable for the whole initial index build.
func TestNewAPIWithWorkspacesAsync_DoesNotBlockOnSlowWorkspace(t *testing.T) {
	lister := &slowLister{unblocked: make(chan struct{})} // never closed

	done := make(chan *API, 1)
	go func() {
		api := NewAPIWithWorkspacesAsync(nil, "")
		api.SetLister(lister)
		done <- api
	}()

	select {
	case api := <-done:
		if api == nil {
			t.Fatal("NewAPIWithWorkspacesAsync returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("NewAPIWithWorkspacesAsync blocked on workspace scan; expected immediate return")
	}
}

// TestAsyncColdStart_ServesEmptyThenPopulatesAfterRebuild exercises the full
// cold-start flow main.go performs: construct with an empty graph (serving
// `[]` on /api/wiki/index and /api/wiki/lint instead of 500/panic/null),
// then run the initial build (as main.go's background goroutine calls
// Rebuild) and confirm the index is populated afterward.
func TestAsyncColdStart_ServesEmptyThenPopulatesAfterRebuild(t *testing.T) {
	root := t.TempDir()
	openspecDir := filepath.Join(root, "openspec")
	changeDir := filepath.Join(openspecDir, "changes", "cold-start-change")
	os.MkdirAll(changeDir, 0755)
	os.WriteFile(filepath.Join(changeDir, ".comet.yaml"), []byte("design_doc: design.md\n"), 0644)
	os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Design\n"), 0644)

	lister := &slowLister{
		workspaces: []WorkspaceConfig{{Alias: "miao", Path: openspecDir}},
		unblocked:  make(chan struct{}),
	}

	api := NewAPIWithWorkspacesAsync(nil, "")
	api.SetLister(lister)

	// Before the build completes, /api/wiki/index must serve `[]`, not
	// null and not a panic/500 on a nil graph.
	indexReq := httptest.NewRequest("GET", "/api/wiki/index", nil)
	indexW := httptest.NewRecorder()
	api.HandleIndex(indexW, indexReq)
	if indexW.Code != http.StatusOK {
		t.Fatalf("HandleIndex before build: expected 200, got %d: %s", indexW.Code, indexW.Body.String())
	}
	if got := indexW.Body.String(); got != "[]\n" {
		t.Errorf("HandleIndex before build: expected empty array `[]`, got %q", got)
	}

	// Likewise /api/wiki/lint must serve `[]`, not `null`, on the empty
	// graph (mirrors the existing nil-normalization contract in
	// HandleLint, now exercised against the async-constructed empty
	// graph rather than an explicitly-built one).
	lintReq := httptest.NewRequest("GET", "/api/wiki/lint", nil)
	lintW := httptest.NewRecorder()
	api.HandleLint(lintW, lintReq)
	if got := lintW.Body.String(); got != "[]\n" {
		t.Errorf("HandleLint before build: expected empty array `[]`, got %q", got)
	}

	// Kick off the initial build the way main.go's background goroutine
	// does, then unblock the "slow" lister and wait for it to finish.
	rebuildDone := make(chan error, 1)
	go func() { rebuildDone <- api.Rebuild() }()
	close(lister.unblocked)

	select {
	case err := <-rebuildDone:
		if err != nil {
			t.Fatalf("Rebuild: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Rebuild did not complete after lister unblocked")
	}

	indexReq2 := httptest.NewRequest("GET", "/api/wiki/index", nil)
	indexW2 := httptest.NewRecorder()
	api.HandleIndex(indexW2, indexReq2)

	var components []Component
	if err := json.Unmarshal(indexW2.Body.Bytes(), &components); err != nil {
		t.Fatalf("decode index response: %v", err)
	}
	wantID := filepath.Join(changeDir, ".comet.yaml")
	var found bool
	for _, c := range components {
		if c.ID == wantID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected index to be populated with %q after async Rebuild, got %+v", wantID, components)
	}
}

func TestRebuildSerializesLiveSnapshotsAndPublishesLatest(t *testing.T) {
	makeWorkspace := func(name string) (WorkspaceConfig, string) {
		root := filepath.Join(t.TempDir(), name)
		changeDir := filepath.Join(root, "changes", name)
		if err := os.MkdirAll(changeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		componentPath := filepath.Join(changeDir, ".comet.yaml")
		if err := os.WriteFile(componentPath, []byte("design_doc: design.md\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(changeDir, "design.md"), []byte("# Design\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return WorkspaceConfig{Alias: name, Path: root}, componentPath
	}
	oldWorkspace, oldID := makeWorkspace("old")
	latestWorkspace, latestID := makeWorkspace("latest")
	lister := &serialLister{
		first:         []WorkspaceConfig{oldWorkspace},
		latest:        []WorkspaceConfig{latestWorkspace},
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
	api := NewAPIWithWorkspacesAsync(nil, "")
	api.SetLister(lister)

	done := make(chan error, 2)
	go func() { done <- api.Rebuild() }()
	select {
	case <-lister.firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first rebuild did not read the live registry")
	}
	go func() { done <- api.Rebuild() }()

	select {
	case <-lister.secondEntered:
		t.Fatal("second rebuild read the registry before the first rebuild released serialization")
	case <-time.After(100 * time.Millisecond):
	}
	close(lister.releaseFirst)
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Rebuild: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("serialized rebuilds did not complete")
		}
	}

	indexW := httptest.NewRecorder()
	api.HandleIndex(indexW, httptest.NewRequest("GET", "/api/wiki/index", nil))
	var components []Component
	if err := json.Unmarshal(indexW.Body.Bytes(), &components); err != nil {
		t.Fatalf("decode index response: %v", err)
	}
	foundLatest := false
	for _, component := range components {
		if component.ID == oldID {
			t.Fatalf("old rebuild overwrote latest registry state: %+v", components)
		}
		if component.ID == latestID {
			foundLatest = true
		}
	}
	if !foundLatest {
		t.Fatalf("latest registry component %q missing from final index: %+v", latestID, components)
	}
}
