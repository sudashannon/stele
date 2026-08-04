import { Icon } from './icons'

type StateBlockProps = {
  kind: 'loading' | 'empty' | 'error'
  title: string
  detail?: string
  action?: { label: string; onClick: () => void }
  hints?: string[]
  compact?: boolean
  /** Passthrough for existing data-testid attributes so tests continue to pass. */
  testId?: string
}

/**
 * A loading / empty / error state block.
 *
 * Compact mode renders a single inline row — use inside cards and narrow
 * columns. The default renders a centred block sized to its content (~480px)
 * with no shadow and no radius, separated from its parent by luminance.
 */
export function StateBlock({
  kind,
  title,
  detail,
  action,
  hints,
  compact = false,
  testId,
}: StateBlockProps): JSX.Element {
  const roleProps =
    kind === 'loading'
      ? ({ role: 'status' } as const)
      : kind === 'error'
        ? ({ role: 'alert' } as const)
        : ({} as const)

  if (compact) {
    const colorClass =
      kind === 'error'
        ? 'text-[var(--color-danger-text)]'
        : 'text-[var(--color-text-secondary)]'

    return (
      <div
        data-testid={testId}
        {...roleProps}
        className={`flex items-center gap-2 text-[length:var(--type-caption)] ${colorClass}`}
      >
        {kind === 'loading' && <Icon name="spinner" size={14} className="animate-spin" />}
        {kind === 'error' && <Icon name="warning" size={14} />}
        {kind === 'empty' && <Icon name="info" size={14} />}
        <span>{title}</span>
        {detail && <span>{detail}</span>}
      </div>
    )
  }

  return (
    <div
      data-testid={testId}
      {...roleProps}
      className="mx-auto flex max-w-[480px] flex-col items-center gap-3 bg-[var(--color-surface)] px-8 py-10 text-center"
    >
      {kind === 'loading' && (
        <Icon name="spinner" size={20} className="animate-spin text-[var(--color-text-tertiary)]" />
      )}
      {kind === 'error' && (
        <Icon name="warning" size={20} className="text-[var(--color-danger-text)]" />
      )}
      {kind === 'empty' && (
        <Icon name="info" size={20} className="text-[var(--color-text-tertiary)]" />
      )}

      <p
        className={
          kind === 'error'
            ? 'text-[length:var(--type-body)] font-medium text-[var(--color-danger-text)]'
            : 'text-[length:var(--type-body)] font-medium text-[var(--color-text-primary)]'
        }
      >
        {title}
      </p>

      {detail && (
        <p className="text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">
          {detail}
        </p>
      )}

      {action && kind !== 'loading' && (
        <button
          type="button"
          onClick={action.onClick}
          className="mt-1 inline-flex items-center gap-1.5 border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-2 text-[length:var(--type-body)] font-medium text-[var(--color-accent)] hover:bg-[var(--color-layer)]"
        >
          {action.label}
        </button>
      )}

      {kind === 'empty' && hints && hints.length > 0 && (
        <div className="mt-2 flex flex-wrap justify-center gap-2">
          {hints.map((hint, index) => (
            <span
              key={index}
              className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-3 py-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"
            >
              {hint}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
