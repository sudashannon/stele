import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchWikiGraph } from '../api/client'
import type { WikiComponent } from '../api/types'
import { GraphFilters } from './GraphFilters'
import { useWikiEvents } from '../hooks/useWikiEvents'

const ROW_HEIGHT = 24
const BAR_HEIGHT = 14
const PX_PER_DAY = 18
const LEFT_LABEL_WIDTH = 120
const MIN_BAR_WIDTH = 3

// Phase→color mapping (Carbon palette)
const PHASE_COLORS: Record<string, string> = {
  open: '#0f62fe',    // blue-60
  design: '#8a3ffc',  // purple-60
  build: '#007d79',   // teal-60
  verify: '#24a148',  // green-60
  archive: '#525252', // gray-70
  planning: '#0f62fe',
  in_progress: '#007d79',
  completed: '#24a148',
  rejected: '#da1e28',
}
const DEFAULT_BAR_COLOR = '#8d8d8d' // gray-50

interface TimelineItem {
  id: string
  title: string
  workspace: string
  phase: string
  start: number
  end: number
  color: string
}

function frontmatterTime(value: unknown): Date | null {
  if (typeof value !== 'string' || value === '') return null
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? null : d
}

function toTimelineItem(c: WikiComponent): TimelineItem {
  const created = frontmatterTime(c.frontmatter?.created_at)
  const updated = c.updatedAt ? new Date(c.updatedAt) : null
  const validUpdated = updated && updated.getFullYear() > 2000 ? updated : null
  const start = created ?? validUpdated ?? new Date()
  const defaultEnd = new Date(start.getTime() + 86400000)
  const end = validUpdated && validUpdated.getTime() > start.getTime() ? validUpdated : defaultEnd
  const phase = typeof c.frontmatter?.phase === 'string' ? (c.frontmatter.phase as string) : ''
  const color = PHASE_COLORS[phase] ?? DEFAULT_BAR_COLOR
  return {
    id: c.id,
    title: c.title,
    workspace: c.workspace,
    phase,
    start: start.getTime(),
    end: end.getTime(),
    color,
  }
}

/** Check if a date is a weekend (Sat=6, Sun=0) */
function isWeekend(d: Date): boolean {
  const day = d.getDay()
  return day === 0 || day === 6
}

export function WikiTimeline() {
  const [rawComponents, setRawComponents] = useState<WikiComponent[]>([])
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [workspaces, setWorkspaces] = useState<string[]>([])
  const [activeWorkspaces, setActiveWorkspaces] = useState<Set<string> | null>(null)
  const [hover, setHover] = useState<{ title: string; phase: string; date: string; x: number; y: number } | null>(null)

  // Fetch wiki graph
  const load = useCallback(async () => {
    try {
      const data = await fetchWikiGraph()
      const changes = (data.components ?? []).filter(
        (c) => c.type === 'change' && c.workspace !== 'root',
      )
      setRawComponents(changes)
      setLoaded(true)
      setLoadError(false)
    } catch {
      setLoadError(true)
    }
  }, [])

  useEffect(() => { load() }, [load])
  useWikiEvents(load)

  // Compute workspaces and items
  const items = useMemo(() => rawComponents.map((c) => toTimelineItem(c)), [rawComponents])
  const allWorkspaces = useMemo(() => {
    const ws = [...new Set(items.map((i) => i.workspace))]
    ws.sort((a, b) => a.localeCompare(b))
    return ws
  }, [items])

  // Auto-select first 4 workspaces
  useEffect(() => {
    if (!loaded || allWorkspaces.length === 0) return
    if (activeWorkspaces === null && workspaces.length === 0) {
      const selected = allWorkspaces.slice(0, Math.min(4, allWorkspaces.length))
      setWorkspaces(selected)
      setActiveWorkspaces(new Set(selected))
    }
  }, [loaded, allWorkspaces, activeWorkspaces, workspaces.length])

  const toggleWorkspace = useCallback((ws: string) => {
    setActiveWorkspaces((prev) => {
      const next = new Set(prev)
      if (next.has(ws)) next.delete(ws)
      else next.add(ws)
      return next
    })
  }, [])

  const filteredItems = useMemo(() => {
    if (!activeWorkspaces) return items
    return items.filter((i) => activeWorkspaces.has(i.workspace))
  }, [items, activeWorkspaces])

  // Filtered workspace list (sorted, only those with visible items)
  const visibleWorkspaces = useMemo(() => {
    if (!activeWorkspaces) return []
    const ws = [...new Set(filteredItems.map((i) => i.workspace))]
    ws.sort((a, b) => a.localeCompare(b))
    return ws
  }, [filteredItems, activeWorkspaces])

  // Time range with padding
  const { minTime, maxTime, chartWidth, chartHeight } = useMemo(() => {
    if (filteredItems.length === 0) {
      const now = Date.now()
      return { minTime: now, maxTime: now + 86400000 * 7, chartWidth: 800, chartHeight: 100 }
    }
    let min = Infinity, max = -Infinity
    for (const item of filteredItems) {
      if (item.start < min) min = item.start
      if (item.end > max) max = item.end
    }
    // 7-day padding on each side
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
  }, [filteredItems, visibleWorkspaces])

  const xForTime = useCallback(
    (t: number) => ((t - minTime) / (maxTime - minTime)) * chartWidth,
    [minTime, maxTime, chartWidth],
  )

  // Month ticks
  const ticks = useMemo(() => {
    const result: { x: number; label: string; isMonthStart: boolean }[] = []
    const cursor = new Date(minTime)
    cursor.setDate(1)
    cursor.setHours(0, 0, 0, 0)
    const end = new Date(maxTime)
    while (cursor <= end) {
      result.push({
        x: xForTime(cursor.getTime()),
        label: `${cursor.getFullYear()}-${String(cursor.getMonth() + 1).padStart(2, '0')}`,
        isMonthStart: cursor.getDate() === 1,
      })
      cursor.setMonth(cursor.getMonth() + 1)
    }
    return result
  }, [minTime, maxTime, xForTime])

  // Weekend highlight bands
  const weekendBands = useMemo(() => {
    const bands: { x: number; width: number }[] = []
    const cursor = new Date(minTime)
    cursor.setHours(0, 0, 0, 0)
    const end = new Date(maxTime)
    while (cursor <= end) {
      if (isWeekend(cursor)) {
        bands.push({
          x: xForTime(cursor.getTime()),
          width: PX_PER_DAY,
        })
      }
      cursor.setDate(cursor.getDate() + 1)
    }
    return bands
  }, [minTime, maxTime, xForTime])

  // Today marker
  const today = useMemo(() => {
    const t = new Date()
    t.setHours(0, 0, 0, 0)
    return { x: xForTime(t.getTime()), label: '今天' }
  }, [xForTime])

  if (loadError) {
    return <div className="flex h-full items-center justify-center text-xs text-[var(--color-danger)]">加载时间线数据失败</div>
  }

  return (
    <div className="relative flex h-[calc(100vh-160px)] min-h-[400px] w-full flex-col">
      {!loaded && (
        <div className="flex flex-1 items-center justify-center text-xs text-[var(--color-text-secondary)]">
          <span className="animate-pulse">加载中…</span>
        </div>
      )}
      {loaded && items.length === 0 && (
        <div className="flex flex-1 items-center justify-center text-xs text-[var(--color-text-secondary)]">
          暂无变更数据
        </div>
      )}
      {loaded && items.length > 0 && (
        <>
          <GraphFilters
            workspaces={allWorkspaces}
            activeWorkspaces={activeWorkspaces ?? new Set(allWorkspaces)}
            onToggleWorkspace={toggleWorkspace}
            communityLabels={{}}
            activeCommunity={null}
            onSelectCommunity={() => {}}
          />
          {filteredItems.length === 0 ? (
            <div className="flex flex-1 items-center justify-center text-xs text-[var(--color-text-secondary)]">
              没有匹配当前筛选条件的变更
            </div>
          ) : (
            <div data-testid="wiki-timeline" className="flex flex-1 overflow-auto border border-[var(--color-border)] bg-[var(--color-surface)]">
              {/* Left label column */}
              <div
                className="sticky left-0 z-10 shrink-0 border-r border-[var(--color-border)] bg-[var(--color-surface)]"
                style={{ width: LEFT_LABEL_WIDTH }}
              >
                <div
                  className="flex items-end border-b border-[var(--color-border)] px-2 text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]"
                  style={{ height: 32 }}
                >
                  工作区
                </div>
                {visibleWorkspaces.map((ws) => (
                  <div
                    key={ws}
                    className="flex items-center truncate border-b border-[var(--color-border-subtle)] px-2 text-[11px] font-medium text-[var(--color-text-primary)]"
                    style={{ height: ROW_HEIGHT }}
                    title={ws}
                  >
                    {ws}
                  </div>
                ))}
              </div>

              {/* Chart area */}
              <div className="relative overflow-hidden" style={{ width: chartWidth, minHeight: chartHeight + 32 }}>
                <svg width={chartWidth} height={chartHeight + 32} data-testid="wiki-timeline-svg">
                  {/* Weekend highlight bands */}
                  {weekendBands.map((band, i) => (
                    <rect
                      key={`we-${i}`}
                      x={band.x}
                      y={32}
                      width={band.width}
                      height={chartHeight}
                      fill="var(--color-layer)"
                    />
                  ))}

                  {/* Month separator lines */}
                  {ticks.map((tick) => (
                    <line
                      key={`tick-${tick.label}`}
                      x1={tick.x}
                      y1={32}
                      x2={tick.x}
                      y2={chartHeight + 32}
                      stroke="var(--color-border-subtle)"
                      strokeWidth={1}
                    />
                  ))}

                  {/* Today marker */}
                  {today.x > 0 && today.x < chartWidth && (
                    <line
                      x1={today.x}
                      y1={32}
                      x2={today.x}
                      y2={chartHeight + 32}
                      stroke="var(--color-accent)"
                      strokeWidth={1.5}
                      strokeDasharray="4 3"
                    />
                  )}

                  {/* Month labels */}
                  {ticks.map((tick) => (
                    <text
                      key={`label-${tick.label}`}
                      x={tick.x + 4}
                      y={18}
                      fontSize={10}
                      fill="var(--color-text-secondary)"
                    >
                      {tick.label}
                    </text>
                  ))}

                  {/* Today label */}
                  {today.x > 0 && today.x < chartWidth && (
                    <text
                      x={today.x + 4}
                      y={32}
                      fontSize={9}
                      fill="var(--color-accent)"
                      fontWeight={600}
                    >
                      {today.label}
                    </text>
                  )}

                  {/* Bars */}
                  {visibleWorkspaces.map((ws, rowIndex) => {
                    const rowItems = filteredItems.filter((item) => item.workspace === ws)
                    const y = rowIndex * ROW_HEIGHT + 32 + (ROW_HEIGHT - BAR_HEIGHT) / 2
                    return (
                      <g key={ws}>
                        {rowItems.map((item) => {
                          const x = xForTime(item.start)
                          const width = Math.max(MIN_BAR_WIDTH, xForTime(item.end) - x)
                          return (
                            <rect
                              key={item.id}
                              data-testid="wiki-timeline-bar"
                              x={x}
                              y={y}
                              width={width}
                              height={BAR_HEIGHT}
                              rx={2}
                              fill={item.color}
                              opacity={0.85}
                              style={{ cursor: 'pointer' }}
                              onMouseEnter={(e) =>
                                setHover({
                                  title: item.title,
                                  phase: item.phase,
                                  date: new Date(item.start).toLocaleDateString('zh-CN'),
                                  x: e.clientX,
                                  y: e.clientY,
                                })
                              }
                              onMouseMove={(e) =>
                                setHover((prev) => prev ? { ...prev, x: e.clientX, y: e.clientY } : null)
                              }
                              onMouseLeave={() => setHover(null)}
                            />
                          )
                        })}
                      </g>
                    )
                  })}
                </svg>
              </div>
            </div>
          )}
        </>
      )}
      {hover && (
        <div
          data-testid="wiki-timeline-tooltip"
          className="pointer-events-none fixed z-20 -translate-x-1/2 -translate-y-full border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-xs text-[var(--color-text-primary)] shadow-sm"
          style={{ left: hover.x, top: hover.y - 10 }}
        >
          <div className="font-medium">{hover.title}</div>
          <div className="flex gap-2 text-[10px] text-[var(--color-text-secondary)]">
            {hover.phase && <span>{hover.phase}</span>}
            <span>{hover.date}</span>
          </div>
        </div>
      )}
    </div>
  )
}
