import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
import { fetchWikiGraph } from '../api/client'
import type { WikiComponent } from '../api/types'
import { GraphFilters } from './GraphFilters'
import { COMMUNITY_COLORS } from './graphPalette'
import { useWikiEvents } from '../hooks/useWikiEvents'

const ROW_HEIGHT = 28
const BAR_HEIGHT = 16
const PX_PER_DAY = 18
const LEFT_LABEL_WIDTH = 140
const MIN_BAR_WIDTH = 6
const MAX_COMMUNITY_LEGEND_ITEMS = 12

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
  title: string
  path: string
  workspace: string
  phase: string
  start: number
  end: number
  color: string
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

function toTimelineItem(c: WikiComponent, communityId: number | null): TimelineItem {
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
    path: c.path,
    workspace: c.workspace,
    phase,
    start: start.getTime(),
    end: end.getTime(),
    color,
    communityId,
  }
}

function isWeekend(d: Date): boolean {
  const day = d.getDay()
  return day === 0 || day === 6
}

export function WikiTimeline({ onOpen }: WikiTimelineProps) {
  const [rawComponents, setRawComponents] = useState<WikiComponent[]>([])
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
      const changes = (data.components ?? []).filter(
        (c) => c.type === 'change' && c.workspace !== 'root',
      )
      setRawComponents(changes)
      setCommunities(data.communities ?? {})
      setCommunityLabels(data.communityLabels ?? {})
      setLoaded(true)
      setLoadError(false)
    } catch {
      setRawComponents([])
      setLoaded(true)
      setLoadError(true)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])
  useWikiEvents(load)

  const items = useMemo(
    () => rawComponents.map((c) => toTimelineItem(c, communities[c.id] ?? null)),
    [rawComponents, communities],
  )

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

  const visibleWorkspaces = useMemo(() => {
    const ws = [...new Set(filteredItems.map((item) => item.workspace))]
    ws.sort((a, b) => a.localeCompare(b))
    return ws
  }, [filteredItems])

  const filterSummary = useMemo(() => {
    const hidden = items.length - filteredItems.length
    const parts = [`显示 ${filteredItems.length} / ${items.length} 条变更`]
    if (hidden > 0) parts.push(`隐藏 ${hidden} 条`) 
    return parts.join(' · ')
  }, [filteredItems.length, items.length])

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
      {loaded && items.length === 0 && (
        <div className="flex flex-1 items-center justify-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          暂无变更数据
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
              没有匹配当前筛选条件的变更
            </div>
          ) : (
            <div
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

              <div className="relative overflow-hidden" style={{ width: chartWidth, minHeight: chartHeight + 36 }}>
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
                    const top = rowIndex * ROW_HEIGHT + 36 + (ROW_HEIGHT - BAR_HEIGHT) / 2
                    return rowItems.map((item) => {
                      const x = xForTime(item.start)
                      const width = Math.max(MIN_BAR_WIDTH, xForTime(item.end) - x)
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
                            top,
                            width,
                            height: BAR_HEIGHT,
                            backgroundColor: item.color,
                            '--timeline-bar-border': accentColor,
                          } as CSSProperties}
                          aria-label={`${item.title}${item.phase ? ` · ${item.phase}` : ''}`}
                          title={`${item.title}${item.phase ? ` · ${item.phase}` : ''}`}
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
                              phase: item.phase,
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
                              phase: item.phase,
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
