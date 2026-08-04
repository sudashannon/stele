import { useState, useCallback, useMemo } from 'react'
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

// Phase colour for the dot fill — fill only, never as glyph colour.
// As glyphs the --viz-* ramp measures as low as 1.29:1; as a 6 px dot it is
// legible by design (surface area + adjacent neutral label).
function phaseDotColor(phase: string): string {
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

// Progress bar inner fill — accent at partial, success green at 100 %.
function barFill(phase: string, pct: number): string {
  if (pct >= 100) return 'var(--color-success)'
  return phaseDotColor(phase)
}

// Display labels for source types that appear in the summary line.
const SOURCE_LABELS: Record<string, string> = {
  openspec: 'OpenSpec',
  trellis: 'Trellis',
  superpowers: 'Superpowers',
  docs: '纯文档',
}

// Display labels for workflow values.
const WORKFLOW_LABELS: Record<string, string> = {
  full: 'full',
  hotfix: 'hotfix',
  tweak: 'tweak',
}

function matchesFilters(
  change: ChangeSummary,
  search: string,
  status: StatusFilter,
  workflow: WorkflowFilter,
  phase: PhaseFilter,
): boolean {
  if (status === 'active' && change.archived) return false
  if (status === 'archived' && !change.archived) return false
  if (workflow !== 'all' && change.workflow !== workflow) return false
  if (phase !== 'all' && change.phase !== phase) return false
  if (search.trim() !== '' && !change.name.toLowerCase().includes(search.trim().toLowerCase())) return false
  return true
}

export function ChangeExplorer({ changes, selected, selectedWorkspace, onSelect }: Props) {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [workflow, setWorkflow] = useState<WorkflowFilter>('all')
  const [phase, setPhase] = useState<PhaseFilter>('all')
  const [archivedOpen, setArchivedOpen] = useState(false)
  const workflowOptions = Array.from(new Set(changes.map((change) => change.workflow).filter(Boolean))).sort()
  const phaseOptions = Array.from(new Set(changes.map((change) => change.phase).filter(Boolean))).sort()

  // Summary line: hoist constant-valued chips (source type, workflow) that
  // were previously repeated on every single row. Only values that actually
  // differ between rows stay in the row (phase, progress, dates).
  const summary = useMemo(() => {
    const sourceTypes = new Set<string>()
    const workflows = new Set<string>()
    for (const c of changes) {
      if (c.sourceType) sourceTypes.add(SOURCE_LABELS[c.sourceType] ?? c.sourceType)
      if (c.workflow) workflows.add(WORKFLOW_LABELS[c.workflow] ?? c.workflow)
    }
    const kindStr = [...sourceTypes].join(' / ')
    const wfStr = workflows.size > 0 ? [...workflows].join(' / ') + ' 工作流' : ''
    return { kinds: kindStr, workflows: wfStr }
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

  // Auto-expand archived when the selected change is archived, or when a filter is
  // active and left nothing in the active section — otherwise a filter that matches
  // only archived rows shows an empty table with the matches hidden below a
  // collapsed divider. Emptiness alone must NOT expand it: a workspace with no
  // active changes still opens collapsed, which is what a reader expects.
  const hasActiveQuery = search.trim() !== '' || status !== 'all' || workflow !== 'all' || phase !== 'all'
  const showArchived = archivedOpen
    || (hasActiveQuery && active.length === 0)
    || (selected !== null && archived.some(
      (c) => c.name === selected && c.workspace === selectedWorkspace,
    ))

  // Context menu wiring.
  const ctx = useContextMenu()
  const handleRowContextMenu = useCallback(
    (change: ChangeSummary) =>
      (event: React.MouseEvent) => {
        event.preventDefault()
        ctx.onContextMenu([
          { id: 'open', label: '打开', run: () => onSelect(change.name, change.workspace) },
          { id: 'copy-name', label: '复制名称', run: () => { copyText(change.name).catch(() => {}) } },
        ])(event)
      },
    [onSelect, ctx],
  )

  function renderRow(change: ChangeSummary) {
    const pct = change.tasksTotal > 0 ? Math.round((change.tasksCompleted / change.tasksTotal) * 100) : 0
    const isSelected = selected === change.name && selectedWorkspace === change.workspace
    const rowKey = `${change.workspace ?? ''}\x00${change.name}`

    return (
      <tr
        key={rowKey}
        onClick={() => onSelect(change.name, change.workspace)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            onSelect(change.name, change.workspace)
          }
        }}
        onContextMenu={handleRowContextMenu(change)}
        role="button"
        tabIndex={0}
        aria-current={isSelected ? 'true' : undefined}
        aria-label={`打开变更 ${change.title || change.name}${change.workspace ? `，工作区 ${change.workspace}` : ''}`}
        className={
          'cursor-pointer ' +
          (isSelected
            ? 'bg-[var(--color-accent-subtle)]'
            : 'hover:bg-[var(--color-hover)]')
        }
      >
        {/* 变更名称 — monospace, no ellipsis, takes remaining width */}
        <td className="font-mono whitespace-nowrap overflow-visible px-3 h-7 text-[length:var(--type-body)] text-[var(--color-text-primary)] border-t border-[var(--color-border-subtle)]">
          {change.name}
        </td>
        {/* 工作区 */}
        <td className="font-mono whitespace-nowrap overflow-hidden text-ellipsis px-3 h-7 text-[length:var(--type-caption)] text-[var(--color-text-secondary)] border-t border-[var(--color-border-subtle)]" style={{ width: 72 }}>
          {change.workspace ?? ''}
        </td>
        {/* 阶段 — coloured dot plus neutral-ink label */}
        <td className="whitespace-nowrap px-3 h-7 border-t border-[var(--color-border-subtle)]" style={{ width: 88 }}>
          <span
            className="inline-block rounded-full align-middle flex-shrink-0"
            style={{ width: 6, height: 6, backgroundColor: phaseDotColor(change.phase), marginRight: 'var(--spacing-02)' }}
          />
          <span className="align-middle text-[length:var(--type-body)] text-[var(--color-text-primary)]">
            {change.phase || '—'}
          </span>
        </td>
        {/* 进度 — the fraction is a fixed-width right-aligned mono box so the
            slashes line up into a column, then the track. Both must be
            whitespace-nowrap: without it `25 / 25` broke at its spaces, wrapping
            to two lines, which made row heights ragged and pushed the track out
            of the column. 4.5em holds `NNN / NNN` at the body size. */}
        <td className="whitespace-nowrap px-3 h-7 border-t border-[var(--color-border-subtle)]" style={{ width: 176 }}>
          <div className="flex items-center gap-2 h-full">
            <span
              className="font-mono tabular-nums text-right whitespace-nowrap shrink-0 text-[length:var(--type-body)] text-[var(--color-text-secondary)]"
              style={{ minWidth: '4.5em' }}
            >
              {change.tasksCompleted} / {change.tasksTotal}
            </span>
            {/* Track uses --color-layer-accent so an empty bar reads as a measured zero, not a divider. */}
            <div className="shrink-0 bg-[var(--color-layer-accent)]" style={{ width: 60, height: 3 }}>
              <div
                style={{ width: `${pct}%`, height: '100%', backgroundColor: barFill(change.phase, pct), minWidth: 0, transition: 'width 0.2s' }}
              />
            </div>
          </div>
        </td>
        {/* 创建日期 — monospace, right-aligned */}
        <td className="font-mono tabular-nums text-right whitespace-nowrap px-3 h-7 text-[length:var(--type-body)] text-[var(--color-text-secondary)] border-t border-[var(--color-border-subtle)]" style={{ width: 100 }}>
          {change.createdAt}
        </td>
      </tr>
    )
  }

  return (
    <div>
      {/* Summary line — hoisted constant-valued chips from every row */}
      {/* The count must describe what the table actually shows. `changes.length`
          counted archived rows too, so a 30-row active table announced 116. */}
      <div className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)] py-1.5">
        {active.length} 个活跃变更
        {archived.length > 0 ? ` · ${archived.length} 个已归档` : ''}
        {summary.kinds ? ` · ${summary.kinds}` : ''}
        {summary.workflows ? ` · ${summary.workflows}` : ''}
      </div>

      {/* Toolbar: search + three filter selects */}
      <div className="flex gap-2 mb-3">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索变更名称…"
          className="flex-1 border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-body)] bg-[var(--color-surface)]"
        />
        <select
          aria-label="状态"
          value={status}
          onChange={(e) => setStatus(e.target.value as StatusFilter)}
          className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] bg-[var(--color-surface)]"
        >
          <option value="all">全部状态</option>
          <option value="active">活跃</option>
          <option value="archived">已归档</option>
        </select>
        <select
          aria-label="工作流"
          value={workflow}
          onChange={(e) => setWorkflow(e.target.value as WorkflowFilter)}
          className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] bg-[var(--color-surface)]"
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
          className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] bg-[var(--color-surface)]"
        >
          <option value="all">全部阶段</option>
          {phaseOptions.map((option) => (
            <option key={option} value={option}>{option}</option>
          ))}
        </select>
      </div>

      {/* Empty-result state when filters produce nothing */}
      {filtered.length === 0 && changes.length > 0 && (
        <div className="flex flex-col items-center gap-2 bg-[var(--color-surface)] py-8 text-center">
          <Icon name="search" size={24} className="text-[var(--color-text-tertiary)]" />
          <div className="text-[length:var(--type-body)] font-medium text-[var(--color-text-secondary)]">无匹配的变更</div>
          <div className="text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">尝试调整搜索关键词或筛选条件</div>
          <button
            type="button"
            onClick={clearFilters}
            className="mt-1 border border-[var(--color-border)] px-3 py-1 text-[length:var(--type-caption)] font-medium text-[var(--color-accent)] hover:bg-[var(--color-accent-subtle)]"
          >
            清除筛选
          </button>
        </div>
      )}

      {/* Full-width change table */}
      {filtered.length > 0 && (
        <div className="overflow-x-auto">
          <table
            className="w-full border-collapse bg-[var(--color-surface)]"
            style={{ tableLayout: 'fixed' }}
          >
            {/* Sticky header on --color-layer (luminance step, not a shadow) */}
            <thead className="sticky top-0 z-10">
              <tr>
                <th className="text-left px-3 h-7 bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)] whitespace-nowrap" style={{ width: 'auto' }}>
                  变更名称
                </th>
                <th className="text-left px-3 h-7 bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)] whitespace-nowrap" style={{ width: 72 }}>
                  工作区
                </th>
                <th className="text-left px-3 h-7 bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)] whitespace-nowrap" style={{ width: 88 }}>
                  阶段
                </th>
                <th className="text-left px-3 h-7 bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)] whitespace-nowrap" style={{ width: 176 }}>
                  进度
                </th>
                <th className="text-right px-3 h-7 bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)] whitespace-nowrap" style={{ width: 100 }}>
                  创建日期
                </th>
              </tr>
            </thead>
            <tbody>
              {active.map(renderRow)}

              {/* Archived section divider — luminance-step band, clickable toggle */}
              {archived.length > 0 && (
                <tr
                  role="button"
                  tabIndex={0}
                  onClick={() => setArchivedOpen((o) => !o)}
                  onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setArchivedOpen((o) => !o) } }}
                  aria-expanded={showArchived}
                  className="cursor-pointer hover:bg-[var(--color-hover)]"
                >
                  <td
                    colSpan={5}
                    className="h-7 px-3 bg-[var(--color-layer)] border-t border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-medium text-[var(--color-text-secondary)]"
                  >
                    已归档 ({archived.length})
                  </td>
                </tr>
              )}

              {showArchived && archived.map(renderRow)}
            </tbody>
          </table>
        </div>
      )}

      {/* Context menu portal */}
      {ctx.renderMenu}
    </div>
  )
}
