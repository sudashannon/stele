export type WorkspaceSourceType = 'openspec' | 'trellis' | 'superpowers' | 'docs'

export interface ChangeSummary {
  name: string
  title?: string
  workflow: string
  sourceType?: WorkspaceSourceType
  phase: string
  archived: boolean
  tasksCompleted: number
  tasksTotal: number
  verifyResult: 'pass' | 'fail' | 'pending' | string
  createdAt: string
  artifacts?: Record<string, boolean>
  visualized?: boolean
  designReviewed?: boolean
  verifyReviewed?: boolean
  verifiedAt?: string
  buildMode?: string
  reviewMode?: string
  tddMode?: string
  autoTransition?: boolean
  stateWarning?: string
  workspace?: string // added in Phase②, optional until then
  componentId?: string // wiki graph node ID (.comet.yaml path); optional until backend populates it
  lifecycle?: LifecycleStep[]
  nextTransition?: TransitionAction
}

export interface Bookmark {
  path: string
  title: string
  type: string
  starredAt: string
}

export interface ChangesResponse {
  changes: ChangeSummary[]
  dir?: string
  failedWorkspaces?: string[]
}

export interface WorkspaceConfig {
  alias: string
  path: string
  color: string
  type?: WorkspaceSourceType
}

export interface LifecycleStep {
  key: string
  label: string
}

export interface TransitionAction {
  target: string
  label: string
  command: string
  blockedReason?: string
}

export type WikiComponentType = 'change' | 'proposal' | 'design' | 'tasks' | 'spec' | 'plan' | 'artifact' | 'diagram' | 'knowledge' | 'report' | 'session' | string

export type WikiEdgeKind = 'reads' | 'edits' | string

export type WikiEdgeSource = 'session' | string

export interface WikiEdge {
  from: string
  to: string
  kind: WikiEdgeKind
  source: WikiEdgeSource
  weight?: number
}

export interface WikiComponentResponse {
  component: { id: string; title: string }
  forward: WikiEdge[]
  backlinks: WikiEdge[]
}

export interface LowQualitySignals {
  chars: number
  headings: number
  emptyHeadings: number
  placeholders: number
  signals: string[]
  /** Carries an upstream `source:` URL: thin, but not authored here. */
  imported: boolean
}

export interface LintIssue {
  rule: string
  componentId: string
  detail: string
  lowQuality?: LowQualitySignals
}

export interface WikiComponent {
  id: string
  type: WikiComponentType
  title: string
  path: string
  workspace: string
  frontmatter?: Record<string, unknown>
  updatedAt?: string
}

export interface RecentItem {
  id: string
  title: string
  type: WikiComponentType
  workspace: string
  updatedAt: string
  path: string
}

// WikiGraphData is the full graph view for the relationship visualization
// (GET /api/wiki/graph): every component alongside every edge, unlike
// fetchWikiIndex()'s nodes-only WikiComponent[].
export interface WikiGraphData {
  components: WikiComponent[]
  edges: WikiEdge[]
  communities?: Record<string, number>
  communityLabels?: Record<string, string>
}

/** One entry of a session's own task tracker, replayed from its operations. */
export interface SessionTodo {
  phase?: string
  content: string
  /** pending | in_progress | completed | dropped | blocked */
  status: string
  /** Why the task is stuck, as the session recorded it. */
  blocker?: string
}

export interface WikiSession {
  id: string
  /** Agent runtime this session came from ("omp"). */
  source?: string
  path: string
  workspace: string
  title: string
  cwd: string
  startedAt: string
  updatedAt: string
  userTurns: number
  toolCalls: Record<string, number>
  /** Documents the session created or overwrote (`write`). */
  writes: string[]
  /** Documents the session patched (`edit`). */
  edits: string[]
  reads: string[]
  intents: string[]
  /** Subagent transcripts whose work is folded into these totals. */
  subagents?: string[]
  /** The session's task list as it stood when the transcript ended. */
  todos?: SessionTodo[]
  /** Tasks finished under earlier lists, absent from `todos`. */
  todosCompleted?: string[]
  /** Counts that survive on the list endpoint even when the lists are withheld. */
  todoOpen?: number
  todoTotal?: number
  todoDone?: number
  /** Turns + tool calls per local date (YYYY-MM-DD): a session spans days. */
  activity?: Record<string, number>
  /** How many times the session replaced its list with a new plan. */
  todoReplans?: number
  todosTruncated?: boolean
  intentsTruncated?: boolean
  pathsTruncated?: boolean
}

export interface ArtifactInfo {
  file: string
  label: string
  exists: boolean
  path?: string
  external?: boolean
  isTasks?: boolean
}

export interface PhaseInfo {
  key: string
  label: string
  status: string
  artifacts: ArtifactInfo[]
}

export interface ChangeDetail extends ChangeSummary {
  phases: PhaseInfo[]
}

export interface ChatProviderConfig {
  api_key: string
  api_base: string
  model: string
  temperature: number
  max_tokens: number
  thinking: string
}

export interface ChatConfig {
  active_provider: string
  providers: Record<string, ChatProviderConfig>
}

// Partial update shape for PUT /api/chat/config: the backend merges any
// provided fields into the existing provider config, so callers omit
// unchanged fields (notably api_key when the user left it blank) rather
// than sending a full ChatProviderConfig.
export interface ChatConfigPatch {
  active_provider?: string
  providers?: Record<string, Partial<ChatProviderConfig>>
}

export interface ChatProviderInfo {
  name: string
  models: string[]
  supports_images: boolean
}

export interface ChatProviders {
  active: string
  providers: ChatProviderInfo[]
}

export type ReportType = 'weekly' | 'monthly'

export interface ReportRequest {
  type: ReportType
  start: string
  end: string
  workspace?: string
}

export interface ReportSkippedDocument {
  path: string
  error: string
}

export interface ReportCoverage {
  sourceDocuments: number
  contextDocuments: number
  readableDocuments: number
  truncatedDocuments: number
  missingEmbeddings: number
  failedWorkspaces?: string[]
  skippedDocuments?: ReportSkippedDocument[]
  clusteringMode?: 'vector' | 'hybrid' | 'lexical'
}

export interface ReportResponse {
  format: 'md' | 'html'
  body: string
  savedName?: string
  coverage?: ReportCoverage
  inputDocumentCount?: number
  clusterCount?: number
  sourceReportIDs?: string[]
}

export interface ReportMeta {
  name: string
  type: ReportType
  start: string
  end: string
  createdAt: string
}

export interface SyncConfigResponse {
  enabled: boolean
  remote: string
}

export interface SyncResult {
  action: 'pushed' | 'pulled' | 'merged' | 'up-to-date' | 'error'
  filesChanged: number
  message: string
}

// ── Todo ────────────────────────────────────────────────────────────────────

export type TodoMetadataSource = 'ui' | 'mcp' | 'omp'
export type TodoStatus = 'open' | 'in_progress' | 'done' | 'blocked' | 'dropped'
export type TodoPriority = 'urgent' | 'high' | 'normal' | 'low'

export interface TodoChangeRef {
  workspace: string
  name: string
}

export interface TodoExternalRef {
  system: 'omp'
  sessionId: string
  taskKey: string
  phase: string
  blocker: string
}

export interface TodoWikiRef {
  componentId: string
  workspace: string
  titleSnapshot: string
}

export interface Todo {
  id: string
  workspace: string
  title: string
  notes: string
  status: TodoStatus
  priority: TodoPriority
  dueAt: string | null
  change: TodoChangeRef | null
  wikiRefs: TodoWikiRef[]
  metadata: { source: TodoMetadataSource }
  externalRef: TodoExternalRef | null
  createdAt: string
  updatedAt: string
  completedAt: string | null
}

export interface TodoCounts {
  total: number
  open: number
  inProgress: number
  done: number
  blocked: number
  dropped: number
}

export interface TodoListResponse {
  items: Todo[]
  counts: TodoCounts
  revision: number
  writable: boolean
}

export interface CreateTodoInput {
  workspace: string
  title: string
  notes?: string
  status?: TodoStatus
  priority?: TodoPriority
  dueAt?: string | null
  change?: TodoChangeRef | null
  wikiRefs?: TodoWikiRef[]
}

export type UpdateTodoInput = Partial<CreateTodoInput>

/** Fill in Go omitempty defaults so consumers always see a well-shaped Todo. */
export function normalizeTodo(t: Todo): Todo {
  return {
    ...t,
    notes: t.notes ?? '',
    dueAt: t.dueAt ?? null,
    change: t.change ?? null,
    wikiRefs: t.wikiRefs ?? [],
    externalRef: t.externalRef ?? null,
    completedAt: t.completedAt ?? null,
  }
}

/** URL-safe encode a todo ID for use in REST path segments. */
export function encodeTodoId(id: string): string {
  return encodeURIComponent(id)
}

/** Decode a URL-encoded todo ID back to its raw value. */
export function decodeTodoId(id: string): string {
  return decodeURIComponent(id)
}
