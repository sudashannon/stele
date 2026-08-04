import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  addBookmark,
  addWorkspace,
  fetchBookmarks,
  fetchChangesWithMeta,
  fetchWikiIndex,
  fetchSessions,
  fetchWorkspaces,
  removeBookmark,
  removeWorkspace,
} from './api/client'
import type { Bookmark, ChangeSummary, SessionTodo, WorkspaceConfig, WikiComponent, WikiSession } from './api/types'
import { ChangeDetail } from './components/ChangeDetail'
import { ChangeExplorer } from './components/ChangeExplorer'
import { ChatBubble } from './components/ChatBubble'
import { CommandPalette } from './components/CommandPalette'
import { Icon } from './components/icons'
import { KpiCards, classifyChanges } from './components/KpiCards'
import { BookmarkPanel } from './components/BookmarkPanel'
import { Modal } from './components/Modal'
import { SettingsPanel } from './components/SettingsPanel'
import { StateBlock } from './components/StateBlock'
import { SideRail, SIDE_RAIL_ITEMS, type SideRailView } from './components/SideRail'
import { StaleBundleNotice } from './components/StaleBundleNotice'
import { WorkspaceChips } from './components/WorkspaceChips'
import { useAppZoom } from './hooks/useAppZoom'
import { useCommandPalette } from './hooks/useCommandPalette'
import type { CommandAction } from './hooks/useCommandPalette'
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts'
import { useTodos } from './hooks/useTodos'
import { useWikiEvents } from './hooks/useWikiEvents'

const LazyCalendarPanel = lazy(() => import('./components/CalendarPanel').then(({ CalendarPanel }) => ({ default: CalendarPanel })))
const LazyLintPanel = lazy(() => import('./components/LintPanel').then(({ LintPanel }) => ({ default: LintPanel })))
const LazyMarkdownViewer = lazy(() => import('./components/MarkdownViewer').then(({ MarkdownViewer }) => ({ default: MarkdownViewer })))
const LazyRecentPanel = lazy(() => import('./components/RecentPanel').then(({ RecentPanel }) => ({ default: RecentPanel })))
const LazyReportView = lazy(() => import('./components/ReportView').then(({ ReportView }) => ({ default: ReportView })))
const LazySessionDetail = lazy(() => import('./components/SessionDetail').then(({ SessionDetail }) => ({ default: SessionDetail })))
const LazySessionsPanel = lazy(() => import('./components/SessionsPanel').then(({ SessionsPanel }) => ({ default: SessionsPanel })))
const LazySemanticSearch = lazy(() => import('./components/SemanticSearch').then(({ SemanticSearch }) => ({ default: SemanticSearch })))
const LazyShareList = lazy(() => import('./components/ShareList').then(({ ShareList }) => ({ default: ShareList })))
const LazyTodoPanel = lazy(() => import('./components/TodoPanel').then(({ TodoPanel }) => ({ default: TodoPanel })))
const LazyWikiGraph = lazy(() => import('./components/WikiGraph').then(({ WikiGraph }) => ({ default: WikiGraph })))
const LazyWikiTimeline = lazy(() => import('./components/WikiTimeline').then(({ WikiTimeline }) => ({ default: WikiTimeline })))

const STUCK_THRESHOLD_DAYS = 14
const SHORTCUT_ITEMS = SIDE_RAIL_ITEMS.filter((item) => item.shortcutKey !== undefined)
const VIEW_LABELS = Object.fromEntries(SIDE_RAIL_ITEMS.map((item) => [item.key, item.label])) as Record<SideRailView, string>

interface ChangeSelection {
  name: string
  workspace?: string
}

interface TodoContext {
  wikiComponent: WikiComponent | null
  changeName: string | null
  changeWorkspace: string | null
}

interface WorkspaceRefreshOptions {
  preferredActiveWorkspace?: string | null
  clearSelected?: boolean
}

function reconcileSelectedChange(
  selection: ChangeSelection | null,
  nextChanges: ChangeSummary[],
): ChangeSelection | null {
  if (!selection) return null

  const exactMatch = nextChanges.find(
    (change) => change.name === selection.name && change.workspace === selection.workspace,
  )
  if (exactMatch) {
    return { name: exactMatch.name, workspace: exactMatch.workspace }
  }

  const fallbackMatches = nextChanges.filter((change) => change.name === selection.name)
  if (fallbackMatches.length === 1) {
    return {
      name: fallbackMatches[0].name,
      workspace: fallbackMatches[0].workspace,
    }
  }

  return null
}

function ViewFallback({ label }: { label: string }) {
  return (
    <div
      data-testid="lazy-view-fallback"
      className="m-4 flex min-h-[10rem] items-center gap-3 border border-[var(--color-border)] bg-[var(--color-surface)] px-[var(--spacing-05)] py-[var(--spacing-05)] text-[length:var(--type-body)] text-[var(--color-text-secondary)] shadow-[var(--shadow-1)]"
    >
      <Icon name="spinner" size={16} className="animate-spin text-[var(--color-accent)]" />
      <div className="space-y-1">
        <p className="text-[length:var(--type-caption)] font-medium text-[var(--color-text-primary)]">
          正在加载{label}
        </p>
        <p className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          保持当前上下文，视图资源仅在进入后按需加载。
        </p>
      </div>
    </div>
  )
}

export default function App() {
  const [changes, setChanges] = useState<ChangeSummary[]>([])
  const [selected, setSelected] = useState<ChangeSelection | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [workspaces, setWorkspaces] = useState<WorkspaceConfig[]>([])
  const [activeWorkspace, setActiveWorkspace] = useState<string | null>(null)
  const [failedWorkspaces, setFailedWorkspaces] = useState<string[]>([])
  const [activeKpiFilter, setActiveKpiFilter] = useState<string | null>(null)
  const [view, setView] = useState<SideRailView>('changes')
  const [wikiComponents, setWikiComponents] = useState<WikiComponent[]>([])
  const [sessionPathById, setSessionPathById] = useState<Record<string, string>>({})
  const [viewerPath, setViewerPath] = useState<string | null>(null)
  // What the caller knows it opened. The index is the fallback, not the
  // authority: a session grafted after page load is absent from `wikiComponents`
  // until the next refresh, and inferring from that stale copy sent transcripts
  // to the Markdown viewer.
  const [viewerKind, setViewerKind] = useState<'session' | 'document' | null>(null)

  // The single entry point for the shared viewer. `kind` is what the caller
  // already knows about the target; omit it to let the index classify. Path and
  // kind always move together, so a stale kind cannot mis-route the next open.
  const openViewer = useCallback((path: string | null, kind: 'session' | 'document' | null = null) => {
    setViewerPath(path)
    setViewerKind(path ? kind : null)
  }, [])
  const [changeArtifacts, setChangeArtifacts] = useState<{ path: string; label: string }[]>([])
  const [bookmarks, setBookmarks] = useState<Bookmark[]>([])
  const [bookmarkPanelOpen, setBookmarkPanelOpen] = useState(false)
  const [wikiIndexing, setWikiIndexing] = useState(false)
  const [wikiIndexingChanged, setWikiIndexingChanged] = useState<number | null>(null)
  const [workspacePendingRemoval, setWorkspacePendingRemoval] = useState<string | null>(null)
  const [workspaceRemovalError, setWorkspaceRemovalError] = useState<string | null>(null)
  const [workspaceRemovalPending, setWorkspaceRemovalPending] = useState(false)

  const selectedRef = useRef<ChangeSelection | null>(selected)
  selectedRef.current = selected
  const activeWorkspaceRef = useRef<string | null>(activeWorkspace)
  activeWorkspaceRef.current = activeWorkspace

  const {
    todos,
    counts: todoCounts,
    writable: todoWritable,
    loading: todoLoading,
    error: todoError,
    createTodo,
    updateTodo,
    deleteTodo,
    refetch: refetchTodos,
  } = useTodos()

  type TodoDraft = {
    change?: { workspace: string; name: string }
    wikiRef?: { componentId: string; workspace: string; titleSnapshot: string }
  }
  const [todoDraft, setTodoDraft] = useState<TodoDraft | null>(null)
  const todoFocusCaptureRef = useRef<(() => void) | null>(null)

  const handleViewChange = useCallback((nextView: SideRailView) => {
    openViewer(null)
    setView(nextView)
  }, [openViewer])

  // A todo projected from a session carries that session's id, not its path.
  // Keeping the mapping here (one small request, refreshed with the session
  // layer) is what makes the Todo panel's origin chip navigable.
  const refreshSessionPaths = useCallback(() => {
    fetchSessions()
      .then((list) => setSessionPathById(Object.fromEntries(list.map((session) => [session.id, session.path]))))
      .catch(() => setSessionPathById({}))
  }, [])

  const refreshWikiIndex = useCallback(() => {
    fetchWikiIndex().then(setWikiComponents).catch(() => setWikiComponents([]))
  }, [])

  const refreshWorkspaceData = useCallback(
    async (options?: WorkspaceRefreshOptions) => {
      const [nextWorkspacesResult, nextChangesResult, nextWikiComponents] = await Promise.all([
        fetchWorkspaces().catch(() => [] as WorkspaceConfig[]),
        fetchChangesWithMeta().catch(() => ({ changes: [] as ChangeSummary[], failedWorkspaces: [] as string[] })),
        fetchWikiIndex().catch(() => [] as WikiComponent[]),
      ])

      const nextWorkspaces = nextWorkspacesResult ?? []
      const nextChanges = nextChangesResult.changes ?? []
      const nextFailedWorkspaces = nextChangesResult.failedWorkspaces ?? []
      const previousSelection = options?.clearSelected ? null : selectedRef.current
      const nextSelection = reconcileSelectedChange(previousSelection, nextChanges)
      const nextKnownAliases = new Set<string>([
        ...nextWorkspaces.map((workspace) => workspace.alias),
        ...nextFailedWorkspaces,
      ])

      let nextActiveWorkspace: string | null = nextSelection?.workspace ?? null
      if (
        options?.preferredActiveWorkspace &&
        nextKnownAliases.has(options.preferredActiveWorkspace)
      ) {
        nextActiveWorkspace = options.preferredActiveWorkspace
      } else if (
        !options?.clearSelected &&
        activeWorkspaceRef.current &&
        nextKnownAliases.has(activeWorkspaceRef.current)
      ) {
        nextActiveWorkspace = activeWorkspaceRef.current
      }

      setWorkspaces(nextWorkspaces)
      setChanges(nextChanges)
      setFailedWorkspaces(nextFailedWorkspaces)
      setWikiComponents(nextWikiComponents)
      setSelected(nextSelection)
      setActiveWorkspace(nextActiveWorkspace)
    },
    [],
  )

  const commandActions: CommandAction[] = useMemo(
    () => [
      ...SIDE_RAIL_ITEMS.map((item) => ({
        id: `nav-${item.key}`,
        label: VIEW_LABELS[item.key],
        category: 'Navigation',
        run: () => handleViewChange(item.key),
      })),
      {
        id: 'new-todo',
        label: '新建待办',
        category: 'Commands',
        run: () => {
          handleViewChange('todos')
          window.setTimeout(() => todoFocusCaptureRef.current?.(), 100)
        },
      },
      {
        id: 'bookmarks',
        label: '收藏夹',
        category: 'Navigation',
        run: () => setBookmarkPanelOpen((open) => !open),
      },
      {
        id: 'settings',
        label: '设置',
        category: 'Navigation',
        run: () => setSettingsOpen(true),
      },
      {
        id: 'refresh',
        label: '刷新数据',
        category: 'Commands',
        run: () => window.location.reload(),
      },
    ],
    [handleViewChange],
  )

  const palette = useCommandPalette(commandActions)
  const appZoom = useAppZoom()

  const shortcutDefs = useMemo(
    () => [
      { key: 'k', ctrlOrCmd: true, label: '命令面板', run: () => {} },
      ...SHORTCUT_ITEMS.map((item) => ({
        key: String(item.shortcutKey),
        ctrlOrCmd: true,
        label: item.label,
        run: () => {},
      })),
      { key: 'b', ctrlOrCmd: true, label: '收藏夹', run: () => {} },
      { key: 'Escape', ctrlOrCmd: false, label: '关闭面板', run: () => {} },
      { key: '+', ctrlOrCmd: true, shift: true, label: '放大', run: () => {} },
      { key: '-', ctrlOrCmd: true, label: '缩小', run: () => {} },
      { key: '0', ctrlOrCmd: true, label: '重置缩放', run: () => {} },
    ],
    [],
  )

  const registeredShortcuts = useMemo(
    () => [
      { key: 'k', ctrlOrCmd: true, label: '命令面板', run: () => palette.togglePalette() },
      ...SHORTCUT_ITEMS.map((item) => ({
        key: String(item.shortcutKey),
        ctrlOrCmd: true,
        label: item.label,
        run: () => handleViewChange(item.key),
      })),
      { key: 'b', ctrlOrCmd: true, label: '收藏夹', run: () => setBookmarkPanelOpen((open) => !open) },
      {
        key: 'Escape',
        ctrlOrCmd: false,
        label: '关闭面板',
        run: () => {
          palette.closePalette()
          openViewer(null)
          setBookmarkPanelOpen(false)
          setSettingsOpen(false)
        },
      },
      { key: '+', ctrlOrCmd: true, shift: true, label: '放大', run: appZoom.zoomIn },
      { key: '=', ctrlOrCmd: true, label: '', run: appZoom.zoomIn },
      { key: '-', ctrlOrCmd: true, label: '缩小', run: appZoom.zoomOut },
      { key: '0', ctrlOrCmd: true, label: '重置缩放', run: appZoom.zoomReset },
    ],
    [appZoom.zoomIn, appZoom.zoomOut, appZoom.zoomReset, handleViewChange, openViewer, palette],
  )

  useKeyboardShortcuts(registeredShortcuts)

  const navigateToChange = useCallback(
    (changeName: string) => {
      let workspace: string | undefined
      if (viewerPath) {
        workspace = wikiComponents.find((component) => component.path === viewerPath || component.id === viewerPath)?.workspace
      }
      if (!workspace) {
        const matches = changes.filter((change) => change.name === changeName && change.workspace)
        if (matches.length === 1) workspace = matches[0].workspace
        else workspace = activeWorkspace ?? undefined
      }
      setView('changes')
      setSelected({ name: changeName, workspace })
      setActiveWorkspace(workspace ?? null)
      openViewer(null)
    },
    [activeWorkspace, changes, openViewer, viewerPath, wikiComponents],
  )

  useEffect(() => {
    fetchWorkspaces().then((nextWorkspaces) => setWorkspaces(nextWorkspaces ?? [])).catch(() => setWorkspaces([]))
  }, [])

  useEffect(() => {
    fetchChangesWithMeta()
      .then((result) => {
        setChanges(result.changes ?? [])
        setFailedWorkspaces(result.failedWorkspaces ?? [])
      })
      .catch(() => setChanges([]))
  }, [])

  useEffect(() => {
    refreshWikiIndex()
    refreshSessionPaths()
  }, [refreshSessionPaths, refreshWikiIndex])

  useEffect(() => {
    fetchBookmarks().then(setBookmarks).catch(() => setBookmarks([]))
  }, [])

  const handleIndexingStarted = useCallback((changed: number | null) => {
    setWikiIndexingChanged(changed)
    setWikiIndexing(true)
  }, [])

  const handleGraphUpdated = useCallback(() => {
    setWikiIndexing(false)
    setWikiIndexingChanged(null)
    refreshWikiIndex()
  }, [refreshWikiIndex])

  const handleTodosUpdated = useCallback(() => {
    refetchTodos()
  }, [refetchTodos])

  // A session graft adds `session` components to the index, and the viewer
  // resolves what to render from that index - so the panel's live refresh is
  // not enough on its own: without this, a session indexed after page load
  // would open in the Markdown viewer as a raw transcript path.
  useWikiEvents({
    onIndexingStarted: handleIndexingStarted,
    onUpdate: handleGraphUpdated,
    onTodosUpdated: handleTodosUpdated,
    onSessionsUpdated: () => {
      refreshWikiIndex()
      refreshSessionPaths()
    },
  })

  useEffect(() => {
    if (!wikiIndexing) return
    const timer = window.setTimeout(() => {
      setWikiIndexing(false)
      setWikiIndexingChanged(null)
    }, 8000)
    return () => window.clearTimeout(timer)
  }, [wikiIndexing])

  const selectedChange = useMemo(
    () =>
      selected
        ? changes.find(
            (change) => change.name === selected.name && change.workspace === selected.workspace,
          ) ?? null
        : null,
    [changes, selected],
  )

  const isBookmarked = useCallback(
    (path: string) => bookmarks.some((bookmark) => bookmark.path === path),
    [bookmarks],
  )

  const handleToggleStar = useCallback(
    (path: string, title: string) => {
      if (isBookmarked(path)) {
        removeBookmark(path).then(setBookmarks).catch(() => {})
        return
      }

      const type = path.split('.').pop() || 'doc'
      addBookmark({ path, title, type }).then(setBookmarks).catch(() => {})
    },
    [isBookmarked],
  )

  const handleRemoveBookmark = useCallback((path: string) => {
    removeBookmark(path).then(setBookmarks).catch(() => {})
  }, [])

  const todoCountByChangeKey = useMemo(() => {
    const counts = new Map<string, number>()
    for (const todo of todos) {
      if (todo.status === 'done' || todo.status === 'dropped' || !todo.change) continue
      const key = `${todo.change.workspace}\x00${todo.change.name}`
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return counts
  }, [todos])

  const viewerComponent = useMemo(
    () => (viewerPath ? wikiComponents.find((component) => component.path === viewerPath || component.id === viewerPath) ?? null : null),
    [viewerPath, wikiComponents],
  )

  const viewerTodoContext = useMemo((): TodoContext => {
    if (!viewerPath) {
      return { wikiComponent: null, changeName: null, changeWorkspace: null }
    }

    const wikiComponent = viewerComponent
    let changeName: string | null = null
    let changeWorkspace: string | null = null

    if (wikiComponent) {
      const match = viewerPath.match(/\/changes\/([^/]+)\//)
      if (match) {
        changeName = match[1]
        changeWorkspace = wikiComponent.workspace ?? null
        const exists = changeWorkspace
          ? changes.some(
              (change) => change.name === changeName && change.workspace === changeWorkspace,
            )
          : false
        if (!exists) {
          changeName = null
          changeWorkspace = null
        }
      }
    }

    return { wikiComponent, changeName, changeWorkspace }
  }, [changes, viewerComponent, viewerPath])

  const createTodoFromViewer = useCallback(() => {
    const context = viewerTodoContext
    if (!context.wikiComponent) return
    setTodoDraft({
      wikiRef: {
        componentId: context.wikiComponent.id,
        workspace: context.wikiComponent.workspace ?? '',
        titleSnapshot: context.wikiComponent.title,
      },
      ...(context.changeName && context.changeWorkspace
        ? { change: { workspace: context.changeWorkspace, name: context.changeName } }
        : {}),
    })
    handleViewChange('todos')
  }, [handleViewChange, viewerTodoContext])

  const viewerTodoHandler = viewerTodoContext.wikiComponent ? createTodoFromViewer : undefined
  const viewerIsSession = viewerKind === 'session' || (viewerKind === null && viewerComponent?.type === 'session')

  // Unfinished tasks are the actionable residue of a session. They become
  // ordinary todos carrying a WikiRef back to the session component - the
  // existing mechanism for "this todo is about that document" - rather than an
  // OMP projection, whose per-session sync cursor is owned by the runtime
  // extension and would fight an import for the same session id.
  const importSessionTodos = useCallback(async (session: WikiSession, items: SessionTodo[]) => {
    const wikiRef = {
      componentId: session.path,
      workspace: session.workspace,
      titleSnapshot: session.title,
    }
    let imported = 0
    for (const item of items) {
      const notes = [item.phase && `阶段：${item.phase}`, item.blocker && `卡在：${item.blocker}`, `来自会话：${session.title}`]
        .filter(Boolean)
        .join('\n')
      await createTodo({
        workspace: session.workspace,
        title: item.content,
        notes,
        status: item.status === 'blocked' ? 'blocked' : 'open',
        wikiRefs: [wikiRef],
      })
      imported++
    }
    await refetchTodos()
    return imported
  }, [refetchTodos])

  const openWikiComponent = useCallback((idOrPath: string) => {
    const component = wikiComponents.find((item) => item.id === idOrPath || item.path === idOrPath)
    openViewer(component?.path ?? idOrPath)
  }, [openViewer, wikiComponents])

  const renderViewer = useCallback((props: {
    artifacts?: { path: string; label: string }[]
    workspace?: string
    onNavigateToChange?: (changeName: string) => void
    onCreateTodo?: () => void
    onSelectArtifact?: (path: string) => void
  } = {}) => {
    if (!viewerPath) return null
    if (viewerIsSession) {
      return (
        <LazySessionDetail
          sessionId={viewerComponent?.path ?? viewerPath}
          onOpenDocument={(path) => openViewer(path, 'document')}
          onClose={() => openViewer(null)}
          onImportTodos={importSessionTodos}
        />
      )
    }
    return (
      <LazyMarkdownViewer
        path={viewerPath}
        artifacts={props.artifacts}
        workspace={props.workspace}
        onSelectArtifact={props.onSelectArtifact}
        onClose={() => openViewer(null)}
        onToggleStar={handleToggleStar}
        isStarred={isBookmarked(viewerPath)}
        onNavigateToChange={props.onNavigateToChange}
        onCreateTodo={props.onCreateTodo}
        onOpenSession={openWikiComponent}
      />
    )
  }, [handleToggleStar, isBookmarked, openViewer, openWikiComponent, viewerComponent, viewerIsSession, viewerPath])

  const handleNavigateWikiFromTodo = useCallback((path: string) => {
    openWikiComponent(path)
    setView('search')
  }, [openWikiComponent])

  const today = new Date()
  const classificationKey = `${today.getFullYear()}-${today.getMonth()}-${today.getDate()}`
  const classificationNow = useMemo(() => new Date(), [classificationKey])

  const workspaceChanges = useMemo(
    () => (activeWorkspace ? changes.filter((change) => change.workspace === activeWorkspace) : changes),
    [activeWorkspace, changes],
  )

  const classified = useMemo(
    () => classifyChanges(workspaceChanges, STUCK_THRESHOLD_DAYS, classificationNow),
    [classificationNow, workspaceChanges],
  )

  const visibleChanges = useMemo(() => {
    const kpiFilterSets: Record<string, ChangeSummary[]> = {
      active: classified.active,
      archived: classified.archived,
      stuck: classified.stuck,
      'verify-failed': classified.verifyFailed,
      'incomplete-tasks': classified.incomplete,
    }

    return activeKpiFilter ? kpiFilterSets[activeKpiFilter] ?? [] : workspaceChanges
  }, [activeKpiFilter, classified, workspaceChanges])

  const lockedWorkspaceAliases = useMemo(() => {
    const aliases = new Set<string>()
    if (activeWorkspace) aliases.add(activeWorkspace)
    if (selected?.workspace) aliases.add(selected.workspace)
    if (viewerTodoContext.wikiComponent?.workspace) {
      aliases.add(viewerTodoContext.wikiComponent.workspace)
    }
    return aliases
  }, [activeWorkspace, selected, viewerTodoContext])

  const openWorkspaceRemoval = useCallback((alias: string) => {
    setWorkspacePendingRemoval(alias)
    setWorkspaceRemovalError(null)
  }, [])

  const closeWorkspaceRemoval = useCallback(() => {
    setWorkspacePendingRemoval(null)
    setWorkspaceRemovalError(null)
  }, [])

  const confirmWorkspaceRemoval = useCallback(async () => {
    if (!workspacePendingRemoval) return

    setWorkspaceRemovalPending(true)
    setWorkspaceRemovalError(null)
    try {
      await removeWorkspace(workspacePendingRemoval)
      await refreshWorkspaceData()
      setWorkspacePendingRemoval(null)
    } catch (error) {
      setWorkspaceRemovalError(error instanceof Error ? error.message : '移除工作区失败')
    } finally {
      setWorkspaceRemovalPending(false)
    }
  }, [refreshWorkspaceData, workspacePendingRemoval])

  return (
    <div
      className="relative flex h-screen overflow-hidden"
      style={{
        zoom: appZoom.zoom,
        backgroundImage:
          'linear-gradient(135deg, var(--color-bg) 0%, var(--color-surface) 55%, var(--color-surface) 100%)',
      }}
    >
      <SideRail
        view={view}
        onSelect={handleViewChange}
        onOpenSettings={() => setSettingsOpen(true)}
        onToggleBookmarks={() => setBookmarkPanelOpen((open) => !open)}
        bookmarkPanelOpen={bookmarkPanelOpen}
        onOpenPalette={() => palette.openPalette()}
        zoomPercent={appZoom.zoomPercent}
        todoCount={todoCounts ? todoCounts.open + todoCounts.inProgress : undefined}
      />

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <div className="flex items-center p-3 wide:hidden">
          <button
            data-testid="hamburger-toggle"
            onClick={() => setSidebarOpen((open) => !open)}
            className="border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2 text-[length:var(--type-caption)] font-medium text-[var(--color-text-primary)]"
          >
            工作区筛选
          </button>
        </div>

        {failedWorkspaces.length > 0 && (
          <div
            data-testid="workspace-warning-banner"
            className="mx-3 mb-3 border border-[var(--color-danger)] bg-[var(--color-danger-subtle)] p-3 text-[length:var(--type-caption)]"
          >
            <div className="flex items-start gap-3">
              <Icon name="warning" size={16} className="mt-0.5 text-[var(--color-danger-text)]" />
              <div className="min-w-0 flex-1 space-y-2">
                <p className="font-medium text-[var(--color-text-primary)]">
                  以下 workspace 无法读取，已暂时跳过。
                </p>
                <ul className="space-y-2">
                  {failedWorkspaces.map((alias) => {
                    const removalLocked = lockedWorkspaceAliases.has(alias)
                    return (
                      <li key={alias} className="flex flex-wrap items-center gap-2">
                        <span className="border border-[var(--color-danger)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
                          {alias}
                        </span>
                        <button
                          type="button"
                          aria-label={`移除 workspace ${alias}`}
                          title={
                            removalLocked
                              ? '当前正在查看此 workspace，先切换到其他 workspace 再移除'
                              : `移除 workspace ${alias}`
                          }
                          disabled={removalLocked}
                          onClick={() => openWorkspaceRemoval(alias)}
                          className={
                            'border px-2 py-1 text-[length:var(--type-caption)] font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] ' +
                            (removalLocked
                              ? 'cursor-not-allowed border-[var(--color-border)] bg-[var(--color-layer)] text-[var(--color-text-tertiary)]'
                              : 'border-[var(--color-danger)] bg-[var(--color-surface)] text-[var(--color-danger-text)] hover:bg-[var(--color-danger-subtle)]')
                          }
                        >
                          移除
                        </button>
                      </li>
                    )
                  })}
                </ul>
              </div>
            </div>
          </div>
        )}

        {wikiIndexing && (
          <div
            data-testid="wiki-indexing-banner"
            className="mx-3 mb-3 flex items-start gap-3 border border-[var(--color-accent)] bg-[var(--color-accent-subtle)] p-3 text-[length:var(--type-caption)]"
          >
            <Icon name="info" size={16} className="mt-0.5 text-[var(--color-accent)]" />
            <p className="text-[var(--color-text-primary)]">
              {typeof wikiIndexingChanged === 'number'
                ? `检测到 ${wikiIndexingChanged} 个文件更新，正在进入搜索库…几秒后即可检索`
                : '已检测到文档更新，正在进入搜索库…几秒后即可检索'}
            </p>
          </div>
        )}

        {view === 'changes' && (
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
            {/* Workspace chips row — moved from the deleted sidebar */}
            <div className="shrink-0 px-3 pt-3">
              <WorkspaceChips
                workspaces={workspaces}
                active={activeWorkspace}
                onSelect={(alias) => {
                  setActiveWorkspace(alias)
                  setSelected(null)
                  openViewer(null)
                  setChangeArtifacts([])
                }}
                onAdd={async (config) => {
                  await addWorkspace(config)
                  setSelected(null)
                  openViewer(null)
                  setChangeArtifacts([])
                  await refreshWorkspaceData({
                    preferredActiveWorkspace: config.alias,
                    clearSelected: true,
                  })
                }}
                onRemove={openWorkspaceRemoval}
                removeDisabledAliases={Array.from(lockedWorkspaceAliases)}
              />
            </div>

            {/* Main content column — full width, no sidebar */}
            <div className="relative flex-1 overflow-hidden p-4">
              <div className="h-full overflow-y-auto space-y-4">
                <KpiCards
                  changes={workspaceChanges}
                  stuckThresholdDays={STUCK_THRESHOLD_DAYS}
                  now={classificationNow}
                  activeFilter={activeKpiFilter}
                  onFilterSelect={setActiveKpiFilter}
                />

                {/* Surface separator: luminance step, no shadow.
                    Separates the KPI readout row (surface) from the table (surface)
                    with a --color-layer band, the middle depth step. */}
                <div className="h-7 bg-[var(--color-layer)] border-t border-[var(--color-border)]" />

                <ChangeExplorer
                  changes={visibleChanges}
                  selected={selected?.name ?? null}
                  selectedWorkspace={selected?.workspace}
                  onSelect={(name, workspace) => {
                    openViewer(null)
                    setChangeArtifacts([])
                    setSelected({ name, workspace })
                  }}
                />

                {selectedChange ? (
                  <>
                    {/* Back-to-list affordance — replaces the sidebar toggle */}
                    <div className="flex items-center gap-2 mt-4 mb-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                      <button
                        type="button"
                        onClick={() => {
                          setSelected(null)
                          openViewer(null)
                          setChangeArtifacts([])
                        }}
                        className="underline hover:text-[var(--color-accent)]"
                      >
                        ← 返回列表
                      </button>
                      <span>·</span>
                      <span>{selectedChange.title || selectedChange.name}</span>
                    </div>
                    <ChangeDetail
                      change={selectedChange}
                      onOpenArtifact={(path) => openViewer(path, 'document')}
                      onArtifactsChanged={setChangeArtifacts}
                      onChangeUpdated={refreshWorkspaceData}
                      onNavigateToTodos={(workspace, changeName) => {
                        setTodoDraft({ change: { workspace, name: changeName } })
                        handleViewChange('todos')
                      }}
                      todoCount={
                        selectedChange
                          ? todoCountByChangeKey.get(
                              `${selectedChange.workspace}\x00${selectedChange.name}`,
                            ) ?? 0
                          : undefined
                      }
                    />
                  </>
                ) : (
                  <StateBlock
                    kind="empty"
                    testId="change-empty-state"
                    title="点击上方表格中的一行查看变更详情"
                    detail="可通过上方 KPI 卡片筛选，或在搜索与筛选中定位目标变更"
                  />
                )}
              </div>

              {/* Document viewer — rendered as an overlay so the change list
                  stays mounted beneath it, preserving scroll position and
                  chat history when switching between artifacts. */}
              {viewerPath && (
                <div className="absolute inset-0 z-10 overflow-y-auto bg-[var(--color-surface)]">
                  <Suspense fallback={<ViewFallback label="文档" />}>
                    {renderViewer({
                      artifacts: changeArtifacts,
                      workspace: selectedChange?.workspace,
                      onSelectArtifact: (path: string) => openViewer(path, 'document'),
                      onCreateTodo: viewerTodoHandler,
                    })}
                  </Suspense>
                </div>
              )}
              </div>
            </div>
        )}

        {view === 'todos' && (
          <Suspense fallback={<ViewFallback label="待办" />}>
            <div className="flex-1 min-h-0 overflow-hidden">
              <LazyTodoPanel
                todos={todos}
                counts={todoCounts}
                writable={todoWritable}
                loading={todoLoading}
                error={todoError}
                onCreate={createTodo}
                onUpdate={updateTodo}
                onDelete={deleteTodo}
                workspaces={workspaces}
                wikiComponents={wikiComponents}
                changes={changes}
                onNavigateWiki={handleNavigateWikiFromTodo}
                onNavigateSession={(path) => {
                  // The todo view renders no viewer of its own, so the jump has to
                  // land where a session can actually be shown - the same shape as
                  // handleNavigateWikiFromTodo switching to the search view.
                  openViewer(path, 'session')
                  setView('sessions')
                }}
                sessionPathById={sessionPathById}
                onNavigateChange={(workspace, changeName) => {
                  setView('changes')
                  setSelected({ name: changeName, workspace })
                  setActiveWorkspace(workspace)
                }}
                draftChange={todoDraft?.change ?? null}
                draftWikiRef={todoDraft?.wikiRef ?? null}
                onDraftConsumed={() => setTodoDraft(null)}
                focusCaptureRef={todoFocusCaptureRef}
                defaultWorkspace={activeWorkspace}
              />
            </div>
          </Suspense>
        )}

        {view === 'graph' && (
          <Suspense fallback={<ViewFallback label="知识图谱" />}>
            <div className="flex-1 min-h-0 p-4">
              {viewerPath ? (
                renderViewer({ onCreateTodo: viewerTodoHandler })
              ) : (
                <LazyWikiGraph
                  onNodeClick={(id) => {
                    openWikiComponent(id)
                  }}
                />
              )}
            </div>
          </Suspense>
        )}

        {view === 'timeline' && (
          <Suspense fallback={<ViewFallback label="时间线" />}>
            <div className="flex-1 min-h-0 p-4">
              {viewerPath ? (
                renderViewer({ onNavigateToChange: navigateToChange, onCreateTodo: viewerTodoHandler })
              ) : (
                <LazyWikiTimeline onOpen={openWikiComponent} />
              )}
            </div>
          </Suspense>
        )}

        {view === 'search' && (
          <Suspense fallback={<ViewFallback label="语义搜索" />}>
            <div className="relative flex-1 min-h-0 overflow-hidden">
              <div className="absolute inset-0 overflow-y-auto p-4">
                <LazySemanticSearch
                  onNodeClick={(id) => {
                    openWikiComponent(id)
                  }}
                />
              </div>
              {viewerPath && (
                <div className="absolute inset-0 z-10 overflow-y-auto bg-[var(--color-surface)]">
                  {renderViewer({ onNavigateToChange: navigateToChange, onCreateTodo: viewerTodoHandler })}
                </div>
              )}
            </div>
          </Suspense>
        )}

        {view === 'report' && (
          <Suspense fallback={<ViewFallback label="报告" />}>
            <div className="flex-1 min-h-0 overflow-y-auto p-4">
              <LazyReportView
                workspace={activeWorkspace}
                workspaces={workspaces}
                onOpenSettings={() => setSettingsOpen(true)}
              />
            </div>
          </Suspense>
        )}

        {view === 'lint' && (
          <Suspense fallback={<ViewFallback label="文档健康" />}>
            <div className="flex-1 min-h-0 overflow-y-auto p-4">
              {viewerPath ? (
                renderViewer({ onNavigateToChange: navigateToChange, onCreateTodo: viewerTodoHandler })
              ) : (
                <LazyLintPanel onOpen={openWikiComponent} />
              )}
            </div>
          </Suspense>
        )}

        {view === 'recent' && (
          <Suspense fallback={<ViewFallback label="最近更新" />}>
            <div className="flex-1 min-h-0 overflow-y-auto p-4">
              {viewerPath ? (
                renderViewer({ onNavigateToChange: navigateToChange, onCreateTodo: viewerTodoHandler })
              ) : (
                <LazyRecentPanel onOpen={openWikiComponent} />
              )}
            </div>
          </Suspense>
        )}

        {view === 'sessions' && (
          <Suspense fallback={<ViewFallback label="Agent 会话" />}>
            <div className="flex-1 min-h-0 overflow-y-auto p-4">
              {viewerPath ? (
                renderViewer({ onNavigateToChange: navigateToChange, onCreateTodo: viewerTodoHandler })
              ) : (
                <LazySessionsPanel onOpen={(path) => openViewer(path, 'session')} />
              )}
            </div>
          </Suspense>
        )}

        {view === 'shares' && (
          <Suspense fallback={<ViewFallback label="分享" />}>
            <div className="flex-1 min-h-0 overflow-y-auto">
              <LazyShareList />
            </div>
          </Suspense>
        )}

        {view === 'calendar' && (
          <Suspense fallback={<ViewFallback label="日历" />}>
            <div className="flex-1 min-h-0 overflow-y-auto">
              {viewerPath ? (
                renderViewer({ onNavigateToChange: navigateToChange, onCreateTodo: viewerTodoHandler })
              ) : (
                <LazyCalendarPanel onOpen={openWikiComponent} />
              )}
            </div>
          </Suspense>
        )}
      </div>

      {bookmarkPanelOpen && (
        <div className="absolute left-[76px] top-5 z-40">
          <BookmarkPanel
            bookmarks={bookmarks}
            onOpen={(path) => {
              openViewer(path, 'document')
              setBookmarkPanelOpen(false)
            }}
            onRemove={handleRemoveBookmark}
            onClose={() => setBookmarkPanelOpen(false)}
          />
        </div>
      )}

      {settingsOpen && (
        <Modal
          title="设置"
          hideTitle
          onClose={() => setSettingsOpen(false)}
          width="max-w-md"
        >
          <SettingsPanel onClose={() => setSettingsOpen(false)} />
        </Modal>
      )}

      {workspacePendingRemoval && (
        <Modal
          title="移除工作区"
          onClose={closeWorkspaceRemoval}
          dismissible={!workspaceRemovalPending}
          data-testid="remove-workspace-modal"
        >
          <div className="space-y-4 p-4">
            <p className="text-[length:var(--type-body)] text-[var(--color-text-primary)]">
              将从当前面板移除 <strong>{workspacePendingRemoval}</strong>。已同步的文档不会被删除，后续仍可重新添加。
            </p>
            {workspaceRemovalError && (
              <div
                data-testid="remove-workspace-error"
                className="border border-[var(--color-danger)] bg-[var(--color-danger-subtle)] px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-danger-text)]"
              >
                {workspaceRemovalError}
              </div>
            )}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={closeWorkspaceRemoval}
                disabled={workspaceRemovalPending}
                className="border border-[var(--color-border)] px-3 py-2 text-[length:var(--type-caption)] font-medium text-[var(--color-text-secondary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
              >
                取消
              </button>
              <button
                type="button"
                data-testid="confirm-remove-workspace"
                onClick={confirmWorkspaceRemoval}
                disabled={workspaceRemovalPending}
                className={
                  'border px-3 py-2 text-[length:var(--type-caption)] font-medium text-[var(--color-text-on-color)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] ' +
                  (workspaceRemovalPending
                    ? 'cursor-wait border-[var(--color-danger)] bg-[var(--color-danger)]/70'
                    : 'border-[var(--color-danger)] bg-[var(--color-danger)] hover:bg-[color-mix(in_srgb,var(--color-danger)_85%,black)]')
                }
              >
                {workspaceRemovalPending ? '正在移除…' : '确认移除'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      <CommandPalette palette={palette} shortcuts={shortcutDefs} />
      <StaleBundleNotice />
      {viewerPath && !viewerIsSession && (
        <ChatBubble
          key={viewerPath}
          changeName={selectedChange?.name}
          workspace={selectedChange?.workspace}
          componentId={selectedChange?.componentId}
          documentPath={viewerPath}
        />
      )}
    </div>
  )
}
