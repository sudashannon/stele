import { useState } from 'react'
import type { WorkspaceConfig, WorkspaceSourceType } from '../api/types'

interface Props {
  workspaces: WorkspaceConfig[]
  active: string | null
  onSelect: (alias: string | null) => void
  onAdd: (cfg: WorkspaceConfig) => Promise<void>
}

export function WorkspaceChips({ workspaces, active, onSelect, onAdd }: Props) {
  const [adding, setAdding] = useState(false)
  const [alias, setAlias] = useState('')
  const [path, setPath] = useState('')
  const [sourceType, setSourceType] = useState<'' | WorkspaceSourceType>('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

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
      // Surface the server's rejection (e.g. path has no openspec/changes) inline
      // so the user gets immediate feedback at add-time, not a post-refresh warning.
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
    <div className="relative flex items-center gap-2 flex-wrap">
      <button
        onClick={() => onSelect(null)}
        className={
          'rounded-full px-3 py-1.5 text-xs ' +
          (active === null ? 'bg-[var(--color-accent)] text-white' : 'bg-[var(--color-bg)] text-[var(--color-text-secondary)]')
        }
      >
        全部
      </button>
      {workspaces.map((w) => (
        <button
          key={w.alias}
          onClick={() => onSelect(w.alias)}
          className={
            'rounded-full px-3 py-1.5 text-xs ' +
            (active === w.alias ? 'bg-[var(--color-accent)] text-white' : 'bg-[var(--color-bg)] text-[var(--color-text-secondary)]')
          }
        >
          <span>{w.alias}</span>
          {w.type && (
            <span className="ml-1 opacity-60">{w.type === 'trellis' ? 'T' : w.type === 'superpowers' ? 'S' : 'O'}</span>
          )}
        </button>
      ))}
      <button onClick={() => setAdding(true)} className="rounded-full px-3 py-1.5 text-xs bg-white border border-dashed border-[var(--color-border)] text-[var(--color-text-secondary)]">
        + 添加
      </button>
      {adding && (
        <div className="absolute top-full left-0 mt-2 z-10 w-64 border border-[var(--color-border)] bg-white p-3 shadow-lg flex flex-col gap-2">
          <input
            data-testid="add-ws-alias"
            placeholder="alias"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
            className="w-full border border-[var(--color-border)] px-2 py-1.5 text-sm"
          />
          <input
            data-testid="add-ws-path"
            placeholder="path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            className="w-full border border-[var(--color-border)] px-2 py-1.5 text-sm"
          />
          <select
            data-testid="add-ws-type"
            value={sourceType}
            onChange={(e) => setSourceType(e.target.value as '' | WorkspaceSourceType)}
            className="w-full border border-[var(--color-border)] bg-white px-2 py-1.5 text-sm"
          >
            <option value="">自动识别类型</option>
            <option value="openspec">OpenSpec</option>
            <option value="trellis">Trellis</option>
            <option value="superpowers">Superpowers</option>
          </select>
          {error && (
            <div data-testid="add-ws-error" className="text-xs text-[var(--color-danger)] leading-snug">
              {error}
            </div>
          )}
          <div className="flex items-center justify-end gap-2 pt-1">
            <button onClick={cancel} className="px-2 py-1 text-xs text-[var(--color-text-secondary)]">
              取消
            </button>
            <button
              data-testid="add-ws-submit"
              onClick={submit}
              disabled={!canSubmit}
              className={
                'px-3 py-1 rounded text-xs text-white ' +
                (canSubmit ? 'bg-[var(--color-accent)]' : 'bg-[color-mix(in_srgb,var(--color-accent)_40%,var(--color-surface))] cursor-not-allowed')
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
