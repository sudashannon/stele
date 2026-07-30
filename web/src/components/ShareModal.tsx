import { useCallback, useState } from 'react'
import { createShareLink, revokeShareLink } from '../api/client'
import { copyText } from '../utils/clipboard'
import { Icon } from './icons'
import { Modal } from './Modal'

interface ShareModalProps {
  path: string | null
  workspace?: string
  onClose: () => void
}

const TTL_OPTIONS: { label: string; value: number }[] = [
  { label: '1 小时', value: 3600 },
  { label: '24 小时', value: 86400 },
  { label: '7 天', value: 604800 },
  { label: '永不过期', value: 0 },
]

type Feedback = { kind: 'success' | 'error'; message: string } | null

export function ShareModal({ path, workspace, onClose }: ShareModalProps) {
  const [ttl, setTtl] = useState(3600)
  const [link, setLink] = useState<string | null>(null)
  const [token, setToken] = useState<string | null>(null)
  const [operation, setOperation] = useState<'create' | 'copy' | 'revoke' | null>(null)
  const [confirmingRevoke, setConfirmingRevoke] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const handleCreate = useCallback(async () => {
    if (!path || operation) return
    setOperation('create')
    setFeedback(null)
    setLink(null)
    setToken(null)
    try {
      const response = await createShareLink(path, workspace, ttl)
      const parsedURL = new URL(response.url, window.location.href)
      const marker = '/share/'
      const parsedToken = parsedURL.pathname.startsWith(marker) ? parsedURL.pathname.slice(marker.length) : ''
      if (!parsedToken) throw new Error('无法从分享链接解析撤销令牌')
      setLink(response.url)
      setToken(parsedToken)
      setFeedback({ kind: 'success', message: '分享链接已创建' })
    } catch (error) {
      setFeedback({ kind: 'error', message: error instanceof Error ? error.message : '创建分享链接失败' })
    } finally {
      setOperation(null)
    }
  }, [operation, path, ttl, workspace])

  const handleCopy = useCallback(async () => {
    if (!link || operation) return
    setOperation('copy')
    setFeedback(null)
    try {
      await copyText(link)
      setFeedback({ kind: 'success', message: '链接已复制' })
    } catch (error) {
      setFeedback({ kind: 'error', message: error instanceof Error ? error.message : '复制链接失败' })
    } finally {
      setOperation(null)
    }
  }, [link, operation])

  const handleRevoke = useCallback(async () => {
    if (!token || operation) return
    setOperation('revoke')
    setFeedback(null)
    try {
      await revokeShareLink(token)
      setLink(null)
      setToken(null)
      setConfirmingRevoke(false)
      setFeedback({ kind: 'success', message: '分享已撤销' })
    } catch (error) {
      setConfirmingRevoke(false)
      setFeedback({ kind: 'error', message: error instanceof Error ? error.message : '撤销分享失败' })
    } finally {
      setOperation(null)
    }
  }, [operation, token])

  if (confirmingRevoke) {
    return (
      <Modal title="确认撤销分享" onClose={() => setConfirmingRevoke(false)} data-testid="share-revoke-confirm">
        <div className="p-4">
        <p className="text-sm text-[var(--color-text-secondary)]">撤销后，现有链接将立即失效。此操作无法撤回。</p>
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setConfirmingRevoke(false)}
            disabled={operation === 'revoke'}
            className="px-3 py-2 text-sm border border-[var(--color-border)] text-[var(--color-text-primary)] disabled:opacity-50"
          >
            取消
          </button>
          <button
            type="button"
            data-testid="share-revoke-confirm-btn"
            onClick={handleRevoke}
            disabled={operation === 'revoke'}
            className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-[var(--color-danger)] text-[var(--color-text-on-color)] disabled:opacity-50"
          >
            <Icon name={operation === 'revoke' ? 'spinner' : 'trash'} size={14} className={operation === 'revoke' ? 'animate-spin' : undefined} />
            {operation === 'revoke' ? '正在撤销…' : '确认撤销'}
          </button>
        </div>
        </div>
      </Modal>
    )
  }

  return (
    <Modal title="分享文档" onClose={onClose} width="max-w-sm" data-testid="share-modal">
      <div className="p-4">
      {!link ? (
        <>
          <fieldset disabled={operation !== null}>
            <legend className="text-xs text-[var(--color-text-secondary)] mb-2">链接有效期</legend>
            <div className="grid grid-cols-2 gap-2 mb-4">
              {TTL_OPTIONS.map((option) => (
                <button
                  type="button"
                  key={option.value}
                  data-testid={`share-ttl-${option.value}`}
                  aria-pressed={ttl === option.value}
                  onClick={() => setTtl(option.value)}
                  className={`px-3 py-1.5 text-xs border transition-colors ${
                    ttl === option.value
                      ? 'bg-[var(--color-accent)] text-[var(--color-text-on-color)] border-[var(--color-accent)]'
                      : 'bg-[var(--color-surface)] text-[var(--color-text-primary)] border-[var(--color-border)] hover:border-[var(--color-accent)]'
                  }`}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </fieldset>
          <button
            type="button"
            onClick={handleCreate}
            disabled={!path || operation !== null}
            data-testid="share-create-btn"
            className="inline-flex w-full items-center justify-center gap-1.5 py-2 bg-[var(--color-accent)] text-[var(--color-text-on-color)] text-sm font-semibold disabled:opacity-50 hover:bg-[var(--color-accent-hover)] transition-colors"
          >
            <Icon name={operation === 'create' ? 'spinner' : 'share'} size={14} className={operation === 'create' ? 'animate-spin' : undefined} />
            {operation === 'create' ? '创建中…' : '生成分享链接'}
          </button>
        </>
      ) : (
        <>
          <label htmlFor="share-link" className="block text-xs text-[var(--color-text-secondary)] mb-1">分享链接</label>
          <div className="flex items-center gap-2 mb-4">
            <input
              id="share-link"
              type="text"
              value={link}
              readOnly
              data-testid="share-link-input"
              className="flex-1 min-w-0 text-xs bg-[var(--color-bg)] px-3 py-2 border border-[var(--color-border)] text-[var(--color-text-primary)] overflow-hidden text-ellipsis"
            />
            <button
              type="button"
              onClick={handleCopy}
              disabled={operation !== null}
              data-testid="share-copy-btn"
              className="inline-flex shrink-0 items-center gap-1.5 px-3 py-2 text-xs font-semibold bg-[var(--color-accent)] text-[var(--color-text-on-color)] hover:bg-[var(--color-accent-hover)] disabled:opacity-50"
            >
              <Icon name={operation === 'copy' ? 'spinner' : 'copy'} size={14} className={operation === 'copy' ? 'animate-spin' : undefined} />
              {operation === 'copy' ? '复制中…' : '复制'}
            </button>
          </div>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={handleCreate}
              disabled={operation !== null}
              className="inline-flex flex-1 items-center justify-center gap-1.5 py-2 border border-[var(--color-border)] text-[var(--color-text-primary)] text-xs font-semibold hover:bg-[var(--color-bg)] disabled:opacity-50"
            >
              <Icon name="refresh" size={14} />
              重新生成
            </button>
            <button
              type="button"
              onClick={() => setConfirmingRevoke(true)}
              disabled={!token || operation !== null}
              data-testid="share-revoke-btn"
              className="inline-flex flex-1 items-center justify-center gap-1.5 py-2 border border-[var(--color-danger)] text-[var(--color-danger)] text-xs font-semibold hover:bg-[color-mix(in_srgb,var(--color-danger)_10%,var(--color-surface))] disabled:opacity-50"
            >
              <Icon name="trash" size={14} />
              撤销分享
            </button>
          </div>
        </>
      )}
      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          data-testid="share-feedback"
          className={`mt-3 text-xs ${feedback.kind === 'error' ? 'text-[var(--color-danger)]' : 'text-[var(--color-success)]'}`}
        >
          <span className="inline-flex items-center gap-1.5">
            <Icon name={feedback.kind === 'error' ? 'warning' : 'check'} size={14} />
            {feedback.message}
          </span>
        </p>
      )}
      </div>
    </Modal>
  )
}
