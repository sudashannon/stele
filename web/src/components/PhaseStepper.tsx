import type { LifecycleStep } from '../api/types'

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

export function PhaseStepper({ currentPhase, lifecycle }: { currentPhase: string; lifecycle?: LifecycleStep[] }) {
  const phases = lifecycle?.length ? lifecycle : PHASES
  const currentIndex = phases.findIndex((p) => p.key === currentPhase)
  const isUnknown = currentIndex === -1

  return (
    <div>
      {isUnknown && (
        <div
          data-testid="phase-unknown-notice"
          className="text-[11px] text-[var(--color-warn)] font-semibold mb-2"
        >
          ⚠ 阶段信息缺失
        </div>
      )}
      <div className="flex items-center flex-col md:flex-row gap-2 md:gap-0">
        {phases.map((p, i) => {
          const state = stateFor(i, currentIndex)
          return (
            <div key={p.key} className="flex items-center w-full md:w-auto md:flex-1">
              <div className="flex flex-col items-center flex-1">
                <div
                  data-testid={`step-${p.key}`}
                  data-state={state}
                  style={state === 'current' ? { boxShadow: '0 0 0 4px color-mix(in srgb, var(--color-accent) 15%, transparent)' } : undefined}
                  className={
                    'w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold ' +
                    (state === 'done'
                      ? 'bg-[var(--color-success)] text-white'
                      : state === 'current'
                        ? 'bg-[var(--color-accent)] text-white'
                        : state === 'unknown'
                          ? 'bg-white border-2 border-[var(--color-warn)] text-[var(--color-warn)]'
                          : 'bg-white border-2 border-[var(--color-border)] text-[var(--color-text-secondary)]')
                  }
                >
                  {state === 'done' ? '✓' : state === 'unknown' ? '?' : i + 1}
                </div>
                <div
                  className={
                    'text-[10px] mt-1 ' +
                    (state === 'pending'
                      ? 'text-[var(--color-text-secondary)]'
                      : state === 'unknown'
                        ? 'text-[var(--color-warn)] font-semibold'
                        : 'text-[var(--color-accent)] font-semibold')
                  }
                >
                  {p.label}
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
