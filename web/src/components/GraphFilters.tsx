import { useMemo, useState } from 'react'
import { COMMUNITY_CATEGORICAL_LIMIT, communityColor } from './graphPalette'
import { Icon } from './icons'

interface GraphFiltersProps {
  workspaces: string[]
  activeWorkspaces: Set<string>
  onToggleWorkspace: (ws: string) => void
  onResetFilters: () => void
  communityLabels: Record<string, string>
  communityCounts: Record<number, number>
  activeCommunity: number | null
  onSelectCommunity: (id: number | null) => void
  communitySelectable?: boolean
  summary?: string
}

// How many community chips the collapsed legend shows. Enough to cover the
// dominant communities on one line at a normal window width; the rest expand.
const COLLAPSED_COMMUNITY_CHIPS = 8

function labelForCommunity(id: number, communityLabels: Record<string, string>) {
  return communityLabels[String(id)] ?? `#${id}`
}

export function GraphFilters({
  workspaces,
  activeWorkspaces,
  onToggleWorkspace,
  onResetFilters,
  communityLabels,
  communityCounts,
  activeCommunity,
  onSelectCommunity,
  communitySelectable = true,
  summary,
}: GraphFiltersProps) {
  const communityIds = useMemo(
    () =>
      Object.keys(communityCounts)
        .map(Number)
        .filter((id) => Number.isFinite(id) && (communityCounts[id] ?? 0) > 0)
        .sort((a, b) => {
          const countDifference = (communityCounts[b] ?? 0) - (communityCounts[a] ?? 0)
          return countDifference !== 0 ? countDifference : a - b
        }),
    [communityCounts],
  )

  const communityRank = useMemo(
    () => new Map(communityIds.map((id, rank) => [id, rank])),
    [communityIds],
  )

  const [expanded, setExpanded] = useState(false)
  const visibleCommunityIds = expanded ? communityIds : communityIds.slice(0, COLLAPSED_COMMUNITY_CHIPS)
  const hiddenCommunityCount = communityIds.length - visibleCommunityIds.length

  const chipClass =
    'inline-flex shrink-0 items-center gap-1.5 border px-2 py-1 text-[length:var(--type-caption)] leading-none transition-colors'

  return (
    <div
      data-testid="graph-filters"
      className="flex shrink-0 flex-wrap items-center gap-3 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2"
    >
      {(activeCommunity !== null || activeWorkspaces.size < workspaces.length) && (
        <button
          type="button"
          data-testid="filter-reset"
          onClick={onResetFilters}
          className={`${chipClass} border-[var(--color-danger)] bg-[var(--color-danger-subtle)] text-[var(--color-danger-text)]`}
        >
          <Icon name="close" size={12} />
          重置筛选
        </button>
      )}

      {workspaces.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
            工作区
          </span>
          {workspaces.map((ws) => {
            const active = activeWorkspaces.has(ws)
            return (
              <button
                key={ws}
                type="button"
                data-testid="workspace-chip"
                aria-pressed={active}
                onClick={() => onToggleWorkspace(ws)}
                className={
                  active
                    ? `${chipClass} border-[var(--color-accent)] bg-[var(--color-accent-subtle)] text-[var(--color-accent)]`
                    : `${chipClass} border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)]`
                }
              >
                {ws}
              </button>
            )
          })}
        </div>
      )}

      {/* The legend wraps instead of scrolling horizontally.

          As a scroller it was both unusable and broken: 26 communities needed
          4411px of scroll inside a 964px strip, so most of the legend was out of
          sight, and the strip was 26px tall while a chip is 22px - on a platform
          whose scrollbars take space (Windows: 15-17px) the scrollbar painted
          across the chips. Reserving height fixed the overlap but kept a
          horizontal scroller in a legend; wrapping a capped subset removes the
          scrollbar, and the rest is one click away. */}
      {communityIds.length > 0 && (
        <div
          data-testid="community-filter-strip"
          aria-label="社区筛选"
          className="flex min-w-0 flex-1 flex-wrap items-center gap-2"
        >
          <span className="shrink-0 pr-1 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
            社区
          </span>
          {visibleCommunityIds.map((id) => {
            const active = activeCommunity === id
            const count = communityCounts[id] ?? 0
            const rank = communityRank.get(id) ?? Infinity
            // Only the top ranks carry a hue. Beyond them a swatch would be the
            // same neutral grey on every chip — 18 chips claiming a colour that
            // identifies nothing, sitting next to 8 that do, which reads as a
            // palette that failed to load. Identity past the ramp is carried by
            // ordering, so those chips show their rank instead of a fake swatch.
            const hasHue = rank < COMMUNITY_CATEGORICAL_LIMIT
            return (
              <button
                key={id}
                type="button"
                data-testid="community-chip"
                aria-pressed={communitySelectable ? active : undefined}
                disabled={!communitySelectable}
                onClick={() => onSelectCommunity(active ? null : id)}
                className={
                  active
                    ? `${chipClass} border-[var(--color-text-primary)] bg-[var(--color-layer)] text-[var(--color-text-primary)]`
                    : `${chipClass} border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)] disabled:hover:border-[var(--color-border)] disabled:hover:bg-[var(--color-surface)] disabled:opacity-100`
                }
              >
                {hasHue ? (
                  <span
                    className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: communityColor(rank) }}
                  />
                ) : (
                  <span className="inline-block w-2.5 shrink-0 text-center font-[family-name:var(--font-mono)] tabular-nums text-[length:var(--type-micro)] text-[var(--color-text-tertiary)]">
                    {rank + 1}
                  </span>
                )}
                <span>{labelForCommunity(id, communityLabels)}</span>
                <span className="text-[var(--color-text-tertiary)]">{count}</span>
              </button>
            )
          })}
          {hiddenCommunityCount > 0 && (
            <button
              type="button"
              data-testid="community-expand"
              onClick={() => setExpanded((open) => !open)}
              className={`${chipClass} border-dashed border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-link)] hover:bg-[var(--color-layer)]`}
            >
              {expanded ? '收起' : `+${hiddenCommunityCount} 个社区`}
            </button>
          )}
          {expanded && hiddenCommunityCount === 0 && communityIds.length > COLLAPSED_COMMUNITY_CHIPS && (
            <button
              type="button"
              data-testid="community-expand"
              onClick={() => setExpanded(false)}
              className={`${chipClass} border-dashed border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-link)] hover:bg-[var(--color-layer)]`}
            >
              收起
            </button>
          )}
        </div>
      )}

      {summary && (
        <div
          data-testid="graph-filter-summary"
          className="ml-auto text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"
        >
          {summary}
        </div>
      )}
    </div>
  )
}
