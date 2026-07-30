import { useState, useMemo, useEffect, useCallback, useRef } from 'react'
import type { Todo, TodoCounts, TodoStatus, TodoPriority, TodoChangeRef, TodoWikiRef, CreateTodoInput, UpdateTodoInput, ChangeSummary } from '../api/types'
import type { WorkspaceConfig, WikiComponent } from '../api/types'
import { Modal } from './Modal'
import { Icon, type IconName } from './icons'
import { SearchableCombobox, type SearchableComboboxOption } from './SearchableCombobox'

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
const TODO_ROW_HEIGHT = 56
const TODO_WINDOW_SIZE = 60


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
  blocked: '已阻塞',
  dropped: '已放弃',
}

// ── Grouping ─────────────────────────────────────────────────────────────────

interface GroupedTodos {
  overdue: Todo[]
  today: Todo[]
  tomorrow: Todo[]
  later: Todo[]
  undated: Todo[]
  blocked: Todo[]
  dropped: Todo[]
  done: Todo[]
}

function groupTodos(todos: Todo[]): GroupedTodos {
  const today = todayKey()
  const tomorrow = tomorrowKey()
  const groups: GroupedTodos = { overdue: [], today: [], tomorrow: [], later: [], undated: [], blocked: [], dropped: [], done: [] }

  for (const todo of todos) {
    if (todo.status === 'done' || todo.status === 'dropped' || todo.status === 'blocked') {
      groups[todo.status].push(todo)
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

const GROUP_SPECS: { key: keyof GroupedTodos; label: string; icon: IconName }[] = [
  { key: 'blocked', label: '已阻塞', icon: 'warning' },
  { key: 'overdue', label: '逾期', icon: 'warning' },
  { key: 'today', label: '今天', icon: 'calendar' },
  { key: 'tomorrow', label: '明天', icon: 'calendar' },
  { key: 'later', label: '稍后', icon: 'calendar' },
  { key: 'undated', label: '无日期', icon: 'calendar' },
  { key: 'done', label: '已完成', icon: 'check' },
  { key: 'dropped', label: '已放弃', icon: 'close' },
]

// ── Status filter type ───────────────────────────────────────────────────────

type StatusFilter = 'all' | 'today' | 'upcoming' | 'undated' | TodoStatus

const STATUS_FILTERS: { key: StatusFilter; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'today', label: '今天' },
  { key: 'upcoming', label: '即将到来' },
  { key: 'undated', label: '无日期' },
  { key: 'open', label: STATUS_LABELS.open },
  { key: 'in_progress', label: STATUS_LABELS.in_progress },
  { key: 'blocked', label: STATUS_LABELS.blocked },
  { key: 'done', label: STATUS_LABELS.done },
  { key: 'dropped', label: STATUS_LABELS.dropped },
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
  const [confirmClearContext, setConfirmClearContext] = useState(false)
  const [windowStart, setWindowStart] = useState(0)
  const listRef = useRef<HTMLDivElement | null>(null)
  const rowRefs = useRef(new Map<string, HTMLDivElement>())
  const pendingFocusId = useRef<string | null>(null)
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
        items = items.filter((t) => t.status !== 'done' && t.status !== 'dropped' && localDateKey(t.dueAt) === today)
        break
      case 'upcoming':
        items = items.filter((t) => t.status !== 'done' && t.status !== 'dropped' && localDateKey(t.dueAt) && localDateKey(t.dueAt)! >= tomorrow)
        break
      case 'undated':
        items = items.filter((t) => t.status !== 'done' && t.status !== 'dropped' && !parseSafeDate(t.dueAt))
        break
      case 'open':
      case 'in_progress':
      case 'done':
      case 'blocked':
      case 'dropped':
        items = items.filter((t) => t.status === statusFilter)
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
    setConfirmClearContext(false)
  }, [])

  const flattenedTodos = useMemo(
    () => GROUP_SPECS.flatMap((spec) => groups[spec.key].map((todo) => ({ todo, group: spec.key }))),
    [groups],
  )
  const visibleTodos = flattenedTodos.slice(windowStart, windowStart + TODO_WINDOW_SIZE)
  const selectedIsVisible = selectedId !== null && visibleTodos.some(({ todo }) => todo.id === selectedId)
  const tabbableId = selectedIsVisible ? selectedId : (visibleTodos[0]?.todo.id ?? null)

  const moveRowFocus = useCallback((currentId: string, key: 'ArrowUp' | 'ArrowDown' | 'Home' | 'End') => {
    const currentIndex = flattenedTodos.findIndex(({ todo }) => todo.id === currentId)
    if (currentIndex === -1) return

    let targetIndex = currentIndex
    if (key === 'ArrowUp') targetIndex = Math.max(0, currentIndex - 1)
    if (key === 'ArrowDown') targetIndex = Math.min(flattenedTodos.length - 1, currentIndex + 1)
    if (key === 'Home') targetIndex = 0
    if (key === 'End') targetIndex = flattenedTodos.length - 1

    const targetId = flattenedTodos[targetIndex]?.todo.id
    if (!targetId) return
    if (targetIndex === currentIndex) {
      rowRefs.current.get(targetId)?.focus()
      return
    }
    pendingFocusId.current = targetId
    setSelectedId(targetId)
    setWindowStart((start) => {
      if (targetIndex < start) return targetIndex
      if (targetIndex >= start + TODO_WINDOW_SIZE) return targetIndex - TODO_WINDOW_SIZE + 1
      return start
    })
  }, [flattenedTodos])

  useEffect(() => {
    setWindowStart(0)
    if (listRef.current) listRef.current.scrollTop = 0
  }, [statusFilter, workspaceFilter, searchQuery])

  useEffect(() => {
    if (windowStart >= flattenedTodos.length) {
      setWindowStart(Math.max(0, flattenedTodos.length - TODO_WINDOW_SIZE))
    }
  }, [flattenedTodos.length, windowStart])

  useEffect(() => {
    const targetId = pendingFocusId.current
    if (!targetId) return
    const target = rowRefs.current.get(targetId)
    if (!target) return
    pendingFocusId.current = null
    target.focus()
  }, [selectedId, windowStart, visibleTodos])

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
  const pendingCount = counts?.open ?? 0
  const inProgressCount = counts?.inProgress ?? 0
  const blockedCount = counts?.blocked ?? 0
  const droppedCount = counts?.dropped ?? 0
  const doneCount = counts?.done ?? 0

  return (
    <div className="flex flex-col h-full bg-[var(--color-surface)]">
      {/* Read-only banner */}
      {writable === false && (
        <div data-testid="todo-readonly-banner" className="flex items-center justify-center gap-1 text-xs bg-[var(--color-warn-subtle)] text-[var(--color-warn-text)] p-2 text-center shrink-0">
          <Icon name="warning" /> 局域网访问 — 只读模式（无法创建、编辑或删除待办）
        </div>
      )}
      {/* Mutation error banner — inline, non-fatal */}
      {error && todos.length > 0 && (
        <div data-testid="todo-mutation-error" className="text-xs bg-[var(--color-danger-subtle)] text-[var(--color-danger)] p-2 text-center shrink-0">
          {error}
        </div>
      )}

      {/* Header — compact: title + counts + daily completion strip */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-[var(--color-border)] shrink-0">
        <div className="flex items-center gap-3">
          <h2 className="text-sm font-semibold text-[var(--color-text-primary)]">待办</h2>
          <span className="text-xs text-[var(--color-text-secondary)]">
            {pendingCount} 个待处理 · {inProgressCount} 个进行中 · {blockedCount} 个阻塞 · {doneCount} 个完成 · {droppedCount} 个放弃
          </span>
        </div>
        {todayStats.total > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-[var(--color-text-secondary)] tabular-nums">
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
              className="border border-[var(--color-border)] px-2 py-1 text-sm focus:outline-none focus:border-[var(--color-accent)] bg-[var(--color-surface)] min-w-0 max-w-[8rem]"
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
              className="bg-[var(--color-accent)] text-[var(--color-text-on-color)] px-3 text-sm disabled:opacity-40"
            >
              添加
            </button>
          </div>
          {(qcChange || qcWikiRefs.length > 0) && (
            <div className="flex items-center gap-2 mt-1.5 text-xs text-[var(--color-text-secondary)]">
              <span>关联:</span>
              {qcChange && (
                <span data-testid="todo-qc-change" className="px-1 py-0.5 bg-[var(--color-accent)]/10 text-[var(--color-accent)]">
                  {qcChange.workspace}/{qcChange.name}
                </span>
              )}
              {qcWikiRefs.map((ref) => (
                <span key={ref.componentId} data-testid={`todo-qc-wikiref-${ref.componentId}`} className="px-1 py-0.5 bg-[var(--color-accent)]/10 text-[var(--color-accent)]">
                  {ref.titleSnapshot}
                </span>
              ))}
              <button onClick={() => setConfirmClearContext(true)} className="flex items-center gap-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]">
                <Icon name="trash" /> 清除
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
                <div className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase mb-1.5">视图</div>
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
                  <div className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase mb-1.5">工作区</div>
                  <select
                    data-testid="todo-workspace-filter"
                    value={workspaceFilter ?? ''}
                    onChange={(e) => setWorkspaceFilter(e.target.value || null)}
                    className="w-full border border-[var(--color-border)] text-xs px-2 py-1 bg-[var(--color-surface)]"
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
                <div className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase mb-1.5">搜索</div>
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
            className="shrink-0 flex items-center gap-1 text-xs px-2 py-1 border-b border-r border-[var(--color-border)] hover:bg-[var(--palette-highlight)]"
          >
            <Icon name={filterOpen ? 'chevron-left' : 'chevron-right'} /> 筛选
          </button>
        )}

        {/* Middle: todo list */}
        <div
          ref={listRef}
          data-testid="todo-list-scroll"
          className="flex-1 min-w-0 overflow-y-auto"
          onScroll={(event) => {
            if (flattenedTodos.length <= TODO_WINDOW_SIZE) return
            const next = Math.max(0, Math.min(
              flattenedTodos.length - TODO_WINDOW_SIZE,
              Math.floor(event.currentTarget.scrollTop / TODO_ROW_HEIGHT) - 10,
            ))
            setWindowStart(next)
          }}
        >
          {isEmpty ? (
            <div data-testid="todo-empty-state" className="flex flex-col items-center justify-center gap-2 text-center py-24 px-6">
              <Icon name="check" size={32} className="text-[var(--color-text-tertiary)]" />
              <p className="text-sm font-medium text-[var(--color-text-primary)]">暂无待办</p>
              <p className="text-xs text-[var(--color-text-secondary)]">使用上方输入框快速添加，或从变更/文档页面创建</p>
            </div>
          ) : (
            <div className="divide-y divide-[var(--color-border-subtle)]">
              {windowStart > 0 && (
                <div
                  aria-hidden="true"
                  data-testid="todo-list-top-spacer"
                  style={{ height: windowStart * TODO_ROW_HEIGHT }}
                />
              )}
              {GROUP_SPECS.map((g) => {
                const visible = visibleTodos.filter((entry) => entry.group === g.key).map((entry) => entry.todo)
                if (visible.length === 0) return null
                return (
                  <div key={g.key} data-testid={`todo-group-${g.key}`}>
                    <div className="sticky top-0 flex items-center gap-1 bg-[var(--color-bg)] px-4 py-1.5 text-xs font-semibold text-[var(--color-text-secondary)] z-10 border-b border-[var(--color-border-subtle)]">
                      <Icon name={g.icon} size={12} /> {g.label} <span className="font-normal">({groups[g.key].length})</span>
                    </div>
                    {visible.map((todo) => (
                      <TodoRow
                        key={todo.id}
                        todo={todo}
                        selected={todo.id === selectedId}
                        tabbable={todo.id === tabbableId}
                        rowRef={(element) => {
                          if (element) rowRefs.current.set(todo.id, element)
                          else rowRefs.current.delete(todo.id)
                        }}
                        onSelect={() => setSelectedId(todo.id === selectedId ? null : todo.id)}
                        onMoveFocus={(key) => moveRowFocus(todo.id, key)}
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
              {windowStart + visibleTodos.length < flattenedTodos.length && (
                <div
                  aria-hidden="true"
                  data-testid="todo-list-bottom-spacer"
                  style={{ height: (flattenedTodos.length - windowStart - visibleTodos.length) * TODO_ROW_HEIGHT }}
                />
              )}
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
      {confirmClearContext && (
        <Modal title="清除关联信息？" onClose={() => setConfirmClearContext(false)} data-testid="todo-clear-context-confirm">
          <div className="space-y-4 p-4 text-sm text-[var(--color-text-secondary)]">
            <p>当前待办草稿中的变更和文档关联将被清除。</p>
            <div className="flex justify-end gap-2">
              <button className="border border-[var(--color-border)] px-3 py-1.5" onClick={() => setConfirmClearContext(false)}>取消</button>
              <button className="bg-[var(--color-danger)] px-3 py-1.5 text-[var(--color-text-on-color)]" onClick={clearQcContext}>确认清除</button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}

// ── TodoRow ──────────────────────────────────────────────────────────────────

function TodoRow({
  todo,
  selected,
  tabbable,
  rowRef,
  onSelect,
  onMoveFocus,
  onToggleDone,
  onNavigateWiki,
  onNavigateChange,
  wikiComponents,
  writable,
}: {
  todo: Todo
  selected: boolean
  tabbable: boolean
  rowRef: (element: HTMLDivElement | null) => void
  onSelect: () => void
  onMoveFocus: (key: 'ArrowUp' | 'ArrowDown' | 'Home' | 'End') => void
  onToggleDone: () => void
  onNavigateWiki: (path: string) => void
  onNavigateChange: (workspace: string, changeName: string) => void
  wikiComponents: WikiComponent[]
  writable: boolean
}) {
  const isDone = todo.status === 'done'

  return (
    <div
      ref={rowRef}
      data-testid={`todo-row-${todo.id}`}
      onClick={onSelect}
      role="button"
      aria-label={`查看待办：${todo.title || '无标题'}`}
      tabIndex={tabbable ? 0 : -1}
      onKeyDown={(event) => {
        if (event.target !== event.currentTarget) return
        if (event.key === 'ArrowUp' || event.key === 'ArrowDown' || event.key === 'Home' || event.key === 'End') {
          event.preventDefault()
          onMoveFocus(event.key)
          return
        }
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onSelect()
        }
      }}
      className={`flex shrink-0 items-start gap-2 overflow-hidden px-4 py-2 cursor-pointer border-l-2 transition-colors ${
        selected
          ? 'border-l-[var(--color-accent)] bg-[var(--color-accent)]/5'
          : 'border-l-transparent hover:bg-[var(--palette-highlight)]'
      }`}
      style={{ height: TODO_ROW_HEIGHT }}
    >
      {/* Priority dot */}
      <span
        className="shrink-0 w-1.5 h-1.5 mt-1.5"
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
          className={`shrink-0 w-4 h-4 border mt-0.5 flex items-center justify-center text-xs ${
            isDone
              ? 'bg-[var(--color-success)] border-[var(--color-success)] text-[var(--color-text-on-color)]'
              : 'border-[var(--color-border)] hover:border-[var(--color-accent)]'
          }`}
          aria-label={isDone ? '标记为未完成' : '标记为已完成'}
        >
          {isDone && <Icon name="check" />}
        </button>
      )}

      {/* Content */}
      <div className="min-w-0 flex-1">
        <div className={`truncate text-sm ${isDone ? 'line-through text-[var(--color-text-tertiary)]' : 'text-[var(--color-text-primary)]'}`}>
          {todo.title || '(无标题)'}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 overflow-hidden whitespace-nowrap">
          {/* Due badge */}
          {todo.dueAt && (
            <span
              className={`text-xs px-1 py-0 ${
                isDone
                  ? 'text-[var(--color-text-tertiary)]'
                  : formatDueBadge(todo.dueAt) === '逾期'
                    ? 'text-[var(--color-danger)] bg-[var(--color-danger-subtle)]'
                    : 'text-[var(--color-text-secondary)]'
              }`}
            >
              {formatDueBadge(todo.dueAt)}
            </span>
          )}

          {/* Workspace tag */}
          {todo.workspace && (
            <span className="text-xs text-[var(--color-text-tertiary)] bg-[var(--color-bg)] px-1">
              {todo.workspace}
            </span>
          )}

          {todo.metadata.source === 'omp' && todo.externalRef && (
            <span
              data-testid={`todo-omp-origin-${todo.id}`}
              className="text-xs text-[var(--color-accent)] bg-[var(--color-accent)]/10 px-1"
              title={todo.externalRef.blocker || `OMP ${todo.externalRef.sessionId}/${todo.externalRef.taskKey}`}
            >
              OMP · {todo.externalRef.phase}
              {todo.externalRef.blocker ? ` · ${todo.externalRef.blocker}` : ''}
            </span>
          )}

          {/* Status tag */}
          {!isDone && (
            <span className={`text-xs px-1 py-0 ${
              todo.status === 'in_progress'
                ? 'bg-[var(--color-warn-subtle)] text-[var(--color-warn-text)]'
                : todo.status === 'blocked'
                  ? 'bg-[var(--color-danger-subtle)] text-[var(--color-danger)]'
                  : todo.status === 'dropped'
                    ? 'bg-[var(--color-bg)] text-[var(--color-text-tertiary)]'
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
              className="text-xs text-[var(--color-accent)] hover:underline"
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
                className="text-xs text-[var(--color-accent)] hover:underline"
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
  const [pendingAction, setPendingAction] = useState<
    | { kind: 'delete' }
    | { kind: 'dueDate' }
    | { kind: 'change' }
    | { kind: 'wikiRef'; componentId: string }
    | null
  >(null)
  const [confirming, setConfirming] = useState(false)
  const confirmingRef = useRef(false)

  // Keep editable fields in sync with refetches, but only dismiss a pending
  // destructive confirmation when the selected todo itself changes.
  useEffect(() => {
    setTitle(todo.title)
    setNotes(todo.notes)
  }, [todo.title, todo.notes])

  useEffect(() => {
    setPendingAction(null)
  }, [todo.id])

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

  const availableWikiComponents = useMemo(() => {
    const linkedIds = new Set(todo.wikiRefs.map((ref) => ref.componentId))
    const seen = new Set<string>()
    return wikiComponents
      .filter((component) => {
        if (linkedIds.has(component.id) || seen.has(component.id)) return false
        seen.add(component.id)
        return true
      })
      .sort((left, right) => {
        const workspaceOrder = Number(right.workspace === todo.workspace) - Number(left.workspace === todo.workspace)
        if (workspaceOrder !== 0) return workspaceOrder
        return (wikiUpdatedTimestamp(right) ?? -Infinity) - (wikiUpdatedTimestamp(left) ?? -Infinity)
          || left.title.localeCompare(right.title, 'zh-CN')
      })
  }, [todo.wikiRefs, todo.workspace, wikiComponents])

  const wikiComboboxOptions = useMemo<SearchableComboboxOption[]>(
    () => availableWikiComponents.map((component) => ({
      value: component.id,
      label: component.title,
      description: `${component.workspace} · ${formatWikiOptionLabel(component)}`,
      group: component.workspace === todo.workspace ? `当前工作区 · ${todo.workspace}` : '其他工作区',
      keywords: `${component.type} ${component.workspace} ${component.path}`,
    })),
    [availableWikiComponents, todo.workspace],
  )
  const confirmAction = useCallback(async () => {
    if (!pendingAction || confirmingRef.current || !writable) return
    const action = pendingAction
    confirmingRef.current = true
    setConfirming(true)
    try {
      if (action.kind === 'delete') {
        await onDelete(todo.id)
        setPendingAction(null)
        onClose()
        return
      }
      if (action.kind === 'dueDate') {
        await onUpdate(todo.id, { dueAt: null })
      } else if (action.kind === 'change') {
        await onUpdate(todo.id, { change: null })
      } else {
        await onUpdate(todo.id, {
          wikiRefs: todo.wikiRefs.filter((ref) => ref.componentId !== action.componentId),
        })
      }
      setPendingAction(null)
    } catch {
      // Mutation errors are surfaced by useTodos; keep the modal open for retry.
    } finally {
      confirmingRef.current = false
      setConfirming(false)
    }
  }, [onClose, onDelete, onUpdate, pendingAction, todo.id, todo.wikiRefs, writable])

  const panel = (
    <div
      data-testid="todo-detail"
      className={`flex flex-col h-full bg-[var(--color-surface)] ${overlay ? 'fixed inset-0 z-30' : 'w-[320px] shrink-0 border-l border-[var(--color-border)]'}`}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-[var(--color-border)] shrink-0">
        <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">待办详情</h3>
        <button onClick={onClose} aria-label="关闭详情" className="text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]">
          <Icon name="close" />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {/* Title */}
        <div>
          <label className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">标题</label>
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
          <label className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">备注</label>
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

        {todo.metadata.source === 'omp' && todo.externalRef && (
          <div data-testid="todo-detail-omp-origin" className="border border-[var(--color-border)] bg-[var(--color-bg)] p-2 text-xs text-[var(--color-text-secondary)]">
            <div className="font-semibold text-[var(--color-accent)]">OMP 投影 · {todo.externalRef.phase}</div>
            <div className="mt-1 break-all">{todo.externalRef.sessionId} / {todo.externalRef.taskKey}</div>
            {todo.externalRef.blocker && (
              <div className="mt-1 text-[var(--color-danger)]">{todo.externalRef.blocker}</div>
            )}
          </div>
        )}

        {/* Status */}
        <div>
          <label className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">状态</label>
          <div className="flex gap-1.5">
            {(['open', 'in_progress', 'blocked', 'done', 'dropped'] as TodoStatus[]).map((s) => (
              <button
                key={s}
                data-testid={`todo-status-${s}`}
                onClick={() => updateField({ status: s })}
                disabled={!writable}
                className={`text-xs px-2.5 py-1 border ${
                  todo.status === s
                    ? 'bg-[var(--color-accent)] text-[var(--color-text-on-color)] border-[var(--color-accent)]'
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
          <label className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">优先级</label>
          <div className="flex gap-1.5">
            {(['urgent', 'high', 'normal', 'low'] as TodoPriority[]).map((p) => (
              <button
                key={p}
                data-testid={`todo-priority-${p}`}
                onClick={() => updateField({ priority: p })}
                disabled={!writable}
                className={`text-xs px-2.5 py-1 border ${
                  todo.priority === p
                    ? 'text-[var(--color-text-on-color)] border-transparent'
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
          <label className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">截止日期</label>
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
                onClick={() => setPendingAction({ kind: 'dueDate' })}
                className="flex items-center gap-1 text-xs text-[var(--color-text-secondary)] hover:text-[var(--color-danger)]"
              >
                <Icon name="trash" /> 清除
              </button>
            )}
          </div>
        </div>
        {/* Change association */}
        <div>
          <label className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">关联变更</label>
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
                    onClick={() => setPendingAction({ kind: 'change' })}
                    aria-label="清除关联变更"
                    className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)]"
                  >
                    <Icon name="close" />
                  </button>
                  <select
                    data-testid="todo-detail-change-select"
                    value=""
                    onChange={(e) => {
                      if (e.target.value) updateField({ change: { workspace: todo.workspace, name: e.target.value } })
                    }}
                    className="border border-[var(--color-border)] text-xs px-1 py-0.5 bg-[var(--color-surface)]"
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
              className="border border-[var(--color-border)] text-xs px-2 py-1 bg-[var(--color-surface)]"
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
          <label className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase block mb-1">
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
                        onClick={() => setPendingAction({ kind: 'wikiRef', componentId: ref.componentId })}
                        aria-label={`移除文档 ${comp?.title ?? ref.titleSnapshot}`}
                        className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-danger)] shrink-0"
                      >
                        <Icon name="close" />
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
            <div className="mt-2">
              <SearchableCombobox
                data-testid="todo-detail-wiki-combobox"
                options={wikiComboboxOptions}
                value=""
                onChange={(componentId) => {
                  const component = availableWikiComponents.find((candidate) => candidate.id === componentId)
                  if (!component) return
                  const newRef: TodoWikiRef = {
                    componentId: component.id,
                    workspace: component.workspace,
                    titleSnapshot: component.title,
                  }
                  updateField({ wikiRefs: [...todo.wikiRefs, newRef] })
                }}
                placeholder="搜索并添加文档…"
                ariaLabel="搜索并添加关联文档"
                emptyText="没有匹配文档"
                maxResults={RECENT_WIKI_OPTION_LIMIT}
              />
            </div>
          )}
        </div>

        {/* Delete */}
        {writable && (
          <div className="pt-2 border-t border-[var(--color-border)]">
            <button
              data-testid="todo-delete-btn"
              onClick={() => setPendingAction({ kind: 'delete' })}
              className="flex items-center gap-1 text-xs text-[var(--color-danger)] hover:underline"
            >
              <Icon name="trash" /> 删除此待办
            </button>
          </div>
        )}

        {/* Timestamps */}
        <div className="text-xs text-[var(--color-text-tertiary)] space-y-0.5">
          <div>创建: {parseSafeDate(todo.createdAt)?.toLocaleString() ?? todo.createdAt}</div>
          <div>更新: {parseSafeDate(todo.updatedAt)?.toLocaleString() ?? todo.updatedAt}</div>
          {todo.completedAt && <div>完成: {parseSafeDate(todo.completedAt)?.toLocaleString() ?? todo.completedAt}</div>}
        </div>

        {saving && <div className="text-xs text-[var(--color-text-secondary)] animate-pulse">保存中…</div>}
      </div>
    </div>
  )

  const confirmation = pendingAction && (
    <Modal
      title={pendingAction.kind === 'delete' ? '删除此待办？' : '确认清除？'}
      onClose={() => {
        if (!confirming) setPendingAction(null)
      }}
      dismissible={!confirming}
      data-testid="todo-destructive-confirm"
    >
      <div className="space-y-4 p-4">
        <p className="text-sm text-[var(--color-text-secondary)]">
          {pendingAction.kind === 'delete'
            ? '此操作将永久删除该待办，且无法撤销。'
            : '此操作将移除当前关联信息。'}
        </p>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            disabled={confirming}
            onClick={() => setPendingAction(null)}
            className="border border-[var(--color-border)] px-3 py-1.5 text-xs disabled:opacity-50"
          >
            取消
          </button>
          <button
            type="button"
            data-testid="todo-destructive-confirm-submit"
            disabled={confirming}
            onClick={confirmAction}
            className="bg-[var(--color-danger)] px-3 py-1.5 text-xs text-[var(--color-text-on-color)] disabled:opacity-50"
          >
            {confirming ? '处理中…' : '确认'}
          </button>
        </div>
      </div>
    </Modal>
  )

  if (overlay) {
    return (
      <>
        <div className="fixed inset-0 z-20 bg-[var(--palette-bg)]" onClick={onClose} />
        {panel}
        {confirmation}
      </>
    )
  }

  return <>{panel}{confirmation}</>
}
