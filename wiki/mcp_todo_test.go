package wiki

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"stele/internal/todo"
)

func helperMCPTodoAPI(t *testing.T) (*API, *todo.Store, func()) {
	t.Helper()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "todos.json")
	store, err := todo.NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	api := NewAPI(BuildGraph(nil, nil))
	api.SetTodoStore(store, []byte("test-token-secret"))

	return api, store, func() {}
}
func mcpTodoRPC(t *testing.T, id any, toolName string, args map[string]any, token string) *http.Request {
	t.Helper()
	params := map[string]any{
		"name":      toolName,
		"arguments": args,
	}
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params":  params,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func parseMCPResult(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, string(body))
	}
	result, ok := resp["result"]
	if !ok {
		errInfo := resp["error"]
		t.Fatalf("no result in response, error: %v\nbody: %s", errInfo, string(body))
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T\nbody: %s", result, string(body))
	}
	return resultMap
}

func mcpContentText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in result: %+v", result)
	}
	item := content[0].(map[string]any)
	return item["text"].(string)
}

func TestMCP_ToolsList_AllTools(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)

	if len(tools) != 17 {
		t.Fatalf("expected 17 tools (8 wiki + 5 todo + 4 claim), got %d", len(tools))
	}

	toolNames := map[string]bool{}
	for _, t := range tools {
		tm := t.(map[string]any)
		toolNames[tm["name"].(string)] = true
	}
	for _, name := range []string{
		"todo_list", "todo_create", "todo_update", "todo_delete", "todo_sync_omp",
		"wiki_context", "wiki_sessions",
	} {
		if !toolNames[name] {
			t.Fatalf("expected tool %s in list", name)
		}
	}
}

func TestMCP_TodoList_Empty(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_list", map[string]any{}, "")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	if !strings.Contains(text, "(no items)") {
		t.Fatalf("expected empty list, got: %s", text)
	}
}

func TestMCP_TodoCreate_Success(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_create", map[string]any{
		"workspace": "test-ws",
		"title":     "MCP todo",
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)

	// Decode the content text as a Todo and assert fields semantically.
	var item todo.Todo
	if err := json.Unmarshal([]byte(text), &item); err != nil {
		t.Fatalf("decode todo from MCP response: %v\ntext: %s", err, text)
	}
	if item.Title != "MCP todo" {
		t.Fatalf("expected title 'MCP todo', got %q", item.Title)
	}
	if item.Metadata.Source != todo.SourceMCP {
		t.Fatalf("expected metadata.source=mcp, got %s", item.Metadata.Source)
	}
}

func TestMCP_TodoCreate_DeniedNoToken(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_create", map[string]any{
		"workspace": "ws",
		"title":     "should fail",
	}, "")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, ok := result["isError"].(bool)
	if !ok || !isError {
		t.Fatalf("expected isError=true, got result: %+v", result)
	}
	text := mcpContentText(t, result)
	if !strings.Contains(text, "denied") {
		t.Fatalf("expected denied message, got: %s", text)
	}
}

func TestMCP_TodoCreate_DeniedWrongToken(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_create", map[string]any{
		"workspace": "ws",
		"title":     "should fail",
	}, "wrong-token")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, ok := result["isError"].(bool)
	if !ok || !isError {
		t.Fatalf("expected isError=true, got result: %+v", result)
	}
}

func TestMCP_TodoUpdate_Success(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, err := store.Create(todo.CreateInput{Workspace: "ws", Title: "original"})
	if err != nil {
		t.Fatal(err)
	}

	req := mcpTodoRPC(t, 1, "todo_update", map[string]any{
		"id":    item.ID,
		"title": "updated via MCP",
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	if !strings.Contains(text, "updated via MCP") {
		t.Fatalf("expected updated title, got: %s", text)
	}
}

func TestMCP_TodoDelete_Success(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, err := store.Create(todo.CreateInput{Workspace: "ws", Title: "to delete"})
	if err != nil {
		t.Fatal(err)
	}

	req := mcpTodoRPC(t, 1, "todo_delete", map[string]any{
		"id": item.ID,
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	_, err = store.Update(item.ID, todo.UpdateInput{})
	if err != todo.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMCP_TodoDelete_Denied(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_delete", map[string]any{
		"id": "some-id",
	}, "")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, ok := result["isError"].(bool)
	if !ok || !isError {
		t.Fatalf("expected isError=true, got result: %+v", result)
	}
}

func TestMCP_TodoList_NoStore(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	req := mcpTodoRPC(t, 1, "todo_list", map[string]any{}, "")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, ok := result["isError"].(bool)
	if !ok || !isError {
		t.Fatalf("expected isError when store is nil, got: %+v", result)
	}
}

func TestMCP_ExistingWikiToolsStillWork(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "wiki_lint", map[string]any{}, "")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for wiki_lint, got %d: %s", rec.Code, rec.Body.String())
	}
	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	if !strings.Contains(text, "lint issues") {
		t.Fatalf("expected lint output, got: %s", text)
	}
}

func TestMCP_TodoList_SharedStateWithREST(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, err := store.Create(todo.CreateInput{Workspace: "ws", Title: "shared item"})
	if err != nil {
		t.Fatal(err)
	}

	req := mcpTodoRPC(t, 1, "todo_list", map[string]any{}, "")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	if !strings.Contains(text, "shared item") {
		t.Fatalf("expected shared item in MCP list, got: %s", text)
	}
	_ = item
}

func TestMCP_TodoList_NonLoopbackStillWorks(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_list", map[string]any{}, "")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-loopback read, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMCP_TodoCreate_RequiresWorkspace(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_create", map[string]any{
		"title": "no workspace",
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, ok := result["isError"].(bool)
	if !ok || !isError {
		t.Fatalf("expected isError=true, got result: %+v", result)
	}
}

func TestMCP_TodoUpdate_ClearChange(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, err := store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "with change",
		Change:    &todo.ChangeRef{Workspace: "ws", Name: "ch"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := mcpTodoRPC(t, 1, "todo_update", map[string]any{
		"id":     item.ID,
		"change": nil,
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	var updated todo.Todo
	if err := json.Unmarshal([]byte(text), &updated); err != nil {
		t.Fatalf("decode response: %v\ntext: %s", err, text)
	}
	if updated.Change != nil {
		t.Fatalf("expected change to be cleared, got %+v", updated.Change)
	}

	// Verify persisted state.
	items, _, _ := store.List(todo.Filter{})
	if len(items) != 1 || items[0].Change != nil {
		t.Fatalf("expected change cleared in store, got %+v", items[0].Change)
	}
}

func TestMCP_TodoUpdate_ClearWikiRefs(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, err := store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "with refs",
		WikiRefs:  []todo.WikiRef{{ComponentID: "c", Workspace: "ws", TitleSnapshot: "t"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := mcpTodoRPC(t, 1, "todo_update", map[string]any{
		"id":       item.ID,
		"wikiRefs": []any{},
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	var updated todo.Todo
	if err := json.Unmarshal([]byte(text), &updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(updated.WikiRefs) != 0 {
		t.Fatalf("expected empty wikirefs, got %d", len(updated.WikiRefs))
	}

	items, _, _ := store.List(todo.Filter{})
	if len(items) != 1 || len(items[0].WikiRefs) != 0 {
		t.Fatalf("expected wikiRefs cleared in store, got %d", len(items[0].WikiRefs))
	}
}

func TestMCP_TodoCreate_WrongTypeChange(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_create", map[string]any{
		"workspace": "ws",
		"title":     "x",
		"change":    "not-an-object",
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, _ := result["isError"].(bool)
	text := mcpContentText(t, result)
	if !isError || !strings.Contains(text, "must be an object") {
		t.Fatalf("expected wrong-type rejection, got isError=%v text=%s", isError, text)
	}
}

func TestMCP_TodoCreate_WrongTypeWikiRefs(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	req := mcpTodoRPC(t, 1, "todo_create", map[string]any{
		"workspace": "ws",
		"title":     "x",
		"wikiRefs":  "not-an-array",
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, _ := result["isError"].(bool)
	text := mcpContentText(t, result)
	if !isError || !strings.Contains(text, "must be an array") {
		t.Fatalf("expected wrong-type rejection, got isError=%v text=%s", isError, text)
	}
}

func TestMCP_TodoUpdate_WrongTypeChange(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, _ := store.Create(todo.CreateInput{Workspace: "ws", Title: "x"})

	req := mcpTodoRPC(t, 1, "todo_update", map[string]any{
		"id":     item.ID,
		"change": "not-an-object",
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, _ := result["isError"].(bool)
	text := mcpContentText(t, result)
	if !isError || !strings.Contains(text, "must be an object") {
		t.Fatalf("expected wrong-type rejection, got isError=%v text=%s", isError, text)
	}
}

func TestMCP_TodoUpdate_WrongTypeWikiRefs(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, _ := store.Create(todo.CreateInput{Workspace: "ws", Title: "x"})

	req := mcpTodoRPC(t, 1, "todo_update", map[string]any{
		"id":       item.ID,
		"wikiRefs": "not-an-array",
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	result := parseMCPResult(t, rec.Body.Bytes())
	isError, _ := result["isError"].(bool)
	text := mcpContentText(t, result)
	if !isError || !strings.Contains(text, "must be an array") {
		t.Fatalf("expected wrong-type rejection, got isError=%v text=%s", isError, text)
	}
}

func TestMCP_TodoCreate_ResolvesWikiTitles(t *testing.T) {
	store, err := todo.NewStore(filepath.Join(t.TempDir(), "todos.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := Component{ID: "/docs/readme.md", Title: "Live Title", Workspace: "ws"}
	g := BuildGraph([]Component{c}, nil)
	api := NewAPI(g)
	api.SetTodoStore(store, []byte("test-token-secret"))

	req := mcpTodoRPC(t, 1, "todo_create", map[string]any{
		"workspace": "ws",
		"title":     "wiki-todo",
		"wikiRefs":  []any{map[string]any{"componentId": "/docs/readme.md", "workspace": "ws"}},
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	var item todo.Todo
	if err := json.Unmarshal([]byte(text), &item); err != nil {
		t.Fatalf("decode: %v\ntext: %s", err, text)
	}
	if len(item.WikiRefs) != 1 {
		t.Fatalf("expected 1 wikiref, got %d", len(item.WikiRefs))
	}
	if item.WikiRefs[0].TitleSnapshot != "Live Title" {
		t.Fatalf("expected resolved title 'Live Title', got %q", item.WikiRefs[0].TitleSnapshot)
	}
}

func TestMCP_TodoUpdate_ClearDueAt(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	item, err := store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "with dueAt",
		DueAt:     "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := mcpTodoRPC(t, 1, "todo_update", map[string]any{
		"id":    item.ID,
		"dueAt": nil,
	}, "test-token-secret")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseMCPResult(t, rec.Body.Bytes())
	text := mcpContentText(t, result)
	var updated todo.Todo
	if err := json.Unmarshal([]byte(text), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.DueAt != "" {
		t.Fatalf("expected dueAt cleared, got %s", updated.DueAt)
	}
}

func TestMCP_TodoSyncOMP_AuthAndOutput(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()
	args := map[string]any{
		"workspace": "ws", "sessionId": "session", "snapshotSeq": 1, "mode": "reconcile",
		"todos": []any{
			map[string]any{"taskKey": "0:0", "phase": "build", "title": "blocked", "status": "blocked", "blocker": "dependency"},
			map[string]any{"taskKey": "0:1", "phase": "build", "title": "dropped", "status": "dropped"},
		},
	}

	denied := httptest.NewRecorder()
	api.HandleMCP(denied, mcpTodoRPC(t, 1, "todo_sync_omp", args, ""))
	deniedResult := parseMCPResult(t, denied.Body.Bytes())
	if deniedResult["isError"] != true {
		t.Fatalf("expected unauthenticated sync rejection: %+v", deniedResult)
	}

	rec := httptest.NewRecorder()
	api.HandleMCP(rec, mcpTodoRPC(t, 2, "todo_sync_omp", args, "test-token-secret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := parseMCPResult(t, rec.Body.Bytes())
	if result["isError"] == true {
		t.Fatalf("unexpected MCP sync error: %+v", result)
	}
	var syncResult todo.OMPSyncResult
	if err := json.Unmarshal([]byte(mcpContentText(t, result)), &syncResult); err != nil {
		t.Fatalf("decode sync output: %v", err)
	}
	if !syncResult.Applied || syncResult.Stale || syncResult.Mode != todo.OMPSyncReconcile || syncResult.Created != 2 || len(syncResult.Items) != 2 {
		t.Fatalf("unexpected sync result: %+v", syncResult)
	}
	for _, item := range syncResult.Items {
		if item.Metadata.Source != todo.SourceOMP || item.ExternalRef == nil ||
			item.ExternalRef.System != "omp" || item.ExternalRef.SessionID != "session" {
			t.Fatalf("missing external identity echo: %+v", item)
		}
	}

	staleRec := httptest.NewRecorder()
	api.HandleMCP(staleRec, mcpTodoRPC(t, 3, "todo_sync_omp", args, "test-token-secret"))
	staleResultMap := parseMCPResult(t, staleRec.Body.Bytes())
	var stale todo.OMPSyncResult
	if err := json.Unmarshal([]byte(mcpContentText(t, staleResultMap)), &stale); err != nil {
		t.Fatal(err)
	}
	if stale.Applied || !stale.Stale || stale.ServerSeq != 1 || stale.SnapshotSeq != 1 || len(stale.Items) != 2 {
		t.Fatalf("unexpected stale response: %+v", stale)
	}
}

func TestMCP_TodoSyncOMP_RejectsMalformedProjection(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	validTodo := func() map[string]any {
		return map[string]any{
			"taskKey": "0:0",
			"phase":   "build",
			"title":   "task",
			"status":  "open",
		}
	}
	baseArgs := func(items ...map[string]any) map[string]any {
		todos := make([]any, len(items))
		for i := range items {
			todos[i] = items[i]
		}
		return map[string]any{
			"workspace":   "ws",
			"sessionId":   "session",
			"snapshotSeq": 1,
			"mode":        "reconcile",
			"todos":       todos,
		}
	}

	tests := []struct {
		name        string
		args        func() map[string]any
		wantMessage string
	}{
		{
			name: "unknown top-level field",
			args: func() map[string]any {
				args := baseArgs(validTodo())
				args["unexpected"] = true
				return args
			},
			wantMessage: "unknown field",
		},
		{
			name: "unknown per-todo field",
			args: func() map[string]any {
				item := validTodo()
				item["unexpected"] = true
				return baseArgs(item)
			},
			wantMessage: "unknown field",
		},
		{
			name: "missing task key",
			args: func() map[string]any {
				item := validTodo()
				delete(item, "taskKey")
				return baseArgs(item)
			},
			wantMessage: "taskKey is required",
		},
		{
			name: "duplicate task key",
			args: func() map[string]any {
				return baseArgs(validTodo(), validTodo())
			},
			wantMessage: "duplicate",
		},
		{
			name: "invalid status",
			args: func() map[string]any {
				item := validTodo()
				item["status"] = "cancelled"
				return baseArgs(item)
			},
			wantMessage: "invalid todos[0].status",
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			api.HandleMCP(rec, mcpTodoRPC(t, i+1, "todo_sync_omp", tc.args(), "test-token-secret"))
			result := parseMCPResult(t, rec.Body.Bytes())
			if result["isError"] != true {
				t.Fatalf("expected malformed projection rejection: %+v", result)
			}
			if text := mcpContentText(t, result); !strings.Contains(text, tc.wantMessage) {
				t.Fatalf("expected error containing %q, got %q", tc.wantMessage, text)
			}
		})
	}
}

func TestMCP_TodoSyncOMP_StaleResponsesAreIsolatedBySession(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()

	sync := func(id int, sessionID string, snapshotSeq int, title string) todo.OMPSyncResult {
		t.Helper()
		items := []any{}
		if title != "" {
			items = append(items, map[string]any{
				"taskKey": "0:0",
				"phase":   "build",
				"title":   title,
				"status":  "open",
			})
		}
		rec := httptest.NewRecorder()
		api.HandleMCP(rec, mcpTodoRPC(t, id, "todo_sync_omp", map[string]any{
			"workspace":   "ws",
			"sessionId":   sessionID,
			"snapshotSeq": snapshotSeq,
			"mode":        "reconcile",
			"todos":       items,
		}, "test-token-secret"))
		resultMap := parseMCPResult(t, rec.Body.Bytes())
		if resultMap["isError"] == true {
			t.Fatalf("unexpected sync error: %+v", resultMap)
		}
		var result todo.OMPSyncResult
		if err := json.Unmarshal([]byte(mcpContentText(t, resultMap)), &result); err != nil {
			t.Fatalf("decode sync output: %v", err)
		}
		return result
	}

	if result := sync(1, "session-a", 2, "A current"); !result.Applied {
		t.Fatalf("session A setup was not applied: %+v", result)
	}
	if result := sync(2, "session-b", 3, "B current"); !result.Applied {
		t.Fatalf("session B setup was not applied: %+v", result)
	}

	staleA := sync(3, "session-a", 1, "")
	if staleA.Applied || !staleA.Stale || staleA.ServerSeq != 2 || len(staleA.Items) != 1 {
		t.Fatalf("unexpected stale session A response: %+v", staleA)
	}
	if item := staleA.Items[0]; item.Title != "A current" || item.ExternalRef == nil || item.ExternalRef.SessionID != "session-a" {
		t.Fatalf("stale session A response leaked or lost another session: %+v", item)
	}

	staleB := sync(4, "session-b", 2, "")
	if staleB.Applied || !staleB.Stale || staleB.ServerSeq != 3 || len(staleB.Items) != 1 {
		t.Fatalf("unexpected stale session B response: %+v", staleB)
	}
	if item := staleB.Items[0]; item.Title != "B current" || item.ExternalRef == nil || item.ExternalRef.SessionID != "session-b" {
		t.Fatalf("stale session B response leaked or lost another session: %+v", item)
	}
}

func TestMCP_TodoSyncOMP_ReconcileReturnsFullCurrentSessionProjection(t *testing.T) {
	api, store, cleanup := helperMCPTodoAPI(t)
	defer cleanup()
	userTodo, err := store.Create(todo.CreateInput{
		Workspace: "ws",
		Title:     "user-owned",
		Status:    todo.StatusOpen,
	})
	if err != nil {
		t.Fatalf("create user-owned Todo: %v", err)
	}

	call := func(id, snapshotSeq int, mode string, items []any) todo.OMPSyncResult {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleMCP(rec, mcpTodoRPC(t, id, "todo_sync_omp", map[string]any{
			"workspace":   "ws",
			"sessionId":   "session",
			"snapshotSeq": snapshotSeq,
			"mode":        mode,
			"todos":       items,
		}, "test-token-secret"))
		resultMap := parseMCPResult(t, rec.Body.Bytes())
		if resultMap["isError"] == true {
			t.Fatalf("unexpected sync error: %+v", resultMap)
		}
		var result todo.OMPSyncResult
		if err := json.Unmarshal([]byte(mcpContentText(t, resultMap)), &result); err != nil {
			t.Fatalf("decode sync output: %v", err)
		}
		return result
	}

	initial := []any{
		map[string]any{"taskKey": "keep", "phase": "build", "title": "old keep", "status": "open"},
		map[string]any{"taskKey": "remove", "phase": "build", "title": "remove", "status": "open"},
	}
	if result := call(1, 1, "upsert", initial); !result.Applied || result.Created != 2 {
		t.Fatalf("unexpected initial projection: %+v", result)
	}

	reconciled := call(2, 2, "reconcile", []any{
		map[string]any{"taskKey": "keep", "phase": "verify", "title": "kept current", "status": "blocked", "blocker": "review"},
		map[string]any{"taskKey": "new", "phase": "verify", "title": "new current", "status": "in_progress"},
	})
	if !reconciled.Applied || reconciled.Stale || reconciled.Created != 1 || reconciled.Updated != 1 ||
		reconciled.Removed != 1 || len(reconciled.Items) != 2 {
		t.Fatalf("unexpected reconcile response: %+v", reconciled)
	}
	itemsByKey := make(map[string]todo.Todo, len(reconciled.Items))
	for _, item := range reconciled.Items {
		if item.ExternalRef == nil || item.ExternalRef.SessionID != "session" {
			t.Fatalf("reconcile response included a foreign or unowned item: %+v", item)
		}
		itemsByKey[item.ExternalRef.TaskKey] = item
	}
	if keep, ok := itemsByKey["keep"]; !ok || keep.Title != "kept current" || keep.Status != todo.StatusBlocked ||
		keep.ExternalRef.Phase != "verify" || keep.ExternalRef.Blocker != "review" {
		t.Fatalf("reconcile response omitted the current retained item: %+v", keep)
	}
	if added, ok := itemsByKey["new"]; !ok || added.Title != "new current" || added.Status != todo.StatusInProgress {
		t.Fatalf("reconcile response omitted the current new item: %+v", added)
	}
	if _, exists := itemsByKey["remove"]; exists {
		t.Fatalf("reconcile response retained a removed item: %+v", reconciled.Items)
	}
	allItems, _, _ := store.List(todo.Filter{})
	userTodoFound := false
	for _, item := range allItems {
		if item.ID == userTodo.ID {
			userTodoFound = true
			if item.Metadata.Source != todo.SourceUI || item.Title != "user-owned" {
				t.Fatalf("reconcile mutated the user-owned Todo: %+v", item)
			}
		}
	}
	if !userTodoFound {
		t.Fatal("reconcile removed the user-owned Todo")
	}
}

func TestMCP_TodoSyncOMP_RejectsNonLoopback(t *testing.T) {
	api, _, cleanup := helperMCPTodoAPI(t)
	defer cleanup()
	req := mcpTodoRPC(t, 1, "todo_sync_omp", map[string]any{
		"workspace": "ws", "sessionId": "session", "snapshotSeq": 1, "mode": "upsert", "todos": []any{},
	}, "test-token-secret")
	req.RemoteAddr = "192.168.1.20:12345"
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)
	result := parseMCPResult(t, rec.Body.Bytes())
	if result["isError"] != true {
		t.Fatalf("expected non-loopback rejection: %+v", result)
	}
}
