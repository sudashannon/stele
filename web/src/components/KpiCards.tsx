import type { ChangeSummary } from '../api/types'
import { Icon } from './icons'
import type { IconName } from './icons'


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

  const cards: Array<{
    key: string
    label: string
    value: number
    testId: string
    icon: IconName
    chip: string
    warn?: boolean
    danger?: boolean
  }> = [
    {
      key: 'active',
      label: '活跃变更',
      value: classification.active.length,
      testId: 'kpi-active',
      icon: 'changes',
      chip: 'bg-[var(--color-accent-subtle)] text-[var(--color-accent)]',
    },
    {
      key: 'archived',
      label: '已归档',
      value: classification.archived.length,
      testId: 'kpi-archived',
      icon: 'check',
      chip: 'bg-[var(--color-success-subtle)] text-[var(--color-success-text)]',
    },
    {
      key: 'stuck',
      label: '卡死预警',
      value: classification.stuck.length,
      testId: 'kpi-stuck',
      warn: classification.stuck.length > 0,
      icon: 'warning',
      chip: 'bg-[var(--color-warn-subtle)] text-[var(--color-warn-text)]',
    },
    {
      key: 'verify-failed',
      label: 'Verify 失败',
      value: classification.verifyFailed.length,
      testId: 'kpi-verify-failed',
      danger: classification.verifyFailed.length > 0,
      icon: 'warning',
      chip: 'bg-[var(--color-danger-subtle)] text-[var(--color-danger-text)]',
    },
    {
      key: 'incomplete-tasks',
      label: '未完成任务',
      value: incompleteTasks,
      testId: 'kpi-incomplete-tasks',
      icon: 'todos',
      chip: 'bg-[var(--color-layer)] text-[var(--color-text-secondary)]',
    },
  ]

  return (
    <div data-testid="kpi-grid" className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-3">
      {cards.map((c) => {
        const isFilterActive = activeFilter === c.key
        const selectCard = () => onFilterSelect(isFilterActive ? null : c.key)

        return (
          <button
            type="button"
            key={c.key}
            data-testid={c.testId}
            data-filter-active={isFilterActive ? 'true' : 'false'}
            aria-pressed={isFilterActive}
            onClick={selectCard}
            className={
              'flex cursor-pointer flex-col gap-2.5 bg-[var(--color-surface)] px-4 py-4 text-left shadow-[var(--shadow-card)]' +
              (c.warn ? ' border-l-4 border-[var(--color-warn-text)] bg-[var(--color-warn-subtle)]' : ' border-l-4 border-transparent') +
              (isFilterActive ? ' outline outline-2 outline-[var(--color-accent)]' : '')
            }
          >
            <div className="flex items-center gap-2.5">
              <div className={'grid h-[34px] w-[34px] place-items-center ' + c.chip}>
                <Icon name={c.icon} size={18} />
              </div>
              <div className={'text-[length:var(--type-caption)] ' + (c.warn ? 'text-[var(--color-warn-text)] font-semibold' : 'text-[var(--color-text-secondary)]')}>
                {c.label}
              </div>
              {isFilterActive && <span className="text-[length:var(--type-caption)] font-semibold text-[var(--color-accent)]">筛选中</span>}
            </div>
            <div className={'text-[length:var(--type-display)] leading-[var(--leading-display)] font-mono tabular-nums font-bold ' + (c.warn ? 'text-[var(--color-warn-text)]' : c.danger ? 'text-[var(--color-danger-text)]' : 'text-[var(--color-text-primary)]')}>
              {c.value}
            </div>
          </button>
        )
      })}
    </div>
  )
}
