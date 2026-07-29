package wiki

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"comet-ui/internal/todo"
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

func TestMCP_ToolsList_TenTools(t *testing.T) {
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

	if len(tools) != 10 {
		t.Fatalf("expected 10 tools (6 wiki + 4 todo), got %d", len(tools))
	}

	toolNames := map[string]bool{}
	for _, t := range tools {
		tm := t.(map[string]any)
		toolNames[tm["name"].(string)] = true
	}
	for _, name := range []string{"todo_list", "todo_create", "todo_update", "todo_delete"} {
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
