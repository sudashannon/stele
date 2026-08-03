import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { fetchSessions, fetchWikiGraph } from '../api/client'
import type { WikiComponent, WikiSession } from '../api/types'
import { GraphFilters } from './GraphFilters'
import { COMMUNITY_COLORS, TYPE_COLORS } from './graphPalette'
import { useWikiEvents } from '../hooks/useWikiEvents'

const ROW_HEIGHT = 28
const BAR_HEIGHT = 16
const DOC_HEIGHT = 8
const SESSION_BAND_HEIGHT = 5
const PX_PER_DAY = 18
const LEFT_LABEL_WIDTH = 140
const MIN_BAR_WIDTH = 6
const MIN_DOC_WIDTH = 3
const MAX_COMMUNITY_LEGEND_ITEMS = 12

// What the timeline draws. It used to draw changes only, which is why it went
// blank from mid-July: the work moved to knowledge documents and superpowers
// artifacts, and the last change here was created on 2026-07-21 while 320
// documents were written after it. A workspace with no changes at all (any
// superpowers workspace) had no row either.
type TimelineScope = 'all' | 'change' | 'document' | 'session'

const SCOPE_LABELS: Record<TimelineScope, string> = {
  all: '全部',
  change: '变更',
  document: '文档',
  session: '会话',
}

const PHASE_COLORS: Record<string, string> = {
  open: 'var(--color-phase-open)',
  design: 'var(--color-phase-design)',
  build: 'var(--color-phase-build)',
  verify: 'var(--color-phase-verify)',
  archive: 'var(--color-phase-archive)',
  planning: 'var(--color-phase-open)',
  in_progress: 'var(--color-phase-build)',
  completed: 'var(--color-phase-verify)',
  rejected: 'var(--color-phase-rejected)',
}
const DEFAULT_BAR_COLOR = 'var(--color-phase-unknown)'

interface TimelineItem {
  id: string
  kind: Exclude<TimelineScope, 'all'>
  title: string
  path: string
  workspace: string
  /** Change phase, document type, or a session day's activity count. */
  detail: string
  start: number
  end: number
  color: string
  height: number
  minWidth: number
  /** Vertical offset inside the row, so lanes do not overlap. */
  laneOffset: number
  communityId: number | null
}

interface WikiTimelineProps {
  onOpen?: (path: string) => void
}

function frontmatterTime(value: unknown): Date | null {
  if (typeof value !== 'string' || value === '') return null
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? null : d
}

// span derives a component's bar from the dates it actually carries: the
// authored created_at when the frontmatter has one, otherwise the indexed update
// time. A document with neither is a single day at its update time - the median
// span across this corpus is one day, so documents read as ticks, not bars.
function span(c: WikiComponent): { start: number; end: number } {
  const created = frontmatterTime(c.frontmatter?.created_at)
  const updated = c.updatedAt ? new Date(c.updatedAt) : null
  const validUpdated = updated && updated.getFullYear() > 2000 ? updated : null
  const start = created ?? validUpdated ?? new Date()
  const defaultEnd = new Date(start.getTime() + 86400000)
  const end = validUpdated && validUpdated.getTime() > start.getTime() ? validUpdated : defaultEnd
  return { start: start.getTime(), end: end.getTime() }
}

function toTimelineItem(c: WikiComponent, communityId: number | null): TimelineItem {
  const { start, end } = span(c)
  const phase = typeof c.frontmatter?.phase === 'string' ? (c.frontmatter.phase as string) : ''
  return {
    id: c.id,
    kind: 'change',
    title: c.title,
    path: c.path,
    workspace: c.workspace,
    detail: phase,
    start,
    end,
    color: PHASE_COLORS[phase] ?? DEFAULT_BAR_COLOR,
    height: BAR_HEIGHT,
    minWidth: MIN_BAR_WIDTH,
    laneOffset: (ROW_HEIGHT - BAR_HEIGHT) / 2,
    communityId,
  }
}

// Documents share the changes' lane but are thinner, so a dense week reads as a
// band without hiding the change bars behind it.
function toDocumentItem(c: WikiComponent, communityId: number | null): TimelineItem {
  const { start, end } = span(c)
  return {
    id: c.id,
    kind: 'document',
    title: c.title,
    path: c.path,
    workspace: c.workspace,
    detail: c.type,
    start,
    end,
    color: TYPE_COLORS[c.type] ?? DEFAULT_BAR_COLOR,
    height: DOC_HEIGHT,
    minWidth: MIN_DOC_WIDTH,
    laneOffset: (ROW_HEIGHT - DOC_HEIGHT) / 2,
    communityId,
  }
}

// One mark per day a session recorded work. A session's start-to-end span covers
// the idle days too (the longest here spans nine), so the per-day counts are the
// only honest way to place its work in time.
function toSessionItems(session: WikiSession): TimelineItem[] {
  return Object.entries(session.activity ?? {}).flatMap(([day, count]) => {
    const start = new Date(`${day}T00:00:00`)
    if (Number.isNaN(start.getTime())) return []
    return [{
      id: `${session.id}:${day}`,
      kind: 'session' as const,
      title: session.title,
      path: session.path,
      workspace: session.workspace,
      detail: `${count} 次活动`,
      start: start.getTime(),
      end: start.getTime() + 86400000,
      color: 'var(--color-accent)',
      height: SESSION_BAND_HEIGHT,
      minWidth: MIN_DOC_WIDTH,
      laneOffset: ROW_HEIGHT - SESSION_BAND_HEIGHT - 2,
      communityId: null,
    }]
  })
}

function isWeekend(d: Date): boolean {
  const day = d.getDay()
  return day === 0 || day === 6
}

export function WikiTimeline({ onOpen }: WikiTimelineProps) {
  const [scope, setScope] = useState<TimelineScope>('all')
  const [rawComponents, setRawComponents] = useState<WikiComponent[]>([])
  const [rawDocuments, setRawDocuments] = useState<WikiComponent[]>([])
  const [rawSessions, setRawSessions] = useState<WikiSession[]>([])
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [communities, setCommunities] = useState<Record<string, number>>({})
  const [communityLabels, setCommunityLabels] = useState<Record<string, string>>({})
  const [activeWorkspaces, setActiveWorkspaces] = useState<Set<string> | null>(null)
  const [activeCommunity, setActiveCommunity] = useState<number | null>(null)
  const [hover, setHover] = useState<{
    title: string
    phase: string
    date: string
    x: number
    y: number
    placement: 'above' | 'below'
  } | null>(null)

  const load = useCallback(async () => {
    try {
      const data = await fetchWikiGraph()
      const components = (data.components ?? []).filter((c) => c.workspace !== 'root')
      setRawComponents(components.filter((c) => c.type === 'change'))
      // Sessions arrive from their own endpoint because the graph component
      // carries no per-day activity; a failure there must not blank the chart.
      setRawDocuments(components.filter((c) => c.type !== 'change' && c.type !== 'session'))
      setCommunities(data.communities ?? {})
      setCommunityLabels(data.communityLabels ?? {})
      setLoaded(true)
      setLoadError(false)
      fetchSessions()
        .then((list) => setRawSessions(list))
        .catch(() => setRawSessions([]))
    } catch {
      setRawComponents([])
      setRawDocuments([])
      setLoaded(true)
      setLoadError(true)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])
  useWikiEvents(load)

  const layers = useMemo(() => ({
    change: rawComponents.map((c) => toTimelineItem(c, communities[c.id] ?? null)),
    document: rawDocuments.map((c) => toDocumentItem(c, communities[c.id] ?? null)),
    session: rawSessions.flatMap(toSessionItems),
  }), [communities, rawComponents, rawDocuments, rawSessions])

  const items = useMemo(() => {
    if (scope === 'all') return [...layers.change, ...layers.document, ...layers.session]
    if (scope !== 'session') return layers[scope]
    // Session marks ride the row's bottom edge so they read as a band under the
    // bars; on their own there is nothing to sit under, so centre them.
    return layers.session.map((item) => ({ ...item, laneOffset: (ROW_HEIGHT - SESSION_BAND_HEIGHT) / 2 }))
  }, [layers, scope])

  const allWorkspaces = useMemo(() => {
    const ws = [...new Set(items.map((i) => i.workspace))]
    ws.sort((a, b) => a.localeCompare(b))
    return ws
  }, [items])

  useEffect(() => {
    setActiveWorkspaces((prev) => {
      if (allWorkspaces.length === 0) return null
      if (prev === null) return null
      const next = new Set([...prev].filter((ws) => allWorkspaces.includes(ws)))
      if (next.size === allWorkspaces.length) return null
      return next
    })
  }, [allWorkspaces])

  const toggleWorkspace = useCallback((ws: string) => {
    setActiveWorkspaces((prev) => {
      const base = prev ?? new Set(allWorkspaces)
      const next = new Set(base)
      if (next.has(ws)) next.delete(ws)
      else next.add(ws)
      if (next.size === allWorkspaces.length) return null
      return next
    })
  }, [allWorkspaces])

  const resetFilters = useCallback(() => {
    setActiveWorkspaces(null)
    setActiveCommunity(null)
  }, [])

  const workspaceFilteredItems = useMemo(() => {
    if (activeWorkspaces === null) return items
    return items.filter((item) => activeWorkspaces.has(item.workspace))
  }, [items, activeWorkspaces])

  const communityCounts = useMemo(() => {
    const counts: Record<number, number> = {}
    workspaceFilteredItems.forEach((item) => {
      if (item.communityId == null || item.communityId < 0) return
      counts[item.communityId] = (counts[item.communityId] ?? 0) + 1
    })
    return counts
  }, [workspaceFilteredItems])

  const effectiveCommunityLabels = useMemo(() => {
    const labels: Record<string, string> = { ...communityLabels }
    Object.keys(communityCounts).forEach((id) => {
      labels[id] = labels[id] ?? `#${id}`
    })
    return labels
  }, [communityLabels, communityCounts])

  useEffect(() => {
    if (activeCommunity === null) return
    if (communityCounts[activeCommunity] == null) setActiveCommunity(null)
  }, [activeCommunity, communityCounts])

  const filteredItems = useMemo(() => {
    if (activeCommunity === null) return workspaceFilteredItems
    return workspaceFilteredItems.filter((item) => item.communityId === activeCommunity)
  }, [workspaceFilteredItems, activeCommunity])

  const droppedSessionItems = useMemo(() => {
    if (activeCommunity === null) return 0
    return workspaceFilteredItems.filter((item) => item.kind === 'session').length
  }, [activeCommunity, workspaceFilteredItems])

  const visibleWorkspaces = useMemo(() => {
    const ws = [...new Set(filteredItems.map((item) => item.workspace))]
    ws.sort((a, b) => a.localeCompare(b))
    return ws
  }, [filteredItems])

  const filterSummary = useMemo(() => {
    const parts = [`显示 ${filteredItems.length} / ${items.length} 项（口径：${SCOPE_LABELS[scope]}）`]
    const hidden = items.length - filteredItems.length
    if (hidden > 0) parts.push(`隐藏 ${hidden} 项`)
    // Sessions carry no community, so a community filter necessarily excludes
    // them; a silent drop would look like the session layer is empty.
    if (droppedSessionItems > 0) parts.push(`社区筛选不含会话（${droppedSessionItems} 个会话日已排除）`)
    return parts.join(' · ')
  }, [droppedSessionItems, filteredItems.length, items.length, scope])

  // What the other scopes hold, so an empty chart can say where the work went
  // instead of implying nothing was tracked.
  const scopeHint = useMemo(() => {
    const counts: [TimelineScope, number][] = [
      ['change', layers.change.length],
      ['document', layers.document.length],
      ['session', layers.session.length],
    ]
    const others = counts.filter(([key, count]) => key !== scope && count > 0)
    if (others.length === 0) return ''
    const unit: Record<string, string> = { change: '条变更', document: '篇文档', session: '个会话日' }
    return `其它口径：${others.map(([key, count]) => `${count} ${unit[key]}`).join(' · ')}`
  }, [layers, scope])

  const { minTime, maxTime, chartWidth, chartHeight } = useMemo(() => {
    if (filteredItems.length === 0) {
      const now = Date.now()
      return { minTime: now, maxTime: now + 86400000 * 7, chartWidth: 800, chartHeight: 100 }
    }
    let min = Infinity
    let max = -Infinity
    for (const item of filteredItems) {
      if (item.start < min) min = item.start
      if (item.end > max) max = item.end
    }
    const pad = 86400000 * 7
    const today = Date.now()
    const effectiveMax = Math.max(max, today) + pad
    const effectiveMin = min - pad
    const days = (effectiveMax - effectiveMin) / 86400000
    return {
      minTime: effectiveMin,
      maxTime: effectiveMax,
      chartWidth: Math.max(800, Math.ceil(days * PX_PER_DAY)),
      chartHeight: visibleWorkspaces.length * ROW_HEIGHT + 40,
    }
  }, [filteredItems, visibleWorkspaces.length])

  const xForTime = useCallback(
    (t: number) => ((t - minTime) / (maxTime - minTime)) * chartWidth,
    [minTime, maxTime, chartWidth],
  )

  const ticks = useMemo(() => {
    const result: { x: number; label: string }[] = []
    const cursor = new Date(minTime)
    cursor.setDate(1)
    cursor.setHours(0, 0, 0, 0)
    const end = new Date(maxTime)
    while (cursor <= end) {
      result.push({
        x: xForTime(cursor.getTime()),
        label: `${cursor.getFullYear()}-${String(cursor.getMonth() + 1).padStart(2, '0')}`,
      })
      cursor.setMonth(cursor.getMonth() + 1)
    }
    return result
  }, [minTime, maxTime, xForTime])

  const weekendBands = useMemo(() => {
    const bands: { x: number; width: number }[] = []
    const cursor = new Date(minTime)
    cursor.setHours(0, 0, 0, 0)
    const end = new Date(maxTime)
    while (cursor <= end) {
      if (isWeekend(cursor)) {
        bands.push({ x: xForTime(cursor.getTime()), width: PX_PER_DAY })
      }
      cursor.setDate(cursor.getDate() + 1)
    }
    return bands
  }, [minTime, maxTime, xForTime])

  const today = useMemo(() => {
    const t = new Date()
    t.setHours(0, 0, 0, 0)
    return { x: xForTime(t.getTime()), label: '今天' }
  }, [xForTime])

  const communityLegend = useMemo(
    () =>
      Object.keys(communityCounts)
        .map(Number)
        .sort((a, b) => {
          const countDifference = (communityCounts[b] ?? 0) - (communityCounts[a] ?? 0)
          return countDifference !== 0 ? countDifference : a - b
        })
        .slice(0, MAX_COMMUNITY_LEGEND_ITEMS)
        .map((id) => ({
          id,
          label: effectiveCommunityLabels[String(id)] ?? `#${id}`,
          count: communityCounts[id],
          color: COMMUNITY_COLORS[id % COMMUNITY_COLORS.length],
        })),
    [communityCounts, effectiveCommunityLabels],
  )
  const hiddenCommunityCount = Math.max(0, Object.keys(communityCounts).length - communityLegend.length)

  const scrollRef = useRef<HTMLDivElement | null>(null)
  const landedScope = useRef<string>('')

  useEffect(() => {
    const container = scrollRef.current
    if (!container || filteredItems.length === 0) return
    const key = `${scope}:${chartWidth}`
    if (landedScope.current === key) return
    // Deferred a frame because assigning scrollLeft in the same commit that
    // changes the chart width gets clamped to 0 - layout has not applied the new
    // width yet. The frame is deliberately not cancelled on re-render and the
    // "landed" mark is only set once the scroll takes: sessions arrive from a
    // second request, and cancelling on that re-render left the chart pinned to
    // its oldest edge, which is what made the recent weeks feel missing.
    requestAnimationFrame(() => {
      if (landedScope.current === key) return
      if (container.scrollWidth <= container.clientWidth) return
      const target = today.x > 0 ? today.x - container.clientWidth * 0.8 : chartWidth
      container.scrollLeft = Math.max(0, target)
      landedScope.current = key
    })
  }, [chartWidth, filteredItems.length, scope, today.x])

  const handleOpen = useCallback(
    (path: string) => {
      if (!onOpen) return
      onOpen(path)
    },
    [onOpen],
  )

  if (loadError) {
    return (
      <div className="flex h-full items-center justify-center text-[length:var(--type-caption)] text-[var(--color-danger)]">
        加载时间线数据失败
      </div>
    )
  }

  return (
    <div className="relative flex h-[calc(100vh-160px)] min-h-[400px] w-full flex-col">
      {!loaded && (
        <div className="flex flex-1 items-center justify-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          <span className="animate-pulse">加载中…</span>
        </div>
      )}
      {loaded && (
        <div
          data-testid="wiki-timeline-scope"
          role="group"
          aria-label="时间线口径"
          className="mb-2 flex shrink-0 flex-wrap items-center gap-2 text-xs"
        >
          {(['all', 'change', 'document', 'session'] as TimelineScope[]).map((key) => {
            const count = key === 'all'
              ? layers.change.length + layers.document.length + layers.session.length
              : layers[key].length
            const active = scope === key
            return (
              <button
                key={key}
                type="button"
                aria-pressed={active}
                onClick={() => setScope(key)}
                className={`border px-2 py-1 tabular-nums ${
                  active
                    ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
                    : 'border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)] hover:bg-[var(--color-layer)]'
                }`}
              >
                {SCOPE_LABELS[key]} {count}
              </button>
            )
          })}
        </div>
      )}
      {loaded && items.length === 0 && (
        <div className="flex flex-1 flex-col items-center justify-center gap-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          <span>当前口径（{SCOPE_LABELS[scope]}）没有数据</span>
          {scopeHint && <span data-testid="wiki-timeline-scope-hint">{scopeHint}</span>}
        </div>
      )}
      {loaded && items.length > 0 && (
        <>
          <GraphFilters
            workspaces={allWorkspaces}
            activeWorkspaces={activeWorkspaces ?? new Set(allWorkspaces)}
            onToggleWorkspace={toggleWorkspace}
            onResetFilters={resetFilters}
            communityLabels={effectiveCommunityLabels}
            communityCounts={communityCounts}
            activeCommunity={activeCommunity}
            onSelectCommunity={setActiveCommunity}
            summary={filterSummary}
          />
          {filteredItems.length === 0 ? (
            <div className="flex flex-1 items-center justify-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
              没有匹配当前筛选条件的{SCOPE_LABELS[scope]}
            </div>
          ) : (
            <div
              ref={scrollRef}
              data-testid="wiki-timeline"
              className="flex flex-1 overflow-auto border border-[var(--color-border)] bg-[var(--color-surface)]"
            >
              <div
                className="sticky left-0 z-10 shrink-0 border-r border-[var(--color-border)] bg-[var(--color-surface)]"
                style={{ width: LEFT_LABEL_WIDTH }}
              >
                <div
                  className="flex items-end border-b border-[var(--color-border)] px-3 pb-2 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]"
                  style={{ height: 36 }}
                >
                  工作区
                </div>
                {visibleWorkspaces.map((ws) => (
                  <div
                    key={ws}
                    className="flex items-center truncate border-b border-[var(--color-border-subtle)] px-3 text-[length:var(--type-caption)] font-medium text-[var(--color-text-primary)]"
                    style={{ height: ROW_HEIGHT }}
                    title={ws}
                  >
                    {ws}
                  </div>
                ))}
              </div>

              {/* shrink-0 is load-bearing: as a flex item the chart was shrunk to
                  the container width, so PX_PER_DAY did nothing, months were
                  squeezed into one screen and the scroller never scrolled. */}
              <div
                className="relative shrink-0 overflow-hidden"
                style={{ width: chartWidth, minHeight: chartHeight + 36 }}
              >
                <svg width={chartWidth} height={chartHeight + 36} data-testid="wiki-timeline-svg">
                  {weekendBands.map((band, i) => (
                    <rect
                      key={`we-${i}`}
                      x={band.x}
                      y={36}
                      width={band.width}
                      height={chartHeight}
                      fill="var(--color-layer)"
                    />
                  ))}

                  {ticks.map((tick) => (
                    <line
                      key={`tick-${tick.label}`}
                      x1={tick.x}
                      y1={36}
                      x2={tick.x}
                      y2={chartHeight + 36}
                      stroke="var(--color-border-subtle)"
                      strokeWidth={1}
                    />
                  ))}

                  {today.x > 0 && today.x < chartWidth && (
                    <line
                      x1={today.x}
                      y1={36}
                      x2={today.x}
                      y2={chartHeight + 36}
                      stroke="var(--color-accent)"
                      strokeWidth={1.5}
                      strokeDasharray="4 3"
                    />
                  )}

                  {ticks.map((tick) => (
                    <text
                      key={`label-${tick.label}`}
                      x={tick.x + 4}
                      y={18}
                      fontSize={12}
                      fill="var(--color-text-secondary)"
                    >
                      {tick.label}
                    </text>
                  ))}

                  {today.x > 0 && today.x < chartWidth && (
                    <text
                      x={today.x + 4}
                      y={32}
                      fontSize={12}
                      fill="var(--color-accent)"
                      fontWeight={600}
                    >
                      {today.label}
                    </text>
                  )}
                </svg>

                <div className="pointer-events-none absolute inset-0">
                  {visibleWorkspaces.map((ws, rowIndex) => {
                    const rowItems = filteredItems.filter((item) => item.workspace === ws)
                    const rowTop = rowIndex * ROW_HEIGHT + 36
                    return rowItems.map((item) => {
                      const x = xForTime(item.start)
                      const width = Math.max(item.minWidth, xForTime(item.end) - x)
                      const accentColor = item.communityId != null
                        ? COMMUNITY_COLORS[item.communityId % COMMUNITY_COLORS.length]
                        : 'var(--color-border)'
                      return (
                        <button
                          key={item.id}
                          type="button"
                          data-testid="wiki-timeline-bar"
                          className="pointer-events-auto absolute border border-[var(--timeline-bar-border)] opacity-90 outline-none focus-visible:border-[var(--color-text-primary)] focus-visible:opacity-100 hover:opacity-100"
                          style={{
                            left: x,
                            top: rowTop + item.laneOffset,
                            width,
                            height: item.height,
                            backgroundColor: item.color,
                            '--timeline-bar-border': accentColor,
                          } as CSSProperties}
                          data-kind={item.kind}
                          aria-label={`${SCOPE_LABELS[item.kind]}：${item.title}${item.detail ? ` · ${item.detail}` : ''}`}
                          title={`${SCOPE_LABELS[item.kind]}：${item.title}${item.detail ? ` · ${item.detail}` : ''}`}
                          onClick={() => handleOpen(item.path)}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter' || event.key === ' ') {
                              event.preventDefault()
                              handleOpen(item.path)
                            }
                          }}
                          onMouseEnter={(event) =>
                            setHover({
                              title: item.title,
                              phase: item.detail,
                              date: new Date(item.start).toLocaleDateString('zh-CN'),
                              x: event.clientX,
                              y: event.clientY,
                              placement: event.clientY < 64 ? 'below' : 'above',
                            })
                          }
                          onMouseMove={(event) =>
                            setHover((prev) =>
                              prev ? { ...prev, x: event.clientX, y: event.clientY } : null,
                            )
                          }
                          onMouseLeave={() => setHover(null)}
                          onFocus={(event) => {
                            const rect = event.currentTarget.getBoundingClientRect()
                            const placement = rect.top < 64 ? 'below' : 'above'
                            setHover({
                              title: item.title,
                              phase: item.detail,
                              date: new Date(item.start).toLocaleDateString('zh-CN'),
                              x: rect.left + rect.width / 2,
                              y: placement === 'below' ? rect.bottom : rect.top,
                              placement,
                            })
                          }}
                          onBlur={() => setHover(null)}
                        />
                      )
                    })
                  })}
                </div>
              </div>
            </div>
          )}

          {communityLegend.length > 0 && (
            <div className="border-x border-b border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
              <div className="mb-2 flex items-center justify-between gap-3 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
                <span>当前社区分布</span>
                {hiddenCommunityCount > 0 && (
                  <span data-testid="wiki-timeline-community-overflow" className="font-normal">
                    显示前 {communityLegend.length} 个 · 另有 {hiddenCommunityCount} 个社区
                  </span>
                )}
              </div>
              <ul className="flex flex-wrap gap-x-4 gap-y-2">
                {communityLegend.map((community) => (
                  <li
                    key={community.id}
                    data-testid="wiki-timeline-community-legend-item"
                    className="flex items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-text-primary)]"
                  >
                    <span
                      className="inline-block h-2.5 w-2.5 rounded-full"
                      style={{ backgroundColor: community.color }}
                    />
                    <span>{community.label}</span>
                    <span className="text-[var(--color-text-secondary)]">{community.count}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
      {hover && (
        <div
          data-testid="wiki-timeline-tooltip"
          className={`pointer-events-none fixed z-20 -translate-x-1/2 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] shadow-sm ${hover.placement === 'above' ? '-translate-y-full' : ''}`}
          style={{ left: hover.x, top: hover.y + (hover.placement === 'above' ? -10 : 10) }}
        >
          <div className="font-medium">{hover.title}</div>
          <div className="flex gap-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            {hover.phase && <span>{hover.phase}</span>}
            <span>{hover.date}</span>
          </div>
        </div>
      )}
    </div>
  )
}
