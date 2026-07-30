package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"comet-ui/internal/source"
)

func TestHandleListWorkspaces_Empty(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	req := httptest.NewRequest("GET", "/api/workspaces", nil)
	w := httptest.NewRecorder()
	handleListWorkspaces(w, req, reg)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []WorkspaceConfig
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %+v", got)
	}
}

func TestHandleAddWorkspace_PersistsAndReturns201(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	// The workspace Path must be a real, existing directory now that
	// validateWorkspacePath rejects non-existent paths at Add() time.
	miaoPath := filepath.Join(t.TempDir(), "miao", "openspec")
	os.MkdirAll(filepath.Join(miaoPath, "changes"), 0755)
	body, _ := json.Marshal(WorkspaceConfig{Alias: "miao", Path: miaoPath, Color: "#0063f8"})
	req := httptest.NewRequest("POST", "/api/workspaces", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAddWorkspace(w, req, reg)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(reg.List()) != 1 {
		t.Fatalf("expected registry to contain 1 workspace, got %d", len(reg.List()))
	}
}

func TestHandleWorkspaces_DeleteStatuses(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		target        string
		wantStatus    int
		wantCallbacks int
	}{
		{name: "success", method: http.MethodDelete, target: "/api/workspaces?alias=miao", wantStatus: http.StatusNoContent, wantCallbacks: 1},
		{name: "missing alias", method: http.MethodDelete, target: "/api/workspaces", wantStatus: http.StatusBadRequest},
		{name: "unknown alias", method: http.MethodDelete, target: "/api/workspaces?alias=unknown", wantStatus: http.StatusNotFound},
		{name: "unsupported method", method: http.MethodPut, target: "/api/workspaces?alias=miao", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "workspaces.yaml")
			if err := persistWorkspaces(configPath, []WorkspaceConfig{{Alias: "miao", Path: "/tmp"}}, SyncConfig{}); err != nil {
				t.Fatal(err)
			}
			reg, err := NewWorkspaceRegistry(configPath)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(tt.method, tt.target, nil)
			w := httptest.NewRecorder()
			callbacks := 0
			registrySizesAtCallback := []int{}

			handleWorkspaces(w, req, reg, func() {
				callbacks++
				registrySizesAtCallback = append(registrySizesAtCallback, len(reg.List()))
			})

			if callbacks != tt.wantCallbacks {
				t.Fatalf("workspace change callbacks = %d, want %d", callbacks, tt.wantCallbacks)
			}
			if w.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantStatus == http.StatusNoContent {
				if w.Body.Len() != 0 {
					t.Fatalf("expected empty 204 response body, got %q", w.Body.String())
				}
				if len(reg.List()) != 0 {
					t.Fatalf("expected successful DELETE to update registry, got %+v", reg.List())
				}
				if len(registrySizesAtCallback) != 1 || registrySizesAtCallback[0] != 0 {
					t.Fatalf("DELETE callback ran before registry removal: sizes=%v", registrySizesAtCallback)
				}
			}
		})
	}
}

func TestHandleListChanges_FallsBackToSingleDirWhenNoWorkspacesRegistered(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "changes", "my-change"), 0755)
	writeYAML(t, filepath.Join(dir, "changes", "my-change"), "phase: build\n")

	reg, _ := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml")) // empty registry

	req := httptest.NewRequest("GET", "/api/changes", nil)
	w := httptest.NewRecorder()
	handleListChangesMultiWorkspace(w, req, dir, reg)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Changes []ChangeSummary `json:"changes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Changes) != 1 {
		t.Fatalf("expected fallback to single-dir scan to find 1 change, got %d", len(body.Changes))
	}
}

func TestHandleGetChange_RoutesViaWorkspaceAlias(t *testing.T) {
	wsA := t.TempDir()
	openspecA := filepath.Join(wsA, "openspec")
	os.MkdirAll(filepath.Join(openspecA, "changes", "change-a"), 0755)
	writeYAML(t, filepath.Join(openspecA, "changes", "change-a"), "phase: build\n")

	wsB := t.TempDir()
	openspecB := filepath.Join(wsB, "openspec")
	os.MkdirAll(filepath.Join(openspecB, "changes", "change-a"), 0755)
	writeYAML(t, filepath.Join(openspecB, "changes", "change-a"), "phase: design\n")

	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))
	reg.Add(WorkspaceConfig{Alias: "a", Path: openspecA, Color: "#000"})
	reg.Add(WorkspaceConfig{Alias: "b", Path: openspecB, Color: "#111"})

	req := httptest.NewRequest("GET", "/api/changes/change-a?workspace=b", nil)
	w := httptest.NewRecorder()
	handleGetChange(w, req, "unused-default", reg)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail ChangeDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Phase != "design" {
		t.Fatalf("expected change-a resolved from workspace b (phase=design), got phase=%q", detail.Phase)
	}
}

func TestHandleGetChange_FallsBackToBaseDirWhenNoWorkspaceParam(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "changes", "my-change"), 0755)
	writeYAML(t, filepath.Join(dir, "changes", "my-change"), "phase: build\n")

	reg, _ := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml"))
	reg.Add(WorkspaceConfig{Alias: "other", Path: t.TempDir(), Color: "#000"})

	req := httptest.NewRequest("GET", "/api/changes/my-change", nil)
	w := httptest.NewRecorder()
	handleGetChange(w, req, dir, reg)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 falling back to baseDir, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetChange_UnregisteredWorkspaceAliasReturns400(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	req := httptest.NewRequest("GET", "/api/changes/my-change?workspace=ghost", nil)
	w := httptest.NewRecorder()
	handleGetChange(w, req, ".", reg)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unregistered workspace alias, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetArtifact_TraversalGuardUsesResolvedWorkspaceRoot(t *testing.T) {
	wsA := t.TempDir()
	openspecA := filepath.Join(wsA, "openspec")
	os.MkdirAll(filepath.Join(openspecA, "changes"), 0755)
	secretA := filepath.Join(wsA, "secret-a.txt")
	os.WriteFile(secretA, []byte("secret-a"), 0644)

	wsB := t.TempDir()
	openspecB := filepath.Join(wsB, "openspec")
	os.MkdirAll(filepath.Join(openspecB, "changes"), 0755)
	secretB := filepath.Join(wsB, "secret-b.txt")
	os.WriteFile(secretB, []byte("secret-b"), 0644)

	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))
	reg.Add(WorkspaceConfig{Alias: "a", Path: openspecA, Color: "#000"})
	reg.Add(WorkspaceConfig{Alias: "b", Path: openspecB, Color: "#111"})

	// Requesting workspace a's artifact while trying to path-escape into
	// workspace b's secret must be rejected — the guard must be recomputed
	// from the resolved workspace root (wsA), not from baseDir.
	req := httptest.NewRequest("GET", "/api/artifact?workspace=a&path="+secretB, nil)
	w := httptest.NewRecorder()
	handleGetArtifact(w, req, "unused-default", reg)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-workspace escape rejected, got %d: %s", w.Code, w.Body.String())
	}

	// A legitimate in-workspace artifact request must still succeed.
	req2 := httptest.NewRequest("GET", "/api/artifact?workspace=a&path="+secretA, nil)
	w2 := httptest.NewRecorder()
	handleGetArtifact(w2, req2, "unused-default", reg)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for in-workspace artifact, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestHandleGetArtifact_SiblingPrefixEscapeBlocked exercises the exact
// vulnerability flagged in review: strings.HasPrefix(absPath, rootAbs) does
// a plain string-prefix comparison, so a sibling directory whose name is
// prefixed by the workspace root's name (e.g. "<root>-evil") satisfies the
// old guard even though it is NOT inside the workspace. This must be
// rejected with 403.
func TestHandleGetArtifact_SiblingPrefixEscapeBlocked(t *testing.T) {
	base := t.TempDir()
	// The traversal guard's root is the PARENT of the resolved workspace
	// dir (openspecPath's parent), so nest the registered path one level
	// deep: rootAbs will resolve to base/ws, and base/ws-evil is a sibling
	// of "ws" at that same level — exactly the string-prefix collision
	// strings.HasPrefix("/base/ws-evil", "/base/ws") falsely allows.
	wsRoot := filepath.Join(base, "ws")
	openspecDir := filepath.Join(wsRoot, "openspec")
	os.MkdirAll(filepath.Join(openspecDir, "changes"), 0755)

	evilRoot := filepath.Join(base, "ws-evil")
	os.MkdirAll(evilRoot, 0755)
	secret := filepath.Join(evilRoot, "secret.txt")
	os.WriteFile(secret, []byte("top-secret"), 0644)

	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))
	reg.Add(WorkspaceConfig{Alias: "w", Path: openspecDir, Color: "#000"})

	req := httptest.NewRequest("GET", "/api/artifact?workspace=w&path="+secret, nil)
	w := httptest.NewRecorder()
	handleGetArtifact(w, req, "unused-default", reg)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for sibling-prefix path escape (ws vs ws-evil), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetArtifact_SuperpowersRestrictsDurableRootsAndSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "superpowers-project")
	designPath := filepath.Join(root, "docs", "superpowers", "specs", "2026-07-20-cache-design.md")
	if err := os.MkdirAll(filepath.Dir(designPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(designPath, []byte("# Cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(root, "secret.md")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(filepath.Dir(designPath), "outside.md")
	if err := os.Symlink(secretPath, symlinkPath); err != nil {
		t.Fatal(err)
	}

	reg, err := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{
		Alias: "superpowers", Path: root, Color: "#123456", Type: source.KindSuperpowers,
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		path string
		want int
	}{
		{name: "durable design", path: designPath, want: http.StatusOK},
		{name: "unrelated project file", path: secretPath, want: http.StatusForbidden},
		{name: "allowlist symlink escape", path: symlinkPath, want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/artifact?workspace=superpowers&path="+test.path, nil)
			recorder := httptest.NewRecorder()
			handleGetArtifact(recorder, req, "unused-default", reg)
			if recorder.Code != test.want {
				t.Fatalf("artifact %q returned %d, want %d: %s", test.path, recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

// TestWorkspaceRegistry_Add_RejectsRootPath ensures registering "/" (or any
// non-absolute / non-existent path) as a workspace is rejected outright,
// since an unvalidated root path makes the traversal guard a no-op and
// permits reading arbitrary files on the host.
func TestWorkspaceRegistry_Add_RejectsRootPath(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	if err := reg.Add(WorkspaceConfig{Alias: "root", Path: "/", Color: "#000"}); err == nil {
		t.Fatal("expected Add to reject Path \"/\", got nil error")
	}
	if len(reg.List()) != 0 {
		t.Fatalf("expected registry to remain empty after rejected root path, got %d", len(reg.List()))
	}
}

func TestWorkspaceRegistry_Add_RejectsNonAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	if err := reg.Add(WorkspaceConfig{Alias: "rel", Path: "relative/path", Color: "#000"}); err == nil {
		t.Fatal("expected Add to reject a non-absolute Path, got nil error")
	}
}

func TestWorkspaceRegistry_Add_RejectsNonExistentPath(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	if err := reg.Add(WorkspaceConfig{Alias: "ghost", Path: filepath.Join(dir, "does-not-exist"), Color: "#000"}); err == nil {
		t.Fatal("expected Add to reject a non-existent Path, got nil error")
	}
}

func TestHandleAddWorkspace_RootPathReturns400(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	body, _ := json.Marshal(WorkspaceConfig{Alias: "root", Path: "/", Color: "#000"})
	req := httptest.NewRequest("POST", "/api/workspaces", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAddWorkspace(w, req, reg)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for root path workspace registration, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAddWorkspace_UnreadableWorkspaceReturns400(t *testing.T) {
	// A path that is absolute, exists, is a directory, and is not the
	// filesystem root — but has neither changes/ nor openspec/changes/ —
	// must be rejected at add-time with the zh-CN "unreadable workspace"
	// message, not silently accepted.
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	emptyPath := filepath.Join(t.TempDir(), "no-changes-here")
	os.MkdirAll(emptyPath, 0755)
	body, _ := json.Marshal(WorkspaceConfig{Alias: "unreadable", Path: emptyPath, Color: "#000"})
	req := httptest.NewRequest("POST", "/api/workspaces", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAddWorkspace(w, req, reg)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a workspace with no supported source layout, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "OpenSpec, Trellis, or Superpowers") {
		t.Fatalf("expected error body to list supported source layouts, got: %s", w.Body.String())
	}
	if len(reg.List()) != 0 {
		t.Fatalf("expected registry to remain empty after rejected unreadable path, got %d", len(reg.List()))
	}
}

func TestHandleAddWorkspace_ReadableWorkspaceWithOpenspecChangesReturns201(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))

	repoRootPath := filepath.Join(t.TempDir(), "repo-root")
	os.MkdirAll(filepath.Join(repoRootPath, "openspec", "changes"), 0755)
	body, _ := json.Marshal(WorkspaceConfig{Alias: "repo-root", Path: repoRootPath, Color: "#000"})
	req := httptest.NewRequest("POST", "/api/workspaces", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAddWorkspace(w, req, reg)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a workspace dir with openspec/changes/, got %d: %s", w.Code, w.Body.String())
	}
	if len(reg.List()) != 1 {
		t.Fatalf("expected registry to contain 1 workspace, got %d", len(reg.List()))
	}
}

func TestHandleListChangesAndDetail_ThreeSourceWorkspaces(t *testing.T) {
	openSpec := filepath.Join(t.TempDir(), "openspec")
	openChange := filepath.Join(openSpec, "changes", "open-change")
	if err := os.MkdirAll(openChange, 0o755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, openChange, "phase: build\n")

	trellisRoot := t.TempDir()
	trellisTask := filepath.Join(trellisRoot, ".trellis", "tasks", "07-26-task")
	writeMainTrellisTask(t, trellisTask, "planning")
	if err := os.WriteFile(filepath.Join(trellisTask, "prd.md"), []byte("# Trellis PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	superpowersRoot := t.TempDir()
	superpowersDesign := writeSuperpowersFile(t, superpowersRoot, "docs/superpowers/specs/2026-07-26-idea-design.md", "# Standalone Idea\n")

	reg, err := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "open", Path: openSpec, Color: "#000"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "trellis", Path: trellisRoot, Color: "#111"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "superpowers", Path: superpowersRoot, Color: "#222"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/changes", nil)
	w := httptest.NewRecorder()
	handleListChangesMultiWorkspace(w, req, openSpec, reg)
	if w.Code != http.StatusOK {
		t.Fatalf("expected mixed list 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Changes []ChangeSummary `json:"changes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Changes) != 3 {
		t.Fatalf("expected all three source types, got %+v", response.Changes)
	}
	types := map[string]string{}
	for _, change := range response.Changes {
		types[change.Workspace] = string(change.SourceType)
	}
	if types["open"] != "openspec" || types["trellis"] != "trellis" || types["superpowers"] != "superpowers" {
		t.Fatalf("unexpected source mapping: %v", types)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/changes/07-26-task?workspace=trellis", nil)
	w = httptest.NewRecorder()
	handleGetChange(w, req, openSpec, reg)
	if w.Code != http.StatusOK {
		t.Fatalf("expected Trellis detail 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail ChangeDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.SourceType != "trellis" || detail.Title != "Beta Task" || len(detail.Phases) != 3 {
		t.Fatalf("unexpected Trellis detail: %+v", detail)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/changes/idea?workspace=superpowers", nil)
	w = httptest.NewRecorder()
	handleGetChange(w, req, openSpec, reg)
	if w.Code != http.StatusOK {
		t.Fatalf("expected Superpowers detail 200, got %d: %s", w.Code, w.Body.String())
	}
	detail = ChangeDetail{}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.SourceType != source.KindSuperpowers || detail.Title != "Standalone Idea" ||
		detail.ComponentID != superpowersDesign || len(detail.Phases) != 5 || detail.NextTransition != nil {
		t.Fatalf("unexpected Superpowers detail: %+v", detail)
	}

	artifactPath := filepath.Join(trellisTask, "prd.md")
	req = httptest.NewRequest(http.MethodGet, "/api/artifact?workspace=trellis&path="+artifactPath, nil)
	w = httptest.NewRecorder()
	handleGetArtifact(w, req, openSpec, reg)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Trellis PRD") {
		t.Fatalf("expected Trellis artifact read, got %d: %s", w.Code, w.Body.String())
	}

	sibling := filepath.Join(filepath.Dir(trellisRoot), "outside-trellis.md")
	if err := os.WriteFile(sibling, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/artifact?workspace=trellis&path="+sibling, nil)
	w = httptest.NewRecorder()
	handleGetArtifact(w, req, openSpec, reg)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected Trellis sibling path to be rejected, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkspacesForRuntimeDetectsTrellisDirFallback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "trellis-project")
	if err := os.MkdirAll(filepath.Join(root, ".trellis", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := workspacesForRuntime(nil, root)
	if len(got) != 1 || got[0].Path != root || got[0].Type != "trellis" {
		t.Fatalf("unexpected --dir fallback: %+v", got)
	}

	configured := []WorkspaceConfig{{Alias: "explicit", Path: "/configured", Type: "openspec"}}
	got = workspacesForRuntime(configured, root)
	if len(got) != 1 || got[0].Alias != "explicit" {
		t.Fatalf("explicit registry must replace fallback: %+v", got)
	}
}

func TestWorkspacesForRuntimeDetectsSuperpowersDirFallback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "superpowers-project")
	writeSuperpowersFile(t, root, "docs/superpowers/plans/2026-07-26-idea.md", "# Plan\n")
	got := workspacesForRuntime(nil, root)
	if len(got) != 1 || got[0].Path != root || got[0].Type != source.KindSuperpowers {
		t.Fatalf("unexpected Superpowers --dir fallback: %+v", got)
	}
}

func TestCoalescedRebuilderSerializesAndRunsLatestRequest(t *testing.T) {
	var mu sync.Mutex
	generation := 1
	runCount := 0
	active := 0
	maxActive := 0
	lastBuilt := 0
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	completed := make(chan error, 1)

	rebuilder := newCoalescedRebuilder(func() error {
		mu.Lock()
		runCount++
		pass := runCount
		active++
		if active > maxActive {
			maxActive = active
		}
		snapshot := generation
		mu.Unlock()

		if pass == 1 {
			close(firstStarted)
			<-releaseFirst
		}

		mu.Lock()
		lastBuilt = snapshot
		active--
		mu.Unlock()
		return nil
	}, func(err error, _ bool) {
		completed <- err
	})

	rebuilder.Request(nil)
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first rebuild did not start")
	}

	mu.Lock()
	generation = 2
	mu.Unlock()
	requestReturned := make(chan struct{})
	go func() {
		rebuilder.Request(nil)
		rebuilder.Request(nil)
		close(requestReturned)
	}()
	select {
	case <-requestReturned:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("coalesced Request blocked on the active rebuild")
	}
	close(releaseFirst)

	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("coalesced rebuild did not complete")
	}

	mu.Lock()
	defer mu.Unlock()
	if runCount != 2 {
		t.Fatalf("run count = %d, want one active pass plus one coalesced pass", runCount)
	}
	if maxActive != 1 {
		t.Fatalf("rebuilds overlapped: maximum active = %d", maxActive)
	}
	if lastBuilt != 2 {
		t.Fatalf("last rebuilt generation = %d, want latest generation 2", lastBuilt)
	}
}
