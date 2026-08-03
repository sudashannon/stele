import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import App from './App'
import { fetchWorkspaces, fetchChangesWithMeta, fetchWikiIndex, fetchLintIssues, fetchRecent, fetchSessionsWithMeta, fetchChatSession, fetchChangeDetail, fetchBookmarks, fetchTodos, addBookmark, removeWorkspace } from './api/client'
import type { ChangeSummary, WorkspaceConfig } from './api/types'

// WikiGraph mounts a real cytoscape instance with a cose layout and
// `cy.fit()` on 'layoutstop'; that layout engine doesn't run correctly in
// jsdom (WikiGraph.test.tsx mocks cytoscape directly to cover that). At the
// App level we only care that switching to 图谱 mounts WikiGraph and wires
// its onNodeClick — so mock the component itself rather than cytoscape.
let wikiGraphShouldSuspend = false
let wikiGraphSuspendPromise: Promise<void> | null = null
let releaseWikiGraph: (() => void) | null = null

function suspendWikiGraphLoad() {
  wikiGraphShouldSuspend = true
  wikiGraphSuspendPromise = new Promise<void>((resolve) => {
    releaseWikiGraph = () => {
      wikiGraphShouldSuspend = false
      wikiGraphSuspendPromise = null
      releaseWikiGraph = null
      resolve()
    }
  })
}

function resetWikiGraphLoad() {
  wikiGraphShouldSuspend = false
  wikiGraphSuspendPromise = null
  releaseWikiGraph = null
}

vi.mock('./components/WikiGraph', () => ({
  WikiGraph: ({ onNodeClick }: { onNodeClick?: (id: string) => void }) => {
    if (wikiGraphShouldSuspend && wikiGraphSuspendPromise) throw wikiGraphSuspendPromise
    return (
      <button type="button" data-testid="wiki-graph-canvas" onClick={() => onNodeClick?.('/tmp/omp/session.jsonl')}>
        图谱
      </button>
    )
  },
}))
vi.mock('./components/WikiTimeline', () => ({
  WikiTimeline: ({ onOpen }: { onOpen?: (path: string) => void }) => (
    <button type="button" onClick={() => onOpen?.('/x/timeline.md')}>
      打开时间线文档
    </button>
  ),
}))
vi.mock('./components/MarkdownViewer', () => ({
  MarkdownViewer: ({
    path,
    onClose,
    onToggleStar,
  }: {
    path: string
    onClose: () => void
    onToggleStar?: (path: string, title: string) => void
  }) => (
    <div data-testid="markdown-viewer">
      <div>{path}</div>
      <button type="button" onClick={onClose}>✕ 关闭</button>
      <button
        type="button"
        aria-label="收藏"
        onClick={() => onToggleStar?.(path, path.split('/').pop() ?? path)}
      >
        收藏
      </button>
    </div>
  ),
}))

vi.mock('./components/SessionDetail', () => ({
  SessionDetail: ({
    sessionId,
    onOpenDocument,
    onClose,
  }: {
    sessionId: string
    onOpenDocument?: (path: string) => void
    onClose: () => void
  }) => (
    <div data-testid="session-detail">
      <div>{sessionId}</div>
      <button type="button" onClick={() => onOpenDocument?.('/x/from-session.md')}>打开文档</button>
      <button type="button" onClick={onClose}>关闭会话</button>
    </div>
  ),
}))

// Regression test for the Critical finding in Task 17 review: the Go backend
// genuinely returns "changes": null (nil slice) in two real scenarios —
// empty/misconfigured single-dir mode, and multi-workspace mode where ALL
// registered workspaces are unreadable. Without a null-guard at the App.tsx
// call site, `setChanges(null)` makes `changes.find(...)` (computing
// selectedChange) throw during render, crashing the whole app with no error
// boundary — before the warning banner (this task's own feature) ever gets a
// chance to render.
vi.mock('./api/client', () => ({
  fetchWorkspaces: vi.fn().mockResolvedValue(null),
  addWorkspace: vi.fn(),
  fetchChangesWithMeta: vi.fn().mockResolvedValue({ changes: null, failedWorkspaces: ['broken-ws'] }),
  fetchWikiIndex: vi.fn().mockResolvedValue([]),
  fetchWikiLint: vi.fn().mockResolvedValue([]),
  fetchLintIssues: vi.fn().mockResolvedValue([]),
  fetchRecent: vi.fn().mockResolvedValue([]),
  fetchSessions: vi.fn().mockResolvedValue([]),
  fetchSessionsWithMeta: vi.fn().mockResolvedValue({ sessions: [], enabled: true }),
  refreshSessions: vi.fn().mockResolvedValue({ changed: 0, sessions: 0 }),
  fetchSession: vi.fn().mockResolvedValue(null),
  fetchChangeDetail: vi.fn().mockResolvedValue({
    name: '', workflow: '', phase: '', archived: false, tasksCompleted: 0, tasksTotal: 0,
    verifyResult: '', createdAt: '', phases: [],
  }),
  fetchArtifactContent: vi.fn().mockResolvedValue(''),
  fetchWikiComponent: vi.fn().mockResolvedValue({ component: { id: '', title: '' }, forward: [], backlinks: [] }),
  fetchBookmarks: vi.fn().mockResolvedValue([]),
  addBookmark: vi.fn().mockResolvedValue([]),
  removeBookmark: vi.fn().mockResolvedValue([]),
  removeWorkspace: vi.fn().mockResolvedValue(undefined),
  fetchChatSession: vi.fn().mockResolvedValue({
    change: '', messages: [], context_files: [], usage: { total_input: 0, total_output: 0 }, created_at: '', updated_at: '',
  }),
  streamChat: vi.fn(),
  fetchChatConfig: vi.fn().mockResolvedValue({ active_provider: '', providers: {} }),
  updateChatConfig: vi.fn(),
  fetchChatProviders: vi.fn().mockResolvedValue({ active: '', providers: [] }),
  generateReport: vi.fn(),
  listReports: vi.fn().mockResolvedValue([]),
  getReport: vi.fn(),
  fetchTodos: vi.fn().mockResolvedValue({ items: [], counts: { total: 0, open: 0, inProgress: 0, done: 0, blocked: 0, dropped: 0 }, revision: 0, writable: true }),
  createTodo: vi.fn(),
  updateTodo: vi.fn(),
  deleteTodo: vi.fn(),
}))

function makeChange(overrides: Partial<ChangeSummary>): ChangeSummary {
  return {
    name: 'x', workflow: 'full', phase: 'build', archived: false,
    tasksCompleted: 0, tasksTotal: 0, verifyResult: 'pending', createdAt: '',
    artifacts: {}, visualized: false, designReviewed: false, verifyReviewed: false,
    verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
    ...overrides,
  }
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  resetWikiGraphLoad()
})

describe('App', () => {
  it('does not crash when fetchChangesWithMeta resolves with changes: null, and still renders the warning banner', async () => {
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')
    expect(screen.getByTestId('kpi-grid')).toBeTruthy()
  })


  it('excludes done and dropped Todos from a Change pending count while retaining blocked', async () => {
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce([
      { alias: 'miao', path: '/x/miao', color: '#0063f8' },
    ])
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({
      changes: [makeChange({ name: 'alpha', workspace: 'miao' })],
      failedWorkspaces: [],
    })
    vi.mocked(fetchTodos).mockResolvedValueOnce({
      items: [
        {
          id: 'open', workspace: 'miao', title: 'open', notes: '', status: 'open', priority: 'normal',
          dueAt: null, change: { workspace: 'miao', name: 'alpha' }, wikiRefs: [], metadata: { source: 'ui' },
          externalRef: null, createdAt: '', updatedAt: '', completedAt: null,
        },
        {
          id: 'blocked', workspace: 'miao', title: 'blocked', notes: '', status: 'blocked', priority: 'normal',
          dueAt: null, change: { workspace: 'miao', name: 'alpha' }, wikiRefs: [], metadata: { source: 'ui' },
          externalRef: null, createdAt: '', updatedAt: '', completedAt: null,
        },
        {
          id: 'done', workspace: 'miao', title: 'done', notes: '', status: 'done', priority: 'normal',
          dueAt: null, change: { workspace: 'miao', name: 'alpha' }, wikiRefs: [], metadata: { source: 'ui' },
          externalRef: null, createdAt: '', updatedAt: '', completedAt: '',
        },
        {
          id: 'dropped', workspace: 'miao', title: 'dropped', notes: '', status: 'dropped', priority: 'normal',
          dueAt: null, change: { workspace: 'miao', name: 'alpha' }, wikiRefs: [], metadata: { source: 'ui' },
          externalRef: null, createdAt: '', updatedAt: '', completedAt: null,
        },
      ],
      counts: { total: 4, open: 1, inProgress: 0, done: 1, blocked: 1, dropped: 1 },
      revision: 4,
      writable: true,
    })

    render(<App />)
    fireEvent.click(await screen.findByText('alpha'))

    expect(await screen.findByRole('button', { name: '待办 2' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: '待办 3' })).toBeNull()
  })

  it('cancels workspace removal without calling the API', async () => {
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce([
      { alias: 'miao', path: '/x/miao', color: '#0063f8' },
    ])
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({
      changes: [makeChange({ name: 'alpha', workspace: 'miao' })],
      failedWorkspaces: ['broken-ws'],
    })

    render(<App />)
    await screen.findByRole('button', { name: '移除 workspace broken-ws' })

    fireEvent.click(screen.getByRole('button', { name: '移除 workspace broken-ws' }))
    await screen.findByRole('dialog', { name: '移除工作区' })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '移除工作区' })).toBeNull())
    expect(removeWorkspace).not.toHaveBeenCalled()
  })

  it('removes a workspace from the chips, refreshes data, and keeps the app stable when activeWorkspace is null', async () => {
    vi.mocked(fetchWorkspaces)
      .mockResolvedValueOnce([
        { alias: 'openspec', path: '/x/open', color: '#0063f8' },
        { alias: 'ideas', path: '/x/ideas', color: '#16a34a' },
      ])
      .mockResolvedValueOnce([
        { alias: 'ideas', path: '/x/ideas', color: '#16a34a' },
      ])
    vi.mocked(fetchChangesWithMeta)
      .mockResolvedValueOnce({
        changes: [
          makeChange({ name: 'cache', workspace: 'openspec' }),
          makeChange({ name: 'cache', workspace: 'ideas' }),
        ],
        failedWorkspaces: [],
      })
      .mockResolvedValueOnce({
        changes: [makeChange({ name: 'cache', workspace: 'ideas' })],
        failedWorkspaces: [],
      })

    render(<App />)
    await screen.findByText('openspec')
    const changesRefreshCount = vi.mocked(fetchChangesWithMeta).mock.calls.length
    const workspaceRefreshCount = vi.mocked(fetchWorkspaces).mock.calls.length
    const wikiRefreshCount = vi.mocked(fetchWikiIndex).mock.calls.length

    fireEvent.click(screen.getByRole('button', { name: '移除 workspace openspec' }))
    await screen.findByRole('dialog', { name: '移除工作区' })
    fireEvent.click(screen.getByTestId('confirm-remove-workspace'))

    await waitFor(() => expect(removeWorkspace).toHaveBeenCalledWith('openspec'))
    await waitFor(() => expect(screen.queryByText('openspec')).toBeNull())
    expect(vi.mocked(fetchChangesWithMeta).mock.calls.length).toBe(changesRefreshCount + 1)
    expect(vi.mocked(fetchWorkspaces).mock.calls.length).toBe(workspaceRefreshCount + 1)
    expect(vi.mocked(fetchWikiIndex).mock.calls.length).toBe(wikiRefreshCount + 1)
    expect(screen.getByText('ideas')).toBeTruthy()
    expect(screen.getByText('cache')).toBeTruthy()
  })

  it('shows a visible error when workspace removal fails', async () => {
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({
      changes: [makeChange({ name: 'alpha', workspace: 'miao' })],
      failedWorkspaces: ['broken-ws'],
    })
    vi.mocked(removeWorkspace).mockRejectedValueOnce(new Error('移除失败：权限不足'))

    render(<App />)
    await screen.findByRole('button', { name: '移除 workspace broken-ws' })

    fireEvent.click(screen.getByRole('button', { name: '移除 workspace broken-ws' }))
    fireEvent.click(await screen.findByTestId('confirm-remove-workspace'))

    await waitFor(() =>
      expect(screen.getByTestId('remove-workspace-error').textContent).toContain('移除失败：权限不足'),
    )
    expect(screen.getByRole('dialog', { name: '移除工作区' })).toBeTruthy()
  })

  it('narrows the visible change list via KPI-card filter, combined (AND) with the workspace filter', async () => {
    const workspaces: WorkspaceConfig[] = [
      { alias: 'ws1', path: '/a', color: '#0063f8' },
      { alias: 'ws2', path: '/b', color: '#16a34a' },
    ]
    const changes = [
      makeChange({ name: 'alpha', archived: false, workspace: 'ws1' }),
      makeChange({ name: 'beta', archived: true, workspace: 'ws1' }),
      makeChange({ name: 'gamma', archived: true, workspace: 'ws2' }),
      makeChange({ name: 'delta', archived: false, workspace: 'ws2' }),
    ]
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce(workspaces)
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({ changes, failedWorkspaces: [] })

    render(<App />)
    await screen.findByText('alpha')
    expect(screen.getByText('beta')).toBeTruthy()
    expect(screen.getByText('gamma')).toBeTruthy()
    expect(screen.getByText('delta')).toBeTruthy()
    expect(screen.getByTestId('kpi-active').textContent).toContain('2') // alpha, delta

    // Click the "已归档" KPI card: narrows the change list to archived-only.
    fireEvent.click(screen.getByTestId('kpi-archived'))
    expect(screen.queryByText('alpha')).toBeNull()
    expect(screen.getByText('beta')).toBeTruthy()
    expect(screen.getByText('gamma')).toBeTruthy()
    expect(screen.queryByText('delta')).toBeNull()
    // Selecting a KPI filter must not distort the OTHER cards' own counts.
    expect(screen.getByTestId('kpi-active').textContent).toContain('2')
    expect(screen.getByTestId('kpi-archived').textContent).toContain('2')

    // Combine with the workspace filter (AND semantics): only ws2 + archived remains.
    fireEvent.click(screen.getByText('ws2'))
    expect(screen.queryByText('beta')).toBeNull()
    expect(screen.getByText('gamma')).toBeTruthy()
  })

  it('selects duplicate change names by workspace-qualified identity', async () => {
    const workspaces: WorkspaceConfig[] = [
      { alias: 'openspec', path: '/x/open', color: '#0063f8', type: 'openspec' },
      { alias: 'ideas', path: '/x/ideas', color: '#16a34a', type: 'superpowers' },
    ]
    const changes = [
      makeChange({ name: 'cache', title: 'OpenSpec Cache', workspace: 'openspec', sourceType: 'openspec' }),
      makeChange({ name: 'cache', title: 'Superpowers Cache', workspace: 'ideas', sourceType: 'superpowers' }),
    ]
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce(workspaces)
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({ changes, failedWorkspaces: [] })

    render(<App />)
    fireEvent.click(await screen.findByText('Superpowers Cache'))

    await waitFor(() => expect(fetchChangeDetail).toHaveBeenCalledWith('cache', 'ideas'))
  })


  it('remounts ChatBubble per viewed change artifact so switching documents does not bleed chat history', async () => {
    const changes = [
      makeChange({ name: 'alpha' }),
      makeChange({ name: 'beta' }),
    ]
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce([])
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({ changes, failedWorkspaces: [] })
    vi.mocked(fetchChangeDetail).mockImplementation(async (name: string) => ({
      name, workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 0, tasksTotal: 0, verifyResult: 'pending', createdAt: '',
      phases: [{
        key: 'design',
        label: '设计',
        status: 'done',
        artifacts: [{
          file: `${name}.md`,
          label: `${name} doc`,
          exists: true,
          path: `/x/${name}.md`,
        }],
      }],
    }))
    vi.mocked(fetchChatSession).mockImplementation(async (change: string) => ({
      change,
      messages:
        change === 'alpha'
          ? [{ role: 'user', content: [{ type: 'text', text: 'alpha 的历史消息' }] }]
          : [],
      context_files: [],
      usage: { total_input: 0, total_output: 0 },
      created_at: '',
      updated_at: '',
    }))

    render(<App />)
    await screen.findByText('alpha')

    fireEvent.click(screen.getByText('alpha'))
    fireEvent.click(await screen.findByText('alpha doc'))
    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('alpha'))
    fireEvent.click(screen.getByTestId('chat-bubble-button'))
    await waitFor(() =>
      expect(screen.getByTestId('chat-messages').textContent).toContain('alpha 的历史消息'),
    )

    fireEvent.click(screen.getByText('beta'))
    expect(screen.queryByTestId('chat-bubble-button')).toBeNull()
    fireEvent.click(await screen.findByText('beta doc'))
    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('beta'))
    // Freshly-mounted ChatBubble for beta starts collapsed again (state was
    // reset), and once opened must NOT show alpha's leaked history.
    fireEvent.click(screen.getByTestId('chat-bubble-button'))
    expect(screen.getByTestId('chat-messages').textContent).not.toContain('alpha 的历史消息')
  })

  it('opening an artifact keeps the change list visible and mounted (persistent 2-pane, not a fullscreen overlay)', async () => {
    const changes = [makeChange({ name: 'alpha' }), makeChange({ name: 'beta' })]
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce([])
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({ changes, failedWorkspaces: [] })
    vi.mocked(fetchChangeDetail).mockResolvedValue({
      name: 'alpha', workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 0, tasksTotal: 0, verifyResult: 'pending', createdAt: '',
      phases: [
        {
          key: 'design',
          label: '设计',
          status: 'done',
          artifacts: [{ file: 'design.md', label: '设计文档', exists: true, path: '/x/alpha/design.md' }],
        },
      ],
    })

    render(<App />)
    await screen.findByText('alpha')

    fireEvent.click(screen.getByText('alpha'))
    const artifactButton = await screen.findByText('设计文档')
    fireEvent.click(artifactButton)

    // The MarkdownViewer panel is showing (its close button is present)...
    await screen.findByText('✕ 关闭')
    // ...but the change list is still mounted and visible alongside it, not
    // hidden behind a fullscreen overlay.
    expect(screen.getByText('alpha')).toBeTruthy()
    expect(screen.getByText('beta')).toBeTruthy()

    fireEvent.click(screen.getByText('✕ 关闭'))
    expect(screen.queryByText('✕ 关闭')).toBeNull()
    await waitFor(() => {})
  })

  it('starring a doc in MarkdownViewer adds it to the bookmark panel, and clicking it there reopens it', async () => {
    const changes = [makeChange({ name: 'alpha' })]
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce([])
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({ changes, failedWorkspaces: [] })
    vi.mocked(fetchChangeDetail).mockResolvedValue({
      name: 'alpha', workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 0, tasksTotal: 0, verifyResult: 'pending', createdAt: '',
      phases: [
        {
          key: 'design',
          label: '设计',
          status: 'done',
          artifacts: [{ file: 'design.md', label: '设计文档', exists: true, path: '/x/alpha/design.md' }],
        },
      ],
    })
    vi.mocked(fetchBookmarks).mockResolvedValueOnce([])
    const bookmark = { path: '/x/alpha/design.md', title: 'design.md', type: 'md', starredAt: '2026-01-01T00:00:00Z' }
    vi.mocked(addBookmark).mockResolvedValueOnce([bookmark])

    render(<App />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByText('alpha'))
    const artifactButton = await screen.findByText('设计文档')
    fireEvent.click(artifactButton)
    await screen.findByText('✕ 关闭')

    fireEvent.click(screen.getAllByRole('button', { name: '收藏' })[0])
    await waitFor(() =>
      expect(addBookmark).toHaveBeenCalledWith({ path: '/x/alpha/design.md', title: 'design.md', type: 'md' }),
    )

    fireEvent.click(screen.getByText('✕ 关闭'))

    fireEvent.click(screen.getAllByRole('button', { name: '收藏夹' })[0])
    await screen.findByTestId('bookmark-panel')
    fireEvent.click(screen.getByText('design.md'))

    await waitFor(() => expect(screen.queryByTestId('bookmark-panel')).toBeNull())
    await screen.findByText('✕ 关闭')
  })
})

describe('App view switcher', () => {
  it('defaults to the 变更列表 view showing KpiCards and ChangeExplorer', async () => {
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')
    expect(screen.getByTestId('kpi-grid')).toBeTruthy()
    expect(screen.queryByTestId('wiki-graph-canvas')).toBeNull()
  })

  it('shows a friendly empty-state guiding the user to pick a change when none is selected', async () => {
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')
    expect(screen.getByTestId('change-empty-state')).toBeTruthy()
    expect(screen.getByText('从左侧选择一个变更查看详情')).toBeTruthy()
  })

  it('switches to the 图谱 view and mounts WikiGraph', async () => {
    const nonEmptyIndex = [
      { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
    ]
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce(nonEmptyIndex).mockResolvedValueOnce(nonEmptyIndex)
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: '知识图谱' }))

    await waitFor(() => expect(fetchWikiIndex).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByTestId('wiki-graph-canvas')).toBeTruthy())
    // 变更列表-only content is no longer mounted.
    expect(screen.queryByTestId('kpi-grid')).toBeNull()
  })


  it('shows a visible suspense fallback before the lazy graph view resolves', async () => {
    const nonEmptyIndex = [
      { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
    ]
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce(nonEmptyIndex)
    suspendWikiGraphLoad()

    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: '知识图谱' }))

    expect(await screen.findByTestId('lazy-view-fallback')).toBeTruthy()
    await act(async () => {
      releaseWikiGraph?.()
    })
    await screen.findByTestId('wiki-graph-canvas')
  })
  it('opens a document from the timeline view through the shared viewer flow', async () => {
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')
    fireEvent.click(screen.getByRole('button', { name: '时间线' }))
    fireEvent.click(await screen.findByText('打开时间线文档'))
    await screen.findByText('✕ 关闭')
  })

  it('opens SessionDetail instead of MarkdownViewer for session graph nodes, and session-linked docs still open in the shared viewer', async () => {
    const nonEmptyIndex = [
      { id: '/tmp/omp/session.jsonl', type: 'session', title: 'Agent session', path: '/tmp/omp/session.jsonl', workspace: 'miao' },
      { id: '/x/from-session.md', type: 'spec', title: 'Session doc', path: '/x/from-session.md', workspace: 'miao' },
    ]
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce(nonEmptyIndex).mockResolvedValueOnce(nonEmptyIndex)

    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: '知识图谱' }))
    fireEvent.click(await screen.findByTestId('wiki-graph-canvas'))

    await screen.findByTestId('session-detail')
    expect(screen.queryByTestId('markdown-viewer')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: '打开文档' }))
    await screen.findByTestId('markdown-viewer')
    expect(screen.queryByTestId('session-detail')).toBeNull()
  })

  it('switches to the Lint view and mounts LintPanel', async () => {
    vi.mocked(fetchLintIssues).mockResolvedValueOnce([
      { rule: 'orphan', componentId: '/x/a.md', detail: '孤立组件' },
    ])
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: '文档健康' }))

    await waitFor(() => expect(fetchLintIssues).toHaveBeenCalled())
    await screen.findByText(/orphan/)
    expect(screen.queryByTestId('kpi-grid')).toBeNull()
  })

  it('switches to the 最近 view and mounts RecentPanel', async () => {
    vi.mocked(fetchRecent).mockResolvedValueOnce([
      { id: '/x/a.md', title: 'A doc', type: 'spec', workspace: 'ws', updatedAt: new Date().toISOString(), path: '/x/a.md' },
    ])
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: '最近更新' }))

    await waitFor(() => expect(fetchRecent).toHaveBeenCalled())
    await screen.findByText('A doc')
    expect(screen.queryByTestId('kpi-grid')).toBeNull()
  })

  it('shows ChatBubble only while a standalone recent document is open', async () => {
    vi.mocked(fetchRecent).mockResolvedValueOnce([
      { id: '/x/a.md', title: 'A doc', type: 'spec', workspace: 'ws', updatedAt: new Date().toISOString(), path: '/x/a.md' },
    ])
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: '最近更新' }))
    const recentDoc = await screen.findByText('A doc')
    expect(screen.queryByTestId('chat-bubble-button')).toBeNull()

    fireEvent.click(recentDoc)
    await screen.findByText('✕ 关闭')
    expect(screen.getByTestId('chat-bubble-button')).toBeTruthy()
    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('a.md'))

    fireEvent.click(screen.getByText('✕ 关闭'))
    expect(screen.queryByTestId('chat-bubble-button')).toBeNull()
    await screen.findByText('暂无最近变更')
  })


  it('maps Ctrl+1…9 to the same view order as the side rail, including Todo, Report and Agent 会话', async () => {
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.keyDown(document, { key: '2', ctrlKey: true })
    expect(screen.getByRole('button', { name: '待办' }).getAttribute('aria-current')).toBe('page')

    fireEvent.keyDown(document, { key: '8', ctrlKey: true })
    expect(screen.getByRole('button', { name: '报告' }).getAttribute('aria-current')).toBe('page')

    fireEvent.keyDown(document, { key: '9', ctrlKey: true })
    expect(screen.getByRole('button', { name: 'Agent 会话' }).getAttribute('aria-current')).toBe('page')

    fireEvent.keyDown(document, { key: '1', ctrlKey: true })
    expect(screen.getByRole('button', { name: '变更仪表盘' }).getAttribute('aria-current')).toBe('page')
  })

  it('switches to the Agent 会话 view, mounts the panel, and opens a session in SessionDetail', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [{
        id: 'sess-live', path: '/tmp/omp/session.jsonl', workspace: 'ws', title: '会话面板条目',
        cwd: '/repo', startedAt: '2026-08-03T01:00:00.000Z', updatedAt: '2026-08-03T02:00:00.000Z',
        userTurns: 2, toolCalls: { read: 3 }, writes: [], edits: ['/x/a.md'], reads: ['/x/b.md'], intents: ['查一下'],
      }],
    })
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: 'Agent 会话' }))

    // The panel reads sessions itself; the shared viewer then resolves the
    // clicked transcript to SessionDetail rather than the Markdown viewer.
    fireEvent.click(await screen.findByText('会话面板条目'))
    expect(await screen.findByTestId('session-detail')).toBeTruthy()
    expect(screen.queryByTestId('markdown-viewer')).toBeNull()
  })

  it('switching back to 变更列表 restores KpiCards and ChangeExplorer', async () => {
    render(<App />)
    await screen.findByTestId('workspace-warning-banner')

    fireEvent.click(screen.getByRole('button', { name: '知识图谱' }))
    await waitFor(() => expect(fetchWikiIndex).toHaveBeenCalled())

    fireEvent.click(screen.getByRole('button', { name: '变更仪表盘' }))
    expect(screen.getByTestId('kpi-grid')).toBeTruthy()
    expect(screen.queryByTestId('wiki-graph-canvas')).toBeNull()
  })

  it('switching view via SideRail closes an open MarkdownViewer', async () => {
    const changes = [makeChange({ name: 'alpha' })]
    vi.mocked(fetchWorkspaces).mockResolvedValueOnce([])
    vi.mocked(fetchChangesWithMeta).mockResolvedValueOnce({ changes, failedWorkspaces: [] })
    vi.mocked(fetchChangeDetail).mockResolvedValue({
      name: 'alpha', workflow: 'full', phase: 'build', archived: false,
      tasksCompleted: 0, tasksTotal: 0, verifyResult: 'pending', createdAt: '',
      phases: [
        {
          key: 'design',
          label: '设计',
          status: 'done',
          artifacts: [{ file: 'design.md', label: '设计文档', exists: true, path: '/x/alpha/design.md' }],
        },
      ],
    })

    render(<App />)
    await screen.findByText('alpha')

    fireEvent.click(screen.getByText('alpha'))
    const artifactButton = await screen.findByText('设计文档')
    fireEvent.click(artifactButton)
    await screen.findByText('✕ 关闭')

    fireEvent.click(screen.getByRole('button', { name: '知识图谱' }))

    // The viewer must be gone immediately on view switch, not just hidden
    // behind the newly-mounted 图谱 view.
    expect(screen.queryByText('✕ 关闭')).toBeNull()
  })

  it('shows a temporary indexing-status banner while watcher updates the search index', async () => {
    class MockEventSource {
      static instance: MockEventSource | null = null
      listeners: Record<string, Array<(event?: { data?: string }) => void>> = {}
      constructor(public url: string) {
        MockEventSource.instance = this
      }
      addEventListener(type: string, cb: (event?: { data?: string }) => void) {
        ;(this.listeners[type] ??= []).push(cb)
      }
      close() {}
      emit(type: string, data?: string) {
        for (const cb of this.listeners[type] ?? []) cb({ data })
      }
    }
    vi.stubGlobal('EventSource', MockEventSource)

    render(<App />)
    await screen.findByTestId('workspace-warning-banner')
    vi.useFakeTimers()
    expect(screen.queryByTestId('wiki-indexing-banner')).toBeNull()

    await act(async () => {
      MockEventSource.instance!.emit('indexing-started', '{"changed":2}')
    })
    expect(screen.getByTestId('wiki-indexing-banner').textContent).toContain('检测到 2 个文件更新')

    await act(async () => {
      MockEventSource.instance!.emit('graph-updated', '{"changed":2}')
    })
    expect(screen.queryByTestId('wiki-indexing-banner')).toBeNull()

    await act(async () => {
      MockEventSource.instance!.emit('indexing-started', '{"changed":1}')
    })
    await act(async () => {
      vi.advanceTimersByTime(9000)
    })
    expect(screen.queryByTestId('wiki-indexing-banner')).toBeNull()
  })
})
