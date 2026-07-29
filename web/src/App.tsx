import { useEffect, useState, useCallback, useMemo, useRef } from 'react'
import { fetchWorkspaces, addWorkspace, fetchChangesWithMeta, fetchWikiIndex, fetchBookmarks, addBookmark, removeBookmark } from './api/client'
import type { ChangeSummary, WorkspaceConfig, WikiComponent, Bookmark } from './api/types'
import { KpiCards, classifyChanges } from './components/KpiCards'
import { ChangeExplorer } from './components/ChangeExplorer'
import { ChangeDetail } from './components/ChangeDetail'
import { ChatBubble } from './components/ChatBubble'
import { WorkspaceChips } from './components/WorkspaceChips'
import { MarkdownViewer } from './components/MarkdownViewer'
import { WikiGraph } from './components/WikiGraph'
import { WikiTimeline } from './components/WikiTimeline'
import { LintPanel } from './components/LintPanel'
import { RecentPanel } from './components/RecentPanel'
import { SideRail } from './components/SideRail'
import { SettingsPanel } from './components/SettingsPanel'
import { ReportView } from './components/ReportView'
import { BookmarkPanel } from './components/BookmarkPanel'
import { SemanticSearch } from './components/SemanticSearch'
import { ShareList } from './components/ShareList'
import { CalendarPanel } from './components/CalendarPanel'
import { TodoPanel } from './components/TodoPanel'
import { useWikiEvents } from './hooks/useWikiEvents'
import { useTodos } from './hooks/useTodos'
import { CommandPalette } from './components/CommandPalette'
import { useKeyboardShortcuts, formatShortcut } from './hooks/useKeyboardShortcuts'
import { useCommandPalette } from './hooks/useCommandPalette'
import { useAppZoom } from './hooks/useAppZoom'
import type { CommandAction } from './hooks/useCommandPalette'

const STUCK_THRESHOLD_DAYS = 14

interface ChangeSelection {
  name: string
  workspace?: string
}

// ── Viewer context helpers ────────────────────────────────────────────────────
// Shared by all MarkdownViewer onCreateTodo callbacks: resolves the current
// wiki component and infers a Change from the path pattern, never stale
// selectedChange.
interface TodoContext {
  wikiComponent: WikiComponent | null
  changeName: string | null
  changeWorkspace: string | null
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
  const [view, setView] = useState<'changes' | 'todos' | 'graph' | 'timeline' | 'search' | 'recent' | 'lint' | 'report' | 'shares' | 'calendar'>('changes')
  const [wikiComponents, setWikiComponents] = useState<WikiComponent[]>([])
  const [viewerPath, setViewerPath] = useState<string | null>(null)
  const [changeArtifacts, setChangeArtifacts] = useState<{ path: string; label: string }[]>([])
  const [bookmarks, setBookmarks] = useState<Bookmark[]>([])
  const [bookmarkPanelOpen, setBookmarkPanelOpen] = useState(false)
  const [wikiIndexing, setWikiIndexing] = useState(false)
  const [wikiIndexingChanged, setWikiIndexingChanged] = useState<number | null>(null)

  // ── Todo state ───────────────────────────────────────────────────────────
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

  // ── Command Palette actions ─────────────────────────────────────────────
  const viewLabels: Record<string, string> = {
    changes: '变更仪表盘',
    todos: '待办',
    graph: '知识图谱',
    timeline: '时间线',
    search: '语义搜索',
    recent: '最近更新',
    lint: '文档健康检查',
    report: '报告生成',
    shares: '分享管理',
    calendar: '产品日历',
  }

  const commandActions: CommandAction[] = useMemo(() => [
    ...Object.entries(viewLabels).map(([v, label]) => ({
      id: `nav-${v}`,
      label,
      category: 'Navigation',
      icon: '📍',
      run: () => handleViewChange(v as typeof view),
    })),
    { id: 'new-todo', label: '新建待办', category: 'Commands', icon: '✅', run: () => { handleViewChange('todos'); setTimeout(() => todoFocusCaptureRef.current?.(), 100) } },
    { id: 'bookmarks', label: '收藏夹', category: 'Navigation', icon: '⭐', run: () => setBookmarkPanelOpen((p) => !p) },
    { id: 'settings', label: '设置', category: 'Navigation', icon: '⚙️', run: () => setSettingsOpen(true) },
    { id: 'refresh', label: '刷新数据', category: 'Commands', icon: '🔄', run: () => window.location.reload() },
  ], [])

  const palette = useCommandPalette(commandActions)
  const appZoom = useAppZoom()

  const shortcutDefs = useMemo(() => [
    { key: 'k', ctrlOrCmd: true, label: '命令面板', run: () => {} },
    { key: '1', ctrlOrCmd: true, label: '变更仪表盘', run: () => {} },
    { key: '2', ctrlOrCmd: true, label: '知识图谱', run: () => {} },
    { key: '3', ctrlOrCmd: true, label: '时间线', run: () => {} },
    { key: '4', ctrlOrCmd: true, label: '语义搜索', run: () => {} },
    { key: '5', ctrlOrCmd: true, label: '最近更新', run: () => {} },
    { key: '6', ctrlOrCmd: true, label: '文档健康', run: () => {} },
    { key: '7', ctrlOrCmd: true, label: '产品日历', run: () => {} },
    { key: '8', ctrlOrCmd: true, label: '待办', run: () => {} },
    { key: 'b', ctrlOrCmd: true, label: '收藏夹', run: () => {} },
    { key: 'Escape', ctrlOrCmd: false, label: '关闭面板', run: () => {} },
    { key: "=", ctrlOrCmd: true, label: "放大", run: () => {} },
    { key: "-", ctrlOrCmd: true, label: "缩小", run: () => {} },
    { key: "0", ctrlOrCmd: true, label: "重置缩放", run: () => {} },
  ], [])

  useKeyboardShortcuts([
    { key: 'k', ctrlOrCmd: true, label: '命令面板', run: () => palette.togglePalette() },
    { key: '1', ctrlOrCmd: true, label: '变更仪表盘', run: () => handleViewChange('changes') },
    { key: '2', ctrlOrCmd: true, label: '知识图谱', run: () => handleViewChange('graph') },
    { key: '3', ctrlOrCmd: true, label: '时间线', run: () => handleViewChange('timeline') },
    { key: '4', ctrlOrCmd: true, label: '语义搜索', run: () => handleViewChange('search') },
    { key: '5', ctrlOrCmd: true, label: '最近更新', run: () => handleViewChange('recent') },
    { key: '6', ctrlOrCmd: true, label: '文档健康', run: () => handleViewChange('lint') },
    { key: '7', ctrlOrCmd: true, label: '产品日历', run: () => handleViewChange('calendar') },
    { key: '8', ctrlOrCmd: true, label: '待办', run: () => handleViewChange('todos') },
    { key: 'b', ctrlOrCmd: true, label: '收藏夹', run: () => setBookmarkPanelOpen((p) => !p) },
    { key: 'Escape', ctrlOrCmd: false, label: '关闭面板', run: () => { palette.closePalette(); setViewerPath(null); setBookmarkPanelOpen(false); setSettingsOpen(false) } },
    { key: "=", ctrlOrCmd: true, label: "放大", run: appZoom.zoomIn },
    { key: "-", ctrlOrCmd: true, label: "缩小", run: appZoom.zoomOut },
    { key: "0", ctrlOrCmd: true, label: "重置缩放", run: appZoom.zoomReset },
  ])

  function navigateToChange(changeName: string) {
    // Resolve workspace: viewer component → unique change match → active → null
    let workspace: string | undefined
    if (viewerPath) {
      workspace = wikiComponents.find((c) => c.path === viewerPath || c.id === viewerPath)?.workspace
    }
    if (!workspace) {
      const matches = changes.filter((c) => c.name === changeName && c.workspace)
      if (matches.length === 1) workspace = matches[0].workspace
      else workspace = activeWorkspace ?? undefined
    }
    setView('changes')
    setSelected({ name: changeName, workspace })
    setActiveWorkspace(workspace ?? null)
    setViewerPath(null)
  }

  function handleViewChange(v: 'changes' | 'todos' | 'graph' | 'timeline' | 'search' | 'recent' | 'lint' | 'report' | 'shares' | 'calendar') {
    setViewerPath(null)
    setView(v)
  }

  useEffect(() => {
    fetchWorkspaces()
      .then((ws) => setWorkspaces(ws ?? []))
      .catch(() => setWorkspaces([]))
  }, [])

  useEffect(() => {
    fetchChangesWithMeta()
      .then((r) => {
        setChanges(r.changes ?? [])
        setFailedWorkspaces(r.failedWorkspaces ?? [])
      })
      .catch(() => setChanges([]))
  }, [])

  const refreshWikiIndex = useCallback(() => {
    fetchWikiIndex()
      .then(setWikiComponents)
      .catch(() => setWikiComponents([]))
  }, [])

  useEffect(() => {
    refreshWikiIndex()
  }, [refreshWikiIndex])

  useEffect(() => {
    fetchBookmarks()
      .then(setBookmarks)
      .catch(() => setBookmarks([]))
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

  useWikiEvents({
    onIndexingStarted: handleIndexingStarted,
    onUpdate: handleGraphUpdated,
    onTodosUpdated: handleTodosUpdated,
  })

  useEffect(() => {
    if (!wikiIndexing) return
    const timer = window.setTimeout(() => {
      setWikiIndexing(false)
      setWikiIndexingChanged(null)
    }, 8000)
    return () => window.clearTimeout(timer)
  }, [wikiIndexing])

  const isBookmarked = (path: string) => bookmarks.some((b) => b.path === path)

  function handleToggleStar(path: string, title: string) {
    if (isBookmarked(path)) {
      removeBookmark(path)
        .then(setBookmarks)
        .catch(() => {})
    } else {
      const type = path.split('.').pop() || 'doc'
      addBookmark({ path, title, type })
        .then(setBookmarks)
        .catch(() => {})
    }
  }

  function handleRemoveBookmark(path: string) {
    removeBookmark(path)
      .then(setBookmarks)
      .catch(() => {})
  }

  const selectedChange = selected
    ? changes.find((change) => change.name === selected.name && change.workspace === selected.workspace) ?? null
    : null

  // ── Todo change counts for ChangeDetail badge ────────────────────────────
  const todoCountByChangeKey = useMemo(() => {
    const map = new Map<string, number>()
    for (const t of todos) {
      if (t.status === 'done' || !t.change) continue
      const key = `${t.change.workspace}\x00${t.change.name}`
      map.set(key, (map.get(key) ?? 0) + 1)
    }
    return map
  }, [todos])

  // Resolve wiki component + inferred change for onCreateTodo in MarkdownViewer.
  // Computed once per render; MarkdownViewer only receives the handler when
  // a wiki component actually matches the current viewerPath.
  const viewerTodoContext = useMemo((): TodoContext => {
    if (!viewerPath) return { wikiComponent: null, changeName: null, changeWorkspace: null }
    const wikiComponent = wikiComponents.find((c) => c.path === viewerPath || c.id === viewerPath) ?? null
    let changeName: string | null = null
    let changeWorkspace: string | null = null
    if (wikiComponent) {
      const m = viewerPath.match(/\/changes\/([^/]+)\//)
      if (m) {
        changeName = m[1]
        changeWorkspace = wikiComponent.workspace ?? null
        const exists = changeWorkspace
          ? changes.some((c) => c.name === changeName && c.workspace === changeWorkspace)
          : false
        if (!exists) { changeName = null; changeWorkspace = null }
      }
    }
    return { wikiComponent, changeName, changeWorkspace }
  }, [viewerPath, wikiComponents, changes])

  // Shared onCreateTodo for all MarkdownViewer callsites — only passed when
  // viewerTodoContext.wikiComponent is non-null (button not rendered otherwise).
  const createTodoFromViewer = useCallback(() => {
    const ctx = viewerTodoContext
    if (!ctx.wikiComponent) return
    setTodoDraft({
      wikiRef: {
        componentId: ctx.wikiComponent.id,
        workspace: ctx.wikiComponent.workspace ?? '',
        titleSnapshot: ctx.wikiComponent.title,
      },
      ...(ctx.changeName && ctx.changeWorkspace
        ? { change: { workspace: ctx.changeWorkspace, name: ctx.changeName } }
        : {}),
    })
    handleViewChange('todos')
  }, [viewerTodoContext])

  const viewerTodoHandler = viewerTodoContext.wikiComponent ? createTodoFromViewer : undefined
  // Todo wiki-chip navigation: switch to a viewer-capable view
  const handleNavigateWikiFromTodo = useCallback((path: string) => {
    setViewerPath(path)
    setView('search')
  }, [])

  const now = new Date()

  const workspaceChanges = activeWorkspace
    ? changes.filter((c) => c.workspace === activeWorkspace)
    : changes

  const classified = classifyChanges(workspaceChanges, STUCK_THRESHOLD_DAYS, now)
  const kpiFilterSets: Record<string, ChangeSummary[]> = {
    active: classified.active,
    archived: classified.archived,
    stuck: classified.stuck,
    'verify-failed': classified.verifyFailed,
    'incomplete-tasks': classified.incomplete,
  }
  const visibleChanges = activeKpiFilter
    ? kpiFilterSets[activeKpiFilter] ?? []
    : workspaceChanges

  return (
    <div
      className="h-screen flex overflow-hidden relative"
      style={{
        zoom: appZoom.zoom,
        backgroundImage: 'linear-gradient(135deg, var(--color-bg) 0%, var(--color-surface) 55%, var(--color-surface) 100%)',
      }}
    >
      <SideRail
        view={view}
        onSelect={handleViewChange}
        onOpenSettings={() => setSettingsOpen(true)}
        onToggleBookmarks={() => setBookmarkPanelOpen((v) => !v)}
        bookmarkPanelOpen={bookmarkPanelOpen}
        onOpenPalette={() => palette.openPalette()}
        zoomPercent={appZoom.zoomPercent}
        todoCount={todoCounts ? todoCounts.open + todoCounts.inProgress : undefined}
      />
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        <div className="xl:hidden flex items-center p-3 shrink-0">
          <button
            data-testid="hamburger-toggle"
            onClick={() => setSidebarOpen((v) => !v)}
            className="text-sm"
          >
            ☰ 工作区
          </button>
        </div>

      {failedWorkspaces.length > 0 && (
        <div data-testid="workspace-warning-banner" className="text-xs bg-[color-mix(in_srgb,var(--color-danger)_10%,var(--color-surface))] text-[var(--color-danger)] p-2 m-3 shrink-0">
          ⚠ 以下 workspace 无法读取，已跳过：{failedWorkspaces.join(', ')}
        </div>
      )}

      {wikiIndexing && (
        <div data-testid="wiki-indexing-banner" className="text-xs bg-[color-mix(in_srgb,var(--color-accent)_10%,var(--color-surface))] text-[var(--color-accent)] p-2 mx-3 mb-3 shrink-0">
          ℹ {typeof wikiIndexingChanged === 'number' ? `检测到 ${wikiIndexingChanged} 个文件更新，正在进入搜索库…` : '已检测到文档更新，正在进入搜索库…'} 几秒后即可检索
        </div>
      )}

      {view === 'changes' && (
        <>
          <div className="flex-1 flex min-h-0">
            <aside
              data-testid="sidebar"
              className={
                (sidebarOpen ? 'block' : 'hidden') +
                ' xl:block w-full xl:w-[340px] shrink-0 border-r border-[var(--color-border)] p-3 overflow-y-auto'
              }
            >
              <WorkspaceChips
                workspaces={workspaces}
                active={activeWorkspace}
                onSelect={(alias) => {
                  setActiveWorkspace(alias)
                  setSelected(null)
                  setViewerPath(null)
                  setChangeArtifacts([])
                }}
                onAdd={async (cfg) => {
                  await addWorkspace(cfg)
                  const refreshedWorkspaces = await fetchWorkspaces()
                  setWorkspaces(refreshedWorkspaces ?? [])
                  const refreshed = await fetchChangesWithMeta()
                  setChanges(refreshed.changes ?? [])
                  setFailedWorkspaces(refreshed.failedWorkspaces ?? [])
                  setSelected(null)
                  setActiveWorkspace(cfg.alias)
                }}
              />
              <ChangeExplorer
                changes={visibleChanges}
                selected={selected?.name ?? null}
                selectedWorkspace={selected?.workspace}
                onSelect={(name, workspace) => {
                  setViewerPath(null)
                  setChangeArtifacts([])
                  setSelected({ name, workspace })
                  setSidebarOpen(false)
                }}
              />
            </aside>

            <main className="flex-1 min-h-0 overflow-y-auto p-4">
              {viewerPath ? (
                <MarkdownViewer
                  path={viewerPath}
                  artifacts={changeArtifacts}
                  workspace={selectedChange?.workspace}
                  onSelectArtifact={setViewerPath}
                  onClose={() => setViewerPath(null)}
                  onToggleStar={handleToggleStar}
                  isStarred={isBookmarked(viewerPath)}
                  onCreateTodo={viewerTodoHandler}
                />
              ) : (
                <div className="space-y-4">
                  <KpiCards
                    changes={workspaceChanges}
                    stuckThresholdDays={STUCK_THRESHOLD_DAYS}
                    now={now}
                    activeFilter={activeKpiFilter}
                    onFilterSelect={setActiveKpiFilter}
                  />
                  {selectedChange ? (
                    <ChangeDetail
                      change={selectedChange}
                      onOpenArtifact={setViewerPath}
                      onArtifactsChanged={setChangeArtifacts}
                      onChangeUpdated={() =>
                        fetchChangesWithMeta()
                          .then((r) => {
                            setChanges(r.changes ?? [])
                            setFailedWorkspaces(r.failedWorkspaces ?? [])
                          })
                          .catch(() => {})
                      }
                      onNavigateToTodos={(workspace, changeName) => {
                        setTodoDraft({ change: { workspace, name: changeName } })
                        handleViewChange('todos')
                      }}
                      todoCount={selectedChange ? todoCountByChangeKey.get(`${selectedChange.workspace}\x00${selectedChange.name}`) ?? 0 : undefined}
                    />
                  ) : (
                    <div
                      data-testid="change-empty-state"
                      className="flex flex-col items-center justify-center gap-2 text-center border border-dashed border-[var(--color-border)] bg-white py-24 px-6"
                    >
                      <span className="text-4xl text-[var(--color-text-tertiary)]" aria-hidden="true">◇</span>
                      <p className="text-sm font-medium text-[var(--color-text-primary)]">从左侧选择一个变更查看详情</p>
                      <p className="text-xs text-[var(--color-text-secondary)]">可通过上方 KPI 卡片筛选，或在左侧工作区与搜索中定位目标变更</p>
                    </div>
                  )}
                </div>
              )}
            </main>
          </div>
        </>
      )}

      {view === 'todos' && (
        <div className="flex-1 min-h-0 overflow-hidden">
          <TodoPanel
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
      )}

      {view === 'graph' && (
        <div className="flex-1 min-h-0 p-4">
          {viewerPath ? (
            <MarkdownViewer
              path={viewerPath}
              onClose={() => setViewerPath(null)}
              onToggleStar={handleToggleStar}
              isStarred={isBookmarked(viewerPath)}
              onCreateTodo={viewerTodoHandler}
            />
          ) : (
            <WikiGraph
              onNodeClick={(id) => {
                const component = wikiComponents.find((c) => c.id === id)
                setViewerPath(component?.path ?? id)
              }}
            />
          )}
        </div>
      )}

      {view === 'timeline' && (
        <div className="flex-1 min-h-0 p-4">
          <WikiTimeline />
        </div>
      )}

      <div className="flex-1 min-h-0 relative overflow-hidden" style={{ display: view === 'search' ? undefined : 'none' }}>
        <div className="absolute inset-0 overflow-y-auto p-4">
          <SemanticSearch
            onNodeClick={(id) => {
              const component = wikiComponents.find((c) => c.id === id)
              setViewerPath(component?.path ?? id)
            }}
          />
        </div>
        {viewerPath && view === 'search' && (
          <div className="absolute inset-0 z-10 overflow-y-auto bg-white">
            <MarkdownViewer
              path={viewerPath}
              onClose={() => setViewerPath(null)}
              onToggleStar={handleToggleStar}
              isStarred={isBookmarked(viewerPath)}
              onNavigateToChange={navigateToChange}
              onCreateTodo={viewerTodoHandler}
            />
          </div>
        )}
      </div>

      {view === 'report' && (
        <div className="flex-1 min-h-0 overflow-y-auto p-4">
          <ReportView workspace={activeWorkspace} workspaces={workspaces} onOpenSettings={() => setSettingsOpen(true)} />
        </div>
      )}

      {view === 'lint' && (
        <div className="flex-1 min-h-0 overflow-y-auto p-4">
          {viewerPath ? (
            <MarkdownViewer
              path={viewerPath}
              onClose={() => setViewerPath(null)}
              onToggleStar={handleToggleStar}
              isStarred={isBookmarked(viewerPath)}
              onNavigateToChange={navigateToChange}
              onCreateTodo={viewerTodoHandler}
            />
          ) : (
            <LintPanel onOpen={(path) => setViewerPath(path)} />
          )}
        </div>
      )}

      {view === 'recent' && (
        <div className="flex-1 min-h-0 overflow-y-auto p-4">
          {viewerPath ? (
            <MarkdownViewer
              path={viewerPath}
              onClose={() => setViewerPath(null)}
              onToggleStar={handleToggleStar}
              isStarred={isBookmarked(viewerPath)}
              onNavigateToChange={navigateToChange}
              onCreateTodo={viewerTodoHandler}
            />
          ) : (
            <RecentPanel onOpen={(path) => setViewerPath(path)} />
          )}
        </div>
      )}

      {view === 'shares' && (
        <div className="flex-1 min-h-0 overflow-y-auto">
          <ShareList />
        </div>
      )}

      {view === 'calendar' && (
        <div className="flex-1 min-h-0 overflow-y-auto">
          {viewerPath ? (
            <MarkdownViewer
              path={viewerPath}
              onClose={() => setViewerPath(null)}
              onToggleStar={handleToggleStar}
              isStarred={isBookmarked(viewerPath)}
              onNavigateToChange={navigateToChange}
              onCreateTodo={viewerTodoHandler}
            />
          ) : (
            <CalendarPanel onOpen={(path) => setViewerPath(path)} />
          )}
        </div>
      )}
      </div>
      {bookmarkPanelOpen && (
        <div className="absolute top-5 left-[76px] z-40">
          <BookmarkPanel
            bookmarks={bookmarks}
            onOpen={(path) => {
              setViewerPath(path)
              setBookmarkPanelOpen(false)
            }}
            onRemove={handleRemoveBookmark}
            onClose={() => setBookmarkPanelOpen(false)}
          />
        </div>
      )}
      {settingsOpen && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4">
          <div className="bg-white shadow-2xl w-full max-w-md max-h-[85vh] overflow-y-auto">
            <SettingsPanel onClose={() => setSettingsOpen(false)} />
          </div>
        </div>
      )}
      <CommandPalette palette={palette} shortcuts={shortcutDefs} />
      {viewerPath && (
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
