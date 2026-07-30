
import { useEffect, useState } from 'react'
import { fetchWikiComponent } from '../api/client'
import type { WikiEdge } from '../api/types'
import { Icon } from './icons'

const KIND_BADGE_STYLES: Record<string, string> = {
  implements: 'border-[var(--color-accent)] bg-[var(--color-accent-subtle)] text-[var(--color-accent)]',
  references: 'border-[var(--color-purple)] bg-[var(--color-purple-subtle)] text-[var(--color-purple)]',
  generates: 'border-[var(--color-success)] bg-[var(--color-success-subtle)] text-[var(--color-success)]',
}

function EdgeKindBadge({ kind }: { kind: string }) {
  return (
    <span
      className={`shrink-0 border px-1.5 py-0.5 text-[var(--type-caption)] font-medium ${KIND_BADGE_STYLES[kind] ?? 'border-[var(--color-border)] bg-[var(--color-layer)] text-[var(--color-text-secondary)]'}`}
    >
      {kind}
    </span>
  )
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-2 border border-[var(--color-border-subtle)] bg-[var(--color-layer)] p-3 text-[var(--type-caption)] text-[var(--color-text-secondary)]">
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
      <div className="text-[var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
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
                <span className="min-w-0 flex-1 break-all text-[var(--type-caption)] text-[var(--color-accent)]" title={path}>
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

export function BacklinksPanel({ componentId }: { componentId: string }) {
  const [data, setData] = useState<{ forward: WikiEdge[]; backlinks: WikiEdge[] } | null>(null)
  const [loadError, setLoadError] = useState(false)

  useEffect(() => {
    let cancelled = false
    setData(null)
    setLoadError(false)
    fetchWikiComponent(componentId)
      .then((response) => {
        if (!cancelled) setData({ forward: response.forward, backlinks: response.backlinks })
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
        <div role="alert" className="flex items-center gap-2 text-[var(--type-caption)] text-[var(--color-danger)]">
          <Icon name="warning" size={14} />
          引用加载失败，请稍后重试
        </div>
      )
    }

    return (
      <div role="status" className="flex items-center gap-2 text-[var(--type-caption)] text-[var(--color-text-secondary)]">
        <Icon name="spinner" size={14} className="animate-spin" />
        正在加载引用
      </div>
    )
  }

  return (
    <div className="space-y-4 text-[var(--type-caption)]">
      <EdgeSection
        heading="引用（forward）"
        edges={data.forward}
        pathKey="to"
        emptyText="本文档未引用其他文档"
      />
      <EdgeSection
        heading="反向引用"
        edges={data.backlinks}
        pathKey="from"
        emptyText="暂无其他文档引用本文档"
      />
    </div>
  )
}
