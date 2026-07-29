import { renderHook } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { useWikiEvents } from './useWikiEvents'

// Minimal EventSource stand-in: jsdom does not implement EventSource (see
// useWikiEvents.ts's typeof guard), so tests exercising the "EventSource
// available" branch need their own mock. addEventListener only tracks the
// 'graph-updated' listener actually used by the hook -- other event names
// are ignored since the hook never registers them.
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
})
