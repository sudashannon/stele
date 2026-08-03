import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchSessionsWithMeta, refreshSessions } from '../api/client'
import type { WikiSession } from '../api/types'
import { SessionsPanel } from './SessionsPanel'

vi.mock('../api/client', () => ({
  fetchSessionsWithMeta: vi.fn(),
  refreshSessions: vi.fn(),
}))

let sessionsUpdated: (() => void) | undefined
vi.mock('../hooks/useWikiEvents', () => ({
  useWikiEvents: (handlers: { onSessionsUpdated?: () => void }) => {
    sessionsUpdated = handlers.onSessionsUpdated
  },
}))

function session(overrides: Partial<WikiSession> = {}): WikiSession {
  return {
    id: 'sess-1',
    path: '/home/u/.omp/agent/sessions/-repo/a.jsonl',
    workspace: 'rx101',
    title: '分析 PCIe 数据路径',
    cwd: '/repo',
    startedAt: '2026-08-03T01:00:00.000Z',
    updatedAt: '2026-08-03T02:00:00.000Z',
    userTurns: 3,
    toolCalls: { read: 7, edit: 2, bash: 1 },
    writes: ['/repo/knowledge/a.md'],
    edits: ['/repo/docs/design.md'],
    reads: ['/repo/docs/plan.md'],
    intents: ['读取设计', '跑一次基准'],
    ...overrides,
  }
}

beforeEach(() => {
  sessionsUpdated = undefined
  vi.mocked(fetchSessionsWithMeta).mockReset()
  vi.mocked(refreshSessions).mockReset()
})

describe('SessionsPanel', () => {
  it('lists sessions newest first with what each one touched', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [
        session({ id: 'old', title: '旧会话', updatedAt: '2026-08-01T02:00:00.000Z' }),
        session({ id: 'new', title: '新会话', updatedAt: '2026-08-03T02:00:00.000Z' }),
      ],
    })
    render(<SessionsPanel />)

    const titles = await waitFor(() => {
      const found = screen.getAllByRole('button').map((b) => b.textContent ?? '').filter((t) => t.includes('会话'))
      expect(found.length).toBeGreaterThan(1)
      return found
    })
    expect(titles[0]).toContain('新会话')
    expect(titles[1]).toContain('旧会话')

    // writes + edits are one actionable number; reads stay separate.
    expect(screen.getAllByTestId('sessions-produced-badge')[0].textContent).toContain('2')
    expect(screen.getByTestId('sessions-summary').textContent).toContain('2')
  })

  it('separates a disabled layer from an enabled but empty one', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({ enabled: false, sessions: [] })
    const { unmount } = render(<SessionsPanel />)
    expect(await screen.findByText(/会话记忆层未启用/)).toBeTruthy()
    unmount()

    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({ enabled: true, sessions: [] })
    render(<SessionsPanel />)
    expect(await screen.findByText(/暂无已索引的会话/)).toBeTruthy()
  })

  it('filters by workspace, by free text over intents and paths, and by having produced something', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [
        session({ id: 'a', title: '甲会话', workspace: 'rx101', intents: ['跑基准'], writes: [], edits: [], reads: ['/repo/x.md'] }),
        session({ id: 'b', title: '乙会话', workspace: 'lz100', intents: ['写交接'], writes: ['/repo/handoff.md'], edits: [], reads: [] }),
      ],
    })
    render(<SessionsPanel />)
    await screen.findByText('甲会话')

    fireEvent.change(screen.getByLabelText('按工作区筛选'), { target: { value: 'lz100' } })
    expect(screen.queryByText('甲会话')).toBeNull()
    expect(screen.getByText('乙会话')).toBeTruthy()

    fireEvent.click(screen.getByText('清除筛选'))
    fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: '基准' } })
    expect(screen.getByText('甲会话')).toBeTruthy()
    expect(screen.queryByText('乙会话')).toBeNull()

    // Paths are searchable too - people remember the file, not the title.
    fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: 'handoff.md' } })
    expect(screen.getByText('乙会话')).toBeTruthy()
    expect(screen.queryByText('甲会话')).toBeNull()

    fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: '' } })
    fireEvent.click(screen.getByLabelText(/仅有产出或改动/, { selector: 'input' }))
    expect(screen.getByText('乙会话')).toBeTruthy()
    expect(screen.queryByText('甲会话')).toBeNull()
  })

  it('reports when a filter matches nothing instead of showing a blank list', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({ enabled: true, sessions: [session()] })
    render(<SessionsPanel />)
    await screen.findByText('分析 PCIe 数据路径')

    fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: '不存在的东西' } })
    expect(screen.getByText('没有匹配的会话。')).toBeTruthy()
    expect(screen.queryByText(/暂无已索引的会话/)).toBeNull()
  })

  it('opens the clicked session by transcript path', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({ enabled: true, sessions: [session()] })
    const onOpen = vi.fn()
    render(<SessionsPanel onOpen={onOpen} />)

    fireEvent.click(await screen.findByText('分析 PCIe 数据路径'))
    expect(onOpen).toHaveBeenCalledWith('/home/u/.omp/agent/sessions/-repo/a.jsonl')
  })

  it('rescans on demand and reports the outcome', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({ enabled: true, sessions: [session()] })
    vi.mocked(refreshSessions).mockResolvedValue({ changed: 2, sessions: 14 })
    render(<SessionsPanel />)
    await screen.findByText('分析 PCIe 数据路径')

    fireEvent.click(screen.getByTestId('sessions-refresh'))
    await waitFor(() => expect(refreshSessions).toHaveBeenCalledTimes(1))
    expect((await screen.findByRole('status')).textContent).toContain('已更新 2 个会话')
    // A rescan must re-read the list, not just report a count.
    expect(vi.mocked(fetchSessionsWithMeta).mock.calls.length).toBeGreaterThan(1)
  })

  it('surfaces a rescan failure with the server message', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({ enabled: true, sessions: [session()] })
    vi.mocked(refreshSessions).mockRejectedValue(new Error('session layer is not enabled'))
    render(<SessionsPanel />)
    await screen.findByText('分析 PCIe 数据路径')

    fireEvent.click(screen.getByTestId('sessions-refresh'))
    expect((await screen.findByRole('alert')).textContent).toContain('session layer is not enabled')
  })

  it('adopts a live session update in place, keeping the active filter', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({ enabled: true, sessions: [session({ title: '甲会话' })] })
    render(<SessionsPanel />)
    await screen.findByText('甲会话')

    fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: 'PCIe' } })
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [session({ title: '甲会话' }), session({ id: 'sess-2', title: 'PCIe 后续', path: '/x/b.jsonl' })],
    })

    sessionsUpdated?.()

    expect(await screen.findByText('PCIe 后续')).toBeTruthy()
    // The filter survives the refresh; the non-matching session stays hidden.
    expect(screen.queryByText('甲会话')).toBeNull()
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('reports a load failure', async () => {
    vi.mocked(fetchSessionsWithMeta).mockRejectedValue(new Error('fetchSessions failed: 500'))
    render(<SessionsPanel />)
    expect((await screen.findByRole('alert')).textContent).toContain('fetchSessions failed: 500')
  })

  // The runtime control is data-driven: with one agent runtime it would be
  // noise, so it only appears once a second runtime has produced sessions.
  it('hides the runtime filter until more than one runtime has sessions', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [
        session({ id: 'a', title: '单一运行时甲', source: 'omp' }),
        session({ id: 'b', title: '单一运行时乙', source: 'omp', path: '/x/b.jsonl' }),
      ],
    })
    const { unmount } = render(<SessionsPanel />)
    await screen.findByText('单一运行时甲')
    expect(screen.queryByLabelText('按 agent 运行时筛选')).toBeNull()
    unmount()

    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [
        session({ id: 'a', title: 'omp 会话', source: 'omp' }),
        session({ id: 'b', title: '别家会话', source: 'other', path: '/x/b.jsonl' }),
      ],
    })
    render(<SessionsPanel />)
    await screen.findByText('omp 会话')

    const select = screen.getByLabelText('按 agent 运行时筛选')
    fireEvent.change(select, { target: { value: 'other' } })
    expect(screen.getByText('别家会话')).toBeTruthy()
    expect(screen.queryByText('omp 会话')).toBeNull()

    fireEvent.click(screen.getByText('清除筛选'))
    expect(screen.getByText('omp 会话')).toBeTruthy()
  })

  // Folded subagent work is aggregated into the parent's numbers, so the row
  // has to say how many transcripts those numbers came from.
  it('marks how many subagents were folded into a session', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [
        session({ id: 'with', subagents: ['Reviewer', 'PlanWriter'] }),
        session({ id: 'without', title: '独立会话', path: '/x/b.jsonl' }),
      ],
    })
    render(<SessionsPanel />)
    await screen.findByText('独立会话')

    const badges = screen.getAllByTestId('sessions-subagent-badge')
    expect(badges).toHaveLength(1)
    expect(badges[0].textContent).toContain('2')
  })

  // Unfinished tasks are the actionable residue of a session; the panel has to
  // make them findable rather than requiring a click into every card.
  it('filters and counts sessions that ended with open tasks', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [
        session({ id: 'open', title: '留有未完成', todoOpen: 3, todoTotal: 8, todoDone: 5 }),
        session({ id: 'clean', title: '全部完成', path: '/x/b.jsonl', todoOpen: 0, todoTotal: 4, todoDone: 4 }),
      ],
    })
    render(<SessionsPanel />)
    await screen.findByText('留有未完成')

    expect(screen.getByTestId('sessions-unfinished-count').textContent).toContain('1 个会话留有未完成任务')
    const badges = screen.getAllByTestId('sessions-open-badge')
    expect(badges).toHaveLength(1)
    expect(badges[0].textContent).toContain('3')

    fireEvent.click(screen.getByLabelText('仅未完成清单'))
    expect(screen.getByText('留有未完成')).toBeTruthy()
    expect(screen.queryByText('全部完成')).toBeNull()

    fireEvent.click(screen.getByText('清除筛选'))
    expect(screen.getByText('全部完成')).toBeTruthy()
  })

  // A resumed session's span is not its duration, so the row reports the number
  // of days that actually saw work.
  it('reports active days for a session that spanned several', async () => {
    vi.mocked(fetchSessionsWithMeta).mockResolvedValue({
      enabled: true,
      sessions: [
        session({ id: 'multi', title: '跨天会话', activity: { '2026-07-30': 12, '2026-08-01': 5, '2026-08-03': 40 } }),
        session({ id: 'single', title: '单天会话', path: '/x/b.jsonl', activity: { '2026-08-03': 7 } }),
      ],
    })
    render(<SessionsPanel />)
    await screen.findByText('跨天会话')

    expect(screen.getByText('活跃 3 天')).toBeTruthy()
    // One day is the unremarkable case and stays out of the row.
    expect(screen.queryByText('活跃 1 天')).toBeNull()
  })
})
