import { useState } from 'react'
import type { WorkspaceSourceType } from '../api/types'
import { Icon } from './icons'
import { Modal } from './Modal'

const PHASE_LABELS: Record<string, string> = {
  open: '启动', design: '设计', build: '构建', verify: '验证', archive: '归档',
}

const EXIT_MARKER_RE = /__GUARD_EXIT__:(\d)(?::(.*))?/

const VALID_CHANGE_NAME_RE = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/

export function isValidChangeName(name: string, sourceType: WorkspaceSourceType = 'openspec'): boolean {
  if (sourceType === 'superpowers') return false
  if (sourceType === 'trellis') {
    return name !== '' && name !== '.' && !name.includes('..') && !/[\\/]/.test(name)
  }
  return VALID_CHANGE_NAME_RE.test(name)
}

interface Props {
  changeName: string
  targetPhase: string
  onComplete: () => void
  blockedReason?: string
  workspace?: string
  sourceType?: WorkspaceSourceType
  label?: string
  command?: string
}

export function GuardButton({
  changeName,
  targetPhase,
  onComplete,
  blockedReason,
  workspace,
  sourceType = 'openspec',
  label,
  command,
}: Props) {
  const [confirming, setConfirming] = useState(false)
  const [output, setOutput] = useState<string[]>([])
  const [running, setRunning] = useState(false)
  const [tone, setTone] = useState<'ok' | 'danger' | null>(null)

  async function execute() {
    setConfirming(false)
    setRunning(true)
    setOutput([])
    setTone(null)
    try {
      const params = new URLSearchParams()
      if (workspace) params.set('workspace', workspace)
      const query = params.size > 0 ? `?${params.toString()}` : ''
      const res = await fetch(`/api/changes/${encodeURIComponent(changeName)}/transition${query}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ targetPhase }),
      })
      if (!res.ok || !res.body) {
        setOutput((o) => [...o, `错误: HTTP ${res.status}`])
        setTone('danger')
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let sawSuccess = false
      let sawFailure = false
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        const chunk = decoder.decode(value)
        const marker = chunk.match(EXIT_MARKER_RE)
        if (marker) {
          if (marker[1] === '0') sawSuccess = true
          else sawFailure = true
          continue // exit marker is protocol, not guard output — don't display it
        }
        setOutput((o) => [...o, chunk])
      }
      if (sawSuccess) {
        setTone('ok')
        onComplete()
        setOutput([]) // auto-clear on success — the change list refresh (via onComplete) is the confirmation
      } else if (sawFailure) {
        setTone('danger')
      }
    } catch (e) {
      setOutput((o) => [...o, `错误: ${(e as Error).message}`])
      setTone('danger')
    } finally {
      setRunning(false)
    }
  }

  const nameValid = isValidChangeName(changeName, sourceType)
  const nameInvalidMsg = sourceType === 'trellis'
    ? 'Trellis 任务目录名无效，无法迁移'
    : sourceType === 'superpowers'
      ? 'Superpowers 工作区为只读，无法迁移'
      : '变更名不满足 guard 规则（需字母开头，小写 kebab-case），无法迁移'
  const disabledReason = !nameValid ? nameInvalidMsg : blockedReason

  return (
    <>
      <button
        type="button"
        data-testid="guard-trigger"
        onClick={() => setConfirming(true)}
        disabled={running || !nameValid || !!blockedReason}
        title={disabledReason}
        className="inline-flex items-center gap-1"
      >
        <Icon name="chevron-right" size={14} />
        <span>{label ?? PHASE_LABELS[targetPhase] ?? targetPhase}</span>
      </button>

      {confirming && (
        <Modal
          title="确认阶段迁移"
          onClose={() => setConfirming(false)}
          data-testid="guard-confirm-dialog"
        >
          <div className="space-y-4 p-4">
            <p className="text-[var(--type-body)] text-[var(--color-text-primary)]">
              即将执行：
            </p>
            <code className="block overflow-x-auto bg-[var(--color-layer)] p-3 font-mono text-[var(--type-caption)] text-[var(--color-text-primary)]">
              {command ?? `comet-guard ${changeName} ${targetPhase} --apply`}
            </code>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setConfirming(false)}
                className="border border-[var(--color-border)] px-3 py-2 text-[var(--type-caption)] text-[var(--color-text-secondary)]"
              >
                取消
              </button>
              <button
                type="button"
                data-testid="guard-confirm-yes"
                onClick={execute}
                className="border border-[var(--color-accent)] bg-[var(--color-accent)] px-3 py-2 text-[var(--type-caption)] font-medium text-[var(--color-text-on-color)]"
              >
                确认
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* On success, output is cleared immediately after onComplete fires (see
          setOutput([]) above), so this panel naturally does not render —
          satisfying the "成功自动关闭" requirement without a separate timer. */}
      {output.length > 0 && (
        <pre
          data-testid="guard-output"
          data-tone={tone}
          className={
            'text-xs p-2 mt-2 max-h-40 overflow-y-auto ' +
            (tone === 'danger'
              ? 'bg-[var(--color-danger)]/10 text-[var(--color-danger)]'
              : 'bg-[var(--color-text-primary)] text-[var(--color-bg)]')
          }
        >
          {output.join('')}
        </pre>
      )}
    </>
  )
}
