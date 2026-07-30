import { Suspense, createElement, startTransition } from 'react'
import { act, render, renderHook } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { useWikiEvents } from './useWikiEvents'

// Minimal EventSource stand-in: jsdom does not implement EventSource (see
// useWikiEvents.ts's typeof guard), so tests exercising that branch install
// this listener-tracking mock.
type EventSourceCallback = (event?: MessageEvent<unknown>) => void

class MockEventSource {
  static instances: MockEventSource[] = []
  listeners: Record<string, EventSourceCallback[]> = {}
  closed = false
  constructor(public url: string) {
    MockEventSource.instances.push(this)
  }
  addEventListener(type: string, cb: EventSourceCallback) {
    ;(this.listeners[type] ??= []).push(cb)
  }
  close() {
    this.closed = true
  }
  emit(type: string, data?: string) {
    for (const cb of this.listeners[type] ?? []) {
      if (data != null) {
        const event = new MessageEvent<unknown>('message', { data })
        cb(event)
      } else {
        cb()
      }
    }
  }
}

afterEach(() => {
  MockEventSource.instances.length = 0
  vi.unstubAllGlobals()
})

describe('useWikiEvents', () => {
  it('connects to /api/wiki/events and invokes onUpdate on a graph-updated event', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const onUpdate = vi.fn()
    renderHook(() => useWikiEvents(onUpdate))

    expect(MockEventSource.instances).toHaveLength(1)
    expect(MockEventSource.instances[0].url).toBe('/api/wiki/events')
    expect(onUpdate).not.toHaveBeenCalled()

    MockEventSource.instances[0].emit('graph-updated')
    expect(onUpdate).toHaveBeenCalledTimes(1)
  })

  it('closes the connection on unmount', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const { unmount } = renderHook(() => useWikiEvents(() => {}))
    expect(MockEventSource.instances[0].closed).toBe(false)
    unmount()
    expect(MockEventSource.instances[0].closed).toBe(true)
  })

  it('does nothing when EventSource is unavailable (e.g. jsdom default)', () => {
    vi.stubGlobal('EventSource', undefined)
    expect(() => renderHook(() => useWikiEvents(() => {}))).not.toThrow()
    expect(MockEventSource.instances).toHaveLength(0)
  })

  it('invokes onTodosUpdated with revision on a todos-updated event', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const onTodosUpdated = vi.fn()
    renderHook(() => useWikiEvents({ onTodosUpdated }))

    expect(MockEventSource.instances).toHaveLength(1)
    expect(onTodosUpdated).not.toHaveBeenCalled()

    MockEventSource.instances[0].emit('todos-updated', '{"revision":7}')
    expect(onTodosUpdated).toHaveBeenCalledTimes(1)
    expect(onTodosUpdated).toHaveBeenCalledWith(7)
  })

  it('does not invoke onTodosUpdated when the event carries a non-number revision', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const onTodosUpdated = vi.fn()
    renderHook(() => useWikiEvents({ onTodosUpdated }))

    MockEventSource.instances[0].emit('todos-updated', '{"revision":"abc"}')
    expect(onTodosUpdated).not.toHaveBeenCalled()
  })

  it('does not invoke onTodosUpdated on a malformed JSON payload', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const onTodosUpdated = vi.fn()
    renderHook(() => useWikiEvents({ onTodosUpdated }))

    MockEventSource.instances[0].emit('todos-updated', 'not-json')
    expect(onTodosUpdated).not.toHaveBeenCalled()
  })

  it('supports backward-compatible form useWikiEvents(onUpdate)', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const onUpdate = vi.fn()
    renderHook(() => useWikiEvents(onUpdate))

    MockEventSource.instances[0].emit('graph-updated')
    expect(onUpdate).toHaveBeenCalledTimes(1)
  })

  it('keeps one connection while switching to the latest callbacks', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const first = vi.fn()
    const latest = vi.fn()
    const { rerender } = renderHook(
      ({ onUpdate }) => useWikiEvents({ onUpdate }),
      { initialProps: { onUpdate: first } },
    )
    expect(MockEventSource.instances).toHaveLength(1)

    rerender({ onUpdate: latest })
    expect(MockEventSource.instances).toHaveLength(1)
    MockEventSource.instances[0].emit('graph-updated')
    expect(first).not.toHaveBeenCalled()
    expect(latest).toHaveBeenCalledTimes(1)
  })

  it('does not publish callbacks from a render that never commits', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const committed = vi.fn()
    const abandoned = vi.fn()
    const never = new Promise<never>(() => {})

    function Harness({ onUpdate, suspend }: { onUpdate: () => void; suspend: boolean }) {
      useWikiEvents(onUpdate)
      if (suspend) throw never
      return null
    }

    const { rerender } = render(
      createElement(
        Suspense,
        { fallback: null },
        createElement(Harness, { onUpdate: committed, suspend: false }),
      ),
    )
    act(() => {
      startTransition(() => {
        rerender(
          createElement(
            Suspense,
            { fallback: null },
            createElement(Harness, { onUpdate: abandoned, suspend: true }),
          ),
        )
      })
    })
    MockEventSource.instances[0].emit('graph-updated')
    expect(committed).toHaveBeenCalledTimes(1)
    expect(abandoned).not.toHaveBeenCalled()
  })

  it('ignores events delivered after the connection is closed', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const onTodosUpdated = vi.fn()
    const { unmount } = renderHook(() => useWikiEvents({ onTodosUpdated }))
    const connection = MockEventSource.instances[0]
    unmount()
    connection.emit('todos-updated', '{"revision":8}')
    expect(onTodosUpdated).not.toHaveBeenCalled()
  })

  it('continues fan-out and asynchronously reports when one subscriber throws', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const error = new Error('subscriber failed')
    const reportError = vi.fn()
    vi.stubGlobal('reportError', reportError)
    const throwingHandler = vi.fn(() => {
      throw error
    })
    const remainingHandler = vi.fn()
    renderHook(() => useWikiEvents(throwingHandler))
    renderHook(() => useWikiEvents(remainingHandler))

    expect(() => MockEventSource.instances[0].emit('graph-updated')).not.toThrow()
    expect(throwingHandler).toHaveBeenCalledTimes(1)
    expect(remainingHandler).toHaveBeenCalledTimes(1)
    expect(reportError).not.toHaveBeenCalled()

    await Promise.resolve()
    expect(reportError).toHaveBeenCalledWith(error)
  })

  it('shares one connection until the final subscriber unmounts', () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const firstHandler = vi.fn()
    const secondHandler = vi.fn()
    const first = renderHook(() => useWikiEvents(firstHandler))
    const second = renderHook(() => useWikiEvents(secondHandler))

    expect(MockEventSource.instances).toHaveLength(1)
    const connection = MockEventSource.instances[0]
    first.unmount()
    expect(connection.closed).toBe(false)

    connection.emit('graph-updated')
    expect(firstHandler).not.toHaveBeenCalled()
    expect(secondHandler).toHaveBeenCalledTimes(1)

    second.unmount()
    expect(connection.closed).toBe(true)
  })
})
