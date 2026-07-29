import type { ChangeSummary } from '../api/types'

interface Props {
  changes: ChangeSummary[]
  stuckThresholdDays: number
  now?: Date
  activeFilter: string | null
  onFilterSelect: (key: string | null) => void
}

export interface ChangeClassification {
  active: ChangeSummary[]
  archived: ChangeSummary[]
  stuck: ChangeSummary[]
  verifyFailed: ChangeSummary[]
  incomplete: ChangeSummary[]
}

function daysSince(dateStr: string, now: Date): number {
  if (!dateStr) return 0
  const then = new Date(dateStr)
  return Math.floor((now.getTime() - then.getTime()) / (1000 * 60 * 60 * 24))
}

export function classifyChanges(
  changes: ChangeSummary[],
  stuckThresholdDays: number,
  now: Date,
): ChangeClassification {
  const active = changes.filter((c) => !c.archived)
  const archived = changes.filter((c) => c.archived)
  const verifyFailed = active.filter((c) => c.verifyResult === 'fail')
  const stuck = active.filter(
    (c) =>
      (c.phase === 'build' || (c.sourceType === 'trellis' && c.phase === 'in_progress')) &&
      daysSince(c.createdAt, now) > stuckThresholdDays,
  )
  const incomplete = active.filter((c) => c.tasksCompleted < c.tasksTotal)

  return { active, archived, stuck, verifyFailed, incomplete }
}

export function KpiCards({
  changes,
  stuckThresholdDays,
  now = new Date(),
  activeFilter,
  onFilterSelect,
}: Props) {
  const classification = classifyChanges(changes, stuckThresholdDays, now)
  const incompleteTasks = classification.active.reduce(
    (sum, c) => sum + (c.tasksTotal - c.tasksCompleted),
    0,
  )

  const cards = [
    {
      key: 'active',
      label: '活跃变更',
      value: classification.active.length,
      testId: 'kpi-active',
      icon: '◔',
      chip: 'bg-[color-mix(in_srgb,var(--color-accent)_10%,white)] text-[var(--color-accent)]',
    },
    {
      key: 'archived',
      label: '已归档',
      value: classification.archived.length,
      testId: 'kpi-archived',
      icon: '✓',
      chip: 'bg-[color-mix(in_srgb,var(--color-success)_10%,white)] text-[var(--color-success)]',
    },
    {
      key: 'stuck',
      label: '卡死预警',
      value: classification.stuck.length,
      testId: 'kpi-stuck',
      warn: classification.stuck.length > 0,
      icon: '⚠',
      chip: 'bg-[color-mix(in_srgb,var(--color-warn)_10%,white)] text-[var(--color-warn)]',
    },
    {
      key: 'verify-failed',
      label: 'Verify 失败',
      value: classification.verifyFailed.length,
      testId: 'kpi-verify-failed',
      danger: classification.verifyFailed.length > 0,
      icon: '◎',
      chip: 'bg-[color-mix(in_srgb,var(--color-danger)_10%,white)] text-[var(--color-danger)]',
    },
    {
      key: 'incomplete-tasks',
      label: '未完成任务',
      value: incompleteTasks,
      testId: 'kpi-incomplete-tasks',
      icon: '▤',
      chip: 'bg-[color-mix(in_srgb,var(--color-text-secondary)_10%,white)] text-[var(--color-text-secondary)]',
    },
  ]

  return (
    <div data-testid="kpi-grid" className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-3">
      {cards.map((c) => {
        const isFilterActive = activeFilter === c.key
        const selectCard = () => onFilterSelect(isFilterActive ? null : c.key)

        return (
          <div
            key={c.key}
            data-testid={c.testId}
            data-filter-active={isFilterActive ? 'true' : 'false'}
            role="button"
            tabIndex={0}
            onClick={selectCard}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                selectCard()
              }
            }}
            className={
              'bg-white px-4 py-4 shadow-[var(--shadow-card)] cursor-pointer flex flex-col gap-2.5' +
              (c.warn ? ' outline outline-[1.5px] outline-[var(--color-warn)] bg-[color-mix(in_srgb,var(--color-warn)_4%,white)]' : '') +
              (isFilterActive ? ' ring-2 ring-[var(--color-accent)]' : '')
            }
          >
            <div className="flex items-center gap-2.5">
              <div className={'w-[34px] h-[34px] grid place-items-center text-base ' + c.chip}>
                <span aria-hidden="true">{c.icon}</span>
              </div>
              <div className={'text-[13px] ' + (c.warn ? 'text-[var(--color-warn)] font-semibold' : 'text-[var(--color-text-secondary)]')}>
                {c.label}
              </div>
            </div>
            <div className={'text-[27px] font-bold leading-none tracking-tight ' + (c.warn ? 'text-[var(--color-warn)]' : c.danger ? 'text-[var(--color-danger)]' : 'text-[var(--color-text-primary)]')}>
              {c.value}
            </div>
          </div>
        )
      })}
    </div>
  )
}
