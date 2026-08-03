import { useMemo } from 'react'
import { COMMUNITY_COLORS } from './graphPalette'
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
          className={`${chipClass} border-[var(--color-danger)] bg-[var(--color-danger-subtle)] text-[var(--color-danger)]`}
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

      {/* A horizontal scroller must be tall enough for its own scrollbar. A chip
          is 22px and a classic Windows scrollbar takes 15-17px inside the box, so
          the previous 26px strip left 9px of client height and the scrollbar
          painted across the chips. Overlay scrollbars (macOS, Linux Chromium)
          hide this, which is why it only showed up on Edge. */}
      {communityIds.length > 0 && (
        <div
          data-testid="community-filter-strip"
          aria-label="社区筛选"
          className="flex min-h-[2.5rem] min-w-0 flex-1 items-center gap-2 overflow-x-auto overscroll-x-contain pb-1 [scrollbar-width:thin]"
        >
          <span className="sticky left-0 z-[1] shrink-0 bg-[var(--color-surface)] pr-2 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
            社区
          </span>
          {communityIds.map((id) => {
            const active = activeCommunity === id
            const count = communityCounts[id] ?? 0
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
                <span
                  className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                  style={{ backgroundColor: COMMUNITY_COLORS[id % COMMUNITY_COLORS.length] }}
                />
                <span>{labelForCommunity(id, communityLabels)}</span>
                <span className="text-[var(--color-text-tertiary)]">{count}</span>
              </button>
            )
          })}
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
