package wiki

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"comet-ui/internal/todo"
)

// MCP JSON-RPC types

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP protocol types

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

var mcpTools = []mcpTool{
	{
		Name:        "wiki_search",
		Description: "语义搜索工程文档。输入自然语言查询,返回最相关的组件列表(按相似度排序)。建议对感兴趣的结果继续调用 wiki_neighbors 查看其图谱关联。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "搜索查询(中文/英文均可)"},
			},
			"required": []string{"query"},
		},
	},
	{
		Name:        "wiki_component",
		Description: "查看某个组件的详细信息,包括类型、所属工作区、前向引用和反向引用。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "组件 ID(绝对路径)"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "wiki_neighbors",
		Description: "查看某个组件的2-hop图谱邻居(直接关联 + 间接关联),了解该组件在知识图谱中的位置。通常在 wiki_search 找到相关文档后使用,可以发现内容不相似但图谱上关联的文档。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "组件 ID(绝对路径)"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "wiki_overview",
		Description: "获取某个主题社区的AI生成综述(需要社区编号,可从 wiki_search 结果推断)。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"community": map[string]any{"type": "number", "description": "社区 ID"},
			},
			"required": []string{"community"},
		},
	},
	{
		Name:        "wiki_read",
		Description: "读取指定路径的文档原始内容(markdown)。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "文件绝对路径"},
			},
			"required": []string{"path"},
		},
	},
	{
		Name:        "wiki_lint",
		Description: "检查文档健康度:列出死链、孤儿节点、缺失验证报告等问题。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "todo_list",
		Description: "列出所有待办事项,可按状态/工作区/Change/Wiki组件/关键词(搜索标题和备注)筛选。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":          map[string]any{"type": "string", "description": "筛选状态: open, in_progress, done"},
				"workspace":       map[string]any{"type": "string", "description": "按 Todo 所属工作区筛选"},
				"change":          map[string]any{"type": "string", "description": "按关联 Change 名称筛选"},
				"wikiComponentId": map[string]any{"type": "string", "description": "按关联的 Wiki 组件 ID 筛选"},
				"q":               map[string]any{"type": "string", "description": "标题和备注关键词搜索"},
			},
		},
	},
	{
		Name:        "todo_create",
		Description: "创建新的待办事项。需要 loopback + Bearer token 鉴权。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"workspace": map[string]any{"type": "string", "description": "所属工作区别名(必填)"},
				"title":     map[string]any{"type": "string", "description": "待办标题(必填)"},
				"notes":     map[string]any{"type": "string", "description": "备注"},
				"status":    map[string]any{"type": "string", "description": "状态: open, in_progress, done (默认 open)"},
				"priority":  map[string]any{"type": "string", "description": "优先级: urgent, high, normal, low (默认 normal)"},
				"dueAt":     map[string]any{"type": "string", "description": "截止时间 RFC3339"},
				"change":    map[string]any{"type": "object", "description": "关联 Change: {workspace, name}"},
				"wikiRefs":  map[string]any{"type": "array", "description": "关联 Wiki 组件: [{componentId, workspace}]"},
			},
			"required": []string{"workspace", "title"},
		},
	},
	{
		Name:        "todo_update",
		Description: "更新待办事项的部分字段。需要 loopback + Bearer token 鉴权。change 设为 null 清除关联; wikiRefs 设为 [] 清除列表。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":        map[string]any{"type": "string", "description": "待办 ID(必填)"},
				"workspace": map[string]any{"type": "string", "description": "新工作区"},
				"title":     map[string]any{"type": "string", "description": "新标题"},
				"notes":     map[string]any{"type": "string", "description": "新备注"},
				"status":    map[string]any{"type": "string", "description": "新状态"},
				"priority":  map[string]any{"type": "string", "description": "新优先级"},
				"dueAt":     map[string]any{"type": []string{"string", "null"}, "description": "新截止时间 RFC3339, null 清除"},
				"change":    map[string]any{"type": []string{"object", "null"}, "description": "新 Change 关联 {workspace,name}, null 清除"},
				"wikiRefs":  map[string]any{"type": "array", "description": "新 WikiRefs 列表 [{componentId,workspace}], 传 [] 清除"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "todo_delete",
		Description: "删除待办事项。需要 loopback + Bearer token 鉴权。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "待办 ID(必填)"},
			},
			"required": []string{"id"},
		},
	},
}

// HandleMCP is the MCP Streamable HTTP endpoint. It accepts POST JSON-RPC
// 2.0 requests (initialize / notifications/initialized / tools/list /
// tools/call). GET (the SSE half of the Streamable HTTP transport, used for
// server-initiated notifications) is not implemented since none of these
// tools push async server events — every response is a synchronous reply to
// a client request.
func (a *API) HandleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		http.Error(w, "SSE not implemented", 405)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", 400)
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	switch req.Method {
	case "initialize":
		a.mcpInitialize(w, req)
	case "notifications/initialized":
		// Client confirms initialization; per spec this is a notification
		// (no id), so no JSON-RPC response body is sent — just 202
		// Accepted to close out the HTTP request cleanly.
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		a.mcpToolsList(w, req)
	case "tools/call":
		a.mcpToolsCall(w, r, req)
	default:
		writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func (a *API) mcpInitialize(w http.ResponseWriter, req jsonRPCRequest) {
	writeRPCResult(w, req.ID, map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "comet-panel-wiki",
			"version": "1.0.0",
		},
	})
}

func (a *API) mcpToolsList(w http.ResponseWriter, req jsonRPCRequest) {
	writeRPCResult(w, req.ID, map[string]any{
		"tools": mcpTools,
	})
}

func (a *API) mcpToolsCall(w http.ResponseWriter, r *http.Request, req jsonRPCRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "invalid params")
		return
	}

	var result mcpToolResult
	switch params.Name {
	case "wiki_search":
		result = a.mcpWikiSearch(params.Arguments)
	case "wiki_component":
		result = a.mcpWikiComponent(params.Arguments)
	case "wiki_neighbors":
		result = a.mcpWikiNeighbors(params.Arguments)
	case "wiki_overview":
		result = a.mcpWikiOverview(params.Arguments)
	case "wiki_read":
		result = a.mcpWikiRead(params.Arguments)
	case "wiki_lint":
		result = a.mcpWikiLint(params.Arguments)
	case "todo_list":
		result = a.mcpTodoList(params.Arguments)
	case "todo_create":
		result = a.mcpTodoCreate(r, params.Arguments)
	case "todo_update":
		result = a.mcpTodoUpdate(r, params.Arguments)
	case "todo_delete":
		result = a.mcpTodoDelete(r, params.Arguments)
	default:
		result = mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "unknown tool: " + params.Name}},
			IsError: true,
		}
	}

	writeRPCResult(w, req.ID, result)
}

// Tool implementations

// mcpScored is a search hit: a component id plus its ranking score. Named
// (rather than anonymous, as in HandleSemanticSearch) purely so sort.Slice's
// closure below can be declared without an inline struct literal type.
type mcpScored struct {
	id  string
	sim float64
}

func (a *API) mcpWikiSearch(args map[string]any) mcpToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "query is required"}}, IsError: true}
	}

	// Use the same embed-and-rank logic as HandleSemanticSearch, but render
	// the results as text instead of a JSON array.
	scriptPath := findEmbedScript()
	queryComps := []Component{{ID: "__query__", Title: query, Path: ""}}
	embedResult, err := ComputeEmbeddings(queryComps, scriptPath)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "embedding failed: " + err.Error()}}, IsError: true}
	}
	queryVec, ok := embedResult["__query__"]
	if !ok || len(queryVec) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "no embedding produced"}}, IsError: true}
	}

	a.mu.RLock()
	embeddings := a.graph.Embeddings()
	components := a.graph.Components()
	a.mu.RUnlock()

	queryNorm := vecNorm(queryVec)
	queryLower := strings.ToLower(query)
	var results []mcpScored
	for id, vec := range embeddings {
		sim := cosineSim(queryVec, vec, queryNorm, vecNorm(vec))
		if sim > 0.15 {
			if c, ok := components[id]; ok && strings.Contains(strings.ToLower(c.Title), queryLower) {
				sim += 0.3
			}
			results = append(results, mcpScored{id, sim})
		}
	}
	// Fallback: if vector search yields nothing, do title substring match.
	if len(results) == 0 {
		for id, c := range components {
			if strings.Contains(strings.ToLower(c.Title), queryLower) {
				results = append(results, mcpScored{id, 0.5})
			}
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].sim > results[j].sim })
	if len(results) > 20 {
		results = results[:20]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d results for \"%s\":\n\n", len(results), query)
	for i, res := range results {
		c := components[res.id]
		fmt.Fprintf(&sb, "%d. [%s] %s (workspace: %s, similarity: %.0f%%)\n   path: %s\n\n",
			i+1, c.Type, c.Title, c.Workspace, res.sim*100, c.Path)
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

func (a *API) mcpWikiComponent(args map[string]any) mcpToolResult {
	id, _ := args["id"].(string)
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "id is required"}}, IsError: true}
	}

	a.mu.RLock()
	c, ok := a.graph.Component(id)
	forward := a.graph.Forward(id)
	backlinks := a.graph.Backlinks(id)
	a.mu.RUnlock()

	if !ok {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "component not found: " + id}}, IsError: true}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\nType: %s\nWorkspace: %s\nPath: %s\n\n", c.Title, c.Type, c.Workspace, c.Path)
	if len(forward) > 0 {
		sb.WriteString("### Forward references:\n")
		for _, e := range forward {
			fmt.Fprintf(&sb, "- [%s] → %s\n", e.Kind, e.To)
		}
	}
	if len(backlinks) > 0 {
		sb.WriteString("\n### Backlinks (referenced by):\n")
		for _, e := range backlinks {
			fmt.Fprintf(&sb, "- [%s] ← %s\n", e.Kind, e.From)
		}
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

func (a *API) mcpWikiNeighbors(args map[string]any) mcpToolResult {
	id, _ := args["id"].(string)
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "id is required"}}, IsError: true}
	}

	a.mu.RLock()
	components := a.graph.Components()
	forward := a.graph.Forward(id)
	backlinks := a.graph.Backlinks(id)
	a.mu.RUnlock()

	seen := map[string]bool{id: true}
	var sb strings.Builder
	fmt.Fprintf(&sb, "2-hop neighbors of: %s\n\n", id)

	// 1-hop
	sb.WriteString("### Direct (1-hop):\n")
	var firstHopIDs []string
	for _, e := range forward {
		if !seen[e.To] {
			if c, ok := components[e.To]; ok {
				fmt.Fprintf(&sb, "- [%s] → %s (%s)\n", e.Kind, c.Title, c.Type)
				firstHopIDs = append(firstHopIDs, e.To)
				seen[e.To] = true
			}
		}
	}
	for _, e := range backlinks {
		if !seen[e.From] {
			if c, ok := components[e.From]; ok {
				fmt.Fprintf(&sb, "- [%s] ← %s (%s)\n", e.Kind, c.Title, c.Type)
				firstHopIDs = append(firstHopIDs, e.From)
				seen[e.From] = true
			}
		}
	}

	// 2-hop
	sb.WriteString("\n### Indirect (2-hop):\n")
	count := 0
	for _, nid := range firstHopIDs {
		a.mu.RLock()
		nf := a.graph.Forward(nid)
		a.mu.RUnlock()
		for _, e := range nf {
			if !seen[e.To] {
				if c, ok := components[e.To]; ok {
					fmt.Fprintf(&sb, "- %s (%s)\n", c.Title, c.Type)
					seen[e.To] = true
					count++
					if count >= 20 {
						break
					}
				}
			}
		}
		if count >= 20 {
			break
		}
	}
	if count == 0 {
		sb.WriteString("(none)\n")
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

func (a *API) mcpWikiOverview(args map[string]any) mcpToolResult {
	communityF, _ := args["community"].(float64) // JSON numbers decode as float64
	communityID := int(communityF)

	a.mu.RLock()
	communities := a.graph.Communities()
	components := a.graph.Components()
	a.mu.RUnlock()

	var members []Component
	for id, cid := range communities {
		if cid == communityID {
			if c, ok := components[id]; ok {
				members = append(members, c)
			}
		}
	}

	if len(members) < 3 {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("community %d has fewer than 3 members", communityID)}},
			IsError: true,
		}
	}

	// Mirror CommunityOverview's cache lookup (api.go) rather than calling
	// it directly: CommunityOverview is keyed by a member's changeID, not
	// by the raw community number this tool takes as input, so we look up
	// the cache file the same way — same directory, same key derivation —
	// but starting from communityID instead of resolving it from a change.
	cacheDir := filepath.Join(a.indexCacheDir, "overviews")
	key := overviewCacheKey(members)
	data, err := os.ReadFile(overviewCachePath(cacheDir, communityID, key))

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Community %d (%d members)\n\n", communityID, len(members))
	if err == nil && len(data) > 0 {
		sb.Write(data)
	} else {
		sb.WriteString("(no cached overview yet)\n\nMembers:\n")
		for _, m := range members {
			fmt.Fprintf(&sb, "- [%s] %s (workspace: %s)\n", m.Type, m.Title, m.Workspace)
		}
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

func (a *API) mcpWikiRead(args map[string]any) mcpToolResult {
	path, _ := args["path"].(string)
	if path == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "path is required"}}, IsError: true}
	}

	// Security: only allow reading files that are indexed components —
	// either by ID (the common case, since Component.ID is the absolute
	// path) or by Path field, mirroring how other components are keyed.
	a.mu.RLock()
	_, found := a.graph.Component(path)
	if !found {
		for _, c := range a.graph.Components() {
			if c.Path == path {
				found = true
				break
			}
		}
	}
	a.mu.RUnlock()
	if !found {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "file not in wiki index (security: only indexed files can be read)"}},
			IsError: true,
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "read error: " + err.Error()}}, IsError: true}
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: string(content)}}}
}

func (a *API) mcpWikiLint(args map[string]any) mcpToolResult {
	a.mu.RLock()
	issues := a.graph.Lint()
	a.mu.RUnlock()

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d lint issues:\n\n", len(issues))

	grouped := map[string][]LintIssue{}
	var rules []string
	for _, i := range issues {
		if _, ok := grouped[i.Rule]; !ok {
			rules = append(rules, i.Rule)
		}
		grouped[i.Rule] = append(grouped[i.Rule], i)
	}
	sort.Strings(rules)
	for _, rule := range rules {
		items := grouped[rule]
		fmt.Fprintf(&sb, "### %s (%d)\n", rule, len(items))
		shown := items
		if len(shown) > 10 {
			shown = shown[:10]
			fmt.Fprintf(&sb, "  (showing first 10 of %d)\n", len(items))
		}
		for _, item := range shown {
			fmt.Fprintf(&sb, "- %s\n", item.Detail)
		}
		sb.WriteString("\n")
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

// mcpAuth checks loopback + Bearer token against the provided token bytes.
func (a *API) mcpAuth(r *http.Request, tokenBytes []byte) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if !net.ParseIP(host).IsLoopback() {
		return false
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	return todo.EqualToken([]byte(auth[len(prefix):]), tokenBytes)
}

// resolveMCPWikiTitles resolves WikiRef titles from the live graph, matching
// the REST behavior. Does not hold graph locks across Store ops.
func (a *API) resolveMCPWikiTitles(refs []todo.WikiRef) []todo.WikiRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]todo.WikiRef, len(refs))
	copy(out, refs)
	a.mu.RLock()
	for i := range out {
		c, ok := a.graph.Component(out[i].ComponentID)
		if ok {
			if c.Title != "" {
				out[i].TitleSnapshot = c.Title
			}
			if c.Workspace != "" {
				out[i].Workspace = c.Workspace
			}
		}
	}
	a.mu.RUnlock()
	return out
}

// --- Todo MCP tools ---

func (a *API) mcpTodoList(args map[string]any) mcpToolResult {
	store, _ := a.TodoStoreSnapshot()
	if store == nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "todo store not available"}}, IsError: true}
	}
	f := todo.Filter{
		Status:          todo.Status(stringArg(args, "status")),
		Workspace:       stringArg(args, "workspace"),
		Change:          stringArg(args, "change"),
		WikiComponentID: stringArg(args, "wikiComponentId"),
		Q:               stringArg(args, "q"),
	}
	items, counts, revision := store.List(f)

	// Resolve current Wiki titles.
	for i := range items {
		items[i].WikiRefs = a.resolveMCPWikiTitles(items[i].WikiRefs)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Todos (%d total, %d open, %d in_progress, %d done) revision %d\n\n", counts.Total, counts.Open, counts.InProgress, counts.Done, revision)
	if len(items) == 0 {
		sb.WriteString("(no items)\n")
	} else {
		for _, item := range items {
			dueStr := ""
			if item.DueAt != "" {
				dueStr = " due:" + item.DueAt
			}
			changeStr := ""
			if item.Change != nil {
				changeStr = fmt.Sprintf(" change:%s/%s", item.Change.Workspace, item.Change.Name)
			}
			wikiStr := ""
			if len(item.WikiRefs) > 0 {
				wikiStr = fmt.Sprintf(" wiki:%d", len(item.WikiRefs))
			}
			notesStr := ""
			if item.Notes != "" {
				notesStr = " notes:" + item.Notes
			}
			fmt.Fprintf(&sb, "- [%s] %s (%s/%s/%s)%s%s%s%s\n", item.ID[:8], item.Title, item.Workspace, item.Status, item.Priority, dueStr, changeStr, wikiStr, notesStr)
		}
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

func (a *API) mcpTodoCreate(r *http.Request, args map[string]any) mcpToolResult {
	store, token := a.TodoStoreSnapshot()
	if store == nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "todo store not available"}}, IsError: true}
	}
	if !a.mcpAuth(r, token) {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "write access denied: loopback + Bearer token required"}}, IsError: true}
	}

	in := todo.CreateInput{
		Workspace: stringArg(args, "workspace"),
		Title:     stringArg(args, "title"),
		Notes:     stringArg(args, "notes"),
		Status:    todo.Status(stringArg(args, "status")),
		Priority:  todo.Priority(stringArg(args, "priority")),
		DueAt:     stringArg(args, "dueAt"),
		Metadata:  todo.Metadata{Source: todo.SourceMCP},
	}
	if v, ok := args["change"]; ok && v != nil {
		ch, ok := v.(map[string]any)
		if !ok {
			return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "change must be an object with workspace and name"}}, IsError: true}
		}
		in.Change = &todo.ChangeRef{
			Workspace: fmt.Sprint(ch["workspace"]),
			Name:      fmt.Sprint(ch["name"]),
		}
	}
	if v, ok := args["wikiRefs"]; ok && v != nil {
		refs, ok := v.([]any)
		if !ok {
			return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "wikiRefs must be an array"}}, IsError: true}
		}
		for _, r := range refs {
			rm, ok := r.(map[string]any)
			if !ok {
				return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "each wikiRefs entry must be an object with componentId and workspace"}}, IsError: true}
			}
			in.WikiRefs = append(in.WikiRefs, todo.WikiRef{
				ComponentID: fmt.Sprint(rm["componentId"]),
				Workspace:   fmt.Sprint(rm["workspace"]),
			})
		}
	}

	item, err := store.Create(in)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "create failed: " + err.Error()}}, IsError: true}
	}
	item.WikiRefs = a.resolveMCPWikiTitles(item.WikiRefs)
	data, _ := json.MarshalIndent(item, "", "  ")
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: string(data)}}}
}

func (a *API) mcpTodoUpdate(r *http.Request, args map[string]any) mcpToolResult {
	store, token := a.TodoStoreSnapshot()
	if store == nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "todo store not available"}}, IsError: true}
	}
	if !a.mcpAuth(r, token) {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "write access denied: loopback + Bearer token required"}}, IsError: true}
	}

	id := stringArg(args, "id")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "id is required"}}, IsError: true}
	}

	var in todo.UpdateInput
	if v, ok := args["workspace"]; ok {
		s := fmt.Sprint(v)
		in.Workspace = &s
	}
	if v, ok := args["title"]; ok {
		s := fmt.Sprint(v)
		in.Title = &s
	}
	if v, ok := args["notes"]; ok {
		s := fmt.Sprint(v)
		in.Notes = &s
	}
	if v, ok := args["status"]; ok {
		s := todo.Status(fmt.Sprint(v))
		in.Status = &s
	}
	if v, ok := args["priority"]; ok {
		s := todo.Priority(fmt.Sprint(v))
		in.Priority = &s
	}
	if v, exists := args["dueAt"]; exists {
		in.DueAtSet = true
		if v != nil {
			s := fmt.Sprint(v)
			in.DueAt = &s
		}
		// v == nil clears dueAt.
	}
	// change: absent = no-op, null = clear, object = replace.
	if v, exists := args["change"]; exists {
		in.ChangeSet = true
		if v != nil {
			ch, ok := v.(map[string]any)
			if !ok {
				return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "change must be an object {workspace,name} or null"}}, IsError: true}
			}
			in.Change = &todo.ChangeRef{
				Workspace: fmt.Sprint(ch["workspace"]),
				Name:      fmt.Sprint(ch["name"]),
			}
		}
	}
	if v, exists := args["wikiRefs"]; exists {
		in.WikiRefsSet = true
		if v != nil {
			refs, ok := v.([]any)
			if !ok {
				return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "wikiRefs must be an array"}}, IsError: true}
			}
			var wr []todo.WikiRef
			for _, r := range refs {
				rm, ok := r.(map[string]any)
				if !ok {
					return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "each wikiRefs entry must be an object with componentId and workspace"}}, IsError: true}
				}
				wr = append(wr, todo.WikiRef{
					ComponentID: fmt.Sprint(rm["componentId"]),
					Workspace:   fmt.Sprint(rm["workspace"]),
				})
			}
			in.WikiRefs = wr
		}
	}

	item, err := store.Update(id, in)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "update failed: " + err.Error()}}, IsError: true}
	}
	item.WikiRefs = a.resolveMCPWikiTitles(item.WikiRefs)
	data, _ := json.MarshalIndent(item, "", "  ")
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: string(data)}}}
}

func (a *API) mcpTodoDelete(r *http.Request, args map[string]any) mcpToolResult {
	store, token := a.TodoStoreSnapshot()
	if store == nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "todo store not available"}}, IsError: true}
	}
	if !a.mcpAuth(r, token) {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "write access denied: loopback + Bearer token required"}}, IsError: true}
	}

	id := stringArg(args, "id")
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "id is required"}}, IsError: true}
	}

	if err := store.Delete(id); err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "delete failed: " + err.Error()}}, IsError: true}
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "deleted " + id}}}
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// Helpers

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}
