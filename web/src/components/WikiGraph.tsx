import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import cytoscape from 'cytoscape'
import { fetchWikiGraph, searchSemantic } from '../api/client'
import type { WikiComponent, WikiEdge } from '../api/types'
import { GraphFilters } from './GraphFilters'
import { useWikiEvents } from '../hooks/useWikiEvents'

/**
 * Color legend for the 8 WikiComponent types shown in the WikiGraph force-directed view.
 *
 * Reused categories map to the app's semantic CSS variables; the remaining
 * categories are derived from those same variables with color-mix() so no
 * hardcoded hex values remain here.
 *
 * Reused mappings:
 *   change   -> --color-accent
 *   tasks    -> --color-warn
 *   plan     -> --color-success
 *   diagram  -> --color-danger
 *   artifact -> --color-text-secondary
 */
export const TYPE_COLORS: Record<string, string> = {
  change: 'var(--color-accent)',
  proposal: 'color-mix(in srgb, var(--color-accent) 45%, var(--color-danger))',
  design: 'color-mix(in srgb, var(--color-success) 55%, var(--color-accent))',
  tasks: 'var(--color-warn)',
  spec: 'color-mix(in srgb, var(--color-warn) 70%, var(--color-danger))',
  plan: 'var(--color-success)',
  artifact: 'var(--color-text-secondary)',
  diagram: 'var(--color-danger)',
}

// EDGE_COLORS distinguishes the three edge kinds the wiki index actually
// computes (see wiki/links.go): implements (design_doc/plan), references
// (verification_report/markdown links), generates (artifact convention).
// These reuse the app's semantic palette directly, since edges are a separate
// visual channel (line color, not fill) from node type colors.
const EDGE_COLORS: Record<string, string> = {
  implements: 'var(--color-accent)',
  references: 'var(--color-success)',
  generates: 'var(--color-warn)',
}
const EDGE_FALLBACK_COLOR = 'var(--color-text-secondary)'

export const COMMUNITY_COLORS = [
  'var(--color-accent)',
  'var(--color-success)',
  'var(--color-danger)',
  'var(--color-warn)',
  'color-mix(in srgb, var(--color-accent) 60%, var(--color-success))',
  'color-mix(in srgb, var(--color-accent) 60%, var(--color-danger))',
  'color-mix(in srgb, var(--color-success) 60%, var(--color-warn))',
  'color-mix(in srgb, var(--color-danger) 60%, var(--color-warn))',
  'color-mix(in srgb, var(--color-accent) 70%, var(--color-surface))',
  'color-mix(in srgb, var(--color-success) 70%, var(--color-surface))',
  'color-mix(in srgb, var(--color-danger) 70%, var(--color-surface))',
  'color-mix(in srgb, var(--color-warn) 70%, var(--color-surface))',
]

type RGB = readonly [number, number, number]

function parseRGB(value: string): RGB | null {
  const hex = /^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i.exec(value.trim())
  if (hex) return [Number.parseInt(hex[1], 16), Number.parseInt(hex[2], 16), Number.parseInt(hex[3], 16)]

  const rgb = /^rgba?\(\s*([\d.]+)(?:\s*,\s*|\s+)([\d.]+)(?:\s*,\s*|\s+)([\d.]+)/i.exec(value.trim())
  if (!rgb) return null
  const channel = (input: string) => Math.max(0, Math.min(255, Math.round(Number(input))))
  return [channel(rgb[1]), channel(rgb[2]), channel(rgb[3])]
}

function readColorToken(styles: CSSStyleDeclaration, name: string, fallback: string): RGB {
  return parseRGB(styles.getPropertyValue(name)) ?? parseRGB(fallback)!
}

function mixRGB(left: RGB, right: RGB, leftWeight: number): RGB {
  const rightWeight = 1 - leftWeight
  return [
    Math.round(left[0] * leftWeight + right[0] * rightWeight),
    Math.round(left[1] * leftWeight + right[1] * rightWeight),
    Math.round(left[2] * leftWeight + right[2] * rightWeight),
  ]
}

function serializeRGB([red, green, blue]: RGB): string {
  return `rgb(${red}, ${green}, ${blue})`
}

function createCytoscapePalette() {
  const styles = getComputedStyle(document.documentElement)
  const accent = readColorToken(styles, '--color-accent', '#0f62fe')
  const success = readColorToken(styles, '--color-success', '#24a148')
  const danger = readColorToken(styles, '--color-danger', '#da1e28')
  const warn = readColorToken(styles, '--color-warn', '#f1c21b')
  const surface = readColorToken(styles, '--color-surface', '#ffffff')
  const textPrimary = readColorToken(styles, '--color-text-primary', '#161616')
  const textSecondary = readColorToken(styles, '--color-text-secondary', '#525252')

  const typeColors: Record<string, string> = {
    change: serializeRGB(accent),
    proposal: serializeRGB(mixRGB(accent, danger, 0.45)),
    design: serializeRGB(mixRGB(success, accent, 0.55)),
    tasks: serializeRGB(warn),
    spec: serializeRGB(mixRGB(warn, danger, 0.7)),
    plan: serializeRGB(success),
    artifact: serializeRGB(textSecondary),
    diagram: serializeRGB(danger),
  }
  const edgeColors: Record<string, string> = {
    implements: serializeRGB(accent),
    references: serializeRGB(success),
    generates: serializeRGB(warn),
  }

  return {
    accent: serializeRGB(accent),
    surface: serializeRGB(surface),
    textPrimary: serializeRGB(textPrimary),
    textSecondary: serializeRGB(textSecondary),
    typeColors,
    communityColors: [
      accent,
      success,
      danger,
      warn,
      mixRGB(accent, success, 0.6),
      mixRGB(accent, danger, 0.6),
      mixRGB(success, warn, 0.6),
      mixRGB(danger, warn, 0.6),
      mixRGB(accent, surface, 0.7),
      mixRGB(success, surface, 0.7),
      mixRGB(danger, surface, 0.7),
      mixRGB(warn, surface, 0.7),
    ].map(serializeRGB),
    edgeColors,
  }
}

const POLL_INTERVAL_MS = 3000
const MAX_POLL_ATTEMPTS = 20
const SEARCH_DEBOUNCE_MS = 300
export function WikiGraph({ onNodeClick }: { onNodeClick: (id: string) => void }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const cyRef = useRef<cytoscape.Core | null>(null)
  const [components, setComponents] = useState<WikiComponent[]>([])
  const [edges, setEdges] = useState<WikiEdge[]>([])
  const [communities, setCommunities] = useState<Record<string, number>>({})
  const [communityLabels, setCommunityLabels] = useState<Record<string, string>>({})
  const [gaveUp, setGaveUp] = useState(false)
  const [hover, setHover] = useState<{ title: string; x: number; y: number } | null>(null)
  // 关系边只占 794 个节点里的一小部分（约 222 条边、189 个有关联节点），其余
  // 605 个孤立节点会被 cose/grid 布局铺成一整屏色点，反而把真正的关系子图挤到
  // 角落。默认只显示有关联的节点，把关系子图放到画面中心；没有边时该开关无意义
  // （筛选后会清空画布），因此仅显示全部节点。
  const [connectedOnly, setConnectedOnly] = useState(true)
  const [activeWorkspaces, setActiveWorkspaces] = useState<Set<string> | null>(null)
  const [activeCommunity, setActiveCommunity] = useState<number | null>(null)
  // Search-box state: each debounced keystroke calls POST
  // /api/wiki/search-semantic (server-side embed + cosine ranking) and
  // highlights the returned node ids in place, rather than fetching the
  // whole corpus and embedding client-side.
  const [searchQuery, setSearchQuery] = useState('')
  const [matchedIds, setMatchedIds] = useState<Set<string> | null>(null)

  useEffect(() => {
    let cancelled = false
    let attempts = 0
    let timer: number | undefined

    const poll = () => {
      fetchWikiGraph()
        .then((data) => {
          if (cancelled) return
          if (data.components.length > 0) {
            setComponents(data.components)
            setEdges(data.edges)
            setCommunities(data.communities ?? {})
            setCommunityLabels(data.communityLabels ?? {})
            return
          }
          setComponents([])
          setEdges([])
          attempts += 1
          if (attempts >= MAX_POLL_ATTEMPTS) {
            setGaveUp(true)
            return
          }
          timer = window.setTimeout(poll, POLL_INTERVAL_MS)
        })
        .catch(() => {
          if (cancelled) return
          setComponents([])
          setEdges([])
          setGaveUp(true)
        })
    }

    poll()

    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  // Refetches the graph once on demand -- wired to the SSE hook below so a
  // watcher-triggered rebuild (see wiki/watcher.go processBatch) refreshes
  // the canvas immediately instead of waiting for the next poll tick.
  const refetchGraph = useCallback(() => {
    fetchWikiGraph()
      .then((data) => {
        setComponents(data.components)
        setEdges(data.edges)
        setCommunities(data.communities ?? {})
        setCommunityLabels(data.communityLabels ?? {})
      })
      .catch(() => {})
  }, [])
  useWikiEvents(refetchGraph)

  useEffect(() => {
    const trimmed = searchQuery.trim()
    if (trimmed === '') {
      setMatchedIds(null)
      return
    }
    let cancelled = false
    const timer = window.setTimeout(() => {
      searchSemantic(trimmed)
        .then((results) => {
          if (cancelled) return
          setMatchedIds(new Set(results.map((r) => r.id)))
        })
        .catch(() => {
          if (cancelled) return
          setMatchedIds(new Set())
        })
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [searchQuery])

  const hasEdges = edges.length > 0
  const topCommunities = Object.entries(
    Object.values(communities).reduce<Record<number, number>>((counts, id) => {
      if (id >= 0) counts[id] = (counts[id] ?? 0) + 1
      return counts
    }, {}),
  )
    .sort((a, b) => b[1] - a[1])
    .slice(0, 8)
    .map(([id]) => Number(id))
  // 后端提供了带语义的 communityLabels 时优先使用；否则退回 "#id" 占位符，
  // 保持旧数据（无 communityLabels 字段）下图例仍可用。
  const effectiveCommunityLabels = useMemo(() => {
    const hasLabels = Object.keys(communityLabels).length > 0
    return topCommunities.reduce<Record<string, string>>((acc, id) => {
      acc[String(id)] = hasLabels ? (communityLabels[String(id)] ?? `#${id}`) : `#${id}`
      return acc
    }, {})
  }, [communityLabels, topCommunities])
  const workspaces = useMemo(() => {
    const set = new Set<string>()
    components.forEach((c) => set.add(c.workspace))
    return [...set].sort()
  }, [components])

  function toggleWorkspace(ws: string) {
    setActiveWorkspaces((prev) => {
      // null means "all active"; materialize the full set minus the clicked
      // one so a single click deselects it instead of selecting only it.
      const base = prev ?? new Set(workspaces)
      const next = new Set(base)
      if (next.has(ws)) next.delete(ws)
      else next.add(ws)
      return next
    })
  }

  useEffect(() => {
    if (!containerRef.current || components.length === 0) return
    const typeOrder = Object.keys(TYPE_COLORS)
    const sorted = [...components].sort(
      (a, b) => typeOrder.indexOf(a.type) - typeOrder.indexOf(b.type),
    )
    // null 表示"未筛选，显示全部"（初始态或用户重新全选）。
    const wsFiltered =
      activeWorkspaces === null ? sorted : sorted.filter((c) => activeWorkspaces.has(c.workspace))
    const filtered =
      activeCommunity === null ? wsFiltered : wsFiltered.filter((c) => communities[c.id] === activeCommunity)
    const componentIds = new Set(filtered.map((c) => c.id))
    // Edges may reference an endpoint the frontend never fetched a node for
    // (e.g. a markdown link target outside the scanned workspace) -- cytoscape
    // throws if an edge names a nonexistent node, so drop those defensively
    // rather than let one bad edge blank the whole graph.
    // Exclude vector/similar edges from visualization — they're too dense
    // (2500+) and make the force layout crawl. They serve search/community,
    // not the graph view. Only structural edges (yaml, markdown-link,
    // convention-internal) are rendered.
    const validEdges = edges
      .filter((e) => e.source !== 'vector' && e.source !== 'bm25')
      .filter((e) => componentIds.has(e.from) && componentIds.has(e.to))
    const connectedIds = new Set<string>()
    validEdges.forEach((e) => {
      connectedIds.add(e.from)
      connectedIds.add(e.to)
    })
    // 794 个节点里约 605 个是孤立节点（无任何关系边），全部铺开会把布局压成
    // 一整屏色点，把真正有价值的关系子图挤到角落。仅显示有关联的节点时把
    // 孤立节点滤掉，让关系子图占满画布；没有边时该过滤没有意义（会清空画布）。
    const visible =
      connectedOnly && validEdges.length > 0 ? filtered.filter((c) => connectedIds.has(c.id)) : filtered
    const visibleIds = new Set(visible.map((c) => c.id))
    const visibleEdges = validEdges.filter((e) => visibleIds.has(e.from) && visibleIds.has(e.to))
    const container = containerRef.current
    // Cytoscape parses colors itself rather than through the browser CSS engine.
    // Resolve Carbon tokens and color mixes to concrete rgb() values first.
    const palette = createCytoscapePalette()
    const cy = cytoscape({
      container,
      elements: [
        ...visible.map((c) => {
          const commColor =
            communities[c.id] != null && communities[c.id] >= 0
              ? palette.communityColors[communities[c.id] % palette.communityColors.length]
              : palette.surface
          return {
            data: {
              id: c.id,
              label: c.title,
              color: palette.typeColors[c.type] ?? palette.textSecondary,
              commColor,
            },
          }
        }),
        ...visibleEdges.map((e, i) => ({
          data: {
            id: `e${i}`,
            source: e.from,
            target: e.to,
            kind: e.kind,
            color: palette.edgeColors[e.kind] ?? palette.textSecondary,
          },
        })),
      ],
      style: [
        {
          selector: 'node',
          style: {
            'background-color': 'data(color)',
            label: 'data(label)',
            'font-size': 7,
            'min-zoomed-font-size': 9,
            color: palette.textPrimary,
            'text-valign': 'bottom',
            'text-margin-y': 3,
            'text-wrap': 'ellipsis',
            'text-max-width': '80px',
            width: 14,
            height: 14,
            'border-width': 2,
            'border-color': 'data(commColor)',
          },
        },
        {
          selector: 'node.hovered',
          style: {
            'border-width': 2.5,
            'border-color': palette.accent,
          },
        },
        {
          selector: 'edge',
          style: {
            width: 0.75,
            'line-color': 'data(color)',
            'target-arrow-color': 'data(color)',
            'target-arrow-shape': 'triangle',
            'arrow-scale': 0.6,
            'curve-style': 'bezier',
            opacity: 0.55,
          },
        },
        {
          selector: 'edge.highlighted',
          style: {
            width: 1.75,
            opacity: 1,
          },
        },
        {
          selector: 'edge[kind="similar"]',
          style: {
            'line-style': 'dashed',
            opacity: 0.3,
            width: 0.5,
          },
        },
        {
          selector: 'node.search-match',
          style: {
            'border-width': 3,
            'border-color': palette.accent,
            'z-index': 10,
          },
        },
        {
          selector: 'node.search-dim',
          style: {
            opacity: 0.25,
          },
        },
      ],
      // 只要索引提供了关系边，就用 cose 力导向布局把结构关系可视化出来（implements/
      // references/generates）；极少数没有任何关系边的情况下退回固定网格布局，
      // 保证 fit() 之后所有节点仍在可视区域内、大小一致、按类型分组。
      layout:
        visibleEdges.length > 0
          ? { name: 'cose', animate: false, padding: 30, nodeRepulsion: 8000 }
          : { name: 'grid', avoidOverlap: true, avoidOverlapPadding: 8, condense: false },
      userZoomingEnabled: true,
      userPanningEnabled: true,
      wheelSensitivity: 0.2,
    })
    cyRef.current = cy
    cy.one('layoutstop', () => cy.fit(undefined, 30))
    cy.on('tap', 'node', (evt) => onNodeClick(evt.target.id()))
    cy.on('mouseover', 'node', (evt) => {
      const node = evt.target
      node.addClass('hovered')
      node.connectedEdges().addClass('highlighted')
      container.style.cursor = 'pointer'
      const pos = node.renderedPosition()
      setHover({ title: node.data('label') as string, x: pos.x, y: pos.y })
    })
    cy.on('mouseout', 'node', (evt) => {
      const node = evt.target
      node.removeClass('hovered')
      node.connectedEdges().removeClass('highlighted')
      container.style.cursor = 'default'
      setHover(null)
    })
    return () => {
      cy.destroy()
      cyRef.current = null
    }
  }, [components, edges, communities, connectedOnly, activeWorkspaces, activeCommunity, onNodeClick])

  // Applies/clears the highlight classes on the LIVE cytoscape instance
  // whenever the match set or the underlying graph changes -- separate from
  // the graph-build effect above so re-searching doesn't tear down and
  // rebuild the whole cytoscape instance (which would reset pan/zoom/layout).
  useEffect(() => {
    const cy = cyRef.current
    if (!cy) return
    if (matchedIds === null) {
      cy.nodes().removeClass('search-match').removeClass('search-dim')
      return
    }
    cy.batch(() => {
      cy.nodes().forEach((node) => {
        const isMatch = matchedIds.has(node.id())
        node.toggleClass('search-match', isMatch)
        node.toggleClass('search-dim', !isMatch)
      })
    })
  }, [matchedIds, components, edges, communities, connectedOnly, activeWorkspaces, activeCommunity])

  return (
    <div className="flex h-[calc(100vh-160px)] min-h-[500px] w-full flex-col">
      {components.length > 0 && workspaces.length > 1 && (
        <GraphFilters
          workspaces={workspaces}
          activeWorkspaces={activeWorkspaces ?? new Set(workspaces)}
          onToggleWorkspace={toggleWorkspace}
          communityLabels={{}}
          activeCommunity={activeCommunity}
          onSelectCommunity={setActiveCommunity}
        />
      )}
      <div className="relative flex-1">
        <div ref={containerRef} data-testid="wiki-graph-canvas" className="w-full h-full" />
        {hover && (
          <div
            data-testid="wiki-graph-tooltip"
            className="pointer-events-none absolute z-20 -translate-x-1/2 -translate-y-full rounded border border-[var(--color-border)] bg-white px-2 py-1 text-xs text-[var(--color-text-primary)] shadow-sm"
            style={{ left: hover.x, top: hover.y - 10 }}
          >
            {hover.title}
          </div>
        )}
        {components.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center text-xs text-[var(--color-text-secondary)]">
            {gaveUp ? (
              <span>索引为空，请先注册工作区并重建（POST /api/wiki/rebuild）</span>
            ) : (
              <span className="animate-pulse">索引构建中…</span>
            )}
          </div>
        )}
        {components.length > 0 && (
          <>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="语义搜索节点…"
              aria-label="图谱语义搜索"
              className="absolute left-2 top-2 z-10 w-48 rounded border border-[var(--color-border)] bg-white px-2 py-1 text-xs text-[var(--color-text-primary)] shadow-sm outline-none focus:border-[var(--color-accent)]"
            />
            <div className="absolute left-2 top-11 z-10 flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => cyRef.current?.fit(undefined, 30)}
                className="rounded border border-[var(--color-border)] bg-white px-2 py-1 text-xs text-[var(--color-text-primary)] shadow-sm hover:bg-[var(--color-bg)]"
              >
                适应窗口
              </button>
              {hasEdges && (
                <label className="flex items-center gap-1 rounded border border-[var(--color-border)] bg-white px-2 py-1 text-xs text-[var(--color-text-primary)] shadow-sm">
                  <input
                    type="checkbox"
                    checked={connectedOnly}
                    onChange={(e) => setConnectedOnly(e.target.checked)}
                  />
                  仅显示有关联的节点
                </label>
              )}
            </div>
            {/* 类型图例 — 左下角 */}
            <div
              data-testid="wiki-graph-legend"
              className="absolute left-2 bottom-2 z-10 w-28 max-h-[50vh] overflow-y-auto border border-[var(--color-border)] bg-white/95 px-2 py-1.5 text-xs text-[var(--color-text-primary)] shadow-sm"
            >
              <div className="mb-1 font-medium text-[var(--color-text-secondary)]">类型</div>
              <ul className="space-y-0.5">
                {Object.entries(TYPE_COLORS).map(([type, color]) => (
                  <li key={type} className="flex items-center gap-1.5">
                    <span className="inline-block h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: color }} />
                    <span className="truncate">{type}</span>
                  </li>
                ))}
              </ul>
            </div>
            {/* 社区图例 — 右下角 */}
            {topCommunities.length > 0 && (
              <div
                data-testid="wiki-graph-community-legend"
                className="absolute right-2 bottom-2 z-10 w-44 max-h-[50vh] overflow-y-auto border border-[var(--color-border)] bg-white/95 px-2 py-1.5 text-xs text-[var(--color-text-primary)] shadow-sm"
              >
                <div className="mb-1 font-medium text-[var(--color-text-secondary)]">社区</div>
                <ul className="space-y-0.5">
                  {topCommunities.map((id) => {
                    const active = activeCommunity === id
                    return (
                      <li key={id}>
                        <button
                          type="button"
                          data-testid="wiki-graph-community-legend-item"
                          aria-pressed={active}
                          onClick={() => setActiveCommunity(active ? null : id)}
                          className={
                            active
                              ? 'flex w-full items-center gap-1 bg-[color-mix(in_srgb,var(--color-text-primary)_10%,var(--color-surface))] px-1 py-0.5 text-left'
                              : 'flex w-full items-center gap-1 px-1 py-0.5 text-left hover:bg-[var(--color-bg)]'
                          }
                        >
                          <span
                            className="inline-block h-2 w-2 shrink-0 rounded-full border border-[color-mix(in_srgb,var(--color-text-primary)_10%,var(--color-surface))]"
                            style={{ backgroundColor: COMMUNITY_COLORS[id % COMMUNITY_COLORS.length] }}
                          />
                          <span className="truncate">{effectiveCommunityLabels[String(id)] ?? `#${id}`}</span>
                        </button>
                      </li>
                    )
                  })}
                </ul>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
