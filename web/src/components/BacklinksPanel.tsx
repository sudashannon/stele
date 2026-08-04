
import { useEffect, useState } from 'react'
import { fetchSessions, fetchWikiComponent } from '../api/client'
import type { WikiEdge, WikiSession } from '../api/types'
import { Icon } from './icons'
import { StateBlock } from './StateBlock'
import { SessionBacklinkList } from './SessionBacklinks'

const KIND_BADGE_STYLES: Record<string, string> = {
  implements: 'border-[var(--color-accent)] bg-[var(--color-accent-subtle)] text-[var(--color-accent)]',
  references: 'border-[var(--color-purple)] bg-[var(--color-purple-subtle)] text-[var(--color-purple)]',
  generates: 'border-[var(--color-success)] bg-[var(--color-success-subtle)] text-[var(--color-success-text)]',
}

function EdgeKindBadge({ kind }: { kind: string }) {
  return (
    <span
      className={`shrink-0 border px-1.5 py-0.5 text-[length:var(--type-caption)] font-medium ${KIND_BADGE_STYLES[kind] ?? 'border-[var(--color-border)] bg-[var(--color-layer)] text-[var(--color-text-secondary)]'}`}
    >
      {kind}
    </span>
  )
}

function formatLocalTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-2 border border-[var(--color-border-subtle)] bg-[var(--color-layer)] p-3 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
      <Icon name="info" className="shrink-0" />
      <span>{text}</span>
    </div>
  )
}

function EdgeSection({
  heading,
  edges,
  pathKey,
  emptyText,
}: {
  heading: string
  edges: WikiEdge[]
  pathKey: 'from' | 'to'
  emptyText: string
}) {
  return (
    <section className="space-y-2">
      <div className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
        {heading}（{edges.length} 处引用）
      </div>
      {edges.length === 0 ? (
        <EmptyState text={emptyText} />
      ) : (
        <ul className="space-y-2">
          {edges.map((edge, index) => {
            const path = edge[pathKey]
            return (
              <li key={`${edge.kind}-${path}-${index}`} className="flex items-start gap-2">
                <span className="min-w-0 flex-1 break-all text-[length:var(--type-caption)] text-[var(--color-accent)] font-mono" title={path}>
                  {path}
                </span>
                <EdgeKindBadge kind={edge.kind} />
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

// Session grouping and rendering live in SessionBacklinks so the document
// viewer and the change detail panel show the same thing.

export function BacklinksPanel({ componentId }: { componentId: string }) {
  const [data, setData] = useState<{ forward: WikiEdge[]; backlinks: WikiEdge[]; sessions: WikiSession[] } | null>(null)
  const [loadError, setLoadError] = useState(false)

  useEffect(() => {
    let cancelled = false
    setData(null)
    setLoadError(false)
    fetchWikiComponent(componentId)
      .then(async (response) => {
        const sessionBacklinks = (response.backlinks ?? []).filter((edge) => edge.source === 'session')
        const sessions = sessionBacklinks.length > 0
          ? await fetchSessions().catch(() => [] as WikiSession[])
          : []
        if (!cancelled) setData({ forward: response.forward ?? [], backlinks: response.backlinks ?? [], sessions })
      })
      .catch(() => {
        if (!cancelled) setLoadError(true)
      })
    return () => {
      cancelled = true
    }
  }, [componentId])

  if (data === null) {
    if (loadError) {
      return (
        <StateBlock kind="error" title="引用加载失败，请稍后重试" compact />
      )
    }

    return (
      <StateBlock kind="loading" title="正在加载引用" compact />
    )
  }

  const structuralBacklinks = data.backlinks.filter((edge) => edge.source !== 'session')
  const sessionBacklinks = data.backlinks.filter((edge) => edge.source === 'session')

  return (
    <div className="space-y-4 text-[length:var(--type-caption)]">
      <EdgeSection
        heading="引用（forward）"
        edges={data.forward}
        pathKey="to"
        emptyText="本文档未引用其他文档"
      />
      <EdgeSection
        heading="反向引用"
        edges={structuralBacklinks}
        pathKey="from"
        emptyText="暂无其他文档引用本文档"
      />
      {sessionBacklinks.length > 0 && (
        <SessionBacklinkList edges={sessionBacklinks} sessions={data.sessions} />
      )}
    </div>
  )
}
