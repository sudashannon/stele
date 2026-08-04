import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import cytoscape from 'cytoscape'
import { fetchCommunityOverview, fetchWikiGraph, searchSemantic } from '../api/client'
import type { WikiComponent, WikiEdge } from '../api/types'
import { GraphFilters } from './GraphFilters'
import { useWikiEvents } from '../hooks/useWikiEvents'
import { Modal } from './Modal'
import { Icon } from './icons'
import { communityColor, COMMUNITY_REST_COLOR, TYPE_COLORS } from './graphPalette'


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
  const layer = readColorToken(styles, '--color-layer', '#f4f4f4')
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

  // Read --viz-1…--viz-8 from the token layer so the graph palette matches
  // the timeline and filter chips. Fallback hex values mirror styles.css.
  const vizFallbacks = ['#0a2f7a', '#1247a8', '#1a5fd0', '#4180e0', '#6f9fe9', '#9dbef1', '#c3d7f7', '#e0eafc']
  const vizColors = vizFallbacks.map((fallback, i) =>
    readColorToken(styles, `--viz-${i + 1}`, fallback),
  )
  const vizRest = readColorToken(styles, '--viz-rest', '#b6c0ca')

  return {
    accent: serializeRGB(accent),
    layer: serializeRGB(layer),
    surface: serializeRGB(surface),
    textPrimary: serializeRGB(textPrimary),
    textSecondary: serializeRGB(textSecondary),
    typeColors,
    vizColors: vizColors.map(serializeRGB),
    vizRest: serializeRGB(vizRest),
    edgeColors,
  }
}

const POLL_INTERVAL_MS = 3000
const MAX_POLL_ATTEMPTS = 20
const SEARCH_DEBOUNCE_MS = 300
const MAX_COSE_NODE_COUNT = 250

function labelForCommunity(id: number, labels: Record<string, string>) {
  return labels[String(id)] ?? `#${id}`
}

export function WikiGraph({ onNodeClick }: { onNodeClick: (id: string) => void }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const cyRef = useRef<cytoscape.Core | null>(null)
  const overviewRequestRef = useRef(0)
  const [components, setComponents] = useState<WikiComponent[]>([])
  const [edges, setEdges] = useState<WikiEdge[]>([])
  const [communities, setCommunities] = useState<Record<string, number>>({})
  const [communityLabels, setCommunityLabels] = useState<Record<string, string>>({})
  const [gaveUp, setGaveUp] = useState(false)
  const [hover, setHover] = useState<{ title: string; x: number; y: number } | null>(null)
  const [connectedOnly, setConnectedOnly] = useState(true)
  const [activeWorkspaces, setActiveWorkspaces] = useState<Set<string> | null>(null)
  const [activeCommunity, setActiveCommunity] = useState<number | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [matchedIds, setMatchedIds] = useState<Set<string> | null>(null)
  const [searchState, setSearchState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle')
  const [selectedNodeTitle, setSelectedNodeTitle] = useState<string | null>(null)
  const [overviewOpen, setOverviewOpen] = useState(false)
  const [overviewLoading, setOverviewLoading] = useState(false)
  const [overviewError, setOverviewError] = useState<string | null>(null)
  const [overviewBody, setOverviewBody] = useState('')

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
            setGaveUp(false)
            return
          }
          setComponents([])
          setEdges([])
          setCommunities({})
          setCommunityLabels({})
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
          setCommunities({})
          setCommunityLabels({})
          attempts += 1
          if (attempts >= MAX_POLL_ATTEMPTS) {
            setGaveUp(true)
            return
          }
          timer = window.setTimeout(poll, POLL_INTERVAL_MS)
        })
    }

    poll()

    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  const refetchGraph = useCallback(() => {
    fetchWikiGraph()
      .then((data) => {
        setComponents(data.components)
        setEdges(data.edges)
        setCommunities(data.communities ?? {})
        setCommunityLabels(data.communityLabels ?? {})
        setGaveUp(false)
      })
      .catch(() => {})
  }, [])
  useWikiEvents(refetchGraph)

  useEffect(() => {
    const trimmed = searchQuery.trim()
    if (trimmed === '') {
      setMatchedIds(null)
      setSearchState('idle')
      return
    }
    const controller = new AbortController()
    let cancelled = false
    setMatchedIds(null)
    setSearchState('loading')
    const timer = window.setTimeout(() => {
      searchSemantic(trimmed, 10, controller.signal)
        .then((results) => {
          if (cancelled) return
          setMatchedIds(new Set(results.map((result) => result.id)))
          setSearchState('ready')
        })
        .catch((error) => {
          if (cancelled || error?.name === 'AbortError') return
          setMatchedIds(new Set())
          setSearchState('error')
        })
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [searchQuery])

  const typeOrder = useMemo(() => Object.keys(TYPE_COLORS), [])
  const typeRank = useCallback((type: string) => {
    const index = typeOrder.indexOf(type)
    return index === -1 ? typeOrder.length : index
  }, [typeOrder])
  const sortedComponents = useMemo(
    () => [...components].sort((a, b) => typeRank(a.type) - typeRank(b.type)),
    [components, typeRank],
  )

  const workspaces = useMemo(() => {
    const set = new Set<string>()
    components.forEach((component) => set.add(component.workspace))
    return [...set].sort()
  }, [components])

  useEffect(() => {
    setActiveWorkspaces((prev) => {
      if (workspaces.length === 0) return null
      if (prev === null) return null
      const next = new Set([...prev].filter((workspace) => workspaces.includes(workspace)))
      if (next.size === workspaces.length) return null
      return next
    })
  }, [workspaces])

  const toggleWorkspace = useCallback((workspace: string) => {
    setActiveWorkspaces((prev) => {
      const base = prev ?? new Set(workspaces)
      const next = new Set(base)
      if (next.has(workspace)) next.delete(workspace)
      else next.add(workspace)
      if (next.size === workspaces.length) return null
      return next
    })
  }, [workspaces])

  const resetFilters = useCallback(() => {
    setActiveWorkspaces(null)
    setActiveCommunity(null)
  }, [])

  const workspaceFilteredComponents = useMemo(() => {
    if (activeWorkspaces === null) return sortedComponents
    return sortedComponents.filter((component) => activeWorkspaces.has(component.workspace))
  }, [sortedComponents, activeWorkspaces])

  const communityCounts = useMemo(() => {
    const counts: Record<number, number> = {}
    workspaceFilteredComponents.forEach((component) => {
      const communityId = communities[component.id]
      if (communityId === null || communityId === undefined || communityId < 0) return
      counts[communityId] = (counts[communityId] ?? 0) + 1
    })
    return counts
  }, [workspaceFilteredComponents, communities])

  const topCommunities = useMemo(
    () =>
      Object.entries(communityCounts)
        .sort((left, right) => right[1] - left[1])
        .slice(0, 8)
        .map(([id]) => Number(id)),
    [communityCounts],
  )

  const communityRank = useMemo(() => {
    const sorted = Object.entries(communityCounts)
      .sort(([, a], [, b]) => b - a)
      .map(([idStr]) => Number(idStr))
    return new Map(sorted.map((id, rank) => [id, rank]))
  }, [communityCounts])

  const effectiveCommunityLabels = useMemo(() => {
    const labels: Record<string, string> = { ...communityLabels }
    topCommunities.forEach((id) => {
      labels[String(id)] = labels[String(id)] ?? `#${id}`
    })
    return labels
  }, [communityLabels, topCommunities])

  useEffect(() => {
    if (activeCommunity === null) return
    if (communityCounts[activeCommunity] === null || communityCounts[activeCommunity] === undefined) {
      setActiveCommunity(null)
    }
  }, [activeCommunity, communityCounts])

  const communityFilteredComponents = useMemo(() => {
    if (activeCommunity === null) return workspaceFilteredComponents
    return workspaceFilteredComponents.filter((component) => communities[component.id] === activeCommunity)
  }, [workspaceFilteredComponents, communities, activeCommunity])

  const structuralEdges = useMemo(
    () => edges.filter((edge) => edge.source !== 'vector' && edge.source !== 'bm25' && edge.source !== 'tag' && edge.source !== 'session'),
    [edges],
  )

  const componentIds = useMemo(
    () => new Set(communityFilteredComponents.map((component) => component.id)),
    [communityFilteredComponents],
  )

  const validEdges = useMemo(
    () => structuralEdges.filter((edge) => componentIds.has(edge.from) && componentIds.has(edge.to)),
    [structuralEdges, componentIds],
  )

  const connectedIds = useMemo(() => {
    const ids = new Set<string>()
    validEdges.forEach((edge) => {
      ids.add(edge.from)
      ids.add(edge.to)
    })
    return ids
  }, [validEdges])

  const visibleComponents = useMemo(() => {
    if (!connectedOnly || validEdges.length === 0) return communityFilteredComponents
    return communityFilteredComponents.filter((component) => connectedIds.has(component.id))
  }, [communityFilteredComponents, connectedIds, connectedOnly, validEdges.length])

  const visibleIds = useMemo(() => new Set(visibleComponents.map((component) => component.id)), [visibleComponents])
  const visibleEdges = useMemo(
    () => validEdges.filter((edge) => visibleIds.has(edge.from) && visibleIds.has(edge.to)),
    [validEdges, visibleIds],
  )

  const hiddenByFilters = components.length - communityFilteredComponents.length
  const hiddenByConnectedOnly = communityFilteredComponents.length - visibleComponents.length
  const activeCommunityLabel =
    activeCommunity === null ? null : labelForCommunity(activeCommunity, effectiveCommunityLabels)
  const visibilitySummary = useMemo(() => {
    const parts = [`显示 ${visibleComponents.length} / ${components.length} 节点`]
    if (hiddenByFilters > 0) parts.push(`筛选隐藏 ${hiddenByFilters} 个`)
    if (hiddenByConnectedOnly > 0) parts.push(`仅关联视图隐藏 ${hiddenByConnectedOnly} 个孤立节点`)
    return parts.join(' · ')
  }, [components.length, hiddenByConnectedOnly, hiddenByFilters, visibleComponents.length])

  const filterSummary = useMemo(() => {
    const hidden = components.length - communityFilteredComponents.length
    const parts = [`工作区/社区范围 ${communityFilteredComponents.length} / ${components.length}`]
    if (hidden > 0) parts.push(`隐藏 ${hidden} 个`)
    return parts.join(' · ')
  }, [communityFilteredComponents.length, components.length])

  const searchSummary = useMemo(() => {
    if (searchQuery.trim() === '') return null
    if (searchState === 'loading') return '正在搜索语义相近节点…'
    if (searchState === 'error') return '语义搜索暂时不可用'
    if (matchedIds) return `匹配 ${matchedIds.size} 个节点`
    return null
  }, [matchedIds, searchQuery, searchState])

  useEffect(() => {
    setSelectedNodeTitle(null)
    const container = containerRef.current
    if (!container || visibleComponents.length === 0) return
    const palette = createCytoscapePalette()
    let selectedNode: cytoscape.NodeSingular | null = null
    const cy = cytoscape({
      container,
      elements: [
        ...visibleComponents.map((component) => {
          const communityId = communities[component.id]
          const commColor =
            communityId !== null && communityId !== undefined && communityId >= 0
              ? (communityRank.has(communityId)
                  ? palette.vizColors[communityRank.get(communityId)!]
                  : palette.vizRest)
              : palette.surface
          return {
            data: {
              id: component.id,
              label: component.title,
              color: palette.typeColors[component.type] ?? palette.textSecondary,
              commColor,
            },
          }
        }),
        ...visibleEdges.map((edge, index) => ({
          data: {
            id: `e${index}`,
            source: edge.from,
            target: edge.to,
            kind: edge.kind,
            color: palette.edgeColors[edge.kind] ?? palette.textSecondary,
          },
        })),
      ],
      style: [
        {
          selector: 'node',
          style: {
            'background-color': 'data(color)',
            label: 'data(label)',
            'font-size': 9,
            'min-zoomed-font-size': 9,
            color: palette.textPrimary,
            'text-valign': 'bottom',
            'text-margin-y': 4,
            'text-wrap': 'ellipsis',
            'text-max-width': '96px',
            'text-background-color': palette.surface,
            'text-background-opacity': 0.92,
            'text-background-padding': '2px',
            width: 14,
            height: 14,
            'border-width': 2,
            'border-color': 'data(commColor)',
          },
        },
        {
          selector: 'node.hovered',
          style: {
            'border-width': 3,
            'border-color': palette.accent,
          },
        },
        {
          selector: 'node.selected',
          style: {
            'border-width': 3.5,
            'border-color': palette.textPrimary,
            'text-background-color': palette.layer,
          },
        },
        {
          selector: 'edge',
          style: {
            width: 0.9,
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
      layout:
        visibleEdges.length === 0
          ? { name: 'grid', avoidOverlap: true, avoidOverlapPadding: 8, condense: false }
          : visibleComponents.length <= MAX_COSE_NODE_COUNT
            ? { name: 'cose', animate: false, padding: 30, nodeRepulsion: 8000 }
            : { name: 'concentric', animate: false, padding: 30, minNodeSpacing: 12, avoidOverlap: true },
      userZoomingEnabled: true,
      userPanningEnabled: true,
      wheelSensitivity: 0.2,
    })
    cyRef.current = cy
    cy.one('layoutstop', () => cy.fit(undefined, 30))
    cy.on('tap', 'node', (event) => {
      selectedNode?.removeClass('selected')
      selectedNode = event.target as cytoscape.NodeSingular
      selectedNode.addClass('selected')
      setSelectedNodeTitle(selectedNode.data('label') as string)
      onNodeClick(event.target.id())
    })
    cy.on('mouseover', 'node', (event) => {
      const node = event.target
      node.addClass('hovered')
      node.connectedEdges().addClass('highlighted')
      container.style.cursor = 'pointer'
      const pos = node.renderedPosition()
      setHover({ title: node.data('label') as string, x: pos.x, y: pos.y })
    })
    cy.on('mouseout', 'node', (event) => {
      const node = event.target
      node.removeClass('hovered')
      node.connectedEdges().removeClass('highlighted')
      container.style.cursor = 'default'
      setHover(null)
    })
    return () => {
      cy.destroy()
      cyRef.current = null
    }
  }, [communities, onNodeClick, visibleComponents, visibleEdges])

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
  }, [matchedIds, visibleComponents, visibleEdges])

  useEffect(() => {
    if (!overviewOpen || activeCommunity === null) return
    const requestId = ++overviewRequestRef.current
    setOverviewLoading(true)
    setOverviewError(null)
    setOverviewBody('')
    fetchCommunityOverview(activeCommunity)
      .then((body) => {
        if (requestId !== overviewRequestRef.current) return
        setOverviewBody(body)
        setOverviewLoading(false)
      })
      .catch((error) => {
        if (requestId !== overviewRequestRef.current) return
        setOverviewError(error instanceof Error ? error.message : '加载社区综述失败')
        setOverviewLoading(false)
      })
  }, [activeCommunity, overviewOpen])

  useEffect(() => {
    if (activeCommunity !== null) return
    setOverviewOpen(false)
  }, [activeCommunity])

  return (
    <div className="flex h-full min-h-[500px] w-full flex-col">
      {components.length > 0 && (
        <GraphFilters
          workspaces={workspaces}
          activeWorkspaces={activeWorkspaces ?? new Set(workspaces)}
          onToggleWorkspace={toggleWorkspace}
          onResetFilters={resetFilters}
          communityLabels={effectiveCommunityLabels}
          communityCounts={communityCounts}
          activeCommunity={activeCommunity}
          onSelectCommunity={setActiveCommunity}
          summary={filterSummary}
        />
      )}
      <div className="relative flex-1 border-x border-b border-[var(--color-border)] bg-[var(--color-surface)]">
        {components.length > 0 && (
          <div className="absolute left-3 top-3 z-10 flex max-w-[28rem] flex-col gap-2">
            <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-2">
              <label className="block text-[length:var(--type-caption)] font-medium text-[var(--color-text-secondary)]" htmlFor="wiki-graph-search">
                语义搜索
              </label>
              <div className="mt-1 flex items-center gap-2">
                <Icon name="search" size={14} className="text-[var(--color-text-secondary)]" />
                <input
                  id="wiki-graph-search"
                  type="text"
                  value={searchQuery}
                  onChange={(event) => setSearchQuery(event.target.value)}
                  placeholder="搜索相近节点标题…"
                  aria-label="图谱语义搜索"
                  className="w-56 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] outline-none focus:border-[var(--color-accent)]"
                />
              </div>
              {searchSummary && (
                <div className="mt-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{searchSummary}</div>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] p-2">
              <button
                type="button"
                onClick={() => cyRef.current?.fit(undefined, 30)}
                className="inline-flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
              >
                <Icon name="refresh" size={14} />
                适应窗口
              </button>
              {visibleComponents.length > 0 && (
                <div
                  data-testid="wiki-graph-visibility-summary"
                  className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"
                >
                  {visibilitySummary}
                </div>
              )}
              {edges.length > 0 && (
                <label className="inline-flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
                  <input
                    type="checkbox"
                    checked={connectedOnly}
                    onChange={(event) => setConnectedOnly(event.target.checked)}
                  />
                  仅显示有关联的节点
                </label>
              )}
              {selectedNodeTitle && (
                <div className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                  已选中：<span className="text-[var(--color-text-primary)]">{selectedNodeTitle}</span>
                </div>
              )}
            </div>
          </div>
        )}

        <div ref={containerRef} data-testid="wiki-graph-canvas" className="h-full w-full" />

        {hover && (
          <div
            data-testid="wiki-graph-tooltip"
            className="pointer-events-none absolute z-20 -translate-x-1/2 -translate-y-full border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] shadow-[var(--shadow-overlay)]"
            style={{ left: hover.x, top: hover.y - 10 }}
          >
            {hover.title}
          </div>
        )}

        {components.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            {gaveUp ? (
              <span>索引为空，请先注册工作区并重建（POST /api/wiki/rebuild）</span>
            ) : (
              <span className="animate-pulse">索引构建中…</span>
            )}
          </div>
        )}

        {components.length > 0 && visibleComponents.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            没有匹配当前筛选条件的节点
          </div>
        )}

        {components.length > 0 && (
          <>
            <div
              data-testid="wiki-graph-legend"
              className="absolute bottom-3 left-3 z-10 w-40 border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-[length:var(--type-caption)] text-[var(--color-text-primary)]"
            >
              <div className="mb-2 font-semibold text-[var(--color-text-secondary)]">类型</div>
              <ul className="space-y-1">
                {Object.entries(TYPE_COLORS).map(([type, color]) => (
                  <li key={type} className="flex items-center gap-2">
                    <span className="inline-block h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: color }} />
                    <span className="truncate">{type}</span>
                  </li>
                ))}
              </ul>
            </div>

            {topCommunities.length > 0 && (
              <div
                data-testid="wiki-graph-community-legend"
                className="absolute bottom-3 right-3 z-10 w-60 border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-[length:var(--type-caption)] text-[var(--color-text-primary)]"
              >
                <div className="mb-2 flex items-center justify-between gap-2">
                  <span className="font-semibold text-[var(--color-text-secondary)]">社区</span>
                  {activeCommunity !== null && (
                    <button
                      type="button"
                      onClick={() => setOverviewOpen(true)}
                      className="inline-flex items-center gap-1 border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
                    >
                      <Icon name="info" size={14} />
                      社区综述
                    </button>
                  )}
                </div>
                <ul className="space-y-1">
                  {topCommunities.map((id, rank) => {
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
                              ? 'flex w-full items-center justify-between gap-2 border border-[var(--color-text-primary)] bg-[var(--color-layer)] px-2 py-1 text-left'
                              : 'flex w-full items-center justify-between gap-2 border border-transparent px-2 py-1 text-left hover:border-[var(--color-border)] hover:bg-[var(--color-layer)]'
                          }
                        >
                          <span className="flex min-w-0 items-center gap-2">
                            <span
                              className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                              style={{ backgroundColor: communityColor(rank) }}
                            />
                            <span className="truncate">{labelForCommunity(id, effectiveCommunityLabels)}</span>
                          </span>
                          <span className="shrink-0 text-[var(--color-text-secondary)]">{communityCounts[id] ?? 0}</span>
                        </button>
                      </li>
                    )
                  })}
                  {Object.keys(communityCounts).length > topCommunities.length && (
                    <li
                      data-testid="wiki-graph-community-legend-item"
                      className="flex items-center gap-2 px-2 py-1"
                    >
                      <span
                        className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                        style={{ backgroundColor: COMMUNITY_REST_COLOR }}
                      />
                      <span className="truncate">其他 {Object.keys(communityCounts).length - topCommunities.length} 个社区</span>
                    </li>
                  )}
                </ul>
              </div>
            )}
          </>
        )}
      </div>

      {overviewOpen && activeCommunity !== null && (
        <Modal
          title={`${activeCommunityLabel ?? `#${activeCommunity}`} 综述`}
          onClose={() => setOverviewOpen(false)}
          width="max-w-2xl"
          data-testid="community-overview-modal"
        >
          <div className="space-y-3 text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
            {overviewLoading && <p>正在加载社区综述…</p>}
            {!overviewLoading && overviewError && (
              <p className="text-[var(--color-danger-text)]">{overviewError}</p>
            )}
            {!overviewLoading && !overviewError && overviewBody && (
              <div data-testid="community-overview-body" className="whitespace-pre-wrap leading-6">
                {overviewBody}
              </div>
            )}
          </div>
        </Modal>
      )}
    </div>
  )
}
