import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchSession } from '../api/client'
import type { SessionTodo, WikiSession } from '../api/types'
import { useWikiEvents } from '../hooks/useWikiEvents'
import { Icon } from './icons'
import { StateBlock } from './StateBlock'
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
        <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"><span className="font-mono tabular-nums">{paths.length}</span> 项</span>
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
                className="w-full truncate border border-transparent px-2 py-1 text-left text-[length:var(--type-caption)] text-[var(--color-accent)] hover:border-[var(--color-border-subtle)] hover:bg-[var(--color-layer)] font-mono"
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

// The tracker's own vocabulary, in the order a reader scans a plan.
const TODO_STATUS: Record<string, { label: string; className: string }> = {
  completed: { label: '已完成', className: 'text-[var(--color-success-text)]' },
  in_progress: { label: '进行中', className: 'text-[var(--color-accent)]' },
  blocked: { label: '阻塞', className: 'text-[var(--color-warning)]' },
  dropped: { label: '已放弃', className: 'text-[var(--color-text-tertiary)] line-through' },
  pending: { label: '待办', className: 'text-[var(--color-text-secondary)]' },
}

/**
 * The session's own task tracker: the list it ended with, plus what it finished
 * under earlier plans.
 *
 * Both halves are needed. A long session re-plans repeatedly - each `init`
 * replaces the list - so the final list alone can show four open tasks for six
 * hours of finished work.
 */
function TodoRecord({ session, onImport }: { session: WikiSession; onImport?: (items: SessionTodo[]) => Promise<number> }) {
  const [importState, setImportState] = useState<'idle' | 'running' | { imported: number } | { error: string }>('idle')
  const todos = session.todos ?? []
  const done = session.todosCompleted ?? []
  if (todos.length === 0 && done.length === 0) return null

  // Group by phase, preserving the order the session declared them in.
  const phases: { phase: string; items: SessionTodo[] }[] = []
  for (const item of todos) {
    const phase = item.phase ?? ''
    const bucket = phases.find((group) => group.phase === phase)
    if (bucket) bucket.items.push(item)
    else phases.push({ phase, items: [item] })
  }
  const openItems = todos.filter((item) => item.status !== 'completed' && item.status !== 'dropped')
  const openCount = openItems.length

  return (
    <section data-testid="session-todos" className="mt-4 space-y-2 border-t border-[var(--color-border-subtle)] pt-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <h3 className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)]">会话待办</h3>
        <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)] tabular-nums">
          当前 {todos.length} 项（未完成 {openCount}）· 历史完成 {done.length} 项
        </span>
        {(session.todoReplans ?? 0) > 0 && (
          <span
            data-testid="session-todo-replans"
            className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"
            title="每次重新规划都会替换整份清单，早先完成的条目归入历史"
          >
            重新规划 {session.todoReplans} 次
          </span>
        )}
        {onImport && openCount > 0 && (
          <button
            type="button"
            data-testid="session-todo-import"
            disabled={importState === 'running' || typeof importState === 'object'}
            onClick={async () => {
              setImportState('running')
              try {
                setImportState({ imported: await onImport(openItems) })
              } catch (error) {
                setImportState({ error: error instanceof Error ? error.message : '导入失败' })
              }
            }}
            className="border border-[var(--color-border)] px-2 py-0.5 text-[length:var(--type-caption)] text-[var(--color-accent)] hover:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:text-[var(--color-text-tertiary)]"
            title="把未完成的任务建成待办，并关联回本会话"
          >
            {importState === 'running' ? '导入中…' : `导入 ${openCount} 项到待办`}
          </button>
        )}
        {typeof importState === 'object' && 'imported' in importState && (
          <span role="status" className="text-[length:var(--type-caption)] text-[var(--color-success-text)]">
            已导入 {importState.imported} 项
          </span>
        )}
        {typeof importState === 'object' && 'error' in importState && (
          <span role="alert" className="text-[length:var(--type-caption)] text-[var(--color-danger-text)]">
            {importState.error}
          </span>
        )}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {phases.length > 0 && (
          <div className="space-y-2">
            {phases.map((group) => (
              <div key={group.phase || '未分组'} className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] p-2">
                {group.phase && (
                  <div className="mb-1 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
                    {group.phase}
                  </div>
                )}
                <ul className="space-y-1">
                  {group.items.map((item, index) => {
                    const status = TODO_STATUS[item.status] ?? { label: item.status, className: 'text-[var(--color-text-secondary)]' }
                    return (
                      <li key={`${item.content}-${index}`} className="text-[length:var(--type-caption)]">
                        <div className="flex items-start justify-between gap-3">
                          <span className={status.className === TODO_STATUS.dropped.className ? status.className : 'text-[var(--color-text-primary)]'}>
                            {item.content}
                          </span>
                          <span className={`shrink-0 ${status.className}`}>{status.label}</span>
                        </div>
                        {item.blocker && (
                          <div data-testid="session-todo-blocker" className="mt-0.5 text-[var(--color-text-secondary)]">
                            卡在：{item.blocker}
                          </div>
                        )}
                      </li>
                    )
                  })}
                </ul>
              </div>
            ))}
          </div>
        )}

        {done.length > 0 && (
          <div className="space-y-1">
            <div className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
              早先完成
            </div>
            <ul className="max-h-48 space-y-1 overflow-y-auto border border-[var(--color-border-subtle)] bg-[var(--color-layer)] p-2">
              {done.map((content, index) => (
                <li key={`${content}-${index}`} className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                  {content}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      {session.todosTruncated && (
        <p className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">待办过多，仅显示部分记录</p>
      )}
    </section>
  )
}

/**
 * Per-day activity: turns plus tool calls, oldest day first.
 *
 * A resumed session's start and end are a range, not a duration - one measured
 * transcript spans 109.5 hours - so this is the only place the card can say when
 * the work actually happened.
 */
function ActivityStrip({ days }: { days: [string, number][] }) {
  if (days.length < 2) return null
  const peak = Math.max(...days.map(([, count]) => count))
  return (
    <section data-testid="session-activity" className="mt-4 space-y-2 border-t border-[var(--color-border-subtle)] pt-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <h3 className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)]">每日活跃</h3>
        <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)] tabular-nums">
          {days.length} 天 · 峰值 {peak} 次
        </span>
      </div>
      <ul className="flex flex-wrap gap-1">
        {days.map(([day, count]) => (
          <li
            key={day}
            title={`${day}：${count} 次轮次与工具调用`}
            className="flex min-w-[4.5rem] flex-col gap-0.5 border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-2 py-1 text-[length:var(--type-caption)]"
          >
            <span className="text-[var(--color-text-secondary)]">{day.slice(5)}</span>
            <span className="tabular-nums font-mono text-[var(--color-text-primary)]">{count}</span>
            <span
              aria-hidden
              className="h-0.5 bg-[var(--color-accent)]"
              style={{ width: `${Math.max(8, Math.round((count / peak) * 100))}%` }}
            />
          </li>
        ))}
      </ul>
    </section>
  )
}

export function SessionDetail({
  sessionId,
  onOpenDocument,
  onClose,
  onImportTodos,
}: {
  sessionId: string
  onOpenDocument: (path: string) => void
  onClose: () => void
  /** Creates todos from the session's unfinished tasks; returns how many landed. */
  onImportTodos?: (session: WikiSession, items: SessionTodo[]) => Promise<number>
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

  // Days with recorded work, oldest first. StartedAt→UpdatedAt spans the gaps
  // too, so the day list is what actually says when the work happened.
  const activeDays = useMemo(
    () => Object.entries(session?.activity ?? {}).sort(([left], [right]) => left.localeCompare(right)),
    [session],
  )

  if (error === '404') {
    return (
      <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4" data-testid="session-detail">
        <StateBlock kind="error" title="未找到该会话" compact />
      </div>
    )
  }

  if (error) {
    return (
      <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4" data-testid="session-detail">
        <StateBlock kind="error" title="会话加载失败，请稍后重试" compact />
      </div>
    )
  }

  if (!session) {
    return (
      <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4" data-testid="session-detail">
        <StateBlock kind="loading" title="正在加载会话" compact />
      </div>
    )
  }

  return (
    <section data-testid="session-detail" className="border border-[var(--color-border)] bg-[var(--color-surface)] p-4 shadow-[var(--shadow-1)]">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-[length:var(--type-body)] leading-[var(--leading-body)] font-semibold text-[var(--color-text-primary)]" title={session.title}>{session.title}</h2>
            <span className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-2 py-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
              {session.workspace}
            </span>
            {session.source && (
              <span
                data-testid="session-source"
                className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-2 py-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"
                title="产生这次会话的 agent 运行时"
              >
                {session.source}
              </span>
            )}
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            <span>时间范围：<span className="font-mono tabular-nums">{formatLocalTime(session.startedAt)}</span> → <span className="font-mono tabular-nums">{formatLocalTime(session.updatedAt)}</span></span>
            {activeDays.length > 1 && (
              <span data-testid="session-active-days" title="有记录活动的天数；起止之间大部分时间并没有人在">
                活跃 {activeDays.length} 天（非连续）
              </span>
            )}
            <span>用户轮次：{session.userTurns}</span>
            {(session.subagents ?? []).length > 0 && (
              <span data-testid="session-subagents" title={(session.subagents ?? []).join('、')}>
                子代理：{(session.subagents ?? []).length}（其工具调用与文档已并入本会话）
              </span>
            )}
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
              <StateBlock kind="empty" title="暂无工具调用记录" compact />
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
              <StateBlock kind="empty" title="暂无意图记录" compact />
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

      <ActivityStrip days={activeDays} />
      <TodoRecord
        session={session}
        onImport={onImportTodos ? (items) => onImportTodos(session, items) : undefined}
      />
    </section>
  )
}
