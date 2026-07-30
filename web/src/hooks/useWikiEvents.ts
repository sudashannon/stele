import { useEffect, useLayoutEffect, useRef } from 'react'

type WikiEventHandlers =
  | (() => void)
  | {
      onUpdate?: () => void
      onIndexingStarted?: (changed: number | null) => void
      onTodosUpdated?: (revision: number) => void
    }

type WikiEventType = 'graph-updated' | 'indexing-started' | 'todos-updated'
type WikiEventSubscriber = (type: WikiEventType, event?: MessageEvent) => void

const subscribers = new Set<WikiEventSubscriber>()
let sharedEventSource: EventSource | null = null

function reportSubscriberError(error: unknown) {
  queueMicrotask(() => {
    const reportError = (globalThis as typeof globalThis & { reportError?: (error: unknown) => void }).reportError
    if (reportError) {
      reportError(error)
      return
    }
    console.error('Wiki event subscriber failed', error)
  })
}

function dispatch(type: WikiEventType, event?: MessageEvent) {
  for (const subscriber of subscribers) {
    try {
      subscriber(type, event)
    } catch (error) {
      reportSubscriberError(error)
    }
  }
}

function ensureEventSource() {
  if (sharedEventSource || typeof EventSource === 'undefined') return
  const source = new EventSource('/api/wiki/events')
  sharedEventSource = source
  const forward = (type: WikiEventType, event?: MessageEvent) => {
    if (sharedEventSource === source) dispatch(type, event)
  }
  source.addEventListener('graph-updated', () => forward('graph-updated'))
  source.addEventListener('indexing-started', (event) => forward('indexing-started', event as MessageEvent))
  source.addEventListener('todos-updated', (event) => forward('todos-updated', event as MessageEvent))
}

// useWikiEvents subscribes to the backend's /api/wiki/events SSE stream.
// Backward compatible form: useWikiEvents(onUpdate)
// Extended form: useWikiEvents({ onUpdate, onIndexingStarted, onTodosUpdated })
export function useWikiEvents(handlers: WikiEventHandlers) {
  const handlersRef = useRef(handlers)
  useLayoutEffect(() => {
    handlersRef.current = handlers
  }, [handlers])

  useEffect(() => {
    if (typeof EventSource === 'undefined') return
    const subscriber: WikiEventSubscriber = (type, event) => {
      const current = handlersRef.current
      if (type === 'graph-updated') {
        const callback = typeof current === 'function' ? current : current.onUpdate
        callback?.()
        return
      }
      if (typeof current === 'function') return
      if (type === 'indexing-started') {
        if (!current.onIndexingStarted) return
        let changed: number | null = null
        try {
          const payload = JSON.parse(event?.data ?? '{}')
          if (typeof payload.changed === 'number') changed = payload.changed
        } catch {}
        current.onIndexingStarted(changed)
        return
      }
      if (!current.onTodosUpdated) return
      try {
        const payload = JSON.parse(event?.data ?? '{}')
        if (typeof payload.revision === 'number') current.onTodosUpdated(payload.revision)
      } catch {}
    }

    subscribers.add(subscriber)
    ensureEventSource()
    return () => {
      subscribers.delete(subscriber)
      if (subscribers.size === 0) {
        sharedEventSource?.close()
        sharedEventSource = null
      }
    }
  }, [])
}
