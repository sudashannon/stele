export const PHASE_COLOR_TOKENS = {
  open: 'var(--color-phase-open)',
  planning: 'var(--color-phase-open)',
  design: 'var(--color-phase-design)',
  plan: 'var(--color-phase-design)',
  build: 'var(--color-phase-build)',
  in_progress: 'var(--color-phase-build)',
  verify: 'var(--color-phase-verify)',
  archive: 'var(--color-phase-archive)',
  completed: 'var(--color-phase-archive)',
  rejected: 'var(--color-phase-rejected)',
} as const

export type PhaseColorKey = keyof typeof PHASE_COLOR_TOKENS

export function phaseColorToken(phase: string): string {
  return Object.prototype.hasOwnProperty.call(PHASE_COLOR_TOKENS, phase)
    ? PHASE_COLOR_TOKENS[phase as PhaseColorKey]
    : 'var(--color-phase-unknown)'
}
