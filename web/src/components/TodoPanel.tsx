import { useState, useMemo, useEffect, useCallback } from 'react'
import type { Todo, TodoCounts, TodoStatus, TodoPriority, TodoChangeRef, TodoWikiRef, CreateTodoInput, UpdateTodoInput, ChangeSummary } from '../api/types'
import type { WorkspaceConfig, WikiComponent } from '../api/types'

// ── Date helpers ─────────────────────────────────────────────────────────────

function parseSafeDate(iso: string | null): Date | null {
  if (!iso) return null
  const d = new Date(iso)
  if (isNaN(d.getTime())) return null
  return d
}

function formatDatetimeLocal(iso: string | null): string {
  const d = parseSafeDate(iso)
  if (!d) return ''
  const yyyy = d.getFullYear()
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  return `${yyyy}-${mm}-${dd}T${hh}:${min}`
}

function localDateKey(iso: string | null): string | null {
  const d = parseSafeDate(iso)
  if (!d) return null
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function todayKey(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function tomorrowKey(): string {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function formatDueBadge(iso: string | null): string {
  const d = parseSafeDate(iso)
  if (!d) return ''
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const tomorrow = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  tomorrow.setDate(tomorrow.getDate() + 1)
  const dueDay = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  if (dueDay.getTime() < today.getTime()) return '逾期'
  if (dueDay.getTime() === today.getTime()) return '今天'
  if (dueDay.getTime() === tomorrow.getTime()) return '明天'
  return `${d.getMonth() + 1}/${d.getDate()}`
}

const RECENT_WIKI_OPTION_LIMIT = 20

function wikiUpdatedTimestamp(component: WikiComponent): number | null {
  if (!component.updatedAt) return null
  const timestamp = Date.parse(component.updatedAt)
  return Number.isNaN(timestamp) ? null : timestamp
}

function formatWikiOptionLabel(component: WikiComponent): string {
  const timestamp = wikiUpdatedTimestamp(component)
  if (timestamp === null) return `${component.type}: ${component.title}`
  const updated = new Date(timestamp)
  return `${updated.getMonth() + 1}/${updated.getDate()} · ${component.type}: ${component.title}`
}

// ── Priority colours & labels ────────────────────────────────────────────────

const PRIORITY_COLORS: Record<TodoPriority, string> = {
  urgent: 'var(--color-danger)',
  high: 'var(--color-warn)',
  normal: 'var(--color-text-tertiary)',
  low: 'var(--color-border-hover)',
}

const PRIORITY_LABELS: Record<TodoPriority, string> = {
  urgent: '紧急',
  high: '高',
  normal: '普通',
  low: '低',
}

const STATUS_LABELS: Record<TodoStatus, string> = {
  open: '待处理',
  in_progress: '进行中',
  done: '已完成',
}

// ── Grouping ─────────────────────────────────────────────────────────────────

interface GroupedTodos {
  overdue: Todo[]
  today: Todo[]
  tomorrow: Todo[]
  later: Todo[]
  undated: Todo[]
  done: Todo[]
}

function groupTodos(todos: Todo[]): GroupedTodos {
  const today = todayKey()
  const tomorrow = tomorrowKey()
  const groups: GroupedTodos = { overdue: [], today: [], tomorrow: [], later: [], undated: [], done: [] }

  for (const todo of todos) {
    if (todo.status === 'done') {
      groups.done.push(todo)
      continue
    }
    const dk = localDateKey(todo.dueAt)
    if (!dk) {
      groups.undated.push(todo)
    } else if (dk < today) {
      groups.overdue.push(todo)
    } else if (dk === today) {
      groups.today.push(todo)
    } else if (dk === tomorrow) {
      groups.tomorrow.push(todo)
    } else {
      groups.later.push(todo)
    }
  }
  return groups
}

// ── Group display order ──────────────────────────────────────────────────────

const GROUP_SPECS: { key: keyof GroupedTodos; label: string; icon: string }[] = [
  { key: 'overdue', label: '逾期', icon: '⚠' },
  { key: 'today', label: '今天', icon: '📅' },
  { key: 'tomorrow', label: '明天', icon: '📅' },
  { key: 'later', label: '稍后', icon: '📅' },
  { key: 'undated', label: '无日期', icon: '📅' },
  { key: 'done', label: '已完成', icon: '✓' },
]

// ── Status filter type ───────────────────────────────────────────────────────

type StatusFilter = 'all' | 'today' | 'upcoming' | 'undated' | 'done'

const STATUS_FILTERS: { key: StatusFilter; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'today', label: '今天' },
  { key: 'upcoming', label: '即将到来' },
  { key: 'undated', label: '无日期' },
  { key: 'done', label: '已完成' },
]

// ── Props ────────────────────────────────────────────────────────────────────

interface TodoPanelProps {
  todos: Todo[]
  counts: TodoCounts | null
  writable: boolean
  loading: boolean
  error: string | null
  onCreate: (data: CreateTodoInput) => Promise<Todo>
  onUpdate: (id: string, patch: UpdateTodoInput) => Promise<Todo>
  onDelete: (id: string) => Promise<void>
  workspaces: WorkspaceConfig[]
  wikiComponents: WikiComponent[]
  onNavigateWiki: (path: string) => void
  onNavigateChange: (workspace: string, changeName: string) => void
  draftChange?: TodoChangeRef | null
  draftWikiRef?: TodoWikiRef | null
  onDraftConsumed: () => void
  // Focus the quick-capture input (used by "新建待办" command from palette)
  focusCaptureRef?: React.MutableRefObject<(() => void) | null>
  defaultWorkspace?: string | null
  changes?: ChangeSummary[]
}

export function TodoPanel({
  todos, counts, writable, loading, error,
  onCreate, onUpdate, onDelete,
  workspaces, wikiComponents,
  onNavigateWiki, onNavigateChange,
  draftChange, draftWikiRef, onDraftConsumed,
  focusCaptureRef,
  defaultWorkspace,
  changes,
}: TodoPanelProps) {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [quickCapture, setQuickCapture] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [workspaceFilter, setWorkspaceFilter] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [qcWorkspace, setQcWorkspace] = useState<string>(() =>
    draftChange?.workspace ?? draftWikiRef?.workspace ?? defaultWorkspace ?? workspaces[0]?.alias ?? '',
  )
  const [qcChange, setQcChange] = useState<TodoChangeRef | null>(null)
  const [qcWikiRefs, setQcWikiRefs] = useState<TodoWikiRef[]>([])
  const [filterOpen, setFilterOpen] = useState(false)
  const captureRef = useCallback(() => {
    const el = document.querySelector<HTMLInputElement>('[data-testid="todo-quick-capture"]')
    el?.focus()
  }, [])

  // Register focus-capture for external triggers (palette "新建待办")
  useEffect(() => {
    if (focusCaptureRef) focusCaptureRef.current = captureRef
  }, [focusCaptureRef, captureRef])

  // Responsive breakpoints
  const [isNarrow, setIsNarrow] = useState(() => typeof window !== 'undefined' && window.innerWidth < 900)
  const [isMedium, setIsMedium] = useState(() => typeof window !== 'undefined' && window.innerWidth >= 900 && window.innerWidth < 1280)

  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth
      setIsNarrow(w < 900)
      setIsMedium(w >= 900 && w < 1280)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useEffect(() => {
    if (draftChange) { setQcChange(draftChange); setQcWorkspace(draftChange.workspace) }
    if (draftWikiRef) { setQcWikiRefs((prev) => [...prev.filter((r) => r.componentId !== draftWikiRef.componentId), draftWikiRef]); setQcWorkspace(draftWikiRef.workspace) }
    if (draftChange || draftWikiRef) onDraftConsumed()
  }, [draftChange, draftWikiRef, onDraftConsumed])

  // Fill qcWorkspace from async sources when it's empty — never overwrite user choice.
  useEffect(() => {
    if (qcWorkspace) return
    const candidate = draftChange?.workspace ?? draftWikiRef?.workspace ?? defaultWorkspace ?? workspaces[0]?.alias ?? todos[0]?.workspace ?? ''
    if (candidate) setQcWorkspace(candidate)
  }, [qcWorkspace, draftChange, draftWikiRef, defaultWorkspace, workspaces, todos])

  // Filter and group
  const groups = useMemo(() => {
    let items = [...todos]
    const today = todayKey()
    const tomorrow = tomorrowKey()

    switch (statusFilter) {
      case 'today':
        items = items.filter((t) => t.status !== 'done' && localDateKey(t.dueAt) === today)
        break
      case 'upcoming':
        items = items.filter((t) => t.status !== 'done' && localDateKey(t.dueAt) && localDateKey(t.dueAt)! >= tomorrow)
        break
      case 'undated':
        items = items.filter((t) => t.status !== 'done' && !parseSafeDate(t.dueAt))
        break
      case 'done':
        items = items.filter((t) => t.status === 'done')
        break
    }

    if (workspaceFilter) items = items.filter((t) => t.workspace === workspaceFilter)

    if (searchQuery) {
      const q = searchQuery.toLowerCase()
      items = items.filter((t) => t.title.toLowerCase().includes(q) || (t.notes ?? '').toLowerCase().includes(q))
    }

    return groupTodos(items)
  }, [todos, statusFilter, workspaceFilter, searchQuery])

  const selectedTodo = useMemo(() => todos.find((t) => t.id === selectedId) ?? null, [todos, selectedId])

  const todayStats = useMemo(() => {
    const today = todayKey()
    let total = 0
    let completed = 0
    for (const t of todos) {
      if (localDateKey(t.dueAt) !== today) continue
      total++
      if (t.status === 'done') completed++
    }
    return { total, completed }
  }, [todos])

  const todoWorkspaces = useMemo(() => {
    const set = new Set(todos.map((t) => t.workspace).filter(Boolean))
    return [...set].sort()
  }, [todos])

  const handleQuickCapture = useCallback(async () => {
    const title = quickCapture.trim()
    if (!title || !writable || !qcWorkspace) return
    const change = qcChange
    const wikiRefs = qcWikiRefs.length > 0 ? [...qcWikiRefs] : undefined
    try {
      const todo = await onCreate({ workspace: qcWorkspace, title, change: change ?? undefined, wikiRefs })
      setSelectedId(todo.id)
      setQuickCapture('')
      setQcChange(null)
      setQcWikiRefs([])
    } catch {
      // error surfaced by useTodos — input retained for retry
    }
  }, [quickCapture, qcWorkspace, qcChange, qcWikiRefs, writable, onCreate])

  const clearQcContext = useCallback(() => {
    setQcChange(null)
    setQcWikiRefs([])
  }, [])

  // ── Render ──────────────────────────────────────────────────────────────────

  if (loading && todos.length === 0) {
    return (
      <div data-testid="todo-panel-loading" className="flex items-center justify-center h-full text-sm text-[var(--color-text-secondary)]">
        加载中…
      </div>
    )
  }

  if (error && todos.length === 0) {
    return (
      <div data-testid="todo-panel-error" className="flex flex-col items-center justify-center h-full gap-2 text-sm text-[var(--color-danger)]">
        <span>{error}</span>
      </div>
    )
  }

  const isEmpty = todos.length === 0 && !loading
  const openCount = (counts?.open ?? 0) + (counts?.inProgress ?? 0)
  const doneCount = counts?.done ?? 0

  return (
    <div className="flex flex-col h-full bg-[var(--color-surface)]">
      {/* Read-only banner */}
      {writable === false && (
        <div data-testid="todo-readonly-banner" className="text-xs bg-[color-mix(in_srgb,var(--color-warn)_15%,var(--color-surface))] text-[var(--color-warn)] p-2 text-center shrink-0">
          ⚠ 局域网访问 — 只读模式（无法创建、编辑或删除待办）
        </div>
      )}
      {/* Mutation error banner — inline, non-fatal */}
      {error && todos.length > 0 && (
        <div data-testid="todo-mutation-error" className="text-xs bg-[color-mix(in_srgb,var(--color-danger)_10%,var(--color-surface))] text-[var(--color-danger)] p-2 text-center shrink-0">
          {error}
        </div>
      )}

      {/* Header — compact: title + counts + daily completion strip */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-[var(--color-border)] shrink-0">
        <div className="flex items-center gap-3">
          <h2 className="text-sm font-semibold text-[var(--color-text-primary)]">待办</h2>
          <span className="text-xs text-[var(--color-text-secondary)]">
            {openCount} 个进行中 · {doneCount} 个已完成
          </span>
        </div>
        {todayStats.total > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-[10px] text-[var(--color-text-secondary)] tabular-nums">
              今天 {todayStats.completed}/{todayStats.total}
            </span>
            <div data-testid="todo-progress-strip" className="w-24 h-1.5 bg-[var(--color-border-subtle)] overflow-hidden">
              <div
                className="h-full bg-[var(--color-text-primary)] transition-all"
                style={{ width: `${Math.round((todayStats.completed / todayStats.total) * 100)}%` }}
              />
            </div>
          </div>
        )}
      </div>

      {/* Quick capture */}
      {writable !== false && (
        <div className="px-4 py-2 border-b border-[var(--color-border)] shrink-0">
          <div className="flex gap-2">
            <select
              data-testid="todo-qc-workspace"
              value={qcWorkspace}
              onChange={(e) => setQcWorkspace(e.target.value)}
              className="border border-[var(--color-border)] px-2 py-1 text-sm focus:outline-none focus:border-[var(--color-accent)] bg-white min-w-0 max-w-[8rem]"
              disabled={!writable}
            >
              <option value="">选择…</option>
              {workspaces.map((ws) => (
                <option key={ws.alias} value={ws.alias}>{ws.alias}</option>
              ))}
            </select>
            <input
              data-testid="todo-quick-capture"
              type="text"
              value={quickCapture}
              onChange={(e) => setQuickCapture(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  handleQuickCapture()
                }
              }}
              placeholder="快速添加待办…"
              className="flex-1 border border-[var(--color-border)] px-2 py-1 text-sm focus:outline-none focus:border-[var(--color-accent)]"
              disabled={!writable}
            />
            <button
              data-testid="todo-quick-capture-btn"
              onClick={handleQuickCapture}
              disabled={!quickCapture.trim() || !qcWorkspace || !writable}
              title={!qcWorkspace ? '请先选择工作区' : undefined}
              className="bg-[var(--color-accent)] text-white px-3 text-sm disabled:opacity-40"
            >
              添加
            </button>
          </div>
          {(qcChange || qcWikiRefs.length > 0) && (
            <div className="flex items-center gap-2 mt-1.5 text-[10px] text-[var(--color-text-secondary)]">
              <span>关联:</span>
              {qcChange && (
                <span data-testid="todo-qc-change" className="rounded px-1 py-0.5 bg-[var(--color-accent)]/10 text-[var(--color-accent)]">
                  {qcChange.workspace}/{qcChange.name}
                </span>
              )}
              {qcWikiRefs.map((ref) => (
                <span key={ref.componentId} data-testid={`todo-qc-wikiref-${ref.componentId}`} className="rounded px-1 py-0.5 bg-[var(--color-accent)]/10 text-[var(--color-accent)]">
                  {ref.titleSnapshot}
                </span>
              ))}
              <button onClick={clearQcContext} className="text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]">
                ✕ 清除
              </button>
            </div>
          )}
        </div>
      )}

      {/* Body: filters | list | detail */}
      <div className="flex-1 flex min-h-0">
        {/* Left filters — hidden on narrow, toggleable on medium */}
        {!isNarrow && (
          <div className={`w-[180px] shrink-0 border-r border-[var(--color-border)] overflow-y-auto ${isMedium && !filterOpen ? 'hidden' : ''}`}>
            <div className="p-3 space-y-3">
              {/* Status filter */}
              <div>
                <div className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase mb-1.5">视图</div>
                <div className="flex flex-col gap-0.5">
                  {STATUS_FILTERS.map((f) => (
                    <button
                      key={f.key}
                      data-testid={`todo-filter-${f.key}`}
                      onClick={() => setStatusFilter(f.key)}
                      className={`text-left text-xs px-2 py-1 ${
                        statusFilter === f.key
                          ? 'bg-[var(--color-accent)]/10 text-[var(--color-accent)] font-medium'
                          : 'text-[var(--color-text-secondary)] hover:bg-[var(--palette-highlight)]'
                      }`}
                    >
                      {f.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Workspace filter */}
              {todoWorkspaces.length > 1 && (
                <div>
                  <div className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase mb-1.5">工作区</div>
                  <select
                    data-testid="todo-workspace-filter"
                    value={workspaceFilter ?? ''}
                    onChange={(e) => setWorkspaceFilter(e.target.value || null)}
                    className="w-full border border-[var(--color-border)] text-xs px-2 py-1 bg-white"
                  >
                    <option value="">全部</option>
                    {todoWorkspaces.map((ws) => (
                      <option key={ws} value={ws}>{ws}</option>
                    ))}
                  </select>
                </div>
              )}

              {/* Search */}
              <div>
                <div className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase mb-1.5">搜索</div>
                <input
                  data-testid="todo-search-input"
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="搜索…"
                  className="w-full border border-[var(--color-border)] text-xs px-2 py-1 focus:outline-none focus:border-[var(--color-accent)]"
                />
              </div>
            </div>
          </div>
        )}

        {/* Filter toggle on medium widths */}
        {isMedium && (
          <button
            onClick={() => setFilterOpen((v) => !v)}
            className="shrink-0 text-xs px-2 py-1 border-b border-r border-[var(--color-border)] hover:bg-[var(--palette-highlight)]"
          >
            {filterOpen ? '◀' : '▶'} 筛选
          </button>
        )}

        {/* Middle: todo list */}
        <div className="flex-1 min-w-0 overflow-y-auto">
          {isEmpty ? (
            <div data-testid="todo-empty-state" className="flex flex-col items-center justify-center gap-2 text-center py-24 px-6">
              <span className="text-4xl text-[var(--color-text-tertiary)]" aria-hidden="true">✅</span>
              <p className="text-sm font-medium text-[var(--color-text-primary)]">暂无待办</p>
              <p className="text-xs text-[var(--color-text-secondary)]">使用上方输入框快速添加，或从变更/文档页面创建</p>
            </div>
          ) : (
            <div className="divide-y divide-[var(--color-border-subtle)]">
              {GROUP_SPECS.map((g) => {
                const items = groups[g.key]
                if (items.length === 0) return null
                return (
                  <div key={g.key} data-testid={`todo-group-${g.key}`}>
                    <div className="sticky top-0 bg-[var(--color-bg)] px-4 py-1.5 text-xs font-semibold text-[var(--color-text-secondary)] z-10 border-b border-[var(--color-border-subtle)]">
                      {g.icon} {g.label} <span className="font-normal">({items.length})</span>
                    </div>
                    {items.map((todo) => (
                      <TodoRow
                        key={todo.id}
                        todo={todo}
                        selected={todo.id === selectedId}
                        onSelect={() => setSelectedId(todo.id === selectedId ? null : todo.id)}
                        onToggleDone={() => {
                          const newStatus: TodoStatus = todo.status === 'done' ? 'open' : 'done'
                          onUpdate(todo.id, { status: newStatus }).catch(() => {})
                        }}
                        onNavigateWiki={onNavigateWiki}
                        onNavigateChange={onNavigateChange}
                        wikiComponents={wikiComponents}
                        writable={writable}
                      />
                    ))}
                  </div>
                )
              })}
            </div>
          )}
        </div>

        {/* Right: detail panel */}
        {selectedTodo && (
          <DetailPanel
            todo={selectedTodo}
            onUpdate={onUpdate}
            onDelete={onDelete}
            onNavigateWiki={onNavigateWiki}
            onNavigateChange={onNavigateChange}
            wikiComponents={wikiComponents}
            writable={writable}
            changes={changes}
            onClose={() => setSelectedId(null)}
            overlay={isNarrow}
          />
        )}
      </div>
    </div>
  )
}

// ── TodoRow ──────────────────────────────────────────────────────────────────

function TodoRow({
  todo,
  selected,
  onSelect,
  onToggleDone,
  onNavigateWiki,
  onNavigateChange,
  wikiComponents,
  writable,
}: {
  todo: Todo
  selected: boolean
  onSelect: () => void
  onToggleDone: () => void
  onNavigateWiki: (path: string) => void
  onNavigateChange: (workspace: string, changeName: string) => void
  wikiComponents: WikiComponent[]
  writable: boolean
}) {
  const isDone = todo.status === 'done'

  return (
    <div
      data-testid={`todo-row-${todo.id}`}
      onClick={onSelect}
      className={`flex items-start gap-2 px-4 py-2 cursor-pointer border-l-2 transition-colors ${
        selected
          ? 'border-l-[var(--color-accent)] bg-[var(--color-accent)]/5'
          : 'border-l-transparent hover:bg-[var(--palette-highlight)]'
      }`}
    >
      {/* Priority dot */}
      <span
        className="shrink-0 w-1.5 h-1.5 rounded-full mt-1.5"
        style={{ backgroundColor: PRIORITY_COLORS[todo.priority] }}
        title={PRIORITY_LABELS[todo.priority]}
      />

      {/* Done checkbox */}
      {writable !== false && (
        <button
          onClick={(e) => {
            e.stopPropagation()
            onToggleDone()
          }}
          className={`shrink-0 w-4 h-4 border mt-0.5 flex items-center justify-center text-[10px] ${
            isDone
              ? 'bg-[var(--color-success)] border-[var(--color-success)] text-white'
              : 'border-[var(--color-border)] hover:border-[var(--color-accent)]'
          }`}
        >
          {isDone && '✓'}
        </button>
      )}

      {/* Content */}
      <div className="min-w-0 flex-1">
        <div className={`text-sm ${isDone ? 'line-through text-[var(--color-text-tertiary)]' : 'text-[var(--color-text-primary)]'}`}>
          {todo.title || '(无标题)'}
        </div>
        <div className="flex items-center gap-1.5 mt-0.5 flex-wrap">
          {/* Due badge */}
          {todo.dueAt && (
            <span
              className={`text-[10px] rounded px-1 py-0 ${
                isDone
                  ? 'text-[var(--color-text-tertiary)]'
                  : formatDueBadge(todo.dueAt) === '逾期'
                    ? 'text-[var(--color-danger)] bg-red-50'
                    : 'text-[var(--color-text-secondary)]'
              }`}
            >
              {formatDueBadge(todo.dueAt)}
            </span>
          )}

          {/* Workspace tag */}
          {todo.workspace && (
            <span className="text-[10px] text-[var(--color-text-tertiary)] rounded bg-[var(--color-bg)] px-1">
              {todo.workspace}
            </span>
          )}

          {/* Status tag */}
          {!isDone && (
            <span className={`text-[10px] rounded px-1 py-0 ${
              todo.status === 'in_progress'
                ? 'bg-amber-50 text-[var(--color-warn)]'
                : 'text-[var(--color-text-tertiary)]'
            }`}>
              {STATUS_LABELS[todo.status]}
            </span>
          )}

          {/* Change chip */}
          {todo.change && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onNavigateChange(todo.change!.workspace, todo.change!.name)
              }}
              className="text-[10px] text-[var(--color-accent)] hover:underline"
            >
              {todo.change.name}
            </button>
          )}

          {/* Wiki ref chips */}
          {todo.wikiRefs.map((ref) => {
            const comp = wikiComponents.find((c) => c.id === ref.componentId)
            return (
              <button
                key={ref.componentId}
                onClick={(e) => {
                  e.stopPropagation()
                  onNavigateWiki(comp?.path ?? ref.componentId)
                }}
                className="text-[10px] text-[var(--color-accent)] hover:underline"
              >
                {ref.titleSnapshot}
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// ── DetailPanel ──────────────────────────────────────────────────────────────

function DetailPanel({
  todo,
  onUpdate,
  onDelete,
  onNavigateWiki,
  onNavigateChange,
  wikiComponents,
  writable,
  onClose,
  overlay,
  changes,
}: {
  todo: Todo
  onUpdate: (id: string, patch: UpdateTodoInput) => Promise<Todo>
  onDelete: (id: string) => Promise<void>
  onNavigateWiki: (path: string) => void
  onNavigateChange: (workspace: string, changeName: string) => void
  wikiComponents: WikiComponent[]
  writable: boolean
  onClose: () => void
  overlay: boolean
  changes?: ChangeSummary[]
}) {
  const [title, setTitle] = useState(todo.title)
  const [notes, setNotes] = useState(todo.notes)
  const [saving, setSaving] = useState(false)

  // Sync when selected todo changes
  useEffect(() => {
    setTitle(todo.title)
    setNotes(todo.notes)
  }, [todo.id, todo.title, todo.notes])

  const saveTitle = useCallback(() => {
    const trimmed = title.trim()
    if (!trimmed || trimmed === todo.title) return
    setSaving(true)
    onUpdate(todo.id, { title: trimmed }).finally(() => setSaving(false))
  }, [todo.id, title, todo.title, onUpdate])

  const saveNotes = useCallback(() => {
    if (notes === (todo.notes ?? '')) return
    setSaving(true)
    onUpdate(todo.id, { notes }).finally(() => setSaving(false))
  }, [todo.id, notes, todo.notes, onUpdate])

  const updateField = useCallback(
    (patch: UpdateTodoInput) => {
      onUpdate(todo.id, patch).catch(() => {})
    },
    [todo.id, onUpdate],
  )

  const handleDelete = useCallback(() => {
    onDelete(todo.id).catch(() => {})
  }, [todo.id, onDelete])

  const wikiOptions = useMemo(() => {
    const linkedIds = new Set(todo.wikiRefs.map((ref) => ref.componentId))
    const available = wikiComponents.filter(
      (component) => component.workspace === todo.workspace && !linkedIds.has(component.id),
    )
    const dated: Array<{ component: WikiComponent; timestamp: number }> = []
    const undated: WikiComponent[] = []
    for (const component of available) {
      const timestamp = wikiUpdatedTimestamp(component)
      if (timestamp === null) undated.push(component)
      else dated.push({ component, timestamp })
    }
    dated.sort((left, right) => right.timestamp - left.timestamp || left.component.title.localeCompare(right.component.title, 'zh-CN'))
    const recent = dated.slice(0, RECENT_WIKI_OPTION_LIMIT).map(({ component }) => component)
    const other = [
      ...dated.slice(RECENT_WIKI_OPTION_LIMIT).map(({ component }) => component),
      ...undated,
    ].sort((left, right) => left.title.localeCompare(right.title, 'zh-CN'))
    return { recent, other }
  }, [todo.wikiRefs, todo.workspace, wikiComponents])

  const panel = (
    <div
      data-testid="todo-detail"
      className={`flex flex-col h-full bg-white ${overlay ? 'fixed inset-0 z-30' : 'w-[320px] shrink-0 border-l border-[var(--color-border)]'}`}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-[var(--color-border)] shrink-0">
        <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">待办详情</h3>
        <button onClick={onClose} className="text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]">
          ✕
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {/* Title */}
        <div>
          <label className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">标题</label>
          <input
            data-testid="todo-detail-title"
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onBlur={saveTitle}
            onKeyDown={(e) => {
              if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
            }}
            disabled={!writable}
            className="w-full border border-[var(--color-border)] px-2 py-1 text-sm focus:outline-none focus:border-[var(--color-accent)] disabled:opacity-60"
          />
        </div>

        {/* Notes */}
        <div>
          <label className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">备注</label>
          <textarea
            data-testid="todo-detail-notes"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            onBlur={saveNotes}
            disabled={!writable}
            rows={3}
            className="w-full border border-[var(--color-border)] px-2 py-1 text-sm resize-y focus:outline-none focus:border-[var(--color-accent)] disabled:opacity-60"
          />
        </div>

        {/* Status */}
        <div>
          <label className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">状态</label>
          <div className="flex gap-1.5">
            {(['open', 'in_progress', 'done'] as TodoStatus[]).map((s) => (
              <button
                key={s}
                data-testid={`todo-status-${s}`}
                onClick={() => updateField({ status: s })}
                disabled={!writable}
                className={`text-xs px-2.5 py-1 border ${
                  todo.status === s
                    ? 'bg-[var(--color-accent)] text-white border-[var(--color-accent)]'
                    : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:bg-[var(--palette-highlight)]'
                } disabled:opacity-50`}
              >
                {STATUS_LABELS[s]}
              </button>
            ))}
          </div>
        </div>

        {/* Priority */}
        <div>
          <label className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">优先级</label>
          <div className="flex gap-1.5">
            {(['urgent', 'high', 'normal', 'low'] as TodoPriority[]).map((p) => (
              <button
                key={p}
                data-testid={`todo-priority-${p}`}
                onClick={() => updateField({ priority: p })}
                disabled={!writable}
                className={`text-xs px-2.5 py-1 border ${
                  todo.priority === p
                    ? 'text-white border-transparent'
                    : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:bg-[var(--palette-highlight)]'
                } disabled:opacity-50`}
                style={todo.priority === p ? { backgroundColor: PRIORITY_COLORS[p] } : undefined}
              >
                {PRIORITY_LABELS[p]}
              </button>
            ))}
          </div>
        </div>
        {/* Due date */}
        <div>
          <label className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">截止日期</label>
          <div className="flex items-center gap-2">
            <input
              data-testid="todo-detail-duedate"
              type="datetime-local"
              value={formatDatetimeLocal(todo.dueAt)}
              onChange={(e) =>
                updateField({ dueAt: e.target.value ? new Date(e.target.value).toISOString() : null })
              }
              disabled={!writable}
              className="border border-[var(--color-border)] px-2 py-1 text-xs focus:outline-none focus:border-[var(--color-accent)] disabled:opacity-60"
            />
            {todo.dueAt && writable && (
              <button
                data-testid="todo-clear-duedate"
                onClick={() => updateField({ dueAt: null })}
                className="text-xs text-[var(--color-text-secondary)] hover:text-[var(--color-danger)]"
              >
                清除
              </button>
            )}
          </div>
        </div>
        {/* Change association */}
        <div>
          <label className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">关联变更</label>
          {todo.change ? (
            <div className="flex items-center gap-2">
              <button
                onClick={() => onNavigateChange(todo.change!.workspace, todo.change!.name)}
                className="text-xs text-[var(--color-accent)] hover:underline"
              >
                {todo.change.workspace}/{todo.change.name}
              </button>
              {writable && (
                <>
                  <button
                    data-testid="todo-clear-change"
                    onClick={() => updateField({ change: null })}
                    className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
                  >
                    ✕
                  </button>
                  <select
                    data-testid="todo-detail-change-select"
                    value=""
                    onChange={(e) => {
                      if (e.target.value) updateField({ change: { workspace: todo.workspace, name: e.target.value } })
                    }}
                    className="border border-[var(--color-border)] text-xs px-1 py-0.5 bg-white"
                  >
                    <option value="">更换…</option>
                    {(changes ?? []).filter((c) => c.workspace === todo.workspace && c.name !== todo.change?.name).map((c) => (
                      <option key={c.name} value={c.name}>{c.name}</option>
                    ))}
                  </select>
                </>
              )}
            </div>
          ) : writable ? (
            <select
              data-testid="todo-detail-change-select"
              value=""
              onChange={(e) => {
                if (e.target.value) updateField({ change: { workspace: todo.workspace, name: e.target.value } })
              }}
              className="border border-[var(--color-border)] text-xs px-2 py-1 bg-white"
            >
              <option value="">选择变更…</option>
              {(changes ?? []).filter((c) => c.workspace === todo.workspace).map((c) => (
                <option key={c.name} value={c.name}>{c.name}</option>
              ))}
            </select>
          ) : (
            <span className="text-xs text-[var(--color-text-tertiary)]">无</span>
          )}
        </div>

        {/* Wiki refs */}
        <div>
          <label className="text-[10px] font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">
            关联文档 ({todo.wikiRefs.length})
          </label>
          {todo.wikiRefs.length > 0 ? (
            <div className="flex flex-col gap-1">
              {todo.wikiRefs.map((ref) => {
                const comp = wikiComponents.find((c) => c.id === ref.componentId)
                return (
                  <div key={ref.componentId} className="flex items-center gap-2">
                    <button
                      onClick={() => onNavigateWiki(comp?.path ?? ref.componentId)}
                      className="text-xs text-[var(--color-accent)] hover:underline truncate"
                    >
                      {comp?.title ?? ref.titleSnapshot}
                    </button>
                    {writable && (
                      <button
                        data-testid={`todo-remove-wikiref-${ref.componentId}`}
                        onClick={() =>
                          updateField({ wikiRefs: todo.wikiRefs.filter((r) => r.componentId !== ref.componentId) })
                        }
                        className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)] shrink-0"
                      >
                        ✕
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          ) : (
            <span className="text-xs text-[var(--color-text-tertiary)]">无</span>
          )}
          {writable && (
            <select
              data-testid="todo-detail-wiki-select"
              value=""
              onChange={(e) => {
                const comp = wikiComponents.find((c) => c.id === e.target.value)
                if (comp) {
                  const newRef: TodoWikiRef = { componentId: comp.id, workspace: comp.workspace, titleSnapshot: comp.title }
                  updateField({ wikiRefs: [...todo.wikiRefs, newRef] })
                }
              }}
              className="border border-[var(--color-border)] text-xs px-2 py-1 bg-white mt-2 w-full"
            >
              <option value="">添加文档…</option>
              {wikiOptions.recent.length > 0 && (
                <optgroup label="最近更新">
                  {wikiOptions.recent.map((component) => (
                    <option key={component.id} value={component.id}>{formatWikiOptionLabel(component)}</option>
                  ))}
                </optgroup>
              )}
              {wikiOptions.other.length > 0 && (
                <optgroup label="其他文档">
                  {wikiOptions.other.map((component) => (
                    <option key={component.id} value={component.id}>{formatWikiOptionLabel(component)}</option>
                  ))}
                </optgroup>
              )}
            </select>
          )}
        </div>

        {/* Delete */}
        {writable && (
          <div className="pt-2 border-t border-[var(--color-border)]">
            <button
              data-testid="todo-delete-btn"
              onClick={handleDelete}
              className="text-xs text-[var(--color-danger)] hover:underline"
            >
              删除此待办
            </button>
          </div>
        )}

        {/* Timestamps */}
        <div className="text-[10px] text-[var(--color-text-tertiary)] space-y-0.5">
          <div>创建: {parseSafeDate(todo.createdAt)?.toLocaleString() ?? todo.createdAt}</div>
          <div>更新: {parseSafeDate(todo.updatedAt)?.toLocaleString() ?? todo.updatedAt}</div>
          {todo.completedAt && <div>完成: {parseSafeDate(todo.completedAt)?.toLocaleString() ?? todo.completedAt}</div>}
        </div>

        {saving && <div className="text-xs text-[var(--color-text-secondary)] animate-pulse">保存中…</div>}
      </div>
    </div>
  )

  if (overlay) {
    return (
      <>
        <div className="fixed inset-0 z-20 bg-black/30" onClick={onClose} />
        {panel}
      </>
    )
  }

  return panel
}
