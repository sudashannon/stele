import { useMemo, useState } from 'react'
import type { WorkspaceConfig, WorkspaceSourceType } from '../api/types'
import { Icon } from './icons'

interface Props {
  workspaces: WorkspaceConfig[]
  active: string | null
  onSelect: (alias: string | null) => void
  onAdd: (cfg: WorkspaceConfig) => Promise<void>
  onRemove?: (alias: string) => void
  removeDisabledAliases?: readonly string[]
}

function workspaceTypeLabel(type?: WorkspaceSourceType): string | null {
  if (type === 'trellis') return 'Trellis'
  if (type === 'superpowers') return 'Superpowers'
  if (type === 'openspec') return 'OpenSpec'
  return null
}

export function WorkspaceChips({
  workspaces,
  active,
  onSelect,
  onAdd,
  onRemove,
  removeDisabledAliases = [],
}: Props) {
  const [adding, setAdding] = useState(false)
  const [alias, setAlias] = useState('')
  const [path, setPath] = useState('')
  const [sourceType, setSourceType] = useState<'' | WorkspaceSourceType>('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const disabledRemoveSet = useMemo(() => new Set(removeDisabledAliases), [removeDisabledAliases])
  const canSubmit = alias.trim() !== '' && path.trim() !== '' && !submitting

  async function submit() {
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      await onAdd({ alias, path, color: 'var(--color-accent)', type: sourceType || undefined })
      setAdding(false)
      setAlias('')
      setPath('')
      setSourceType('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '添加失败')
    } finally {
      setSubmitting(false)
    }
  }

  function cancel() {
    setAdding(false)
    setAlias('')
    setPath('')
    setSourceType('')
    setError(null)
  }

  return (
    <div className="relative flex flex-wrap items-center gap-2">
      <button
        type="button"
        onClick={() => onSelect(null)}
        className={
          'border px-3 py-1.5 text-[var(--type-caption)] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] ' +
          (active === null
            ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
            : 'border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)]')
        }
      >
        全部
      </button>

      {workspaces.map((workspace) => {
        const isActive = active === workspace.alias
        const typeLabel = workspaceTypeLabel(workspace.type)
        const removeDisabled = disabledRemoveSet.has(workspace.alias)
        const sharedButtonClass =
          'border px-3 py-1.5 text-[var(--type-caption)] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]'

        return (
          <div key={workspace.alias} className="flex items-stretch shadow-[var(--shadow-1)]">
            <button
              type="button"
              onClick={() => onSelect(workspace.alias)}
              className={
                sharedButtonClass +
                ' flex items-center gap-2 font-medium ' +
                (isActive
                  ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
                  : 'border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)]')
              }
            >
              <span>{workspace.alias}</span>
              {typeLabel && (
                <span
                  className={
                    'border px-1.5 py-[1px] text-[var(--type-caption)] leading-none ' +
                    (isActive
                      ? 'border-[color-mix(in_srgb,var(--color-text-on-color)_40%,transparent)] text-[var(--color-text-on-color)]'
                      : 'border-[var(--color-border-subtle)] text-[var(--color-text-tertiary)]')
                  }
                >
                  {typeLabel}
                </span>
              )}
            </button>
            {onRemove && (
              <button
                type="button"
                aria-label={`移除 workspace ${workspace.alias}`}
                title={removeDisabled ? '当前筛选或内容正在使用此 workspace，先切换后再移除' : `移除 workspace ${workspace.alias}`}
                onClick={() => onRemove(workspace.alias)}
                disabled={removeDisabled}
                className={
                  sharedButtonClass +
                  ' border-l-0 px-2 ' +
                  (removeDisabled
                    ? 'cursor-not-allowed border-[var(--color-border)] bg-[var(--color-layer)] text-[var(--color-text-tertiary)]'
                    : isActive
                      ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)] hover:bg-[var(--color-accent-hover)]'
                      : 'border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)]')
                }
              >
                <Icon name="trash" size={14} />
              </button>
            )}
          </div>
        )
      })}

      <button
        type="button"
        onClick={() => setAdding(true)}
        className="inline-flex items-center gap-2 border border-dashed border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-1.5 text-[var(--type-caption)] font-medium text-[var(--color-text-secondary)] transition-colors hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
      >
        <Icon name="plus" size={14} />
        <span>添加 workspace</span>
      </button>

      {adding && (
        <div className="absolute left-0 top-full z-10 mt-2 flex w-72 flex-col gap-2 border border-[var(--color-border)] bg-[var(--color-surface)] p-3 shadow-[var(--shadow-overlay)]">
          <input
            data-testid="add-ws-alias"
            placeholder="alias"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
            className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-[var(--type-body)] text-[var(--color-text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
          />
          <input
            data-testid="add-ws-path"
            placeholder="path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-[var(--type-body)] text-[var(--color-text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
          />
          <select
            data-testid="add-ws-type"
            value={sourceType}
            onChange={(e) => setSourceType(e.target.value as '' | WorkspaceSourceType)}
            className="w-full border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1.5 text-[var(--type-body)] text-[var(--color-text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
          >
            <option value="">自动识别类型</option>
            <option value="openspec">OpenSpec</option>
            <option value="trellis">Trellis</option>
            <option value="superpowers">Superpowers</option>
          </select>
          {error && (
            <div
              data-testid="add-ws-error"
              className="border border-[var(--color-danger)] bg-[var(--color-danger-subtle)] px-2 py-1.5 text-[var(--type-caption)] leading-snug text-[var(--color-danger)]"
            >
              {error}
            </div>
          )}
          <div className="flex items-center justify-end gap-2 pt-1">
            <button
              type="button"
              onClick={cancel}
              className="border border-[var(--color-border)] px-3 py-1 text-[var(--type-caption)] text-[var(--color-text-secondary)] transition-colors hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
            >
              取消
            </button>
            <button
              type="button"
              data-testid="add-ws-submit"
              onClick={submit}
              disabled={!canSubmit}
              className={
                'border px-3 py-1 text-[var(--type-caption)] font-medium text-[var(--color-text-on-color)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] ' +
                (canSubmit
                  ? 'border-[var(--color-accent)] bg-[var(--color-accent)] hover:bg-[var(--color-accent-hover)]'
                  : 'cursor-not-allowed border-[var(--color-border)] bg-[var(--color-layer-accent)] text-[var(--color-text-tertiary)]')
              }
            >
              提交
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
