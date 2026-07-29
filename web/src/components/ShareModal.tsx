import { useState, useCallback } from 'react'
import { createShareLink, revokeShareLink } from '../api/client'

interface ShareModalProps {
  path: string | null
  workspace?: string
  onClose: () => void
}

const TTL_OPTIONS: { label: string; value: number | 0 }[] = [
  { label: '1 小时', value: 3600 },
  { label: '24 小时', value: 86400 },
  { label: '7 天', value: 604800 },
  { label: '永不过期', value: 0 },
]

export function ShareModal({ path, workspace, onClose }: ShareModalProps) {
  const [ttl, setTtl] = useState<number>(3600)
  const [link, setLink] = useState<string | null>(null)
  const [token, setToken] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleCreate = useCallback(async () => {
    if (!path) return
    setLoading(true)
    setError(null)
    try {
      // The backend owns the public share base URL; window.location.origin may
      // be localhost even when recipients must use the Windows LAN address.
      const resp = await createShareLink(path, workspace, ttl)
      setLink(resp.url)
      const parts = resp.url.split('/share/')
      if (parts.length === 2) setToken(parts[1])
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建失败')
    } finally {
      setLoading(false)
    }
  }, [path, workspace, ttl])

  const handleRevoke = useCallback(async () => {
    if (!token) return
    setLoading(true)
    try {
      await revokeShareLink(token)
      setLink(null)
      setToken(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '撤销失败')
    } finally {
      setLoading(false)
    }
  }, [token])

  const handleCopy = useCallback(async () => {
    const urlToCopy = link
    if (!urlToCopy) return
    try {
      await navigator.clipboard.writeText(urlToCopy)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // fallback for non-HTTPS contexts
      const el = document.createElement('textarea')
      el.value = urlToCopy
      document.body.appendChild(el)
      el.select()
      document.execCommand('copy')
      document.body.removeChild(el)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }, [link])

  return (
    <div
      className="fixed inset-0 z-50 bg-black/30 flex items-center justify-center p-4"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="bg-white shadow-2xl w-full max-w-sm p-6" data-testid="share-modal">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-bold text-[var(--color-text-primary)]">分享文档</h2>
          <button
            onClick={onClose}
            className="text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] text-lg leading-none"
            aria-label="关闭"
          >✕</button>
        </div>

        {!link ? (
          <>
            <label className="block text-xs text-[var(--color-text-secondary)] mb-1">链接有效期</label>
            <div className="flex gap-2 mb-4">
              {TTL_OPTIONS.map((opt) => (
                <button
                  key={opt.value}
                  data-testid={`share-ttl-${opt.value}`}
                  aria-pressed={ttl === opt.value}
                  onClick={() => setTtl(opt.value)}
                  className={`px-3 py-1.5 text-xs border transition-colors ${
                    ttl === opt.value
                      ? 'bg-[var(--color-accent)] text-white border-[var(--color-accent)]'
                      : 'bg-white text-[var(--color-text-primary)] border-[var(--color-border)] hover:border-[var(--color-accent)]'
                  }`}
                >{opt.label}</button>
              ))}
            </div>
            <button
              onClick={handleCreate}
              disabled={loading}
              data-testid="share-create-btn"
              className="w-full py-2 bg-[var(--color-accent)] text-white text-sm font-semibold disabled:opacity-50 hover:bg-[var(--color-accent-hover)] transition-colors"
            >{loading ? '创建中…' : '生成分享链接'}</button>
          </>
        ) : (
          <>
            <div className="flex items-center gap-2 mb-4">
              <input
                type="text"
                value={link}
                readOnly
                data-testid="share-link-input"
                className="flex-1 text-xs bg-[var(--color-bg)] px-3 py-2 border border-[var(--color-border)] text-[var(--color-text-primary)] overflow-hidden text-ellipsis"
              />
              <button
                onClick={handleCopy}
                data-testid="share-copy-btn"
                className={`shrink-0 px-3 py-2 text-xs font-semibold transition-colors ${
                  copied ? 'bg-[var(--color-success)] text-white' : 'bg-[var(--color-accent)] text-white hover:bg-[var(--color-accent-hover)]'
                }`}
              >{copied ? '已复制' : '复制'}</button>
            </div>
            <div className="flex gap-2">
              <button
                onClick={handleCreate}
                disabled={loading}
                className="flex-1 py-2 border border-[var(--color-border)] text-[var(--color-text-primary)] text-xs font-semibold hover:bg-[var(--color-bg)] transition-colors"
              >重新生成</button>
              <button
                onClick={handleRevoke}
                disabled={loading}
                data-testid="share-revoke-btn"
                className="flex-1 py-2 border border-[var(--color-danger)] text-[var(--color-danger)] text-xs font-semibold hover:bg-[color-mix(in_srgb,var(--color-danger)_10%,var(--color-surface))] transition-colors"
              >撤销分享</button>
            </div>
          </>
        )}
        {error && <p className="mt-3 text-xs text-[var(--color-danger)]">{error}</p>}
      </div>
    </div>
  )
}
