import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import cytoscape from 'cytoscape'
import { fetchCommunityOverview, fetchWikiGraph, searchSemantic } from '../api/client'
import type { WikiComponent, WikiEdge } from '../api/types'
import { GraphFilters } from './GraphFilters'
import { useWikiEvents } from '../hooks/useWikiEvents'
import { Modal } from './Modal'
import { Icon } from './icons'
import { TYPE_SHAPES, TYPE_SHAPE_ORDER } from './graphPalette'


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


function serializeRGB([red, green, blue]: RGB): string {
  return `rgb(${red}, ${green}, ${blue})`
}

function createCytoscapePalette() {
  const styles = getComputedStyle(document.documentElement)
  const accent = readColorToken(styles, '--color-accent', '#0f62fe')
  const success = readColorToken(styles, '--color-success', '#24a148')
  const warn = readColorToken(styles, '--color-warn', '#f1c21b')
  const surface = readColorToken(styles, '--color-surface', '#ffffff')
  const layer = readColorToken(styles, '--color-layer', '#f4f4f4')
  const textPrimary = readColorToken(styles, '--color-text-primary', '#161616')
  const textSecondary = readColorToken(styles, '--color-text-secondary', '#525252')

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
  const vizAxis = readColorToken(styles, '--viz-axis', '#7d8794')
  const vizRest = readColorToken(styles, '--viz-rest', '#b6c0ca')

  return {
    accent: serializeRGB(accent),
    layer: serializeRGB(layer),
    surface: serializeRGB(surface),
    textPrimary: serializeRGB(textPrimary),
    textSecondary: serializeRGB(textSecondary),
    vizColors: vizColors.map(serializeRGB),
    vizRest: serializeRGB(vizRest),
    vizAxis: serializeRGB(vizAxis),
    edgeColors,
  }
}

const POLL_INTERVAL_MS = 3000
const MAX_POLL_ATTEMPTS = 20
const SEARCH_DEBOUNCE_MS = 300

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
  const [expandedCommunity, setExpandedCommunity] = useState<number | null>(null)
  const [expandAllNodes, setExpandAllNodes] = useState(false)
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

  const typeOrder = useMemo(() => TYPE_SHAPE_ORDER, [])
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
    setExpandedCommunity(null)
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

  // Community members grouped by community id — used for aggregation
  const communityMembers = useMemo(() => {
    const map: Record<number, string[]> = {}
    communityFilteredComponents.forEach((comp) => {
      const cid = communities[comp.id]
      if (cid !== undefined && cid !== null && cid >= 0) {
        (map[cid] ??= []).push(comp.id)
      }
    })
    return map
  }, [communityFilteredComponents, communities])

  // Clear expanded community when it has no members in the current filtered set
  useEffect(() => {
    if (expandedCommunity === null) return
    const members = communityMembers[expandedCommunity]
    if (!members || members.length === 0) {
      setExpandedCommunity(null)
    }
  }, [expandedCommunity, communityMembers])

  // Whether to aggregate into super-nodes — false when expandAllNodes or when
  // there are no communities to collapse into.
  const aggregateMode = !expandAllNodes && Object.keys(communityMembers).length > 0

  const hiddenByFilters = components.length - communityFilteredComponents.length
  const hiddenByConnectedOnly = communityFilteredComponents.length - visibleComponents.length
  const activeCommunityLabel =
    activeCommunity === null ? null : labelForCommunity(activeCommunity, effectiveCommunityLabels)
  const visibilitySummary = useMemo(() => {
    if (aggregateMode) {
      const communityCount = Object.keys(communityMembers).length
      const totalMemberCount = Object.values(communityMembers).reduce((sum, ids) => sum + ids.length, 0)
      // Nodes without a community are rendered individually even in aggregate mode
      const unassignedCount = communityFilteredComponents.length - totalMemberCount
      const drawnCount = communityCount + unassignedCount
      if (expandedCommunity !== null) {
        const expandedLabel = labelForCommunity(expandedCommunity, effectiveCommunityLabels)
        const expandedCount = (communityMembers[expandedCommunity] ?? []).length
        return `${expandedLabel} · ${expandedCount} 个节点已展开 · 共 ${drawnCount} 个社区`
      }
      const parts = [`${communityCount} 个社区 · 共 ${communityFilteredComponents.length} 个节点`]
      if (hiddenByConnectedOnly > 0) parts.push(`仅关联视图隐藏 ${hiddenByConnectedOnly} 个孤立节点`)
      return parts.join(' · ')
    }
    const parts = [`显示 ${visibleComponents.length} / ${components.length} 节点`]
    if (hiddenByFilters > 0) parts.push(`筛选隐藏 ${hiddenByFilters} 个`)
    if (hiddenByConnectedOnly > 0) parts.push(`仅关联视图隐藏 ${hiddenByConnectedOnly} 个孤立节点`)
    return parts.join(' · ')
  }, [aggregateMode, communityMembers, communityFilteredComponents.length, expandedCommunity, effectiveCommunityLabels, hiddenByConnectedOnly, components.length, hiddenByFilters, visibleComponents.length])

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
    if (!container) return

    const palette = createCytoscapePalette()

    // Resolve community colour for a single component
    const resolveCommColor = (componentId: string) => {
      const communityId = communities[componentId]
      if (communityId !== null && communityId !== undefined && communityId >= 0) {
        if (communityRank.has(communityId)) {
          return palette.vizColors[communityRank.get(communityId)!]
        }
        return palette.vizRest
      }
      return palette.surface
    }

    // Build node data for a single component (individual node)
    const individualNode = (component: WikiComponent) => ({
      data: {
        id: component.id,
        label: component.title,
        commColor: resolveCommColor(component.id),
        shape: TYPE_SHAPES[component.type] ?? 'ellipse',
      },
    })

    let nodes: Array<{ data: Record<string, unknown> }> = []
    let edges: Array<{ data: Record<string, unknown> }> = []
    let hasLayoutEdges = false

    if (!aggregateMode) {
      // ── Full individual node path ──
      if (visibleComponents.length === 0) return
      nodes = visibleComponents.map(individualNode)
      edges = visibleEdges.map((edge, index) => ({
        data: {
          id: `e${index}`,
          source: edge.from,
          target: edge.to,
          kind: edge.kind,
          color: palette.edgeColors[edge.kind] ?? palette.textSecondary,
        },
      }))
      hasLayoutEdges = visibleEdges.length > 0
    } else {
      // ── Aggregated or drill-down mode ──
      const assignedIds = new Set<string>()
      const memberSetByCommunity = new Map<number, Set<string>>()
      for (const [cid, ids] of Object.entries(communityMembers)) {
        const s = new Set(ids)
        memberSetByCommunity.set(Number(cid), s)
        ids.forEach((id) => assignedIds.add(id))
      }

      // Collect unassigned components (no community)
      const unassignedComponents = communityFilteredComponents.filter((c) => !assignedIds.has(c.id))

      const isExpanded = expandedCommunity !== null
      const expandedMemberIds = isExpanded
        ? new Set(communityMembers[expandedCommunity!] ?? [])
        : null

      // Communities present in the CURRENT view, which is what the fold decision
      // must be based on. Basing it on global rank was wrong: filtering the strip
      // down to one community still folded it, so clicking any community from
      // rank 9 onward drew a single anonymous 「其他 1 个社区」 blob that could not
      // be expanded (its communityId was -1, matching nothing). The tail exists
      // to stop colour from lying when there are more groups than hues — it has
      // no business appearing when the view holds few enough to colour.
      const presentCids = Object.keys(communityMembers)
        .map(Number)
        .filter((cid) => !(isExpanded && cid === expandedCommunity))
        .sort((a, b) => (communityMembers[b]?.length ?? 0) - (communityMembers[a]?.length ?? 0))
      const foldTail = presentCids.length > palette.vizColors.length

      // One map drives both node ids and fills, so the node builder and the edge
      // router cannot disagree about what folded — they did, and edges then
      // pointed at nodes that were never created.
      const superIdByCid = new Map<number, string>()
      const fillByCid = new Map<number, string>()
      presentCids.forEach((cid, index) => {
        const withinRamp = index < palette.vizColors.length
        superIdByCid.set(cid, foldTail && !withinRamp ? 'super-rest' : `super-${cid}`)
        fillByCid.set(cid, withinRamp ? palette.vizColors[index] : palette.vizRest)
      })
      const superIdFor = (cid: number) => superIdByCid.get(cid) ?? `super-${cid}`

      let tailMembers = 0
      const tailCommunityIds: number[] = []
      for (const cid of presentCids) {
        const count = communityMembers[cid]?.length ?? 0
        if (superIdByCid.get(cid) === 'super-rest') {
          tailMembers += count
          tailCommunityIds.push(cid)
          continue
        }
        nodes.push({
          data: {
            id: `super-${cid}`,
            label: `${labelForCommunity(cid, effectiveCommunityLabels)} · ${count}`,
            commColor: fillByCid.get(cid) ?? palette.vizRest,
            shape: 'ellipse',
            communityId: cid,
            superNode: true,
            memberCount: count,
          },
        })
      }
      if (tailCommunityIds.length > 0) {
        nodes.push({
          data: {
            id: 'super-rest',
            label: `其他 ${tailCommunityIds.length} 个社区 · ${tailMembers}`,
            commColor: palette.vizRest,
            shape: 'ellipse',
            communityId: -1,
            superNode: true,
            memberCount: tailMembers,
          },
        })
      }

      // Expanded community members as individual nodes
      if (isExpanded && expandedMemberIds) {
        for (const comp of communityFilteredComponents) {
          if (expandedMemberIds.has(comp.id)) {
            nodes.push(individualNode(comp))
          }
        }
      }

      // Unassigned components as individual nodes
      for (const comp of unassignedComponents) {
        nodes.push(individualNode(comp))
      }

      // ── Edges ──
      const visibleCommunities = new Set(Object.keys(communityMembers).map(Number))
      if (isExpanded) visibleCommunities.delete(expandedCommunity!)

      // We build aggregate edge counts: key "<smaller>-<larger>" → count
      const superEdgeWeights: Record<string, number> = {}
      const superCommunityIds = new Set(nodes.filter((n) => n.data.superNode).map((n) => (n.data as { communityId: number }).communityId))

      for (const edge of structuralEdges) {
        const fromCid = communities[edge.from]
        const toCid = communities[edge.to]

        // Both unassigned: skip for now (unassigned don't form edges well; rare)
        if ((fromCid === undefined || fromCid === null || fromCid < 0) &&
            (toCid === undefined || toCid === null || toCid < 0)) continue

        if (isExpanded) {
          const fromExpanded = fromCid === expandedCommunity
          const toExpanded = toCid === expandedCommunity
          const fromCidStr = fromCid !== undefined && fromCid !== null && fromCid >= 0 ? String(fromCid) : null
          const toCidStr = toCid !== undefined && toCid !== null && toCid >= 0 ? String(toCid) : null

          if (fromExpanded && toExpanded) {
            // Intra-expanded: individual edge (but only if both nodes are visible)
            if (componentIds.has(edge.from) && componentIds.has(edge.to)) {
              const keep = connectedOnly
                ? (connectedIds.has(edge.from) && connectedIds.has(edge.to))
                : true
              if (keep) {
                edges.push({
                  data: {
                    id: `e-intra-${edges.length}`,
                    source: edge.from,
                    target: edge.to,
                    kind: edge.kind,
                    color: palette.edgeColors[edge.kind] ?? palette.textSecondary,
                  },
                })
              }
            }
          } else if (fromExpanded && toCidStr && superCommunityIds.has(toCid!)) {
            // From expanded member to another community's super-node
            const key = `${edge.from}→${superIdFor(toCid!)}`
            superEdgeWeights[key] = (superEdgeWeights[key] ?? 0) + 1
          } else if (toExpanded && fromCidStr && superCommunityIds.has(fromCid!)) {
            const key = `${superIdFor(fromCid!)}→${edge.to}`
            superEdgeWeights[key] = (superEdgeWeights[key] ?? 0) + 1
          } else if (fromCidStr && toCidStr && superCommunityIds.has(fromCid!) && superCommunityIds.has(toCid!)) {
            // Both in non-expanded communities → super-edge
            const a = superIdFor(fromCid!)
            const b = superIdFor(toCid!)
            if (a !== b) {
              const key = [a, b].sort().join('↔')
              superEdgeWeights[key] = (superEdgeWeights[key] ?? 0) + 1
            }
          }
        } else {
          // Pure aggregated: only inter-community super-edges
          if (fromCid === undefined || fromCid === null || fromCid < 0) continue
          if (toCid === undefined || toCid === null || toCid < 0) continue
          if (fromCid === toCid) continue
          if (!superCommunityIds.has(fromCid) || !superCommunityIds.has(toCid)) continue
          const a = superIdFor(fromCid)
          const b = superIdFor(toCid)
          // Two tail communities both fold into super-rest, so their crossing
          // edges become a self-loop we do not draw.
          if (a === b) continue
          const key = [a, b].sort().join('↔')
          superEdgeWeights[key] = (superEdgeWeights[key] ?? 0) + 1
        }
      }

      // Emit super-edges
      for (const [key, weight] of Object.entries(superEdgeWeights)) {
        if (key.includes('→')) {
          // Member-to-super edge
          const [source, target] = key.split('→')
          edges.push({
            data: {
              id: `super-e-${edges.length}`,
              source,
              target,
              superEdge: true,
              weight,
              edgeWidth: Math.max(1.5, Math.min(6, 1.5 + Math.log2(weight + 1) * 1.2)),
            },
          })
        } else {
          // Super-to-super edge. Keys already hold full node ids joined by ↔,
          // including the folded `super-rest`, so no id surgery is needed.
          const [source, target] = key.split('↔')
          edges.push({
            data: {
              id: `super-e-${edges.length}`,
              source,
              target,
              superEdge: true,
              weight,
              edgeWidth: Math.max(1.5, Math.min(6, 1.5 + Math.log2(weight + 1) * 1.2)),
            },
          })
        }
      }

      hasLayoutEdges = edges.length > 0

      if (nodes.length === 0 && unassignedComponents.length === 0) {
        // Nothing to render
        return
      }
    }

    let selectedNode: cytoscape.NodeSingular | null = null
    const cy = cytoscape({
      container,
      elements: [...nodes, ...edges],
      style: [
        {
          selector: 'node',
          style: {
            'background-color': 'data(commColor)',
            shape: 'data(shape)' as unknown as cytoscape.Css.Node['shape'],
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
            'border-width': 1,
            'border-color': palette.surface,
          },
        },
        {
          // Super-nodes: larger, bolder labels, size maps to member count
          selector: 'node[superNode]',
          style: {
            'font-size': 13,
            'min-zoomed-font-size': 11,
            'font-weight': 'bold',
            'text-background-opacity': 0,
            width: "mapData(memberCount, 1, 400, 24, 72)" as unknown as number,
            height: "mapData(memberCount, 1, 400, 24, 72)" as unknown as number,
            'border-width': 2,
            'border-color': palette.surface,
            'z-index': 5,
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
          // Super-edges: weight-proportional width, viz-axis colour with real contrast
          selector: 'edge[superEdge]',
          style: {
            width: 'data(edgeWidth)',
            'line-color': palette.vizAxis,
            'target-arrow-color': palette.vizAxis,
            'arrow-scale': 0.8,
            opacity: 0.7,
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
      // `nodeDimensionsIncludeLabels` is the load-bearing option here: without it
      // cose packs by node circle only, so at the aggregated level 26 super-nodes
      // sat close enough that their labels overlapped into unreadable mush
      // (「jetson · co安全 · 方案 · opt · wan2bus · md」). Repulsion alone cannot fix
      // that, because the labels are far wider than the circles they belong to.
      layout:
        !hasLayoutEdges
          ? { name: 'grid', avoidOverlap: true, avoidOverlapPadding: 8, condense: false, nodeDimensionsIncludeLabels: true }
          : { name: 'cose', animate: false, padding: 40, nodeRepulsion: 12000, idealEdgeLength: 120, nodeDimensionsIncludeLabels: true },
      userZoomingEnabled: true,
      userPanningEnabled: true,
      wheelSensitivity: 0.2,
      // `fit` scales to fill the viewport, so a filtered view holding one or two
      // super-nodes zoomed until its label rendered around 100px tall and spilled
      // across the canvas. Cap the scale: fitting a small graph should centre it,
      // not magnify it.
      maxZoom: 1.6,
      minZoom: 0.05,
    })
    cyRef.current = cy
    cy.one('layoutstop', () => cy.fit(undefined, 30))

    // Tap handler: super-nodes drill down, individual nodes navigate
    cy.on('tap', 'node', (event) => {
      selectedNode?.removeClass('selected')
      selectedNode = event.target as cytoscape.NodeSingular
      selectedNode.addClass('selected')
      setSelectedNodeTitle(selectedNode.data('label') as string)
      if (selectedNode.data('superNode') && typeof selectedNode.data('communityId') === 'number') {
        // Drill into community — toggle: tap again to collapse
        const cid = selectedNode.data('communityId') as number
        setExpandedCommunity((prev) => (prev === cid ? null : cid))
      } else {
        // Individual node — navigate to document
        onNodeClick(event.target.id())
      }
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
  }, [aggregateMode, communities, communityFilteredComponents, communityMembers, communityRank, connectedIds, connectedOnly, effectiveCommunityLabels, expandedCommunity, onNodeClick, structuralEdges, visibleComponents, visibleEdges])

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
            <div className="border border-[var(--color-border)] bg-[var(--color-surface)] p-2 shadow-[var(--shadow-overlay)]">
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

            <div className="flex flex-wrap items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] p-2 shadow-[var(--shadow-overlay)]">
              <button
                type="button"
                onClick={() => cyRef.current?.fit(undefined, 30)}
                className="inline-flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
              >
                <Icon name="refresh" size={14} />
                适应窗口
              </button>
              {(visibleComponents.length > 0 || aggregateMode) && (
                <div
                  data-testid="wiki-graph-visibility-summary"
                  className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"
                >
                  {visibilitySummary}
                </div>
              )}
              {expandedCommunity !== null && (
                <button
                  type="button"
                  data-testid="wiki-graph-back-to-global"
                  onClick={() => setExpandedCommunity(null)}
                  className="inline-flex items-center gap-1 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
                >
                  <Icon name="chevron-left" size={14} />
                  返回全局
                </button>
              )}
              {activeCommunity !== null && (
                <button
                  type="button"
                  onClick={() => setOverviewOpen(true)}
                  className="inline-flex items-center gap-1 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
                >
                  <Icon name="info" size={14} />
                  社区综述
                </button>
              )}
              {edges.length > 0 && !aggregateMode && (
                <label className="inline-flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
                  <input
                    type="checkbox"
                    checked={connectedOnly}
                    onChange={(event) => setConnectedOnly(event.target.checked)}
                  />
                  仅显示有关联的节点
                </label>
              )}
              {Object.keys(communityMembers).length > 0 && (
                <label className="inline-flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
                  <input
                    type="checkbox"
                    data-testid="expand-all-nodes-toggle"
                    checked={expandAllNodes}
                    onChange={(event) => {
                      setExpandAllNodes(event.target.checked)
                      if (event.target.checked) setExpandedCommunity(null)
                    }}
                  />
                  展开全部节点（绘制 {components.length} 个节点，可能较慢）
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
          <div
            data-testid="wiki-graph-legend"
            className="absolute bottom-3 left-3 z-10 border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-[length:var(--type-caption)] text-[var(--color-text-primary)] shadow-[var(--shadow-overlay)]"
          >
            <div className="mb-2 font-semibold text-[var(--color-text-secondary)]">类型</div>
            <div className="grid grid-cols-2 gap-x-3 gap-y-1">
              {TYPE_SHAPE_ORDER.map((type) => (
                <div key={type} className="flex items-center gap-1.5">
                  <svg width="10" height="10" viewBox="0 0 10 10" className="shrink-0 text-[var(--color-text-secondary)]" fill="none" stroke="currentColor" strokeWidth="1">
                    {TYPE_SHAPES[type] === 'ellipse' && <circle cx="5" cy="5" r="4"/>}
                    {TYPE_SHAPES[type] === 'rectangle' && <rect x="1" y="1" width="8" height="8"/>}
                    {TYPE_SHAPES[type] === 'round-rectangle' && <rect x="1" y="1" width="8" height="8" rx="1.5"/>}
                    {TYPE_SHAPES[type] === 'triangle' && <polygon points="5,1 9,9 1,9"/>}
                    {TYPE_SHAPES[type] === 'diamond' && <polygon points="5,1 9,5 5,9 1,5"/>}
                    {TYPE_SHAPES[type] === 'pentagon' && <polygon points="5,1 8.8,3.8 7.4,8.8 2.6,8.8 1.2,3.8"/>}
                    {TYPE_SHAPES[type] === 'hexagon' && <polygon points="5,1 8.5,3 8.5,7 5,9 1.5,7 1.5,3"/>}
                    {TYPE_SHAPES[type] === 'heptagon' && <polygon points="5,1 7.8,2.2 9,5 7.8,7.8 5,9 2.2,7.8 1,5"/>}
                    {TYPE_SHAPES[type] === 'octagon' && <polygon points="3,1 7,1 9,3 9,7 7,9 3,9 1,7 1,3"/>}
                    {TYPE_SHAPES[type] === 'star' && <polygon points="5,1 6.2,3.5 9,3.8 6.9,5.7 7.5,8.5 5,7 2.5,8.5 3.1,5.7 1,3.8 3.8,3.5"/>}
                    {TYPE_SHAPES[type] === 'vee' && <polygon points="1,2 5,9 9,2"/>}
                  </svg>
                  <span className="truncate">{type}</span>
                </div>
              ))}
            </div>
          </div>
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
