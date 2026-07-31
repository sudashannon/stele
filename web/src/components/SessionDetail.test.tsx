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
})
