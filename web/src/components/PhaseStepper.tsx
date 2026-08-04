import type { LifecycleStep } from '../api/types'
import { Icon } from './icons'
import { phaseColorToken } from './phasePalette'


const PHASES = [
  { key: 'open', label: '启动' },
  { key: 'design', label: '设计' },
  { key: 'build', label: '构建' },
  { key: 'verify', label: '验证' },
  { key: 'archive', label: '归档' },
] as const

type StepState = 'done' | 'current' | 'pending' | 'unknown'

function stateFor(index: number, currentIndex: number): StepState {
  if (currentIndex === -1) return 'unknown'
  if (index < currentIndex) return 'done'
  if (index === currentIndex) return 'current'
  return 'pending'
}

const STATE_LABELS: Record<StepState, string> = {
  done: '已完成',
  current: '当前',
  pending: '待开始',
  unknown: '未知',
}

const STEP_CLASSES: Record<StepState, string> = {
  done: 'border-[var(--color-success)] bg-[var(--color-success)] text-[var(--color-text-on-color)]',
  current: 'border-transparent text-[var(--color-text-on-color)]',
  pending: 'border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)]',
  unknown: 'border-[var(--color-warn-text)] bg-[var(--color-surface)] text-[var(--color-warn-text)]',
}

const STATE_TEXT_CLASSES: Record<StepState, string> = {
  done: 'text-[var(--color-text-secondary)]',
  current: '',
  pending: 'text-[var(--color-text-secondary)]',
  unknown: 'text-[var(--color-warn-text)]',
}

function stepContent(state: StepState, index: number) {
  if (state === 'done') return <Icon name="check" size={14} />
  if (state === 'unknown') return <Icon name="info" size={14} />
  return index + 1
}

export function PhaseStepper({ currentPhase, lifecycle }: { currentPhase: string; lifecycle?: LifecycleStep[] }) {
  const phases = lifecycle?.length ? lifecycle : PHASES
  const currentIndex = phases.findIndex((p) => p.key === currentPhase)
  const isUnknown = currentIndex === -1

  return (
    <div>
      {isUnknown && (
        <div
          data-testid="phase-unknown-notice"
          className="mb-2 flex items-center gap-1 text-[length:var(--type-caption)] font-semibold text-[var(--color-warn-text)]"
        >
          <Icon name="warning" size={14} />
          阶段信息缺失
        </div>
      )}
      <div className="flex items-center flex-col md:flex-row gap-2 md:gap-0">
        {phases.map((p, i) => {
          const state = stateFor(i, currentIndex)
          const phaseColor = phaseColorToken(p.key)
          return (
            <div key={p.key} className="flex items-center w-full md:w-auto md:flex-1">
              <div className="flex flex-col items-center flex-1">
                <div
                  data-testid={`step-${p.key}`}
                  data-state={state}
                  style={
                    state === 'current'
                      ? {
                          backgroundColor: phaseColor,
                          boxShadow: `0 0 0 3px color-mix(in srgb, ${phaseColor} 15%, transparent)`,
                        }
                      : undefined
                  }
                  className={`flex h-7 w-7 items-center justify-center border-2 text-[length:var(--type-caption)] font-bold ${STEP_CLASSES[state]}`}
                >
                  {stepContent(state, i)}
                </div>
                <div className="mt-1 text-center text-[length:var(--type-caption)]">
                  <div className="font-semibold text-[var(--color-text-primary)]">{p.label}</div>
                  <div
                    className={STATE_TEXT_CLASSES[state]}
                    style={state === 'current' ? { color: phaseColor } : undefined}
                  >
                    {STATE_LABELS[state]}
                  </div>
                </div>
              </div>
              {i < phases.length - 1 && (
                <div
                  className={
                    'hidden md:block flex-1 h-[2px] ' +
                    (i < currentIndex ? 'bg-[var(--color-success)]' : 'bg-[var(--color-border)]')
                  }
                />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
