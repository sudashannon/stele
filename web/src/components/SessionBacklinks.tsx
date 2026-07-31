import { useCallback, useEffect, useState } from 'react'
import { fetchSessions, fetchWikiComponent } from '../api/client'
import type { WikiEdge, WikiSession } from '../api/types'
import { useWikiEvents } from '../hooks/useWikiEvents'

/**
 * Sessions that read or edited a document. The relationship comes from
 * session→document edges, which are excluded from the visual graph, so this is
 * the only place a reader sees "which agent runs touched this file".
 *
 * Renders nothing when the document has no session activity: an untouched
 * document should look exactly as it did before this layer existed.
 */
export function SessionBacklinks({
  componentId,
  onOpenSession,
}: {
  componentId: string
  onOpenSession?: (sessionId: string) => void
}) {
  const [edges, setEdges] = useState<WikiEdge[]>([])
  const [sessions, setSessions] = useState<WikiSession[]>([])
  const load = useCallback(() => {
    let cancelled = false
    if (!componentId) return () => {}
    fetchWikiComponent(componentId)
      .then(async (response) => {
        const sessionEdges = (response.backlinks ?? []).filter((edge) => edge.source === 'session')
        if (cancelled) return
        if (sessionEdges.length === 0) {
          setEdges([])
          return
        }
        const known = await fetchSessions().catch(() => [] as WikiSession[])
        if (cancelled) return
        setEdges(sessionEdges)
        setSessions(known)
      })
      .catch(() => {
        // A path that is not an indexed component (e.g. a raw artifact) simply
        // has no session relationships to show.
      })
    return () => {
      cancelled = true
    }
  }, [componentId])

  useEffect(() => {
    setEdges([])
    setSessions([])
    return load()
  }, [load])

  // The backend re-parses transcript tails on its own schedule; a document open
  // while an agent works on it should gain the new session without a reload.
  useWikiEvents({ onSessionsUpdated: () => load() })

  if (edges.length === 0) return null
  return <SessionBacklinkList edges={edges} sessions={sessions} onOpenSession={onOpenSession} />
}

function formatLocalTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

/**
 * Groups session edges by session. Edges identify a session by its transcript
 * path, which is the component id; the sessions API keys the same session by
 * both `path` and its runtime `id`, so both are indexed here.
 */
export function SessionBacklinkList({
  edges,
  sessions,
  onOpenSession,
}: {
  edges: WikiEdge[]
  sessions: WikiSession[]
  onOpenSession?: (sessionId: string) => void
}) {
  const byKey = new Map<string, WikiSession>()
  sessions.forEach((session) => {
    if (session.path) byKey.set(session.path, session)
    if (session.id) byKey.set(session.id, session)
  })

  const grouped = new Map<string, { session: WikiSession | null; kinds: Set<string> }>()
  edges.forEach((edge) => {
    const existing = grouped.get(edge.from)
    if (existing) {
      existing.kinds.add(edge.kind)
      return
    }
    grouped.set(edge.from, { session: byKey.get(edge.from) ?? null, kinds: new Set([edge.kind]) })
  })

  const items = [...grouped.entries()]
    .map(([id, value]) => ({ id, session: value.session, kinds: [...value.kinds].sort() }))
    .sort((left, right) => {
      const leftTime = Date.parse(left.session?.updatedAt ?? '')
      const rightTime = Date.parse(right.session?.updatedAt ?? '')
      if (Number.isNaN(leftTime) || Number.isNaN(rightTime) || leftTime === rightTime) {
        return (left.session?.title ?? left.id).localeCompare(right.session?.title ?? right.id)
      }
      return rightTime - leftTime
    })

  return (
    <section data-testid="session-backlinks" className="space-y-2">
      <div className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
        相关会话（{items.length} 个）
      </div>
      <ul className="space-y-2">
        {items.map(({ id, session, kinds }) => {
          const inner = (
            <div className="flex w-full flex-wrap items-start justify-between gap-2 text-left">
              <div className="min-w-0">
                <div
                  className="truncate text-[length:var(--type-caption)] font-medium text-[var(--color-text-primary)]"
                  title={session?.title ?? id}
                >
                  {session?.title ?? id}
                </div>
                <div className="mt-1 flex flex-wrap items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                  <span>关联：{kinds.map((kind) => (kind === 'edits' ? '改动' : '阅读')).join(' / ')}</span>
                  {session?.updatedAt && <span>更新于 {formatLocalTime(session.updatedAt)}</span>}
                  {typeof session?.userTurns === 'number' && <span>{session.userTurns} 轮对话</span>}
                </div>
              </div>
              {session?.workspace && (
                <span className="shrink-0 border border-[var(--color-border-subtle)] bg-[var(--color-surface)] px-2 py-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                  {session.workspace}
                </span>
              )}
            </div>
          )
          return (
            <li key={id} className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)]">
              {onOpenSession ? (
                <button
                  type="button"
                  onClick={() => onOpenSession(id)}
                  className="flex w-full p-3 hover:bg-[var(--color-bg)]"
                  title={session?.title ?? id}
                >
                  {inner}
                </button>
              ) : (
                <div className="p-3">{inner}</div>
              )}
            </li>
          )
        })}
      </ul>
    </section>
  )
}
