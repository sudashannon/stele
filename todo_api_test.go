package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"comet-ui/internal/todo"
	"comet-ui/wiki"
)

func helperTodoAPI(t *testing.T) (*todoAPI, *todo.Store, *httptest.Server) {
	t.Helper()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "todos.json")
	store, err := todo.NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	wikiAPI := wiki.NewAPI(wiki.BuildGraph(nil, nil))
	api := newTodoAPI(store, wikiAPI)
	srv := httptest.NewServer(http.HandlerFunc(api.ServeHTTP))
	t.Cleanup(srv.Close)
	return api, store, srv
}

func doReq(t *testing.T, method, url string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func validCreateBody() []byte {
	return []byte(`{"workspace":"test-ws","title":"test todo"}`)
}

func TestTodoAPI_ListEmpty(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	items, ok := body["items"].([]any)
	if !ok {
		t.Fatal("expected items array")
	}
	if len(items) != 0 {
		t.Fatalf("expected empty, got %d", len(items))
	}
	revision := body["revision"].(float64)
	if revision != 0 {
		t.Fatalf("expected revision 0, got %v", revision)
	}
}

func TestTodoAPI_ListWithFilterByStatus(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	store.Create(todo.CreateInput{Workspace: "ws", Title: "open1"})
	store.Create(todo.CreateInput{Workspace: "ws", Title: "done1", Status: todo.StatusDone})

	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos?status=done", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestTodoAPI_ListFilterByWorkspace(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	store.Create(todo.CreateInput{Workspace: "ws-a", Title: "a"})
	store.Create(todo.CreateInput{Workspace: "ws-b", Title: "b"})

	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos?workspace=ws-a", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestTodoAPI_ListQ_SearchesTitleAndNotes(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	store.Create(todo.CreateInput{Workspace: "ws", Title: "Hello World"})
	store.Create(todo.CreateInput{Workspace: "ws", Title: "Task B", Notes: "hello notes"})

	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos?q=hello", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items matching q=hello, got %d", len(items))
	}
}

func TestTodoAPI_ListWritable(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	writable, ok := body["writable"].(bool)
	if !ok || !writable {
		t.Fatalf("expected writable=true on loopback, got %v", writable)
	}
}

func TestTodoAPI_CreateSuccess(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodPost, srv.URL+"/api/todos", validCreateBody(), nil)
	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
	var item todo.Todo
	json.NewDecoder(resp.Body).Decode(&item)
	resp.Body.Close()

	if item.ID == "" || item.Title != "test todo" || item.Status != todo.StatusOpen {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.Metadata.Source != todo.SourceUI {
		t.Fatalf("expected metadata.source=ui, got %s", item.Metadata.Source)
	}
}

func TestTodoAPI_CreateRequiresWorkspace(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodPost, srv.URL+"/api/todos", []byte(`{"title":"no ws"}`), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_CreateBadBody(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodPost, srv.URL+"/api/todos", []byte(`not json`), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_CreateTrailingData(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodPost, srv.URL+"/api/todos", []byte(`{"workspace":"ws","title":"x"}trailing`), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing data, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_UpdateSuccess(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	item, _ := store.Create(todo.CreateInput{Workspace: "ws", Title: "original"})

	newTitle := "updated"
	body, _ := json.Marshal(todo.UpdateInput{Title: &newTitle})
	resp := doReq(t, http.MethodPatch, srv.URL+"/api/todos/"+item.ID, body, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var updated todo.Todo
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()

	if updated.Title != "updated" {
		t.Fatalf("expected updated title, got %+v", updated)
	}
}

func TestTodoAPI_UpdateNotFound(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	newTitle := "x"
	body, _ := json.Marshal(todo.UpdateInput{Title: &newTitle})
	resp := doReq(t, http.MethodPatch, srv.URL+"/api/todos/nonexistent", body, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_DeleteSuccess(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	item, _ := store.Create(todo.CreateInput{Workspace: "ws", Title: "to delete"})

	resp := doReq(t, http.MethodDelete, srv.URL+"/api/todos/"+item.ID, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	_, err := store.Update(item.ID, todo.UpdateInput{})
	if err != todo.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTodoAPI_DeleteNotFound(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodDelete, srv.URL+"/api/todos/nonexistent", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_MethodNotAllowed(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodPut, srv.URL+"/api/todos/xxx", nil, nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_WriteGuardNonLoopback(t *testing.T) {
	if writeGuard(&http.Request{
		RemoteAddr: "192.168.1.1:12345",
		Host:       "localhost:8989",
	}) {
		t.Fatal("expected writeGuard to deny non-loopback")
	}
}

func TestTodoAPI_WriteGuardOriginMismatch(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	body := validCreateBody()
	resp := doReq(t, http.MethodPost, srv.URL+"/api/todos", body, map[string]string{
		"Origin": "http://evil.com:8989",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for origin mismatch, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_WriteGuardLoopbackOk(t *testing.T) {
	if !writeGuard(&http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Host:       "localhost:8989",
	}) {
		t.Fatal("expected writeGuard to allow loopback without Origin")
	}
}

func TestTodoAPI_WriteGuardLoopbackSameOrigin(t *testing.T) {
	if !writeGuard(&http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Host:       "localhost:8989",
		Header:     http.Header{"Origin": []string{"http://localhost:8989"}},
	}) {
		t.Fatal("expected writeGuard to allow loopback with same origin")
	}
}

func TestTodoAPI_WriteGuardLoopbackOriginMismatch(t *testing.T) {
	if writeGuard(&http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Host:       "localhost:8989",
		Header:     http.Header{"Origin": []string{"http://evil.com:8989"}},
	}) {
		t.Fatal("expected writeGuard to deny mismatched origin")
	}
}

func TestTodoAPI_CurrentTitleFallback(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "wiki-todo",
		WikiRefs: []todo.WikiRef{
			{ComponentID: "/some/component.md", Workspace: "ws", TitleSnapshot: "Old Snapshot"},
		},
	})

	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos?q=wiki-todo", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	itemMap := items[0].(map[string]any)
	refs := itemMap["wikiRefs"].([]any)
	if len(refs) != 1 {
		t.Fatalf("expected 1 wikiref, got %d", len(refs))
	}
	ref := refs[0].(map[string]any)
	if ref["titleSnapshot"] != "Old Snapshot" {
		t.Fatalf("expected fallback snapshot title, got %v", ref["titleSnapshot"])
	}
}

func TestTodoAPI_LANReadable(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for LAN GET, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTodoAPI_MetadataDefaultsUI(t *testing.T) {
	_, _, srv := helperTodoAPI(t)
	resp := doReq(t, http.MethodPost, srv.URL+"/api/todos", validCreateBody(), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var item todo.Todo
	json.NewDecoder(resp.Body).Decode(&item)
	resp.Body.Close()

	if item.Metadata.Source != todo.SourceUI {
		t.Fatalf("expected metadata.source=ui from REST, got %s", item.Metadata.Source)
	}
}

func TestTodoAPI_PatchClearChange(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	item, err := store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "with change",
		Change:    &todo.ChangeRef{Workspace: "ws", Name: "ch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Change == nil {
		t.Fatal("expected change to be set")
	}

	// PATCH raw JSON with "change":null.
	resp := doReq(t, http.MethodPatch, srv.URL+"/api/todos/"+item.ID,
		[]byte(`{"change":null}`), nil)
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
	var updated todo.Todo
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()

	if updated.Change != nil {
		t.Fatalf("expected change to be cleared, got %+v", updated.Change)
	}

	// Verify persisted state.
	items, _, _ := store.List(todo.Filter{})
	if len(items) != 1 || items[0].Change != nil {
		t.Fatalf("expected change cleared in store, got %+v", items[0].Change)
	}
}

func TestTodoAPI_PatchClearWikiRefs(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	item, err := store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "with refs",
		WikiRefs:  []todo.WikiRef{{ComponentID: "c", Workspace: "ws", TitleSnapshot: "t"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(item.WikiRefs) != 1 {
		t.Fatal("expected 1 wikiref")
	}

	// PATCH raw JSON with "wikiRefs":[].
	resp := doReq(t, http.MethodPatch, srv.URL+"/api/todos/"+item.ID,
		[]byte(`{"wikiRefs":[]}`), nil)
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
	var updated todo.Todo
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()

	if len(updated.WikiRefs) != 0 {
		t.Fatalf("expected empty wikirefs, got %d", len(updated.WikiRefs))
	}

	items, _, _ := store.List(todo.Filter{})
	if len(items) != 1 || len(items[0].WikiRefs) != 0 {
		t.Fatalf("expected wikiRefs cleared in store, got %d", len(items[0].WikiRefs))
	}
}

func TestTodoAPI_PatchClearDueAt(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	item, err := store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "with due",
		DueAt:     "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.DueAt == "" {
		t.Fatal("expected dueAt to be set")
	}

	resp := doReq(t, http.MethodPatch, srv.URL+"/api/todos/"+item.ID,
		[]byte(`{"dueAt":null}`), nil)
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
	var updated todo.Todo
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()

	if updated.DueAt != "" {
		t.Fatalf("expected dueAt cleared, got %s", updated.DueAt)
	}

	items, _, _ := store.List(todo.Filter{})
	if len(items) != 1 || items[0].DueAt != "" {
		t.Fatalf("expected dueAt cleared in store, got %s", items[0].DueAt)
	}
}

func TestTodoAPI_ListRendersExtendedStatusesAndOMPIdentity(t *testing.T) {
	_, store, srv := helperTodoAPI(t)
	if _, err := store.Create(todo.CreateInput{Workspace: "ws", Title: "blocked", Status: todo.StatusBlocked}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(todo.CreateInput{Workspace: "ws", Title: "dropped", Status: todo.StatusDropped}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SyncOMP(todo.OMPSyncInput{
		Workspace: "ws", SessionID: "session", SnapshotSeq: 1, Mode: todo.OMPSyncReconcile,
		Todos: []todo.OMPSyncTodo{{TaskKey: "0:0", Phase: "build", Title: "omp blocked", Status: todo.StatusBlocked, Blocker: "waiting"}},
	}); err != nil {
		t.Fatal(err)
	}

	resp := doReq(t, http.MethodGet, srv.URL+"/api/todos", nil, nil)
	defer resp.Body.Close()
	var body struct {
		Items  []todo.Todo `json:"items"`
		Counts todo.Counts `json:"counts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Counts.Blocked != 2 || body.Counts.Dropped != 1 || body.Counts.Total != 3 {
		t.Fatalf("unexpected extended counts: %+v", body.Counts)
	}
	foundOMP := false
	for _, item := range body.Items {
		if item.Metadata.Source == todo.SourceOMP {
			foundOMP = item.ExternalRef != nil && item.ExternalRef.System == "omp" &&
				item.ExternalRef.SessionID == "session" && item.ExternalRef.Blocker == "waiting"
		}
	}
	if !foundOMP {
		t.Fatalf("OMP external identity missing from REST response: %+v", body.Items)
	}
}
