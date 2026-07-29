import { useEffect } from 'react'

type WikiEventHandlers =
  | (() => void)
  | {
      onUpdate?: () => void
      onIndexingStarted?: (changed: number | null) => void
      onTodosUpdated?: (revision: number) => void
    }

// useWikiEvents subscribes to the backend's /api/wiki/events SSE stream.
// Backward compatible form: useWikiEvents(onUpdate)
// Extended form: useWikiEvents({ onUpdate, onIndexingStarted, onTodosUpdated })
export function useWikiEvents(handlers: WikiEventHandlers) {
  const onUpdate = typeof handlers === 'function' ? handlers : handlers.onUpdate
  const onIndexingStarted = typeof handlers === 'function' ? undefined : handlers.onIndexingStarted
  const onTodosUpdated = typeof handlers === 'function' ? undefined : handlers.onTodosUpdated
  useEffect(() => {
    if (typeof EventSource === 'undefined') return
    const es = new EventSource('/api/wiki/events')
    if (onUpdate) es.addEventListener('graph-updated', onUpdate)
    if (onIndexingStarted) {
      es.addEventListener('indexing-started', (event: MessageEvent) => {
        let changed: number | null = null
        try {
          const payload = JSON.parse(event.data ?? '{}')
          if (typeof payload.changed === 'number') changed = payload.changed
        } catch {}
        onIndexingStarted(changed)
      })
    }
    if (onTodosUpdated) {
      es.addEventListener('todos-updated', (event: MessageEvent) => {
        try {
          const payload = JSON.parse(event.data ?? '{}')
          if (typeof payload.revision === 'number') onTodosUpdated(payload.revision)
        } catch {}
      })
    }
    return () => es.close()
  }, [onUpdate, onIndexingStarted, onTodosUpdated])
}
