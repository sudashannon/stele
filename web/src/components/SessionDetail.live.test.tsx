import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { SessionDetail } from './SessionDetail'

function sessionPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 'sess-live',
    path: '/tmp/live.jsonl',
    workspace: 'rx101',
    title: '实时会话',
    cwd: '/repo',
    startedAt: '2026-07-31T01:00:00Z',
    updatedAt: '2026-07-31T02:00:00Z',
    userTurns: 3,
    toolCalls: { read: 2 },
    writes: [] as string[],
    edits: [] as string[],
    reads: ['/docs/a.md'],
    intents: ['第一步'],
    ...overrides,
  }
}

afterEach(() => vi.restoreAllMocks())

describe('SessionDetail live refresh', () => {
  it('refreshes in place when the backend announces new session activity', async () => {
    const listeners = new Map<string, (event: MessageEvent) => void>()
    class FakeEventSource {
      constructor(public url: string) {}
      addEventListener(type: string, handler: (event: MessageEvent) => void) {
        listeners.set(type, handler)
      }
      close() {}
    }
    vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)

    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({ ok: true, json: async () => sessionPayload() } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => sessionPayload({ intents: ['第一步', '第二步'], writes: ['/docs/new.md'] }),
      } as Response)

    render(<SessionDetail sessionId="/tmp/live.jsonl" onOpenDocument={vi.fn()} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('第一步')).toBeTruthy())
    expect(screen.queryByText('第二步')).toBeNull()

    const handler = listeners.get('sessions-updated')
    expect(handler).toBeTruthy()
    handler?.({ data: '{"changed":1}' } as MessageEvent)

    await waitFor(() => expect(screen.getByText('第二步')).toBeTruthy())
    // The refresh must not flash the card back to its loading state.
    expect(screen.queryByText('正在加载会话')).toBeNull()
    expect(screen.getByText('产出文档')).toBeTruthy()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
