import { useState, useCallback, useMemo } from 'react'
import type { ReactNode } from 'react'
import type { ChangeSummary } from '../api/types'
import { copyText } from '../utils/clipboard'
import { useContextMenu } from './ContextMenu'
import { Icon } from './icons'


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
    case 'open': case 'planning': return 'var(--color-phase-open)'
    case 'design': case 'plan': return 'var(--color-phase-design)'
    case 'build': case 'in_progress': return 'var(--color-phase-build)'
    case 'verify': return 'var(--color-phase-verify)'
    case 'archive': case 'completed': return 'var(--color-phase-archive)'
    case 'rejected': return 'var(--color-phase-rejected)'
    default: return 'var(--color-phase-unknown)'
  }
}

const PHASE_STYLES: Record<string, string> = {
  open: 'bg-[var(--color-accent-subtle)] text-[var(--color-phase-open)]',
  planning: 'bg-[var(--color-accent-subtle)] text-[var(--color-phase-open)]',
  design: 'bg-[var(--color-purple-subtle)] text-[var(--color-phase-design)]',
  plan: 'bg-[var(--color-purple-subtle)] text-[var(--color-phase-design)]',
  build: 'bg-[var(--color-layer)] text-[var(--color-phase-build)]',
  in_progress: 'bg-[var(--color-layer)] text-[var(--color-phase-build)]',
  verify: 'bg-[var(--color-success-subtle)] text-[var(--color-phase-verify)]',
  archive: 'bg-[var(--color-layer)] text-[var(--color-phase-archive)]',
  completed: 'bg-[var(--color-layer)] text-[var(--color-phase-archive)]',
  rejected: 'bg-[var(--color-danger-subtle)] text-[var(--color-phase-rejected)]',
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

function Badge({
  className,
  children,
  testId,
}: {
  className: string
  children: ReactNode
  testId?: string
}) {
  return (
    <span
      data-testid={testId}
      className={'inline-flex shrink-0 items-center gap-1 border border-[var(--color-border-subtle)] px-1.5 py-1 text-xs font-medium leading-none ' + className}
    >
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
  showWorkspace,
}: {
  change: ChangeSummary
  selected: boolean
  onSelect: () => void
  showWorkspace: boolean
}) {
  const progress = change.tasksTotal > 0 ? change.tasksCompleted / change.tasksTotal : 0
  const phaseStyle = PHASE_STYLES[change.phase] ?? 'bg-[var(--color-bg)] text-[var(--color-text-secondary)]'
  const workspaceMetadata = showWorkspace ? change.workspace?.trim() ?? '' : ''
  const nameMetadata = change.title && change.title !== change.name ? change.name.trim() : ''
  const metadata = `${workspaceMetadata ? `${workspaceMetadata} / ` : ''}${nameMetadata}`

  const ctx = useContextMenu()
  const [copyError, setCopyError] = useState<string | null>(null)
  const handleCopy = useCallback(() => {
    void copyText(change.name)
      .then(() => setCopyError(null))
      .catch(() => setCopyError('复制失败，请手动复制'))
  }, [change.name])
  const handleContextMenu = useCallback((event: React.MouseEvent) => {
    event.preventDefault()
    ctx.onContextMenu([
      { id: 'open', label: '打开', run: onSelect },
      { id: 'copy-name', label: '复制名称', run: handleCopy },
    ])(event)
  }, [handleCopy, onSelect, ctx])
  return (
    <div
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onSelect()
        }
      }}
      role="button"
      tabIndex={0}
      aria-current={selected ? 'true' : undefined}
      aria-label={`打开变更 ${change.title || change.name}${change.workspace ? `，工作区 ${change.workspace}` : ''}`}
      onContextMenu={handleContextMenu}
      className={
        'px-2.5 py-2.5 border cursor-pointer ' +
        (selected
          ? 'border-[var(--color-accent)] bg-[var(--color-accent-subtle)]'
          : 'border-[var(--color-border)] hover:bg-[var(--color-layer)]')
      }
    >
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="text-sm font-medium truncate" title={change.title || change.name}>{change.title || change.name}</div>
          {metadata && (
            <div className="truncate text-xs text-[var(--color-text-secondary)]">
              {metadata}
            </div>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {selected && <Badge className="bg-[var(--color-accent-subtle)] text-[var(--color-accent)]"><Icon name="check" size={13} />已选择</Badge>}
          <Badge className={phaseStyle}><Icon name="recent" size={13} />{change.phase}</Badge>
          <Badge className="bg-[var(--color-layer)] text-[var(--color-text-secondary)]">
            {WORKFLOW_LABELS[change.workflow] ?? change.workflow}
          </Badge>
          {change.sourceType && (
            <Badge className="bg-[var(--color-layer)] text-[var(--color-text-secondary)]">
              {SOURCE_LABELS[change.sourceType] ?? change.sourceType}
            </Badge>
          )}
          {change.verifyResult === 'pass' && (
            <Badge className="bg-[var(--color-success-subtle)] text-[var(--color-success)]"><Icon name="check" size={13} />通过</Badge>
          )}
          {change.verifyResult === 'fail' && (
            <Badge className="bg-[var(--color-danger-subtle)] text-[var(--color-danger)]"><Icon name="warning" size={13} />失败</Badge>
          )}
          {change.stateWarning && (
            <Badge className="bg-[var(--color-warn-subtle)] text-[var(--color-warn-text)]" testId={`warning-${change.name}`}>
              <Icon name="warning" size={13} />状态异常
            </Badge>
          )}
        </div>
      </div>
      <div className="mt-1.5 flex items-center gap-2">
        <div className="h-[5px] flex-1 bg-[var(--color-layer-accent)]" role="progressbar" aria-label="任务进度" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(progress * 100)}>
          <div
            className="h-[5px]"
            style={{ width: `${Math.round(progress * 100)}%`, backgroundColor: barColor(change.phase, progress * 100) }}
          />
        </div>
        <div className="text-xs text-[var(--color-text-secondary)] shrink-0">
          {change.tasksCompleted}/{change.tasksTotal}
        </div>
      </div>
      {copyError && <div role="alert" className="mt-1.5 text-xs text-[var(--color-danger)]">{copyError}</div>}
      {/* useContextMenu() only wires the handler; the caller must render its
       * portal. Without this the right-click handler fired and set state while
       * nothing ever appeared on screen. */}
      {ctx.renderMenu}
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
  const duplicateNames = useMemo(() => {
    const seen = new Set<string>()
    const duplicates = new Set<string>()
    for (const change of changes) {
      if (seen.has(change.name)) duplicates.add(change.name)
      else seen.add(change.name)
    }
    return duplicates
  }, [changes])

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
          <Icon name="search" size={24} className="text-[var(--color-text-tertiary)]" />
          <div className="text-sm font-medium text-[var(--color-text-secondary)]">无匹配的变更</div>
          <div className="text-xs text-[var(--color-text-tertiary)]">尝试调整搜索关键词或筛选条件</div>
          <button
            type="button"
            onClick={clearFilters}
            className="mt-1 border border-[var(--color-border)] px-3 py-1 text-xs font-medium text-[var(--color-accent)] hover:bg-[var(--color-accent-subtle)]"
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
          showWorkspace={duplicateNames.has(c.name)}
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
                  showWorkspace={duplicateNames.has(c.name)}
                />
              ))}
            </div>
          </details>
        </>
      )}
    </div>
  )
}
