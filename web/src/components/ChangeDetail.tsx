import { useEffect } from 'react'
import { fetchChangeDetail } from '../api/client'
import type { ChangeSummary } from '../api/types'
import { PhaseStepper } from './PhaseStepper'
import { TaskDonut } from './TaskDonut'
import { ReviewBadges } from './ReviewBadges'
import { BacklinksPanel } from './BacklinksPanel'
import { ArtifactList } from './ArtifactList'
import { GuardButton } from './GuardButton'
import { Icon } from './icons'


// Legacy fallback used only when an older OpenSpec response does not provide
// nextTransition. Source-aware responses provide their own lifecycle action.
const PHASE_ORDER = ['open', 'design', 'build', 'verify', 'archive']
const SOURCE_LABELS: Record<string, string> = {
  openspec: 'OpenSpec',
  trellis: 'Trellis',
  superpowers: 'Superpowers',
  docs: '纯文档',
}

const VERIFY_STATUSES = {
  pass: { label: '已通过', icon: 'check' as const, className: 'text-[var(--color-success-text)] bg-[var(--color-success-subtle)]' },
  fail: { label: '失败', icon: 'warning' as const, className: 'text-[var(--color-danger-text)] bg-[var(--color-danger-subtle)]' },
  pending: { label: '待验证', icon: 'info' as const, className: 'text-[var(--color-text-secondary)] bg-[var(--color-layer)]' },
}

const UNKNOWN_VERIFY_STATUS = {
  label: '未知',
  icon: 'info' as const,
  className: 'text-[var(--color-text-secondary)] bg-[var(--color-layer)]',
}

function formatLocalTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function VerifyStatus({ result }: { result: string }) {
  const status = VERIFY_STATUSES[result as keyof typeof VERIFY_STATUSES] ?? UNKNOWN_VERIFY_STATUS
  return (
    <span
      data-testid="change-verify-result"
      className={`inline-flex items-center gap-1 border border-[var(--color-border-subtle)] px-2 py-1 text-[length:var(--type-caption)] font-semibold ${status.className}`}
    >
      <Icon name={status.icon} size={14} />
      {status.label}
    </span>
  )
}

function MetadataItem({ label, value, testId, mono }: { label: string; value: string; testId: string; mono?: boolean }) {
  return (
    <div data-testid={testId} className="min-w-0 border-l-2 border-[var(--color-border)] pl-2">
      <div className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{label}</div>
      <div className={`truncate text-[length:var(--type-body)] leading-[var(--leading-body)] font-semibold text-[var(--color-text-primary)] ${mono ? 'font-mono' : ''}`} title={value}>{value}</div>
    </div>
  )
}

export function ChangeDetail({
  change,
  onChangeUpdated,
  onOpenArtifact,
  onArtifactsChanged,
  onNavigateToTodos,
  todoCount,
}: {
  change: ChangeSummary
  onChangeUpdated: () => void
  onOpenArtifact: (path: string) => void
  onArtifactsChanged?: (artifacts: { path: string; label: string }[]) => void
  onNavigateToTodos?: (workspace: string, changeName: string) => void
  todoCount?: number
}) {
  const isOpenSpec = change.sourceType === undefined || change.sourceType === 'openspec'
  useEffect(() => {
    if (!onArtifactsChanged) return
    let cancelled = false
    fetchChangeDetail(change.name, change.workspace)
      .then((detail) => {
        if (cancelled) return
        const artifacts = (detail.phases ?? [])
          .flatMap((phase) => phase.artifacts)
          .filter((a) => a.exists && a.path)
          .map((a) => ({ path: a.path!, label: a.label }))
        onArtifactsChanged(artifacts)
      })
      .catch(() => {
        if (!cancelled) onArtifactsChanged([])
      })
    return () => {
      cancelled = true
    }
  }, [change.name, change.workspace, onArtifactsChanged])

  return (
    <div className="bg-[var(--color-surface)] p-5 shadow-[var(--shadow-card)] space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-[length:var(--type-body)] leading-[var(--leading-body)] font-semibold">{change.title || change.name}</h3>
          {change.title && change.title !== change.name && (
            <div className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)] font-mono">{change.name}</div>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {isOpenSpec && (
            <ReviewBadges
              visualized={change.visualized}
              designReviewed={change.designReviewed}
              verifyReviewed={change.verifyReviewed}
            />
          )}
          {onNavigateToTodos && change.workspace && (
            <button
              type="button"
              data-testid="change-todo-action"
              onClick={() => onNavigateToTodos(change.workspace!, change.name)}
              className="inline-flex shrink-0 items-center gap-1 border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] hover:border-[var(--color-accent)] hover:bg-[var(--color-accent-subtle)]"
              title="添加待办"
            >
              <Icon name="plus" size={14} />
              {todoCount && todoCount > 0 ? '待办 ' + todoCount : '待办'}
            </button>
          )}
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3 border-y border-[var(--color-border-subtle)] py-3 sm:grid-cols-3 lg:grid-cols-5">
        <MetadataItem label="工作流" value={change.workflow} testId="metadata-workflow" mono />
        {change.sourceType && <MetadataItem label="来源" value={SOURCE_LABELS[change.sourceType] ?? change.sourceType} testId="metadata-source" mono />}
        {change.buildMode && <MetadataItem label="构建模式" value={change.buildMode} testId="metadata-build-mode" mono />}
        {change.reviewMode && <MetadataItem label="审查模式" value={change.reviewMode} testId="metadata-review-mode" mono />}
        {change.tddMode && <MetadataItem label="TDD 模式" value={change.tddMode} testId="metadata-tdd-mode" mono />}
        {change.autoTransition !== undefined && (
          <MetadataItem label="自动流转" value={change.autoTransition ? '已开启' : '已关闭'} testId="metadata-auto-transition" />
        )}
        {change.verifiedAt && <MetadataItem label="验证时间" value={formatLocalTime(change.verifiedAt)} testId="metadata-verified-at" mono />}
        <div className="min-w-0">
          <div className="mb-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">验证结果</div>
          <VerifyStatus result={change.verifyResult} />
        </div>
      </div>
      {change.stateWarning && (
        <div className="flex items-start gap-2 border-l-4 border-[var(--color-warn)] bg-[var(--color-warn-subtle)] p-2 text-[length:var(--type-caption)] text-[var(--color-warn-text)]">
          <Icon name="warning" size={16} className="mt-px shrink-0" />
          <span>{change.stateWarning}</span>
        </div>
      )}
      <div className="flex flex-col lg:flex-row gap-4">
        <div className="flex-[2]">
          <PhaseStepper currentPhase={change.phase} lifecycle={change.lifecycle} />
        </div>
        <div className="flex-1">
          <TaskDonut completed={change.tasksCompleted} total={change.tasksTotal} />
        </div>
      </div>
      {(() => {
        const transition = change.nextTransition
        const fallbackIndex = PHASE_ORDER.indexOf(change.phase)
        let fallbackTarget: string | null = null
        if (fallbackIndex >= 0 && fallbackIndex < PHASE_ORDER.length - 1) {
          fallbackTarget = PHASE_ORDER[fallbackIndex + 1]
        }
        let target = transition?.target ?? null
        if (!target && isOpenSpec) target = fallbackTarget
        if (!target) return null
        const fallbackBlockedReason =
          change.phase === 'build' && target === 'verify' && !(change.tasksCompleted === change.tasksTotal && change.tasksTotal > 0)
            ? `任务未全部完成 (${change.tasksCompleted}/${change.tasksTotal})，无法进入验证`
            : undefined
        return (
          <GuardButton
            changeName={change.name}
            targetPhase={target}
            onComplete={onChangeUpdated}
            blockedReason={transition?.blockedReason ?? fallbackBlockedReason}
            workspace={change.workspace}
            sourceType={change.sourceType}
            label={transition?.label}
            command={transition?.command}
          />
        )
      })()}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div className="border border-[var(--color-border)] p-3">
          <h4 className="text-[length:var(--type-body)] leading-[var(--leading-body)] font-semibold text-[var(--color-text-primary)] mb-2">产出物</h4>
          <ArtifactList changeName={change.name} workspace={change.workspace} onSelectArtifact={onOpenArtifact} />
        </div>
        <div className="border border-[var(--color-border)] p-3">
          <h4 className="text-[length:var(--type-body)] leading-[var(--leading-body)] font-semibold text-[var(--color-text-primary)] mb-2">文档关联</h4>
          <BacklinksPanel componentId={change.componentId ?? change.name} />
        </div>
      </div>
    </div>
  )
}
