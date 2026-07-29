import { useEffect } from 'react'
import { fetchChangeDetail } from '../api/client'
import type { ChangeSummary } from '../api/types'
import { PhaseStepper } from './PhaseStepper'
import { TaskDonut } from './TaskDonut'
import { ReviewBadges } from './ReviewBadges'
import { BacklinksPanel } from './BacklinksPanel'
import { ArtifactList } from './ArtifactList'
import { GuardButton } from './GuardButton'

// Legacy fallback used only when an older OpenSpec response does not provide
// nextTransition. Source-aware responses provide their own lifecycle action.
const PHASE_ORDER = ['open', 'design', 'build', 'verify', 'archive']

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
    <div className="bg-white p-5 shadow-[0_8px_26px_rgba(30,32,60,0.06),0_1px_2px_rgba(0,0,0,0.03)] space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold">{change.title || change.name}</h3>
          {change.title && change.title !== change.name && (
            <div className="text-[10px] text-[var(--color-text-secondary)]">{change.name}</div>
          )}
        </div>
        {isOpenSpec && (
          <ReviewBadges
            visualized={change.visualized}
            designReviewed={change.designReviewed}
            verifyReviewed={change.verifyReviewed}
          />
        )}
        {onNavigateToTodos && change.workspace && (
          <button
            data-testid="change-todo-action"
            onClick={() => onNavigateToTodos(change.workspace!, change.name)}
            className="shrink-0 text-xs px-2 py-1 border border-[var(--color-border)] hover:bg-[var(--palette-highlight)] hover:border-[var(--color-accent)]"
            title="添加待办"
          >
            {todoCount && todoCount > 0 ? '待办 ' + todoCount : '+ 待办'}
          </button>
        )}
      </div>
      {change.stateWarning && (
        <div className="text-xs text-[var(--color-danger)] bg-red-50 rounded p-2">
          ⚠ {change.stateWarning}
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
        const fallbackTarget =
          fallbackIndex >= 0 && fallbackIndex < PHASE_ORDER.length - 1 ? PHASE_ORDER[fallbackIndex + 1] : null
        const target = transition?.target ?? (isOpenSpec ? fallbackTarget : null)
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
          <h4 className="text-xs font-semibold text-[var(--color-text-primary)] mb-2">产出物</h4>
          <ArtifactList changeName={change.name} workspace={change.workspace} onSelectArtifact={onOpenArtifact} />
        </div>
        <div className="border border-[var(--color-border)] p-3">
          <h4 className="text-xs font-semibold text-[var(--color-text-primary)] mb-2">文档关联</h4>
          <BacklinksPanel componentId={change.componentId ?? change.name} />
        </div>
      </div>
    </div>
  )
}
