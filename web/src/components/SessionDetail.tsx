import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchSession } from '../api/client'
import type { WikiSession } from '../api/types'
import { useWikiEvents } from '../hooks/useWikiEvents'
import { Icon } from './icons'

function formatLocalTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function PathSection({
  heading,
  paths,
  onOpenDocument,
}: {
  heading: string
  paths: string[]
  onOpenDocument: (path: string) => void
}) {
  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)]">{heading}</h3>
        <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{paths.length} 项</span>
      </div>
      {paths.length === 0 ? (
        <div className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          暂无记录
        </div>
      ) : (
        <ul className="max-h-32 space-y-1 overflow-y-auto">
          {paths.map((path) => (
            <li key={`${heading}-${path}`}>
              <button
                type="button"
                onClick={() => onOpenDocument(path)}
                className="w-full truncate border border-transparent px-2 py-1 text-left text-[length:var(--type-caption)] text-[var(--color-accent)] hover:border-[var(--color-border-subtle)] hover:bg-[var(--color-layer)]"
                title={path}
              >
                {path}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export function SessionDetail({
  sessionId,
  onOpenDocument,
  onClose,
}: {
  sessionId: string
  onOpenDocument: (path: string) => void
  onClose: () => void
}) {
  const [session, setSession] = useState<WikiSession | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback((options: { reset: boolean }) => {
    let cancelled = false
    if (options.reset) {
      setSession(null)
      setError(null)
    }
    fetchSession(sessionId)
      .then((next) => {
        if (cancelled) return
        setSession(next)
        setError(null)
      })
      .catch((err) => {
        if (cancelled) return
        const message = err instanceof Error ? err.message : String(err)
        setError(message.includes('404') ? '404' : message)
      })
    return () => {
      cancelled = true
    }
  }, [sessionId])

  useEffect(() => load({ reset: true }), [load])

  // A live session keeps appending: the backend re-parses the transcript tail
  // on its own schedule and announces it, so refresh in place rather than
  // flashing the whole card back to its loading state.
  useWikiEvents({ onSessionsUpdated: () => load({ reset: false }) })

  const topToolCalls = useMemo(
    () =>
      session
        ? Object.entries(session.toolCalls ?? {})
            .sort((left, right) => right[1] - left[1] || left[0].localeCompare(right[0]))
            .slice(0, 5)
        : [],
    [session],
  )

  if (error === '404') {
    return (
      <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4" data-testid="session-detail">
        <div role="alert" className="flex items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-danger)]">
          <Icon name="warning" size={14} />
          未找到该会话
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4" data-testid="session-detail">
        <div role="alert" className="flex items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-danger)]">
          <Icon name="warning" size={14} />
          会话加载失败，请稍后重试
        </div>
      </div>
    )
  }

  if (!session) {
    return (
      <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4" data-testid="session-detail">
        <div role="status" className="flex items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          <Icon name="spinner" size={14} className="animate-spin" />
          正在加载会话
        </div>
      </div>
    )
  }

  return (
    <section data-testid="session-detail" className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4 shadow-[var(--shadow-1)]">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-sm font-semibold text-[var(--color-text-primary)]" title={session.title}>{session.title}</h2>
            <span className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-2 py-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
              {session.workspace}
            </span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            <span>时间范围：{formatLocalTime(session.startedAt)} → {formatLocalTime(session.updatedAt)}</span>
            <span>用户轮次：{session.userTurns}</span>
          </div>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="shrink-0 border border-[var(--color-border)] px-3 py-1 text-[length:var(--type-caption)] text-[var(--color-accent)] hover:bg-[var(--color-layer)]"
        >
          关闭
        </button>
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
        <div className="space-y-4">
          <section className="space-y-2">
            <h3 className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)]">工具调用</h3>
            {topToolCalls.length === 0 ? (
              <div className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                暂无工具调用记录
              </div>
            ) : (
              <ul className="space-y-1">
                {topToolCalls.map(([name, count]) => (
                  <li key={name} className="flex items-center justify-between gap-3 border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-2 text-[length:var(--type-caption)]">
                    <span className="truncate text-[var(--color-text-primary)]">{name}</span>
                    <span className="tabular-nums text-[var(--color-text-secondary)]">{count}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="space-y-2">
            <h3 className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)]">意图</h3>
            {session.intents.length === 0 ? (
              <div className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                暂无意图记录
              </div>
            ) : (
              <div className="space-y-2">
                <ul className="max-h-40 space-y-1 overflow-y-auto border border-[var(--color-border-subtle)] bg-[var(--color-layer)] p-2">
                  {session.intents.map((intent, index) => (
                    <li key={`${intent}-${index}`} className="text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
                      {intent}
                    </li>
                  ))}
                </ul>
                {session.intentsTruncated && (
                  <p className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">仅显示最近若干条意图</p>
                )}
              </div>
            )}
          </section>
        </div>

        <div className="space-y-4">
          {(session.writes ?? []).length > 0 && (
            <PathSection heading="产出文档" paths={session.writes ?? []} onOpenDocument={onOpenDocument} />
          )}
          {(session.edits ?? []).length > 0 && (
            <PathSection heading="改动文档" paths={session.edits ?? []} onOpenDocument={onOpenDocument} />
          )}
          <PathSection heading="读取文档" paths={session.reads ?? []} onOpenDocument={onOpenDocument} />
          {session.pathsTruncated && (
            <p className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
              文档过多，仅显示前若干条
            </p>
          )}
        </div>
      </div>
    </section>
  )
}
