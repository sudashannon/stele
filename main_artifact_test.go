package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"stele/internal/source"
)

func newArtifactTestWorkspace(t *testing.T, alias string) (*WorkspaceRegistry, string, string) {
	t.Helper()
	repo := t.TempDir()
	openspec := filepath.Join(repo, "openspec")
	if err := os.MkdirAll(filepath.Join(openspec, "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: alias, Path: openspec, Color: "#123456"}); err != nil {
		t.Fatal(err)
	}
	return reg, repo, openspec
}

func artifactTestRequest(method, alias, path string, body []byte) *http.Request {
	query := url.Values{"workspace": {alias}, "path": {path}}
	return httptest.NewRequest(method, "/api/artifact?"+query.Encode(), bytes.NewReader(body))
}

type artifactTestLocker struct {
	onLock func()
}

func (l artifactTestLocker) Lock() {
	if l.onLock != nil {
		l.onLock()
	}
}

func (artifactTestLocker) Unlock() {}

func artifactTestETag(t *testing.T, reg *WorkspaceRegistry, alias, path string) string {
	t.Helper()
	req := artifactTestRequest(http.MethodGet, alias, path, nil)
	w := httptest.NewRecorder()
	handleGetArtifact(w, req, "unused-default", reg)
	if w.Code != http.StatusOK {
		t.Fatalf("GET artifact returned %d: %s", w.Code, w.Body.String())
	}
	return w.Header().Get("ETag")
}

func TestHandleArtifact_ReadAndSaveContract(t *testing.T) {
	reg, repo, _ := newArtifactTestWorkspace(t, "docs")
	path := filepath.Join(repo, "docs", "note.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	lock := &sync.Mutex{}

	get := artifactTestRequest(http.MethodGet, "docs", path, nil)
	getW := httptest.NewRecorder()
	handleArtifact(getW, get, "unused-default", reg, lock)
	if getW.Code != http.StatusOK || getW.Body.String() != "# Before\n" {
		t.Fatalf("GET = %d %q", getW.Code, getW.Body.String())
	}
	etag := getW.Header().Get("ETag")
	if etag == "" || etag != artifactETag([]byte("# Before\n")) {
		t.Fatalf("GET ETag = %q", etag)
	}

	head := artifactTestRequest(http.MethodHead, "docs", path, nil)
	headW := httptest.NewRecorder()
	handleArtifact(headW, head, "unused-default", reg, lock)
	if headW.Code != http.StatusOK || headW.Body.Len() != 0 || headW.Header().Get("ETag") != etag {
		t.Fatalf("HEAD = %d body=%q ETag=%q", headW.Code, headW.Body.String(), headW.Header().Get("ETag"))
	}

	put := artifactTestRequest(http.MethodPut, "docs", path, []byte("# After\n"))
	put.Header.Set("If-Match", etag)
	putW := httptest.NewRecorder()
	handleArtifact(putW, put, "unused-default", reg, lock)
	if putW.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", putW.Code, putW.Body.String())
	}
	var response struct {
		ETag  string `json:"etag"`
		Path  string `json:"path"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal(putW.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Path != path || response.Bytes != len("# After\n") || response.ETag != artifactETag([]byte("# After\n")) || putW.Header().Get("ETag") != response.ETag {
		t.Fatalf("PUT response = %+v, header ETag=%q", response, putW.Header().Get("ETag"))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# After\n" {
		t.Fatalf("saved content = %q", content)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("saved mode = %v, error = %v", info.Mode(), err)
	}
}

func TestHandleArtifact_PutAllowsRelativeDefaultWorkspace(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)

	defaultDir := "workspace"
	if err := os.MkdirAll(filepath.Join(defaultDir, "openspec", "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(defaultDir, "note.md")
	if err := os.WriteFile(path, []byte("# Before\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/artifact?"+url.Values{"path": {path}}.Encode(), nil)
	getW := httptest.NewRecorder()
	handleArtifact(getW, get, defaultDir, nil, &sync.Mutex{})
	if getW.Code != http.StatusOK {
		t.Fatalf("GET relative default workspace artifact = %d: %s", getW.Code, getW.Body.String())
	}

	put := httptest.NewRequest(http.MethodPut, "/api/artifact?"+url.Values{"path": {path}}.Encode(), bytes.NewReader([]byte("# After\n")))
	put.Header.Set("If-Match", getW.Header().Get("ETag"))
	putW := httptest.NewRecorder()
	handleArtifact(putW, put, defaultDir, nil, &sync.Mutex{})
	if putW.Code != http.StatusOK {
		t.Fatalf("PUT relative default workspace artifact = %d: %s", putW.Code, putW.Body.String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# After\n" {
		t.Fatalf("saved relative default workspace content = %q", content)
	}
}

func TestHandleArtifact_MethodAndPreconditionStatuses(t *testing.T) {
	reg, repo, _ := newArtifactTestWorkspace(t, "docs")
	path := filepath.Join(repo, "note.md")
	if err := os.WriteFile(path, []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := &sync.Mutex{}

	postW := httptest.NewRecorder()
	handleArtifact(postW, artifactTestRequest(http.MethodPost, "docs", path, nil), "unused-default", reg, lock)
	if postW.Code != http.StatusMethodNotAllowed || postW.Header().Get("Allow") != "GET, HEAD, PUT" {
		t.Fatalf("POST = %d Allow=%q", postW.Code, postW.Header().Get("Allow"))
	}

	for _, ifMatch := range []string{"", "*"} {
		w := httptest.NewRecorder()
		req := artifactTestRequest(http.MethodPut, "docs", path, []byte("next"))
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		handleArtifact(w, req, "unused-default", reg, lock)
		if w.Code != http.StatusPreconditionRequired {
			t.Fatalf("If-Match %q = %d, want 428: %s", ifMatch, w.Code, w.Body.String())
		}
	}

	for _, ifMatch := range []string{"W/\"weak\"", "not-an-etag"} {
		w := httptest.NewRecorder()
		req := artifactTestRequest(http.MethodPut, "docs", path, []byte("next"))
		req.Header.Set("If-Match", ifMatch)
		handleArtifact(w, req, "unused-default", reg, lock)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("If-Match %q = %d, want 400: %s", ifMatch, w.Code, w.Body.String())
		}
	}

	staleW := httptest.NewRecorder()
	stale := artifactTestRequest(http.MethodPut, "docs", path, []byte("next"))
	stale.Header.Set("If-Match", artifactETag([]byte("old")))
	handleArtifact(staleW, stale, "unused-default", reg, lock)
	if staleW.Code != http.StatusPreconditionFailed || staleW.Header().Get("ETag") != artifactETag([]byte("# Note\n")) {
		t.Fatalf("stale PUT = %d ETag=%q: %s", staleW.Code, staleW.Header().Get("ETag"), staleW.Body.String())
	}

	tooLargeW := httptest.NewRecorder()
	tooLarge := artifactTestRequest(http.MethodPut, "docs", path, make([]byte, maxArtifactBodyBytes+1))
	tooLarge.Header.Set("If-Match", artifactETag([]byte("# Note\n")))
	handleArtifact(tooLargeW, tooLarge, "unused-default", reg, lock)
	if tooLargeW.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized PUT = %d: %s", tooLargeW.Code, tooLargeW.Body.String())
	}
}

func TestHandleArtifact_RejectsMissingBinaryAndEscapedPaths(t *testing.T) {
	reg, repo, _ := newArtifactTestWorkspace(t, "one")
	otherReg, otherRepo, _ := newArtifactTestWorkspace(t, "two")
	for _, workspace := range otherReg.List() {
		if err := reg.Add(workspace); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(repo, "note.md")
	if err := os.WriteFile(path, []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(otherRepo, "note.md")
	if err := os.WriteFile(otherPath, []byte("# Other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := &sync.Mutex{}

	missing := filepath.Join(repo, "missing.md")
	missingW := httptest.NewRecorder()
	missingReq := artifactTestRequest(http.MethodPut, "one", missing, []byte("new"))
	missingReq.Header.Set("If-Match", artifactETag([]byte("missing")))
	handleArtifact(missingW, missingReq, "unused-default", reg, lock)
	if missingW.Code != http.StatusNotFound {
		t.Fatalf("missing PUT = %d: %s", missingW.Code, missingW.Body.String())
	}

	crossW := httptest.NewRecorder()
	crossReq := artifactTestRequest(http.MethodPut, "one", otherPath, []byte("new"))
	crossReq.Header.Set("If-Match", artifactETag([]byte("# Other\n")))
	handleArtifact(crossW, crossReq, "unused-default", reg, lock)
	if crossW.Code != http.StatusForbidden {
		t.Fatalf("cross-workspace PUT = %d: %s", crossW.Code, crossW.Body.String())
	}

	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("# Outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(repo, "escape.md")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	escapeW := httptest.NewRecorder()
	escapeReq := artifactTestRequest(http.MethodPut, "one", escape, []byte("new"))
	escapeReq.Header.Set("If-Match", artifactETag([]byte("# Outside\n")))
	handleArtifact(escapeW, escapeReq, "unused-default", reg, lock)
	if escapeW.Code != http.StatusForbidden {
		t.Fatalf("non-Superpowers symlink escape PUT = %d: %s", escapeW.Code, escapeW.Body.String())
	}
	outsideContent, err := os.ReadFile(outside)
	if err != nil || string(outsideContent) != "# Outside\n" {
		t.Fatalf("outside content = %q, error = %v", outsideContent, err)
	}

	insideTarget := filepath.Join(repo, "target.md")
	if err := os.WriteFile(insideTarget, []byte("# Target\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	insideLink := filepath.Join(repo, "linked.md")
	if err := os.Symlink(insideTarget, insideLink); err != nil {
		t.Fatal(err)
	}
	insideW := httptest.NewRecorder()
	insideReq := artifactTestRequest(http.MethodPut, "one", insideLink, []byte("# Updated\n"))
	insideReq.Header.Set("If-Match", artifactETag([]byte("# Target\n")))
	handleArtifact(insideW, insideReq, "unused-default", reg, lock)
	if insideW.Code != http.StatusOK {
		t.Fatalf("in-workspace symlink PUT = %d: %s", insideW.Code, insideW.Body.String())
	}
	info, err := os.Lstat(insideLink)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("artifact symlink was replaced: mode=%v", info.Mode())
	}
	insideContent, err := os.ReadFile(insideTarget)
	if err != nil || string(insideContent) != "# Updated\n" {
		t.Fatalf("resolved target content = %q, error = %v", insideContent, err)
	}

	binary := filepath.Join(repo, "binary.md")
	if err := os.WriteFile(binary, []byte{0xff, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	binaryW := httptest.NewRecorder()
	binaryReq := artifactTestRequest(http.MethodPut, "one", binary, []byte("new"))
	binaryReq.Header.Set("If-Match", artifactETag([]byte{0xff, 0x00}))
	handleArtifact(binaryW, binaryReq, "unused-default", reg, lock)
	if binaryW.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("binary PUT = %d: %s", binaryW.Code, binaryW.Body.String())
	}

	invalidW := httptest.NewRecorder()
	invalidReq := artifactTestRequest(http.MethodPut, "one", path, []byte{0xff})
	invalidReq.Header.Set("If-Match", artifactTestETag(t, reg, "one", path))
	handleArtifact(invalidW, invalidReq, "unused-default", reg, lock)
	if invalidW.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-UTF8 PUT = %d: %s", invalidW.Code, invalidW.Body.String())
	}
}

func TestHandleArtifact_SuperpowersRejectsResolvedProjectFileOutsideDurableRoots(t *testing.T) {
	project := t.TempDir()
	durablePath := filepath.Join(project, "docs", "superpowers", "specs", "design.md")
	if err := os.MkdirAll(filepath.Dir(durablePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(durablePath, []byte("# Durable\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(project, "README.md")
	if err := os.WriteFile(outsidePath, []byte("# Project root\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	reg, err := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "ideas", Path: project, Type: source.KindSuperpowers}); err != nil {
		t.Fatal(err)
	}

	req := artifactTestRequest(http.MethodPut, "ideas", durablePath, []byte("# Replacement\n"))
	req.Header.Set("If-Match", artifactETag([]byte("# Durable\n")))
	w := httptest.NewRecorder()
	lock := artifactTestLocker{onLock: func() {
		if err := os.Remove(durablePath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsidePath, durablePath); err != nil {
			t.Fatal(err)
		}
	}}
	handleArtifact(w, req, "unused-default", reg, lock)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Superpowers durable-root escape PUT = %d: %s", w.Code, w.Body.String())
	}
	content, err := os.ReadFile(outsidePath)
	if err != nil || string(content) != "# Project root\n" {
		t.Fatalf("project-root content after denied PUT = %q, error = %v", content, err)
	}
	if info, err := os.Stat(outsidePath); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("project-root mode after denied PUT = %v, error = %v", info.Mode(), err)
	}
}

func TestHandleArtifact_ConcurrentStaleETagAllowsOnlyOneSave(t *testing.T) {
	reg, repo, _ := newArtifactTestWorkspace(t, "docs")
	path := filepath.Join(repo, "note.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	etag := artifactTestETag(t, reg, "docs", path)
	lock := &sync.Mutex{}
	start := make(chan struct{})
	statuses := make(chan int, 2)
	for _, content := range [][]byte{[]byte("first"), []byte("second")} {
		go func(content []byte) {
			<-start
			req := artifactTestRequest(http.MethodPut, "docs", path, content)
			req.Header.Set("If-Match", etag)
			w := httptest.NewRecorder()
			handleArtifact(w, req, "unused-default", reg, lock)
			statuses <- w.Code
		}(content)
	}
	close(start)
	first, second := <-statuses, <-statuses
	if !((first == http.StatusOK && second == http.StatusPreconditionFailed) || (second == http.StatusOK && first == http.StatusPreconditionFailed)) {
		t.Fatalf("concurrent statuses = %d, %d; want one 200 and one 412", first, second)
	}
}

func TestWriteArtifactAtomicallyWithRename_PreservesOriginalOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("rename failed")
	err := writeArtifactAtomicallyWithRename(path, []byte("replacement"), 0o640, func(string, string) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("write error = %v, want %v", err, wantErr)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "original" {
		t.Fatalf("content after failed write = %q, error = %v", content, readErr)
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode after failed write = %v, error = %v", info.Mode(), statErr)
	}
}
