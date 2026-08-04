import { useEffect, useState } from 'react'
import { Icon } from './icons'

/**
 * A deploy rewrites every hashed chunk, so a tab opened before it keeps asking
 * for file names that no longer exist. React's lazy() boundaries then reject
 * with "Failed to fetch dynamically imported module" and — with no error
 * boundary above them — the view simply never appears: clicking the rail looks
 * like it does nothing. Watch for that specific failure anywhere on the page
 * (views, mermaid, katex, cytoscape) and tell the user to reload.
 *
 * `Cache-Control: no-cache` on index.html (see staticHandler in main.go) stops
 * new loads from pinning stale hashes; this covers the tab that is already open.
 */
const STALE_CHUNK_PATTERN =
  /failed to fetch dynamically imported module|error loading dynamically imported module|importing a module script failed/i

function isStaleChunkFailure(reason: unknown): boolean {
  if (!reason) return false
  const message = reason instanceof Error ? `${reason.name}: ${reason.message}` : String(reason)
  return STALE_CHUNK_PATTERN.test(message)
}

export function StaleBundleNotice({ onReload }: { onReload?: () => void }) {
  const [stale, setStale] = useState(false)

  useEffect(() => {
    const onError = (event: ErrorEvent) => {
      if (isStaleChunkFailure(event.error ?? event.message)) setStale(true)
    }
    const onRejection = (event: PromiseRejectionEvent) => {
      if (isStaleChunkFailure(event.reason)) setStale(true)
    }
    window.addEventListener('error', onError)
    window.addEventListener('unhandledrejection', onRejection)
    return () => {
      window.removeEventListener('error', onError)
      window.removeEventListener('unhandledrejection', onRejection)
    }
  }, [])

  if (!stale) return null

  return (
    <div
      data-testid="stale-bundle-notice"
      role="alert"
      className="fixed left-1/2 top-4 z-50 flex max-w-[36rem] -translate-x-1/2 items-start gap-3 border border-[var(--color-warn)] bg-[var(--color-warn-subtle)] px-4 py-3 shadow-[var(--shadow-2)]"
    >
      <Icon name="warning" size={16} className="mt-0.5 shrink-0 text-[var(--color-warn-text)]" />
      <div className="space-y-2">
        <p className="text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
          面板已发布新版本，当前页面加载的资源已失效，部分视图无法打开。刷新后即可恢复。
        </p>
        <button
          type="button"
          data-testid="stale-bundle-reload"
          onClick={() => (onReload ? onReload() : window.location.reload())}
          className="border border-[var(--color-accent)] bg-[var(--color-accent)] px-3 py-1.5 text-[length:var(--type-caption)] font-medium text-[var(--color-text-on-color)]"
        >
          刷新页面
        </button>
      </div>
    </div>
  )
}
