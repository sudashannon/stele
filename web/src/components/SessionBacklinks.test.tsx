import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { SessionBacklinks, SessionBacklinkList } from './SessionBacklinks'
import type { WikiEdge, WikiSession } from '../api/types'

const session: WikiSession = {
  id: 'sess-1',
  path: '/home/u/.omp/agent/sessions/-repo/2026-07-30T00-00-00-000Z_sess-1.jsonl',
  workspace: 'rx101',
  title: '分析 PCIe 数据路径',
  cwd: '/repo',
  startedAt: '2026-07-30T01:00:00Z',
  updatedAt: '2026-07-30T02:00:00Z',
  userTurns: 7,
  toolCalls: { read: 3, edit: 1 },
  writes: ['/repo/docs/new.md'],
  reads: ['/repo/docs/a.md'],
  edits: ['/repo/docs/b.md'],
  intents: ['读取设计'],
}

function mockComponent(backlinks: WikiEdge[]) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/api/wiki/component/')) {
      return Promise.resolve({ ok: true, json: async () => ({ component: {}, forward: [], backlinks }) } as Response)
    }
    if (url.includes('/api/wiki/sessions')) {
      return Promise.resolve({ ok: true, json: async () => ({ sessions: [session] }) } as Response)
    }
    return Promise.reject(new Error(`unexpected fetch: ${url}`))
  })
}

afterEach(() => vi.restoreAllMocks())

describe('SessionBacklinks', () => {
  it('renders the sessions that touched a document, resolved by transcript path', async () => {
    mockComponent([{ from: session.path, to: '/repo/docs/a.md', kind: 'reads', source: 'session' }])

    render(<SessionBacklinks componentId="/repo/docs/a.md" />)

    await waitFor(() => expect(screen.getByTestId('session-backlinks')).toBeTruthy())
    // Edges identify a session by transcript path, so the title must resolve
    // from `path` — keying only by runtime id would render the raw path.
    expect(screen.getByText('分析 PCIe 数据路径')).toBeTruthy()
    expect(screen.getByText('rx101')).toBeTruthy()
    expect(screen.getByText(/阅读/)).toBeTruthy()
    expect(screen.getByText(/7 轮对话/)).toBeTruthy()
  })

  it('renders nothing when the document has no session activity', async () => {
    mockComponent([{ from: '/repo/docs/c.md', to: '/repo/docs/a.md', kind: 'references', source: 'markdown-link' }])

    const { container } = render(<SessionBacklinks componentId="/repo/docs/a.md" />)

    await waitFor(() => expect(screen.queryByTestId('session-backlinks')).toBeNull())
    expect(container.textContent).toBe('')
  })

  it('stays silent when the path is not an indexed component', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('404'))

    const { container } = render(<SessionBacklinks componentId="/tmp/raw-artifact.md" />)

    await waitFor(() => expect(container.textContent).toBe(''))
  })

  it('collapses one session into a single entry listing every relationship', () => {
    render(
      <SessionBacklinkList
        edges={[
          { from: session.path, to: '/repo/docs/a.md', kind: 'reads', source: 'session' },
          { from: session.path, to: '/repo/docs/a.md', kind: 'edits', source: 'session' },
        ]}
        sessions={[session]}
      />,
    )

    expect(screen.getByText('相关会话（1 个）')).toBeTruthy()
    expect(screen.getByText(/改动 \/ 阅读/)).toBeTruthy()
  })

  it('falls back to the transcript path when the session digest is unknown', () => {
    render(
      <SessionBacklinkList
        edges={[{ from: '/unknown/session.jsonl', to: '/repo/docs/a.md', kind: 'edits', source: 'session' }]}
        sessions={[]}
      />,
    )

    expect(screen.getByText('/unknown/session.jsonl')).toBeTruthy()
  })

  it('opens a session by transcript path when a handler is provided', async () => {
    const onOpenSession = vi.fn()
    mockComponent([{ from: session.path, to: '/repo/docs/a.md', kind: 'edits', source: 'session' }])

    render(<SessionBacklinks componentId="/repo/docs/a.md" onOpenSession={onOpenSession} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /分析 PCIe 数据路径/ })).toBeTruthy())
    screen.getByRole('button', { name: /分析 PCIe 数据路径/ }).click()
    expect(onOpenSession).toHaveBeenCalledWith(session.path)
  })

  it('stays non-interactive when no handler is provided', () => {
    render(
      <SessionBacklinkList
        edges={[{ from: session.path, to: '/repo/docs/a.md', kind: 'edits', source: 'session' }]}
        sessions={[session]}
      />,
    )

    expect(screen.queryByRole('button')).toBeNull()
  })
})
