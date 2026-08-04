import { Icon } from './icons'

interface Props {
  visualized?: boolean
  designReviewed?: boolean
  verifyReviewed?: boolean
}

function Badge({ testId, tone, label }: { testId: string; tone: 'ok' | 'neutral'; label: string }) {
  const toneClass =
    tone === 'ok'
      ? 'bg-[var(--color-success-subtle)] text-[var(--color-success-text)]'
      : 'bg-[var(--color-layer)] text-[var(--color-text-secondary)]'
  return (
    <span
      data-testid={testId}
      data-tone={tone}
      className={`inline-flex items-center gap-1 border border-[var(--color-border-subtle)] px-2 py-1 text-[length:var(--type-caption)] font-semibold ${toneClass}`}
    >
      <Icon name={tone === 'ok' ? 'check' : 'info'} size={14} />
      {label}
    </span>
  )
}

export function ReviewBadges({ visualized, designReviewed, verifyReviewed }: Props) {
  return (
    <div className="flex gap-2">
      <Badge testId="badge-visualized" tone={visualized ? 'ok' : 'neutral'} label={visualized ? '可视化已完成' : '未可视化'} />
      <Badge testId="badge-design-reviewed" tone={designReviewed ? 'ok' : 'neutral'} label={designReviewed ? '设计已审' : '设计未审'} />
      <Badge testId="badge-verify-reviewed" tone={verifyReviewed ? 'ok' : 'neutral'} label={verifyReviewed ? '验证已审' : '验证未审'} />
    </div>
  )
}
