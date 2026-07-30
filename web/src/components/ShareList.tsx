import { useCallback, useEffect, useState } from 'react'
import { revokeShareLink } from '../api/client'
import { copyText } from '../utils/clipboard'
import { Icon } from './icons'
import { Modal } from './Modal'

interface ShareEntry {
  token: string
  path: string
  workspace: string
  expires_at: string
  created_at: string
  url: string
}

type Feedback = { kind: 'success' | 'error'; message: string } | null

export function ShareList() {
  const [shares, setShares] = useState<ShareEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [operation, setOperation] = useState<string | null>(null)
  const [confirmToken, setConfirmToken] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<Feedback>(null)

  const fetchShares = useCallback(async () => {
    setLoading(true)
    setFeedback(null)
    try {
      const response = await fetch('/api/share/list')
      if (!response.ok) throw new Error(`加载分享列表失败 (${response.status})`)
      const data: unknown = await response.json()
      setShares(Array.isArray(data) ? data : [])
    } catch (error) {
      setShares([])
      setFeedback({ kind: 'error', message: error instanceof Error ? error.message : '加载分享列表失败' })
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchShares()
  }, [fetchShares])

  const handleCopy = async (share: ShareEntry) => {
    if (operation) return
    setOperation(`copy:${share.token}`)
    setFeedback(null)
    try {
      await copyText(share.url)
      setFeedback({ kind: 'success', message: `“${filename(share.path)}”的链接已复制` })
    } catch (error) {
      setFeedback({ kind: 'error', message: error instanceof Error ? error.message : '复制链接失败' })
    } finally {
      setOperation(null)
    }
  }

  const handleRevoke = async () => {
    if (!confirmToken || operation) return
    const share = shares.find((entry) => entry.token === confirmToken)
    setOperation(`revoke:${confirmToken}`)
    setFeedback(null)
    try {
      await revokeShareLink(confirmToken)
      setShares((previous) => previous.filter((entry) => entry.token !== confirmToken))
      setConfirmToken(null)
      setFeedback({ kind: 'success', message: share ? `“${filename(share.path)}”的分享已撤销` : '分享已撤销' })
    } catch (error) {
      setFeedback({ kind: 'error', message: error instanceof Error ? error.message : '撤销分享失败' })
    } finally {
      setOperation(null)
    }
  }

  const formatExpiry = (expiresAt: string) => {
    if (!expiresAt || expiresAt === '0001-01-01T00:00:00Z') return '永不过期'
    const date = new Date(expiresAt)
    if (Number.isNaN(date.getTime())) return '过期时间未知'
    const diff = date.getTime() - Date.now()
    if (diff <= 0) return `已于 ${date.toLocaleString('zh-CN')} 过期`
    return `${date.toLocaleString('zh-CN')} 过期`
  }

  const formatCreated = (createdAt: string) => {
    const date = new Date(createdAt)
    return Number.isNaN(date.getTime()) ? '创建时间未知' : `${date.toLocaleString('zh-CN')} 创建`
  }

  const filename = (path: string) => path.split('/').pop() || path
  const confirmingShare = shares.find((entry) => entry.token === confirmToken)

  return (
    <div className="p-4" data-testid="share-list">
      <h2 className="text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider mb-3">已分享文档</h2>
      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          data-testid="share-list-feedback"
          className={`mb-3 text-xs ${feedback.kind === 'error' ? 'text-[var(--color-danger)]' : 'text-[var(--color-success)]'}`}
        >
          <span className="inline-flex items-center gap-1.5">
            <Icon name={feedback.kind === 'error' ? 'warning' : 'check'} size={14} />
            {feedback.message}
          </span>
        </p>
      )}
      {loading ? (
        <div role="status" className="inline-flex items-center gap-2 text-[var(--color-text-secondary)] text-sm py-4">
          <Icon name="spinner" size={16} className="animate-spin" />
          正在加载分享…
        </div>
      ) : shares.length === 0 ? (
        <p className="text-sm text-[var(--color-text-secondary)]">暂无分享。可在 Markdown 查看器中创建分享链接。</p>
      ) : (
        <div className="space-y-2">
          {shares.map((share) => {
            const copying = operation === `copy:${share.token}`
            return (
              <article key={share.token} className="bg-[var(--color-surface)] border border-[var(--color-border)] p-3 text-sm">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 flex-1">
                    <div className="font-medium text-[var(--color-text-primary)] truncate" title={share.path}>
                      {filename(share.path)}
                    </div>
                    <div className="text-xs text-[var(--color-text-secondary)] mt-1 space-y-0.5">
                      <div>Workspace：{share.workspace || '未指定'}</div>
                      <time className="block" dateTime={share.created_at}>{formatCreated(share.created_at)}</time>
                      <time className="block" dateTime={share.expires_at || undefined}>{formatExpiry(share.expires_at)}</time>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <button
                      type="button"
                      onClick={() => void handleCopy(share)}
                      disabled={operation !== null}
                      className="inline-flex items-center gap-1.5 text-xs px-2 py-1 border border-[var(--color-border)] hover:bg-[var(--color-bg)] hover:border-[var(--color-accent)] disabled:opacity-50"
                      aria-label={`复制 ${filename(share.path)} 的分享链接`}
                    >
                      <Icon name={copying ? 'spinner' : 'copy'} size={14} className={copying ? 'animate-spin' : undefined} />
                      {copying ? '复制中…' : '复制'}
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmToken(share.token)}
                      disabled={operation !== null}
                      className="inline-flex items-center gap-1.5 text-xs px-2 py-1 border border-[var(--color-border)] text-[var(--color-danger)] hover:bg-[color-mix(in_srgb,var(--color-danger)_10%,var(--color-surface))] hover:border-[var(--color-danger)] disabled:opacity-50"
                      aria-label={`撤销 ${filename(share.path)} 的分享`}
                    >
                      <Icon name="trash" size={14} />
                      撤销
                    </button>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      )}

      {confirmingShare && (
        <Modal
          title="确认撤销分享"
          onClose={() => {
            if (!operation) setConfirmToken(null)
          }}
          dismissible={operation === null}
          data-testid="share-list-revoke-confirm"
        >
          <div className="p-4">
          <p className="text-sm text-[var(--color-text-secondary)]">
            撤销“{filename(confirmingShare.path)}”的分享后，现有链接将立即失效。
          </p>
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setConfirmToken(null)}
              disabled={operation !== null}
              className="px-3 py-2 text-sm border border-[var(--color-border)] text-[var(--color-text-primary)] disabled:opacity-50"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void handleRevoke()}
              disabled={operation !== null}
              data-testid="share-list-revoke-confirm-btn"
              className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-[var(--color-danger)] text-[var(--color-text-on-color)] disabled:opacity-50"
            >
              <Icon
                name={operation?.startsWith('revoke:') ? 'spinner' : 'trash'}
                size={14}
                className={operation?.startsWith('revoke:') ? 'animate-spin' : undefined}
              />
              {operation?.startsWith('revoke:') ? '正在撤销…' : '确认撤销'}
            </button>
          </div>
          </div>
        </Modal>
      )}
    </div>
  )
}
