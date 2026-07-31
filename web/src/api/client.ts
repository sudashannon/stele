import { encodeTodoId, normalizeTodo } from './types'
import type { ChangeSummary, ChangesResponse, WorkspaceConfig, WikiComponentResponse, LintIssue, WikiComponent, WikiGraphData, WikiSession, RecentItem, ChangeDetail, ChatConfig, ChatConfigPatch, ChatProviders, ReportRequest, ReportResponse, ReportMeta, Bookmark, SyncConfigResponse, SyncResult, TodoListResponse, Todo, TodoStatus, CreateTodoInput, UpdateTodoInput } from './types'

export async function fetchChanges(): Promise<ChangeSummary[]> {
  const res = await fetch('/api/changes')
  if (!res.ok) {
    throw new Error(`fetchChanges failed: ${res.status}`)
  }
  const body: ChangesResponse = await res.json()
  return body.changes ?? []
}

export async function fetchWorkspaces(): Promise<WorkspaceConfig[]> {
  const res = await fetch('/api/workspaces')
  if (!res.ok) throw new Error(`fetchWorkspaces failed: ${res.status}`)
  return res.json()
}

export async function addWorkspace(cfg: WorkspaceConfig): Promise<void> {
  const res = await fetch('/api/workspaces', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `添加工作区失败 (${res.status})`)
  }
}

// removeWorkspace unregisters a workspace by alias. Without it, a workspace
// whose path stopped resolving (the "以下 workspace 无法读取" banner) was a
// dead end -- the only fix was hand-editing ~/.comet-panel/workspaces.yaml.
export async function removeWorkspace(alias: string): Promise<void> {
  const res = await fetch('/api/workspaces?alias=' + encodeURIComponent(alias), {
    method: 'DELETE',
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `移除工作区失败 (${res.status})`)
  }
}

export async function fetchBookmarks(): Promise<Bookmark[]> {
  const res = await fetch('/api/bookmarks')
  if (!res.ok) throw new Error(`fetchBookmarks failed: ${res.status}`)
  return res.json()
}

export async function addBookmark(b: { path: string; title: string; type: string }): Promise<Bookmark[]> {
  const res = await fetch('/api/bookmarks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(b),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `addBookmark failed (${res.status})`)
  }
  return res.json()
}

export async function removeBookmark(path: string): Promise<Bookmark[]> {
  const res = await fetch('/api/bookmarks?path=' + encodeURIComponent(path), { method: 'DELETE' })
  if (!res.ok) throw new Error(`removeBookmark failed: ${res.status}`)
  return res.json()
}

// Distinct from fetchChanges() (Task 4), which discards the envelope's
// metadata for callers that only need the bare array. This variant keeps
// failedWorkspaces so App.tsx can surface the "workspace unreadable"
// warning banner (design doc error-handling table requirement).
export async function fetchChangesWithMeta(): Promise<ChangesResponse> {
  const res = await fetch('/api/changes')
  if (!res.ok) throw new Error(`fetchChangesWithMeta failed: ${res.status}`)
  return res.json()
}

export async function fetchWikiComponent(id: string): Promise<WikiComponentResponse> {
  const res = await fetch('/api/wiki/component/x?id=' + encodeURIComponent(id))
  if (!res.ok) throw new Error(`fetchWikiComponent failed: ${res.status}`)
  return res.json()
}

export async function fetchWikiLint(): Promise<LintIssue[]> {
  const res = await fetch('/api/wiki/lint')
  if (!res.ok) throw new Error(`fetchWikiLint failed: ${res.status}`)
  return res.json()
}

// Kept for LintPanel.tsx, which already consumes this name; delegates to
// fetchWikiLint() to avoid duplicating the request logic.
export async function fetchLintIssues(): Promise<LintIssue[]> {
  return fetchWikiLint()
}

export async function fetchWikiIndex(): Promise<WikiComponent[]> {
  const res = await fetch('/api/wiki/index')
  if (!res.ok) throw new Error(`fetchWikiIndex failed: ${res.status}`)
  return res.json()
}

// fetchWikiGraph is the relationship-graph counterpart to fetchWikiIndex()
// above: it returns components AND edges from GET /api/wiki/graph so
// WikiGraph.tsx can render actual relationships instead of a nodes-only
// grid. Deliberately separate from fetchWikiIndex() -- that function's
// Promise<WikiComponent[]> signature is depended on by App.tsx and must
// not change.
export async function fetchWikiGraph(): Promise<WikiGraphData> {
  const res = await fetch('/api/wiki/graph')
  if (!res.ok) throw new Error(`fetchWikiGraph failed: ${res.status}`)
  return res.json()
}

export async function fetchSessions(): Promise<WikiSession[]> {
  const res = await fetch('/api/wiki/sessions')
  if (!res.ok) throw new Error(`fetchSessions failed: ${res.status}`)
  const body: { sessions?: WikiSession[] | null } = await res.json()
  return body.sessions ?? []
}

export async function fetchSession(id: string): Promise<WikiSession> {
  const res = await fetch('/api/wiki/session?id=' + encodeURIComponent(id))
  if (!res.ok) throw new Error(`fetchSession failed: ${res.status}`)
  return res.json()
}

export async function fetchRecent(offset?: number, limit?: number): Promise<RecentItem[]> {
  const params = new URLSearchParams()
  if (offset !== undefined) params.set('offset', String(offset))
  if (limit !== undefined) params.set('limit', String(limit))
  const qs = params.toString() ? '?' + params.toString() : ''
  const res = await fetch('/api/wiki/recent' + qs)
  if (!res.ok) throw new Error(`fetchRecent failed: ${res.status}`)
  return res.json()
}

export async function fetchChangeDetail(name: string, workspace?: string): Promise<ChangeDetail> {
  const q = workspace ? '?workspace=' + encodeURIComponent(workspace) : ''
  const res = await fetch('/api/changes/' + encodeURIComponent(name) + q)
  if (!res.ok) throw new Error(`fetchChangeDetail failed: ${res.status}`)
  return res.json()
}

export async function fetchArtifactContent(path: string, workspace?: string): Promise<string> {
  const params = new URLSearchParams({ path })
  if (workspace) params.set('workspace', workspace)
  const res = await fetch('/api/artifact?' + params.toString())
  if (!res.ok) throw new Error(`fetchArtifactContent failed: ${res.status}`)
  return res.text()
}

export interface ChatStreamEvent {
  type: 'thinking' | 'delta' | 'done'
  content?: string
}

export interface ChatSessionMessage {
  role: string
  content: { type: string; text?: string; thinking?: string }[]
}

export interface ChatSession {
  change: string
  messages: ChatSessionMessage[]
  context_files: string[]
  usage: { total_input: number; total_output: number }
  created_at: string
  updated_at: string
}

export async function fetchChatSession(change: string): Promise<ChatSession> {
  const res = await fetch('/api/chat/session?change=' + encodeURIComponent(change))
  if (!res.ok) throw new Error(`fetchChatSession failed: ${res.status}`)
  return res.json()
}

export async function fetchChatConfig(): Promise<ChatConfig> {
  const res = await fetch('/api/chat/config')
  if (!res.ok) throw new Error(`fetchChatConfig failed: ${res.status}`)
  return res.json()
}

export async function updateChatConfig(patch: ChatConfigPatch): Promise<ChatConfig> {
  const res = await fetch('/api/chat/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
  if (!res.ok) throw new Error(`updateChatConfig failed: ${res.status}`)
  return res.json()
}

export async function fetchChatProviders(): Promise<ChatProviders> {
  const res = await fetch('/api/chat/providers')
  if (!res.ok) throw new Error(`fetchChatProviders failed: ${res.status}`)
  return res.json()
}

export async function fetchSyncConfig(): Promise<SyncConfigResponse> {
  const res = await fetch('/api/sync/config')
  if (!res.ok) throw new Error(`fetchSyncConfig failed: ${res.status}`)
  return res.json()
}

export async function updateSyncConfig(remote: string): Promise<SyncConfigResponse> {
  const res = await fetch('/api/sync/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ remote }),
  })
  if (!res.ok) throw new Error(`updateSyncConfig failed: ${res.status}`)
  return res.json()
}

export async function triggerSync(): Promise<SyncResult> {
  const res = await fetch('/api/sync', { method: 'POST' })
  if (!res.ok) throw new Error(`triggerSync failed: ${res.status}`)
  return res.json()
}

// Mirrors V1's static/app.js fetch+reader loop (lines ~366-401): the backend
// streams `data: {json}\n\n` SSE frames with {type, content} where type is
// thinking/delta/done — there is NO in-stream error event. Auth/provider
// errors (e.g. missing API key) are a pre-stream HTTP 4xx/5xx JSON body
// ({"message": "..."}), so res.ok MUST be checked before touching
// res.body.getReader().
export async function streamChat(
  change: string,
  message: string,
  contextFiles: string[],
  onEvent: (event: ChatStreamEvent) => void,
  includeGraph?: boolean,
  componentId?: string,
): Promise<void> {
  const res = await fetch('/api/chat/message', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      change,
      message,
      context_files: contextFiles,
      includeGraph: !!includeGraph,
      ...(componentId ? { componentId } : {}),
    }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string; error?: string })
    throw new Error(body.message || body.error || res.statusText)
  }

  const reader = res.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''

    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      try {
        const event = JSON.parse(line.slice(6)) as ChatStreamEvent
        onEvent(event)
      } catch {
        // malformed frame; skip it and keep streaming
      }
    }
  }
}

// Gate: POST /api/report 400s when no provider api_key is configured (see
// isProviderReady() in ReportView.tsx, which pre-checks this client-side so
// the request round-trip isn't the only signal). Error body follows the
// same { error } shape as addWorkspace() above.
export async function generateReport(req: ReportRequest): Promise<ReportResponse> {
  const res = await fetch('/api/report', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `生成报告失败 (${res.status})`)
  }
  return res.json()
}

export async function listReports(): Promise<ReportMeta[]> {
  const res = await fetch('/api/reports')
  if (!res.ok) throw new Error(`listReports failed: ${res.status}`)
  return res.json()
}

export async function getReport(name: string): Promise<ReportResponse> {
  const res = await fetch('/api/reports/get?name=' + encodeURIComponent(name))
  if (!res.ok) throw new Error(`getReport failed: ${res.status}`)
  return res.json()
}

export async function deleteReport(name: string): Promise<void> {
  const res = await fetch('/api/reports/get?name=' + encodeURIComponent(name), { method: 'DELETE' })
  if (!res.ok) throw new Error(`deleteReport failed: ${res.status}`)
}

export interface SemanticSearchResult {
  id: string
  title: string
  workspace: string
  type: string
  similarity: number
  /** Frontmatter tags, omitted by the backend when a document has none. */
  tags?: string[]
}

// searchSemantic is the semantic-search data source: the backend embeds the
// query server-side (bun scripts/embed.ts) and ranks it against every
// precomputed component embedding by cosine similarity, returning only the
// top matches -- no corpus fetch or client-side WASM encoder required.
//
// `topK` MUST be >= 1. The backend only truncates when `req.TopK > 0`
// (wiki/api.go HandleSemanticSearch), so passing 0 silently asks for the
// entire matching corpus -- SemanticSearch.tsx used to do exactly that.
// `signal` lets a caller cancel a superseded keystroke instead of racing it.
export async function searchSemantic(
  query: string,
  topK = 10,
  signal?: AbortSignal,
): Promise<SemanticSearchResult[]> {
  if (topK < 1) throw new Error(`searchSemantic: topK must be >= 1, got ${topK}`)
  const res = await fetch('/api/wiki/search-semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, topK }),
    signal,
  })
  // A failed embed is not "no results" -- surface it so the UI can say the
  // search backend is degraded rather than showing an empty state.
  if (!res.ok) {
    const detail = await res.text().catch(() => '')
    throw new Error(detail || `searchSemantic failed: ${res.status}`)
  }
  return res.json()
}

// fetchCachedSummary asks whether a summary already exists (204 = no) without
// generating one. summarizeDocument cannot answer that: a cache miss there
// calls the LLM, so the viewer could not probe on open without billing a
// generation for every document merely opened.
export async function fetchCachedSummary(path: string, signal?: AbortSignal): Promise<string | null> {
  const res = await fetch('/api/wiki/summary?id=' + encodeURIComponent(path), { signal })
  if (res.status === 204 || res.status === 404) return null
  if (!res.ok) throw new Error(`fetchCachedSummary failed: ${res.status}`)
  const body = (await res.json()) as { summary?: string }
  return body.summary?.trim() ? body.summary : null
}

// summarizeDocument reaches GET /api/wiki/summarize, an LLM summary cached
// under ~/.comet-panel/wiki/summaries. The endpoint has been registered in
// main.go since the wiki API landed but had no caller, so the capability was
// unreachable for a human user.
export async function summarizeDocument(id: string): Promise<string> {
  const res = await fetch('/api/wiki/summarize?id=' + encodeURIComponent(id))
  if (res.status === 404) throw new Error('该文档不在当前索引中')
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { error?: string })
    throw new Error(body.error || `summarizeDocument failed: ${res.status}`)
  }
  const data = (await res.json()) as { summary: string }
  return data.summary
}

// fetchCommunityOverview reaches GET /api/wiki/overview. Like summarize it was
// registered but uncalled, so only MCP agents could read community overviews.
// The backend returns 404 for communities smaller than 3 members.
export async function fetchCommunityOverview(community: number): Promise<string> {
  const res = await fetch('/api/wiki/overview?community=' + encodeURIComponent(String(community)))
  if (res.status === 404) throw new Error('该社区成员少于 3 个，未生成综述')
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { error?: string })
    throw new Error(body.error || `fetchCommunityOverview failed: ${res.status}`)
  }
  const data = (await res.json()) as { body: string }
  return data.body
}

export async function rebuildWiki(): Promise<void> {
  const res = await fetch('/api/wiki/rebuild', { method: 'POST' })
  if (!res.ok) throw new Error(`rebuild failed: ${res.status}`)
}

export interface FixDeadLinkRequest {
  sourceId: string
  oldPath: string
  newPath: string
}

export interface FixDeadLinkResult {
  sourceId: string
  fixed: boolean
  error?: string
}

export async function fixDeadLinks(reqs: FixDeadLinkRequest[]): Promise<FixDeadLinkResult[]> {
  const res = await fetch('/api/wiki/fix-dead-links', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(reqs),
  })
  if (!res.ok) throw new Error(`fix dead links failed: ${res.status}`)
  const data = await res.json()
  return data.results
}

// Share

interface CreateShareResponse {
  url: string
}

export async function createShareLink(path: string, workspace?: string, ttl?: number): Promise<CreateShareResponse> {
  const res = await fetch('/api/share/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, workspace, ttl }),
  })
  if (!res.ok) throw new Error(`Create share link failed: ${res.status}`)
  return res.json()
}

export async function revokeShareLink(token: string): Promise<void> {
  const tokenParam = encodeURIComponent(token)
  const res = await fetch(`/api/share/revoke?token=${tokenParam}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Revoke share link failed: ${res.status}`)
}

// ── Todo API ─────────────────────────────────────────────────────────────────

export interface TodoQueryParams {
  status?: TodoStatus
  workspace?: string
  change?: string
  wikiComponentId?: string
  q?: string
}

export async function fetchTodos(params?: TodoQueryParams): Promise<TodoListResponse> {
  const qs = new URLSearchParams()
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null && v !== '') qs.set(k, v)
    }
  }
  const url = `/api/todos${qs.toString() ? '?' + qs.toString() : ''}`
  const res = await fetch(url)
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(body.error || `获取待办失败 (${res.status})`)
  }
  const data: TodoListResponse = await res.json()
  data.items = data.items.map(normalizeTodo)
  return data
}

export async function createTodo(data: CreateTodoInput): Promise<Todo> {
  const res = await fetch('/api/todos', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(body.error || `创建待办失败 (${res.status})`)
  }
  const todo: Todo = await res.json()
  return normalizeTodo(todo)
}

export async function updateTodo(id: string, patch: UpdateTodoInput): Promise<Todo> {
  const res = await fetch(`/api/todos/${encodeTodoId(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(body.error || `更新待办失败 (${res.status})`)
  }
  const todo: Todo = await res.json()
  return normalizeTodo(todo)
}

export async function deleteTodo(id: string): Promise<void> {
  const res = await fetch(`/api/todos/${encodeTodoId(id)}`, { method: 'DELETE' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(body.error || `删除待办失败 (${res.status})`)
  }
}
