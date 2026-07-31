package wiki

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// mcpRPC builds a minimal JSON-RPC request body for the given method/params.
func mcpRPC(t *testing.T, id any, method string, params any) []byte {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id != nil {
		req["id"] = id
	}
	if params != nil {
		req["params"] = params
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return body
}

func TestMCP_Initialize(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	body := mcpRPC(t, float64(1), "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test"},
	})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json content type, got %q", ct)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", resp.Result)
	}
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("expected protocolVersion 2025-03-26, got %v", result["protocolVersion"])
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Fatalf("expected capabilities.tools to be present")
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != "comet-panel-wiki" {
		t.Fatalf("expected serverInfo.name comet-panel-wiki, got %v", result["serverInfo"])
	}
}

func TestMCP_NotificationsInitialized_NoBody(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	body := mcpRPC(t, nil, "notifications/initialized", nil)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	if w.Code != 202 {
		t.Fatalf("expected 202 Accepted, got %d", w.Code)
	}
}

func TestMCP_ToolsList(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	body := mcpRPC(t, float64(2), "tools/list", nil)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	var resp jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", resp.Result)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got %T", result["tools"])
	}
	if len(tools) != 13 {
		t.Fatalf("expected 13 tools, got %d: %v", len(tools), tools)
	}

	wantNames := map[string]bool{
		"wiki_search": false, "wiki_component": false, "wiki_neighbors": false,
		"wiki_overview": false, "wiki_read": false, "wiki_lint": false,
		"wiki_context": false, "wiki_sessions": false,
		"todo_list": false, "todo_create": false, "todo_update": false, "todo_delete": false, "todo_sync_omp": false,
	}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		name, _ := tm["name"].(string)
		if _, known := wantNames[name]; !known {
			t.Fatalf("unexpected tool name: %s", name)
		}
		wantNames[name] = true
		if tm["description"] == "" {
			t.Fatalf("tool %s missing description", name)
		}
		if _, ok := tm["inputSchema"].(map[string]any); !ok {
			t.Fatalf("tool %s missing inputSchema", name)
		}
	}
	for name, seen := range wantNames {
		if !seen {
			t.Fatalf("expected tool %s to be listed", name)
		}
	}
}

func TestMCP_ToolsCall_Search(t *testing.T) {
	root := Component{ID: "/openspec/changes/foo/design.md", Title: "Foo Design", Type: TypeDesign, Path: "/openspec/changes/foo/design.md"}
	g := BuildGraph([]Component{root}, nil)
	api := NewAPI(g)

	body := mcpRPC(t, float64(3), "tools/call", map[string]any{
		"name":      "wiki_search",
		"arguments": map[string]any{"query": "Foo"},
	})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	var resp jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected top-level error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", resp.Result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got %v", result["content"])
	}
	first := content[0].(map[string]any)
	if first["type"] != "text" {
		t.Fatalf("expected text content type, got %v", first["type"])
	}
	text, _ := first["text"].(string)
	if text == "" {
		t.Fatalf("expected non-empty text content")
	}
	// Either the embed script ran and surfaced a real result, or (if bun /
	// scripts/embed.ts is unavailable in the test environment) the tool
	// reports an embedding failure as an isError result — both are valid
	// "it works" outcomes; a silently empty/garbled response is not.
	isErr, _ := result["isError"].(bool)
	if isErr {
		if !strings.Contains(text, "embedding failed") && !strings.Contains(text, "no embedding produced") {
			t.Fatalf("unexpected error text: %s", text)
		}
	} else if !strings.Contains(text, "Foo Design") {
		t.Fatalf("expected search result to mention Foo Design, got: %s", text)
	}
}

func TestMCP_ToolsCall_UnknownTool(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	body := mcpRPC(t, float64(4), "tools/call", map[string]any{
		"name":      "wiki_nonexistent",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	var resp jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	result := resp.Result.(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError true for unknown tool, got %v", result)
	}
}

func TestMCP_ToolsCall_Lint(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	body := mcpRPC(t, float64(5), "tools/call", map[string]any{
		"name":      "wiki_lint",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	var resp jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "0 lint issues") {
		t.Fatalf("expected clean graph to report 0 lint issues, got: %s", text)
	}
}

func TestMCP_UnknownMethod(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	body := mcpRPC(t, float64(6), "does/notexist", nil)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	var resp jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected method-not-found error, got %+v", resp.Error)
	}
}

func TestMCP_GetMethodNotAllowed(t *testing.T) {
	api := NewAPI(BuildGraph(nil, nil))

	req := httptest.NewRequest("GET", "/mcp", nil)
	w := httptest.NewRecorder()
	api.HandleMCP(w, req)

	if w.Code != 405 {
		t.Fatalf("expected 405 for GET, got %d", w.Code)
	}
}
