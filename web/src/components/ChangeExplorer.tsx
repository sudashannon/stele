import { useState, useCallback } from 'react'
import type { ReactNode } from 'react'
import type { ChangeSummary } from '../api/types'
import { useContextMenu } from './ContextMenu'

interface Props {
  changes: ChangeSummary[]
  selected: string | null
  selectedWorkspace?: string
  onSelect: (name: string, workspace?: string) => void
}

type StatusFilter = 'all' | 'active' | 'archived'
type WorkflowFilter = string
type PhaseFilter = string

function barColor(phase: string, pct: number): string {
  if (pct >= 100) return 'var(--color-success)'
  switch (phase) {
    case 'design': case 'planning': return 'var(--color-accent)'
    case 'verify': return 'rebeccapurple'
    case 'archive': case 'completed': return 'var(--color-success)'
    case 'build': case 'in_progress': return 'var(--color-warn)'
    case 'rejected': return 'var(--color-danger)'
    default: return 'var(--color-border-hover)'
  }
}

const PHASE_STYLES: Record<string, string> = {
  open: 'bg-[var(--color-bg)] text-[var(--color-text-secondary)]',
  planning: 'bg-blue-50 text-[var(--color-accent)]',
  design: 'bg-blue-50 text-[var(--color-accent)]',
  plan: 'bg-blue-50 text-[var(--color-accent)]',
  build: 'bg-amber-50 text-[var(--color-warn)]',
  in_progress: 'bg-amber-50 text-[var(--color-warn)]',
  verify: 'bg-violet-50 text-violet-600',
  archive: 'bg-green-50 text-[var(--color-success)]',
  completed: 'bg-green-50 text-[var(--color-success)]',
  rejected: 'bg-red-50 text-[var(--color-danger)]',
}

const WORKFLOW_LABELS: Record<string, string> = {
  full: 'full',
  hotfix: 'hotfix',
  tweak: 'tweak',
}

const SOURCE_LABELS: Record<string, string> = {
  openspec: 'OpenSpec',
  trellis: 'Trellis',
  superpowers: 'Superpowers',
}

function Badge({ className, children }: { className: string; children: ReactNode }) {
  return (
    <span className={'shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium leading-none ' + className}>
      {children}
    </span>
  )
}

// Renders a single change card. Extracted so the active and archived lists can
// share identical card markup without doubling the JSX.
function ChangeCard({
  change,
  selected,
  onSelect,
}: {
  change: ChangeSummary
  selected: boolean
  onSelect: () => void
}) {
  const progress = change.tasksTotal > 0 ? change.tasksCompleted / change.tasksTotal : 0
  const phaseStyle = PHASE_STYLES[change.phase] ?? 'bg-[var(--color-bg)] text-[var(--color-text-secondary)]'

  const ctx = useContextMenu()
  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    ctx.onContextMenu([
      { id: 'open', label: '打开', icon: '📂', run: onSelect },
      { id: 'copy-name', label: '复制名称', icon: '📋', run: () => navigator.clipboard.writeText(change.name) },
    ])(e)
  }, [change.name, onSelect, ctx])

  return (
    <div
      onClick={onSelect}
      onContextMenu={handleContextMenu}
      className={
        'px-2.5 py-2.5 border cursor-pointer ' +
        (selected
          ? 'border-transparent bg-blue-50 shadow-[inset_0_0_0_1px_var(--color-border)]'
          : 'border-[var(--color-border)] hover:bg-[var(--color-bg)]')
      }
    >
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="text-sm font-medium truncate" title={change.title || change.name}>{change.title || change.name}</div>
          {change.title && change.title !== change.name && (
            <div className="text-[10px] text-[var(--color-text-secondary)] truncate">{change.name}</div>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Badge className={phaseStyle}>{change.phase}</Badge>
          <Badge className="bg-[var(--color-bg)] text-[var(--color-text-secondary)]">
            {WORKFLOW_LABELS[change.workflow] ?? change.workflow}
          </Badge>
          {change.sourceType && (
            <Badge className="bg-[var(--color-bg)] text-[var(--color-text-secondary)]">
              {SOURCE_LABELS[change.sourceType] ?? change.sourceType}
            </Badge>
          )}
          {change.verifyResult === 'pass' && (
            <Badge className="bg-green-50 text-[var(--color-success)]">✓ pass</Badge>
          )}
          {change.verifyResult === 'fail' && (
            <Badge className="bg-red-50 text-[var(--color-danger)]">✗ fail</Badge>
          )}
          {change.stateWarning && (
            <Badge className="bg-amber-50 text-[var(--color-warn)]" data-testid={`warning-${change.name}`}>
              ⚠
            </Badge>
          )}
        </div>
      </div>
      <div className="mt-1.5 flex items-center gap-2">
        <div className="h-[5px] flex-1 rounded-full bg-[var(--color-bg)]">
          <div
            className="h-[5px] rounded-full"
            style={{ width: `${Math.round(progress * 100)}%`, backgroundColor: barColor(change.phase, progress * 100) }}
          />
        </div>
        <div className="text-xs text-[var(--color-text-secondary)] shrink-0">
          {change.tasksCompleted}/{change.tasksTotal}
        </div>
      </div>
    </div>
  )
}

function matchesFilters(
  change: ChangeSummary,
  search: string,
  status: StatusFilter,
  workflow: WorkflowFilter,
  phase: PhaseFilter,
) {
  if (search && !`${change.name} ${change.title ?? ''}`.toLowerCase().includes(search.toLowerCase())) return false
  if (status === 'active' && change.archived) return false
  if (status === 'archived' && !change.archived) return false
  if (workflow !== 'all' && change.workflow !== workflow) return false
  if (phase !== 'all' && change.phase !== phase) return false
  return true
}

export function ChangeExplorer({ changes, selected, selectedWorkspace, onSelect }: Props) {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [workflow, setWorkflow] = useState<WorkflowFilter>('all')
  const [phase, setPhase] = useState<PhaseFilter>('all')
  const workflowOptions = Array.from(new Set(changes.map((change) => change.workflow).filter(Boolean))).sort()
  const phaseOptions = Array.from(new Set(changes.map((change) => change.phase).filter(Boolean))).sort()

  const filtered = changes.filter((c) => matchesFilters(c, search, status, workflow, phase))
  const active = filtered.filter((c) => !c.archived)
  const archived = filtered.filter((c) => c.archived)

  const clearFilters = () => {
    setSearch('')
    setStatus('all')
    setWorkflow('all')
    setPhase('all')
  }

  // Auto-expand the archived section when the selected change is archived OR
  // when the user is actively searching/filtering — otherwise a search whose
  // only matches are archived looks like "无匹配" behind a collapsed group.
  const hasActiveQuery = search.trim() !== '' || status !== 'all' || workflow !== 'all' || phase !== 'all'
  const selectedIsArchived =
    (selected !== null && archived.some((change) => change.name === selected && change.workspace === selectedWorkspace)) ||
    hasActiveQuery

  return (
    <div className="space-y-2">
      <div className="space-y-2">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索变更名称…"
          className="w-full border border-[var(--color-border)] px-2 py-1 text-sm"
        />
        <div className="flex gap-2">
          <select
            aria-label="状态"
            value={status}
            onChange={(e) => setStatus(e.target.value as StatusFilter)}
            className="flex-1 border border-[var(--color-border)] px-2 py-1 text-xs"
          >
            <option value="all">全部状态</option>
            <option value="active">活跃</option>
            <option value="archived">已归档</option>
          </select>
          <select
            aria-label="工作流"
            value={workflow}
            onChange={(e) => setWorkflow(e.target.value as WorkflowFilter)}
            className="flex-1 border border-[var(--color-border)] px-2 py-1 text-xs"
          >
            <option value="all">全部工作流</option>
            {workflowOptions.map((option) => (
              <option key={option} value={option}>{option}</option>
            ))}
          </select>
          <select
            aria-label="阶段"
            value={phase}
            onChange={(e) => setPhase(e.target.value as PhaseFilter)}
            className="flex-1 border border-[var(--color-border)] px-2 py-1 text-xs"
          >
            <option value="all">全部阶段</option>
            {phaseOptions.map((option) => (
              <option key={option} value={option}>{option}</option>
            ))}
          </select>
        </div>
      </div>
      {active.length === 0 && archived.length === 0 && (
        <div className="flex flex-col items-center gap-2 border border-dashed border-[var(--color-border)] py-8 text-center">
          <span className="text-2xl text-[var(--color-text-tertiary)]" aria-hidden="true">🔍</span>
          <div className="text-sm font-medium text-[var(--color-text-secondary)]">无匹配的变更</div>
          <div className="text-xs text-[var(--color-text-tertiary)]">尝试调整搜索关键词或筛选条件</div>
          <button
            type="button"
            onClick={clearFilters}
            className="mt-1 border border-[var(--color-border)] px-3 py-1 text-xs font-medium text-[var(--color-accent)] hover:bg-blue-50"
          >
            清除筛选
          </button>
        </div>
      )}
      {active.map((c) => (
        <ChangeCard
          key={`${c.workspace ?? ''}\u0000${c.name}`}
          change={c}
          selected={selected === c.name && selectedWorkspace === c.workspace}
          onSelect={() => onSelect(c.name, c.workspace)}
        />
      ))}
      {archived.length > 0 && (
        <>
          <div className="border-t border-[var(--color-border)] my-3" />
          <details open={selectedIsArchived}>
            <summary className="text-xs text-[var(--color-text-secondary)] cursor-pointer select-none font-medium">
              已归档 ({archived.length})
            </summary>
            <div className="space-y-2 mt-2">
              {archived.map((c) => (
                <ChangeCard
                  key={`${c.workspace ?? ''}\u0000${c.name}`}
                  change={c}
                  selected={selected === c.name && selectedWorkspace === c.workspace}
                  onSelect={() => onSelect(c.name, c.workspace)}
                />
              ))}
            </div>
          </details>
        </>
      )}
    </div>
  )
}
