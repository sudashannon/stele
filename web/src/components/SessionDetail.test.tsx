import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SessionDetail } from './SessionDetail'

afterEach(() => vi.restoreAllMocks())

describe('SessionDetail', () => {
  it('renders fetched session data, shows truncated intents hint, and opens documents from grouped lists', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/agent.jsonl',
        path: '/tmp/agent.jsonl',
        workspace: 'miao',
        title: '排查图谱会话',
        startedAt: '2026-07-30T08:00:00Z',
        updatedAt: '2026-07-30T09:30:00Z',
        userTurns: 6,
        toolCalls: { read: 7, edit: 2, bash: 1 },
        reads: ['/docs/a.md', '/docs/b.md'],
        writes: ['/docs/new.md'],
        edits: ['/docs/c.md'],
        intents: ['梳理节点', '检查边'],
        intentsTruncated: true,
      }),
    } as Response)
    const onOpenDocument = vi.fn()

    render(<SessionDetail sessionId="/tmp/agent.jsonl" onOpenDocument={onOpenDocument} onClose={vi.fn()} />)

    expect(screen.getByRole('status').textContent).toContain('正在加载会话')
    await waitFor(() => expect(screen.getByText('排查图谱会话')).toBeTruthy())
    expect(screen.getByText('miao')).toBeTruthy()
    expect(screen.getByText(/用户轮次：6/)).toBeTruthy()
    expect(screen.getByText('read')).toBeTruthy()
    expect(screen.getByText('仅显示最近若干条意图')).toBeTruthy()

    // Produced (`write`) documents read differently from patched (`edit`) ones,
    // so the card must not merge them into a single list.
    expect(screen.getByText('产出文档')).toBeTruthy()
    expect(screen.getByText('改动文档')).toBeTruthy()
    expect(screen.getByText('读取文档')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '/docs/new.md' }))
    expect(onOpenDocument).toHaveBeenCalledWith('/docs/new.md')

    fireEvent.click(screen.getByRole('button', { name: '/docs/c.md' }))
    expect(onOpenDocument).toHaveBeenCalledWith('/docs/c.md')
  })

  it('shows an explicit not-found state for 404s', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 404 } as Response)

    render(<SessionDetail sessionId="/tmp/missing.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('未找到该会话'))
  })

  it('shows a generic error state for non-404 failures', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)

    render(<SessionDetail sessionId="/tmp/error.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('会话加载失败，请稍后重试'))
  })

  // Totals in this card can be an aggregate of several transcripts, so the
  // provenance - which runtime, how many folded subagents - has to be visible.
  it('shows the agent runtime and how many subagents were folded in', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/agent.jsonl',
        path: '/tmp/agent.jsonl',
        workspace: 'miao',
        source: 'omp',
        title: '带子代理的会话',
        startedAt: '2026-07-30T08:00:00Z',
        updatedAt: '2026-07-30T09:30:00Z',
        userTurns: 2,
        toolCalls: { read: 3 },
        reads: ['/docs/a.md'],
        writes: [],
        edits: [],
        intents: [],
        subagents: ['Reviewer', 'PlanWriter'],
      }),
    } as Response)

    render(<SessionDetail sessionId="/tmp/agent.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByText('带子代理的会话')).toBeTruthy())
    expect(screen.getByTestId('session-source').textContent).toBe('omp')
    const folded = screen.getByTestId('session-subagents')
    expect(folded.textContent).toContain('2')
    expect(folded.getAttribute('title')).toBe('Reviewer、PlanWriter')
  })

  it('omits the provenance line for a session without subagents', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/solo.jsonl', path: '/tmp/solo.jsonl', workspace: 'miao', title: '独立会话',
        startedAt: '2026-07-30T08:00:00Z', updatedAt: '2026-07-30T08:30:00Z', userTurns: 1,
        toolCalls: {}, reads: [], writes: [], edits: [], intents: [],
      }),
    } as Response)

    render(<SessionDetail sessionId="/tmp/solo.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByText('独立会话')).toBeTruthy())
    expect(screen.queryByTestId('session-subagents')).toBeNull()
    expect(screen.queryByTestId('session-source')).toBeNull()
  })

  // The list a session ends with is not the whole story: each re-plan replaces
  // it, so the card has to show finished work too or six hours of it vanishes.
  it('renders the session task record: current phases, statuses, and earlier completions', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/agent.jsonl', path: '/tmp/agent.jsonl', workspace: 'lz100', title: '带清单的会话',
        startedAt: '2026-07-30T08:00:00Z', updatedAt: '2026-07-30T09:30:00Z', userTurns: 4,
        toolCalls: { todo: 930 }, reads: [], writes: [], edits: [], intents: [],
        todos: [
          { phase: 'Review', content: '确定 diff 范围', status: 'completed' },
          { phase: 'Review', content: '跑验证套件', status: 'pending' },
          { phase: 'Report', content: '汇报阻塞点', status: 'blocked' },
        ],
        todosCompleted: ['定位 wiki 产物', '读一手资料'],
        todoReplans: 106,
      }),
    } as Response)

    render(<SessionDetail sessionId="/tmp/agent.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('session-todos')).toBeTruthy())
    const record = screen.getByTestId('session-todos')
    // Counts separate what is still open from what is merely listed.
    expect(record.textContent).toContain('当前 3 项（未完成 2）')
    expect(record.textContent).toContain('历史完成 2 项')
    expect(screen.getByTestId('session-todo-replans').textContent).toContain('106')
    // Phases group the list the way the session declared it.
    expect(screen.getByText('Review')).toBeTruthy()
    expect(screen.getByText('Report')).toBeTruthy()
    expect(screen.getByText('跑验证套件')).toBeTruthy()
    expect(record.textContent).toContain('阻塞')
    expect(screen.getByText('定位 wiki 产物')).toBeTruthy()
  })

  it('omits the task record for a session that never used the tracker', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/plain.jsonl', path: '/tmp/plain.jsonl', workspace: 'miao', title: '没有清单',
        startedAt: '2026-07-30T08:00:00Z', updatedAt: '2026-07-30T08:30:00Z', userTurns: 1,
        toolCalls: { read: 2 }, reads: [], writes: [], edits: [], intents: [],
      }),
    } as Response)

    render(<SessionDetail sessionId="/tmp/plain.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByText('没有清单')).toBeTruthy())
    expect(screen.queryByTestId('session-todos')).toBeNull()
  })

  // A session whose final list is empty still has a record worth showing.
  it('shows only the earlier completions when the final list is empty', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/cleared.jsonl', path: '/tmp/cleared.jsonl', workspace: 'miao', title: '清空过清单',
        startedAt: '2026-07-30T08:00:00Z', updatedAt: '2026-07-30T08:30:00Z', userTurns: 1,
        toolCalls: { todo: 12 }, reads: [], writes: [], edits: [], intents: [],
        todos: [], todosCompleted: ['做完的事'], todosTruncated: true,
      }),
    } as Response)

    render(<SessionDetail sessionId="/tmp/cleared.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('session-todos')).toBeTruthy())
    expect(screen.getByText('做完的事')).toBeTruthy()
    expect(screen.getByTestId('session-todos').textContent).toContain('当前 0 项')
    expect(screen.getByText('待办过多，仅显示部分记录')).toBeTruthy()
  })

  // "blocked" on its own is not actionable: the reason the session recorded is
  // the whole point of keeping the record.
  it('shows why a task is blocked and offers to import the unfinished ones', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/agent.jsonl', path: '/tmp/agent.jsonl', workspace: 'rx101', title: '带阻塞的会话',
        startedAt: '2026-07-30T08:00:00Z', updatedAt: '2026-08-03T09:30:00Z', userTurns: 9,
        toolCalls: { todo: 40 }, reads: [], writes: [], edits: [], intents: [],
        todos: [
          { phase: 'Fix', content: '恢复 Gen3 x2', status: 'blocked', blocker: '需 SI 复核 + lifecycle 门禁' },
          { phase: 'Fix', content: '跑定向测试', status: 'pending' },
          { phase: 'Fix', content: '读设计', status: 'completed' },
        ],
      }),
    } as Response)
    const onImportTodos = vi.fn().mockResolvedValue(2)

    render(<SessionDetail sessionId="/tmp/agent.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} onImportTodos={onImportTodos} />)

    await waitFor(() => expect(screen.getByTestId('session-todos')).toBeTruthy())
    expect(screen.getByTestId('session-todo-blocker').textContent).toContain('需 SI 复核 + lifecycle 门禁')

    const button = screen.getByTestId('session-todo-import')
    expect(button.textContent).toContain('导入 2 项到待办')
    fireEvent.click(button)

    await waitFor(() => expect(onImportTodos).toHaveBeenCalledTimes(1))
    // Only the unfinished tasks travel: a completed one is not work to redo.
    const [, items] = onImportTodos.mock.calls[0]
    expect(items.map((item: { content: string }) => item.content)).toEqual(['恢复 Gen3 x2', '跑定向测试'])
    expect((await screen.findByRole('status')).textContent).toContain('已导入 2 项')
    // Re-importing the same list would duplicate it.
    expect((screen.getByTestId('session-todo-import') as HTMLButtonElement).disabled).toBe(true)
  })

  it('reports an import failure instead of pretending it worked', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/a.jsonl', path: '/tmp/a.jsonl', workspace: 'rx101', title: '导入失败',
        startedAt: '2026-07-30T08:00:00Z', updatedAt: '2026-07-30T09:00:00Z', userTurns: 1,
        toolCalls: {}, reads: [], writes: [], edits: [], intents: [],
        todos: [{ content: '待做', status: 'pending' }],
      }),
    } as Response)
    const onImportTodos = vi.fn().mockRejectedValue(new Error('待办不可写'))

    render(<SessionDetail sessionId="/tmp/a.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} onImportTodos={onImportTodos} />)
    fireEvent.click(await screen.findByTestId('session-todo-import'))

    expect((await screen.findByRole('alert')).textContent).toContain('待办不可写')
  })

  // StartedAt→UpdatedAt spans idle days too, so the card shows where the work was.
  it('places a multi-day session in time and stays quiet for a single-day one', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/long.jsonl', path: '/tmp/long.jsonl', workspace: 'lz100', title: '跨天会话',
        startedAt: '2026-07-27T08:00:00Z', updatedAt: '2026-08-03T09:30:00Z', userTurns: 294,
        toolCalls: { read: 2779 }, reads: [], writes: [], edits: [], intents: [],
        activity: { '2026-07-27': 120, '2026-07-29': 40, '2026-08-03': 900 },
      }),
    } as Response)

    const { unmount } = render(<SessionDetail sessionId="/tmp/long.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByTestId('session-activity')).toBeTruthy())
    expect(screen.getByTestId('session-active-days').textContent).toContain('活跃 3 天')
    expect(screen.getByTestId('session-activity').textContent).toContain('峰值 900 次')
    unmount()

    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: '/tmp/short.jsonl', path: '/tmp/short.jsonl', workspace: 'lz100', title: '单天会话',
        startedAt: '2026-08-03T08:00:00Z', updatedAt: '2026-08-03T09:00:00Z', userTurns: 2,
        toolCalls: {}, reads: [], writes: [], edits: [], intents: [],
        activity: { '2026-08-03': 12 },
      }),
    } as Response)
    render(<SessionDetail sessionId="/tmp/short.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('单天会话')).toBeTruthy())
    expect(screen.queryByTestId('session-activity')).toBeNull()
    expect(screen.queryByTestId('session-active-days')).toBeNull()
  })
})
