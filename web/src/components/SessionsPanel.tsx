import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchSessionsWithMeta, refreshSessions } from '../api/client'
import type { WikiSession } from '../api/types'
import { useWikiEvents } from '../hooks/useWikiEvents'
import { copyText } from '../utils/clipboard'
import { useContextMenu } from './ContextMenu'
import { Icon } from './icons'

const DAY_MS = 24 * 60 * 60 * 1000

function startOfDay(value: number): number {
  const date = new Date(value)
  date.setHours(0, 0, 0, 0)
  return date.getTime()
}

/** Day heading for a session group: relative for the two days people think in. */
function formatDayLabel(dayStart: number): string {
  const today = startOfDay(Date.now())
  if (dayStart === today) return '今天'
  if (dayStart === today - DAY_MS) return '昨天'
  return new Date(dayStart).toLocaleDateString('zh-CN')
}

function formatClock(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
}

function totalToolCalls(session: WikiSession): number {
  return Object.values(session.toolCalls ?? {}).reduce((sum, count) => sum + count, 0)
}

/** Tool names by call count, highest first - the shape of what a session did. */
function topTools(session: WikiSession, limit: number): [string, number][] {
  return Object.entries(session.toolCalls ?? {})
    .sort(([leftName, left], [rightName, right]) => right - left || leftName.localeCompare(rightName))
    .slice(0, limit)
}

// Days with recorded work. A resumed session's StartedAt→UpdatedAt is a range,
// not a duration, so the count of active days is the honest effort signal.
function activeDays(session: WikiSession): number {
  return Object.keys(session.activity ?? {}).length
}

function producedCount(session: WikiSession): number {
  return (session.writes ?? []).length + (session.edits ?? []).length
}

type SessionGroup = { dayStart: number; label: string; sessions: WikiSession[] }

function groupByDay(sessions: WikiSession[]): SessionGroup[] {
  const byDay = new Map<number, WikiSession[]>()
  for (const session of sessions) {
    const stamp = new Date(session.updatedAt).getTime()
    const dayStart = Number.isNaN(stamp) ? 0 : startOfDay(stamp)
    const bucket = byDay.get(dayStart)
    if (bucket) bucket.push(session)
    else byDay.set(dayStart, [session])
  }
  return [...byDay.entries()]
    .sort(([left], [right]) => right - left)
    .map(([dayStart, group]) => ({
      dayStart,
      label: dayStart === 0 ? '时间未知' : formatDayLabel(dayStart),
      sessions: group,
    }))
}

function CountBadge({ label, value, testId }: { label: string; value: number; testId?: string }) {
  return (
    <span
      data-testid={testId}
      className="shrink-0 border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-1.5 py-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)] tabular-nums"
    >
      {label} {value}
    </span>
  )
}

/**
 * The session layer's own view. Documents reach their sessions through
 * backlinks; this is the other direction - every indexed session, what it
 * touched, and a way into the one that did the work you are looking for.
 */
export function SessionsPanel({ onOpen }: { onOpen?: (path: string) => void }) {
  const [sessions, setSessions] = useState<WikiSession[]>([])
  const [enabled, setEnabled] = useState(true)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [workspace, setWorkspace] = useState('')
  const [source, setSource] = useState('')
  const [query, setQuery] = useState('')
  const [producedOnly, setProducedOnly] = useState(false)
  const [unfinishedOnly, setUnfinishedOnly] = useState(false)
  const ctx = useContextMenu()

  const load = useCallback(async (options?: { spinner?: boolean }) => {
    if (options?.spinner) setLoading(true)
    try {
      const response = await fetchSessionsWithMeta()
      setSessions(response.sessions)
      setEnabled(response.enabled)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载会话失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load({ spinner: true })
  }, [load])

  // The layer re-reads transcripts on its own schedule; adopt the new digests
  // in place rather than flashing the whole panel back to its loading state.
  useWikiEvents({ onSessionsUpdated: () => void load() })

  const handleRefresh = useCallback(async () => {
    if (refreshing) return
    setRefreshing(true)
    setNotice(null)
    try {
      const result = await refreshSessions()
      await load()
      setNotice(result.changed > 0 ? `已更新 ${result.changed} 个会话` : '没有会话需要更新')
    } catch (err) {
      setError(err instanceof Error ? err.message : '重扫会话失败')
    } finally {
      setRefreshing(false)
    }
  }, [load, refreshing])

  const workspaces = useMemo(
    () => [...new Set(sessions.map((session) => session.workspace).filter(Boolean))].sort((a, b) => a.localeCompare(b)),
    [sessions],
  )

  // Runtimes come from the sessions themselves, so the control only appears
  // once more than one agent runtime has actually produced work.
  const sources = useMemo(
    () => [...new Set(sessions.map((session) => session.source).filter((name): name is string => Boolean(name)))].sort((a, b) => a.localeCompare(b)),
    [sessions],
  )

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return sessions
      .filter((session) => !workspace || session.workspace === workspace)
      .filter((session) => !source || session.source === source)
      .filter((session) => !producedOnly || producedCount(session) > 0)
      .filter((session) => !unfinishedOnly || (session.todoOpen ?? 0) > 0)
      .filter((session) => {
        if (!needle) return true
        // Search what a person remembers about a session: its title, what it
        // was doing, and the documents it touched.
        const haystack = [
          session.title,
          ...(session.intents ?? []),
          ...(session.writes ?? []),
          ...(session.edits ?? []),
          ...(session.reads ?? []),
        ]
        return haystack.some((text) => text.toLowerCase().includes(needle))
      })
      .slice()
      .sort((left, right) => new Date(right.updatedAt).getTime() - new Date(left.updatedAt).getTime())
  }, [producedOnly, query, sessions, source, unfinishedOnly, workspace])

  const stats = useMemo(() => {
    const produced = new Set<string>()
    const read = new Set<string>()
    for (const session of visible) {
      for (const path of [...(session.writes ?? []), ...(session.edits ?? [])]) produced.add(path)
      for (const path of session.reads ?? []) read.add(path)
    }
    return { produced: produced.size, read: read.size, unfinished: visible.filter((s) => (s.todoOpen ?? 0) > 0).length }
  }, [visible])

  const groups = useMemo(() => groupByDay(visible), [visible])
  const filtersActive = Boolean(workspace || source || query.trim() || producedOnly || unfinishedOnly)

  const clearFilters = useCallback(() => {
    setWorkspace('')
    setSource('')
    setQuery('')
    setProducedOnly(false)
    setUnfinishedOnly(false)
  }, [])

  const handleCopy = useCallback((text: string) => {
    void copyText(text)
      .then(() => setNotice('已复制'))
      .catch(() => setError('复制失败，请手动复制'))
  }, [])

  return (
    <div className="space-y-3" data-testid="sessions-panel">
      <div className="flex flex-wrap items-center gap-2">
        <input
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="搜索标题、意图或文档路径"
          aria-label="搜索会话"
          className="min-w-0 flex-1 border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-1.5 text-xs text-[var(--color-text-primary)] placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--color-accent)] focus:outline-none"
        />
        <select
          value={workspace}
          onChange={(event) => setWorkspace(event.target.value)}
          aria-label="按工作区筛选"
          className="border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-xs text-[var(--color-text-primary)] focus:border-[var(--color-accent)] focus:outline-none"
        >
          <option value="">全部工作区</option>
          {workspaces.map((alias) => (
            <option key={alias} value={alias}>{alias}</option>
          ))}
        </select>
        {sources.length > 1 && (
          <select
            value={source}
            onChange={(event) => setSource(event.target.value)}
            aria-label="按 agent 运行时筛选"
            className="border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-xs text-[var(--color-text-primary)] focus:border-[var(--color-accent)] focus:outline-none"
          >
            <option value="">全部运行时</option>
            {sources.map((name) => (
              <option key={name} value={name}>{name}</option>
            ))}
          </select>
        )}
        <label className="flex items-center gap-1.5 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-xs text-[var(--color-text-secondary)]">
          <input
            type="checkbox"
            checked={producedOnly}
            onChange={(event) => setProducedOnly(event.target.checked)}
          />
          仅有产出或改动
        </label>
        <label className="flex items-center gap-1.5 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-xs text-[var(--color-text-secondary)]">
          <input
            type="checkbox"
            checked={unfinishedOnly}
            onChange={(event) => setUnfinishedOnly(event.target.checked)}
            aria-label="仅未完成清单"
          />
          仅未完成清单
        </label>
        <button
          type="button"
          onClick={() => void handleRefresh()}
          disabled={refreshing}
          data-testid="sessions-refresh"
          className="flex items-center gap-1.5 border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-1.5 text-xs text-[var(--color-accent)] hover:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:text-[var(--color-text-tertiary)]"
        >
          <Icon name="refresh" size={13} className={refreshing ? 'animate-spin' : undefined} />
          重扫
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-[var(--color-text-secondary)]">
        <span data-testid="sessions-summary" className="tabular-nums">
          会话 {visible.length}
          {visible.length !== sessions.length && ` / ${sessions.length}`}
        </span>
        <span className="tabular-nums">产出或改动 {stats.produced} 篇</span>
        <span className="tabular-nums">读取 {stats.read} 篇</span>
        {stats.unfinished > 0 && (
          <span data-testid="sessions-unfinished-count" className="tabular-nums text-[var(--color-warning)]">
            {stats.unfinished} 个会话留有未完成任务
          </span>
        )}
        {filtersActive && (
          <button type="button" onClick={clearFilters} className="text-[var(--color-link)] hover:underline">
            清除筛选
          </button>
        )}
      </div>

      {notice && <div role="status" className="text-xs text-[var(--color-success)]">{notice}</div>}
      {error && <div role="alert" className="text-xs text-[var(--color-danger)]">{error}</div>}

      {loading && sessions.length === 0 && (
        <div role="status" className="flex items-center gap-2 text-xs text-[var(--color-text-secondary)]">
          <Icon name="spinner" size={14} className="animate-spin" />
          正在加载会话
        </div>
      )}

      {!loading && !enabled && (
        <div className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-2 text-xs text-[var(--color-text-secondary)]">
          会话记忆层未启用：未配置会话记录目录，或 <code>--sessions-dir</code> 指向的目录不存在。
        </div>
      )}

      {!loading && enabled && sessions.length === 0 && (
        <div className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-2 text-xs text-[var(--color-text-secondary)]">
          暂无已索引的会话。只有工作目录落在已注册 workspace 内的会话才会入图。
        </div>
      )}

      {!loading && sessions.length > 0 && visible.length === 0 && (
        <div className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-2 text-xs text-[var(--color-text-secondary)]">
          没有匹配的会话。
        </div>
      )}

      <div className="space-y-4">
        {groups.map((group) => (
          <section key={group.dayStart} aria-labelledby={`sessions-day-${group.dayStart}`}>
            <h3
              id={`sessions-day-${group.dayStart}`}
              className="mb-1.5 text-xs font-semibold text-[var(--color-text-secondary)]"
            >
              {group.label}
            </h3>
            <ul className="space-y-1.5">
              {group.sessions.map((session) => {
                const produced = producedCount(session)
                const reads = (session.reads ?? []).length
                const tools = topTools(session, 3)
                const subagents = (session.subagents ?? []).length
                const days = activeDays(session)
                const open = session.todoOpen ?? 0
                return (
                  <li key={session.id}>
                    <button
                      type="button"
                      onClick={() => onOpen?.(session.path)}
                      onContextMenu={ctx.onContextMenu([
                        { id: 'open', label: '打开会话', run: () => onOpen?.(session.path) },
                        { id: 'copy-path', label: '复制会话路径', run: () => handleCopy(session.path) },
                        { id: 'copy-title', label: '复制标题', run: () => handleCopy(session.title) },
                      ])}
                      className="w-full space-y-1 border border-[var(--color-border)] px-3 py-2 text-left text-xs hover:bg-[var(--color-layer)]"
                    >
                      <span className="flex items-center gap-2">
                        <span className="flex-1 truncate font-medium text-[var(--color-text-primary)]" title={session.title}>
                          {session.title}
                        </span>
                        {produced > 0 && <CountBadge label="产出/改动" value={produced} testId="sessions-produced-badge" />}
                        <CountBadge label="读取" value={reads} />
                        {subagents > 0 && <CountBadge label="子代理" value={subagents} testId="sessions-subagent-badge" />}
                        {open > 0 && (
                          <span
                            data-testid="sessions-open-badge"
                            title="结束时仍未完成的任务"
                            className="shrink-0 border border-[var(--color-warning)] px-1.5 py-0.5 text-[length:var(--type-caption)] text-[var(--color-warning)] tabular-nums"
                          >
                            未完成 {open}
                          </span>
                        )}
                        <span className="shrink-0 text-[var(--color-text-secondary)]">{session.workspace || '—'}</span>
                        <span className="shrink-0 tabular-nums text-[var(--color-text-secondary)]">
                          {formatClock(session.updatedAt)}
                        </span>
                      </span>
                      <span className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                        <span className="tabular-nums">{session.userTurns} 轮</span>
                        {days > 1 && <span className="tabular-nums" title="有记录活动的天数">活跃 {days} 天</span>}
                        <span className="tabular-nums">{totalToolCalls(session)} 次工具</span>
                        {tools.length > 0 && (
                          <span className="truncate">
                            {tools.map(([name, count]) => `${name} ${count}`).join(' · ')}
                          </span>
                        )}
                      </span>
                      {(session.intents ?? []).length > 0 && (
                        <span className="block truncate text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">
                          {(session.intents ?? []).slice(0, 2).join(' · ')}
                        </span>
                      )}
                    </button>
                  </li>
                )
              })}
            </ul>
          </section>
        ))}
      </div>
      {ctx.renderMenu}
    </div>
  )
}
