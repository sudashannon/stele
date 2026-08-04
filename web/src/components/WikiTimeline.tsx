import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { fetchSessions, fetchWikiGraph } from '../api/client'
import type { WikiComponent, WikiSession } from '../api/types'
import { Icon } from './icons'
import { GraphFilters } from './GraphFilters'
import { typeBadgeClass } from './graphPalette'
import { useWikiEvents } from '../hooks/useWikiEvents'

const LEFT_LABEL_WIDTH = 140
const HEADER_HEIGHT = 36
// Row height and day width are derived from the host box, not fixed: a fixed
// 40px row left a 196px chart under a 900px viewport, and a fixed 18px day
// forced a horizontal scroll even when the window could show the whole window.
// Both are clamped: below MIN_PX_PER_DAY the chart scrolls rather than squeeze
// months into one screen, and rows never grow past MAX_ROW_HEIGHT so a two-row
// chart does not become two slabs.
const MIN_ROW_HEIGHT = 40
const MAX_ROW_HEIGHT = 72
const MIN_PX_PER_DAY = 8
// Share of the host height the chart may claim; the detail list takes the rest.
const CHART_HEIGHT_SHARE = 0.42

// Single-hue sequential ramp — darker = more activity. The step count matches
// the 8-step data-viz palette declared in the token contract; a ninth colour
// would not be distinguishable, so anything past --viz-8 is the same step.
const VIZ_COLORS = [
  'var(--viz-1)', 'var(--viz-2)', 'var(--viz-3)', 'var(--viz-4)',
  'var(--viz-5)', 'var(--viz-6)', 'var(--viz-7)', 'var(--viz-8)',
]

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
    communityId,
  }
}

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
    return layers[scope]
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

  // Host box drives the chart geometry. window resize is enough: the view fills
  // the app shell, so it only changes size when the window does.
  const hostRef = useRef<HTMLDivElement | null>(null)
  const [host, setHost] = useState({ width: 1200, height: 800 })
  useEffect(() => {
    const measure = () => {
      const el = hostRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      if (rect.width > 0 && rect.height > 0) setHost({ width: rect.width, height: rect.height })
    }
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
    // `loaded` re-measures once data lands: on the very first paint the box can
    // still be 0px, and the guard above would otherwise leave the fallback.
  }, [loaded])

  const { minTime, maxTime, chartWidth, chartHeight, rowHeight, pxPerDay } = useMemo(() => {
    const rows = Math.max(1, visibleWorkspaces.length)
    const rowBudget = Math.floor(host.height * CHART_HEIGHT_SHARE) - HEADER_HEIGHT
    const rowHeight = Math.min(
      MAX_ROW_HEIGHT,
      Math.max(MIN_ROW_HEIGHT, Math.floor(rowBudget / rows)),
    )
    if (filteredItems.length === 0) {
      const now = Date.now()
      return { minTime: now, maxTime: now + 86400000 * 7, chartWidth: 800, chartHeight: rowHeight * rows, rowHeight, pxPerDay: MIN_PX_PER_DAY }
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
    const days = Math.max(1, Math.ceil((effectiveMax - effectiveMin) / 86400000))
    // Fit the window when the days allow it; scroll only when a day would fall
    // below MIN_PX_PER_DAY, where a cell stops being a readable mark.
    const available = Math.max(320, host.width - LEFT_LABEL_WIDTH)
    const pxPerDay = Math.max(MIN_PX_PER_DAY, Math.floor(available / days))
    return {
      minTime: effectiveMin,
      maxTime: effectiveMax,
      chartWidth: days * pxPerDay,
      // Rows own the whole chart height; the +40 tail was a phantom row.
      chartHeight: rowHeight * rows,
      rowHeight,
      pxPerDay,
    }
  }, [filteredItems, host.height, host.width, visibleWorkspaces.length])

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
        bands.push({ x: xForTime(cursor.getTime()), width: pxPerDay })
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

  // Aggregate items per workspace per day into heatmap cells coloured by the
  // single-hue --viz-* ramp, so 1523 items across four rows read as a readable
  // surface instead of a band of confetti. Identity is carried by row and date;
  // the colour ramp encodes magnitude (darker = more activity).
  //
  // Buckets are RANK-based, not value-based: a linear scale against the global
  // max collapses everything short of the largest day into the lightest step
  // (measured: a single 189-file import day left ~95% of cells on --viz-8).
  // Ranking spreads the actual distribution across the ramp; the legend shows
  // each step's count range, so magnitude stays readable without exact bars.
  const aggregatedCells = useMemo(() => {
    if (filteredItems.length === 0) return { bucket: new Map<string, Map<number, TimelineItem[]>>(), steps: [] }
    const bucket = new Map<string, Map<number, TimelineItem[]>>()
    for (const item of filteredItems) {
      const d = new Date(item.start)
      d.setHours(0, 0, 0, 0)
      const ts = d.getTime()
      let wsMap = bucket.get(item.workspace)
      if (!wsMap) { wsMap = new Map(); bucket.set(item.workspace, wsMap) }
      const list = wsMap.get(ts) ?? []
      list.push(item)
      wsMap.set(ts, list)
    }
    const counts = [...bucket.values()].flatMap((m) => [...m.values()].map((l) => l.length)).sort((a, b) => a - b)
    const total = counts.length
    // cut[0] = min, cut[8] = max; step i covers (cut[i], cut[i+1]]. Equal cuts
    // (fewer distinct counts than steps) collapse, so the legend omits them.
    const cut = [counts[0]]
    for (let i = 1; i < VIZ_COLORS.length; i++) cut.push(counts[Math.min(total - 1, Math.floor((total * i) / VIZ_COLORS.length))])
    const stepOf = (n: number) => {
      for (let i = 0; i < cut.length - 1; i++) {
        if (n <= cut[i + 1]) return i
      }
      return cut.length - 2
    }
    const steps: Array<{ color: string; min: number; max: number }> = []
    for (let i = 0; i < cut.length - 1; i++) {
      if (cut[i] === cut[i + 1]) continue
      steps.push({ color: VIZ_COLORS[VIZ_COLORS.length - 1 - i], min: cut[i], max: cut[i + 1] })
    }
    return { bucket, steps, stepOf }
  }, [filteredItems])

  // A cell may cover many items, so clicking one selects the day instead of
  // opening a single document; the detail list below the chart shows the items.
  const [selectedCell, setSelectedCell] = useState<{ ws: string; ts: number } | null>(null)
  useEffect(() => {
    setSelectedCell(null)
  }, [scope, filteredItems])

  // With nothing selected the panel lists the newest items in the window, so the
  // space below a 196px chart carries content instead of reading as a void. The
  // cap keeps the DOM small; the list scrolls.
  const recentItems = useMemo(
    () => [...filteredItems].sort((a, b) => b.start - a.start).slice(0, 200),
    [filteredItems],
  )

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
      <div className="flex h-full items-center justify-center text-[length:var(--type-caption)] text-[var(--color-danger-text)]">
        加载时间线数据失败
      </div>
    )
  }

  return (
    <div ref={hostRef} className="relative flex h-full min-h-[400px] w-full flex-col">
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
          className="mb-2 flex shrink-0 flex-wrap items-center gap-2 text-[length:var(--type-caption)]"
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
              className="flex flex-none overflow-auto border border-[var(--color-border)] bg-[var(--color-surface)]"
            >
              <div
                className="sticky left-0 z-10 shrink-0 border-r border-[var(--color-border)] bg-[var(--color-surface)]"
                style={{ width: LEFT_LABEL_WIDTH }}
              >
                <div
                  className="flex items-end border-b border-[var(--color-border)] px-3 pb-2 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]"
                  style={{ height: HEADER_HEIGHT }}
                >
                  工作区
                </div>
                {visibleWorkspaces.map((ws) => (
                  <div
                    key={ws}
                    className="flex items-center truncate border-b border-[var(--color-border-subtle)] px-3 text-[length:var(--type-caption)] font-medium text-[var(--color-text-primary)]"
                    style={{ height: rowHeight }}
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
                style={{ width: chartWidth, height: chartHeight + HEADER_HEIGHT }}
              >
                <svg width={chartWidth} height={chartHeight + HEADER_HEIGHT} data-testid="wiki-timeline-svg">
                  {weekendBands.map((band, i) => (
                    <rect
                      key={`we-${i}`}
                      x={band.x}
                      y={HEADER_HEIGHT}
                      width={band.width}
                      height={chartHeight}
                      fill="var(--color-layer)"
                    />
                  ))}

                  {ticks.map((tick) => (
                    <line
                      key={`tick-${tick.label}`}
                      x1={tick.x}
                      y1={HEADER_HEIGHT}
                      x2={tick.x}
                      y2={chartHeight + HEADER_HEIGHT}
                      stroke="var(--viz-grid)"
                      strokeWidth={1}
                    />
                  ))}

                  {today.x > 0 && today.x < chartWidth && (
                    <line
                      x1={today.x}
                      y1={HEADER_HEIGHT}
                      x2={today.x}
                      y2={chartHeight + HEADER_HEIGHT}
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
                      fill="var(--viz-axis)"
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

                {/* Magnitude key: a rank-quantile ramp has no natural axis, so
                    without this strip the darkest and lightest cells would carry
                    no readable magnitude. Swatch tooltips give the exact range. */}
                <div className="absolute right-2 top-1 flex items-center gap-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                  <span>少</span>
                  {aggregatedCells.steps.map((s) => (
                    <span
                      key={`${s.min}-${s.max}`}
                      className="inline-block h-3 w-3 rounded-full"
                      style={{ backgroundColor: s.color }}
                      title={`${s.min}~${s.max} 项`}
                    />
                  ))}
                  <span>多</span>
                </div>

                <div className="pointer-events-none absolute inset-0">
                  {visibleWorkspaces.map((ws, rowIndex) => {
                    const wsBucket = aggregatedCells.bucket.get(ws)
                    const rowTop = rowIndex * rowHeight + HEADER_HEIGHT
                    const cells: JSX.Element[] = []
                    const dayCursor = new Date(minTime)
                    dayCursor.setHours(0, 0, 0, 0)
                    const endDay = new Date(maxTime)
                    endDay.setHours(0, 0, 0, 0)
                    while (dayCursor <= endDay) {
                      const ts = dayCursor.getTime()
                      const items = wsBucket?.get(ts)
                      const count = items?.length ?? 0
                      if (count > 0 && aggregatedCells.stepOf) {
                        const idx = aggregatedCells.stepOf(count)
                        const cellColor = VIZ_COLORS[VIZ_COLORS.length - 1 - idx]
                        const x = xForTime(ts)
                        const dayLabel = dayCursor.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
                        const selected = selectedCell?.ws === ws && selectedCell?.ts === ts
                        cells.push(
                          <button
                            key={`${ws}-${ts}`}
                            type="button"
                            data-testid="wiki-timeline-bar"
                            className={`pointer-events-auto absolute outline-none opacity-90 hover:opacity-100 focus-visible:opacity-100 focus-visible:z-10 ${
                              selected ? 'z-10 ring-1 ring-[var(--color-accent)]' : ''
                            }`}
                            style={{
                              left: x,
                              top: rowTop,
                              width: pxPerDay - 1,
                              height: rowHeight - 1,
                              backgroundColor: cellColor,
                            }}
                            aria-label={`${count} 项 · ${dayLabel} · 查看明细`}
                            aria-pressed={selected}
                            title={`${count} 项 · ${dayLabel} · 查看明细`}
                            onClick={() => {
                              setSelectedCell(selected ? null : { ws, ts })
                            }}
                            onKeyDown={(event) => {
                              if (event.key === 'Enter' || event.key === ' ') {
                                event.preventDefault()
                                setSelectedCell(selected ? null : { ws, ts })
                              }
                            }}
                            onMouseEnter={(event) =>
                              setHover({
                                title: `${count} 项`,
                                phase: '',
                                date: dayLabel,
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
                                title: `${count} 项`,
                                phase: '',
                                date: dayLabel,
                                x: rect.left + rect.width / 2,
                                y: placement === 'below' ? rect.bottom : rect.top,
                                placement,
                              })
                            }}
                            onBlur={() => setHover(null)}
                          />,
                        )
                      }
                      dayCursor.setDate(dayCursor.getDate() + 1)
                    }
                    return cells
                  })}
                </div>
              </div>
            </div>
          )}

          {/* Detail panel. A cell can cover many items, so clicking one selects
              the day and this list is the click target that opens a document.
              With nothing selected it lists the newest items in the window, so
              the area below a 196px chart is never a blank void. */}
          {(() => {
            const dayItems = selectedCell
              ? aggregatedCells.bucket.get(selectedCell.ws)?.get(selectedCell.ts) ?? []
              : null
            const list = dayItems ?? recentItems
            if (list.length === 0) return null
            const heading = selectedCell
              ? `${selectedCell.ws} · ${new Date(selectedCell.ts).toLocaleDateString('zh-CN', { month: 'long', day: 'numeric' })} · ${list.length} 项`
              : `最近动态 · 最新 ${list.length} 项`
            return (
              <div
                data-testid="wiki-timeline-detail"
                className="flex min-h-0 flex-1 flex-col border border-t-0 border-[var(--color-border)] bg-[var(--color-surface)]"
              >
                <div className="flex shrink-0 items-center justify-between border-b border-[var(--color-border-subtle)] px-3 py-2 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
                  <span>{heading}</span>
                  {selectedCell && (
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
                      aria-label="关闭明细"
                      onClick={() => setSelectedCell(null)}
                    >
                      <Icon name="chevron-left" size={14} />
                      返回最近动态
                    </button>
                  )}
                </div>
                <div className="min-h-0 flex-1 divide-y divide-[var(--color-border-subtle)] overflow-y-auto">
                  {list.map((item) => (
                    <button
                      key={item.id}
                      type="button"
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[length:var(--type-body)] hover:bg-[var(--color-layer)]"
                      onClick={() => handleOpen(item.path)}
                      title={item.path}
                    >
                      {/* Same badge as 最近更新, from the shared token map: a
                          document's `detail` carries its real wiki type, so the
                          list names design/spec/knowledge rather than "document". */}
                      <span
                        className={`shrink-0 px-1.5 py-0.5 font-mono text-[length:var(--type-caption)] font-medium ${typeBadgeClass(
                          item.kind === 'document' ? item.detail : item.kind,
                        )}`}
                      >
                        {item.kind === 'document' ? item.detail : item.kind}
                      </span>
                      <span className="flex-1 truncate font-medium text-[var(--color-text-primary)]">{item.title}</span>
                      {/* Workspace and date vary across the recent list; a
                          selected day already names both in its heading. */}
                      {!selectedCell && (
                        <>
                          <span className="shrink-0 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                            {item.workspace}
                          </span>
                          <span className="shrink-0 font-mono tabular-nums text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                            {new Date(item.start).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })}
                          </span>
                        </>
                      )}
                    </button>
                  ))}
                </div>
              </div>
            )
          })()}

        </>
      )}
      {hover && (
        <div
          className={`pointer-events-none fixed z-20 -translate-x-1/2 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] shadow-[var(--shadow-overlay)] ${hover.placement === 'above' ? '-translate-y-full' : ''}`}
          data-testid="wiki-timeline-tooltip"
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
