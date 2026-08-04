import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchRecent } from '../api/client'
import type { RecentItem } from '../api/types'
import { copyText } from '../utils/clipboard'
import { useContextMenu } from './ContextMenu'
import { StateBlock } from './StateBlock'
import { typeBadgeClass } from './graphPalette'

const MINUTE_MS = 60_000
const HOUR_MS = 60 * MINUTE_MS
const DAY_MS = 24 * HOUR_MS

function formatRelativeTime(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return iso
  const diff = Date.now() - then
  if (diff < MINUTE_MS) return '刚刚'
  if (diff < HOUR_MS) return `${Math.floor(diff / MINUTE_MS)}分钟前`
  if (diff < DAY_MS) return `${Math.floor(diff / HOUR_MS)}小时前`
  if (diff < 7 * DAY_MS) return `${Math.floor(diff / DAY_MS)}天前`
  return new Date(then).toLocaleDateString('zh-CN')
}

type RecentGroup = {
  key: 'today' | 'yesterday' | 'older'
  label: '今天' | '昨天' | '更早'
  items: RecentItem[]
}

function groupRecentItems(items: RecentItem[]): RecentGroup[] {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)

  const sorted = items
    .map((item, index) => ({ item, index }))
    .sort((a, b) => {
      const timeDiff = new Date(b.item.updatedAt).getTime() - new Date(a.item.updatedAt).getTime()
      return timeDiff || a.index - b.index
    })
    .map(({ item }) => item)

  const groups: RecentGroup[] = [
    { key: 'today', label: '今天', items: [] },
    { key: 'yesterday', label: '昨天', items: [] },
    { key: 'older', label: '更早', items: [] },
  ]
  for (const item of sorted) {
    const updatedAt = new Date(item.updatedAt)
    let group = groups[2]
    if (updatedAt >= today) group = groups[0]
    else if (updatedAt >= yesterday) group = groups[1]
    group.items.push(item)
  }
  return groups.filter((group) => group.items.length > 0)
}


export function RecentPanel({ onOpen }: { onOpen?: (path: string) => void }) {
  const [items, setItems] = useState<RecentItem[]>([])
  const [loadError, setLoadError] = useState(false)
  const [loading, setLoading] = useState(true)
  const [hasMore, setHasMore] = useState(true)
  const [copyError, setCopyError] = useState<string | null>(null)
  const ctx = useContextMenu()

  const load = useCallback(async (offset: number) => {
    const CHUNK = 20
    try {
      const data = await fetchRecent(offset, CHUNK)
      setItems((prev) => offset === 0 ? data : [...prev, ...data])
      setHasMore(data.length === CHUNK)
      setLoadError(false)
    } catch {
      setLoadError(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load(0)
  }, [load])

  const groups = useMemo(() => groupRecentItems(items), [items])
  const openItem = useCallback((path: string) => onOpen?.(path), [onOpen])
  const handleCopy = useCallback((text: string) => {
    void copyText(text)
      .then(() => setCopyError(null))
      .catch(() => setCopyError('复制失败，请手动复制'))
  }, [])

  if (loading && items.length === 0) {
    return <StateBlock kind="loading" title="加载中…" compact />
  }
  if (loadError && items.length === 0) {
    return <StateBlock kind="error" title="加载失败" compact />
  }
  if (items.length === 0) {
    return <StateBlock kind="empty" title="暂无最近变更" compact />
  }

  return (
    <div>
      <div className="space-y-4">
        {groups.map((group) => (
          <section key={group.key} aria-labelledby={`recent-${group.key}`}>
            <h3
              id={`recent-${group.key}`}
              className="mb-1.5 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]"
            >
              {group.label}
            </h3>
            <ul className="space-y-1.5 text-[length:var(--type-body)]">
              {group.items.map((item) => (
                <li key={item.id}>
                  <button
                    type="button"
                    onClick={() => openItem(item.path)}
                    onContextMenu={ctx.onContextMenu([
                      { id: 'open', label: '打开', run: () => openItem(item.path) },
                      { id: 'copy-path', label: '复制路径', run: () => handleCopy(item.path) },
                      { id: 'copy-title', label: '复制标题', run: () => handleCopy(item.title) },
                    ])}
                    className="flex w-full items-center gap-2 border border-[var(--color-border)] px-3 py-2 text-left hover:bg-[var(--color-layer)]"
                  >
                    <span
                      className={`shrink-0 px-1.5 py-0.5 text-[length:var(--type-caption)] font-medium font-mono ${typeBadgeClass(item.type)}`}
                    >
                      {item.type}
                    </span>
                    <span className="flex-1 truncate font-medium">{item.title}</span>
                    <span className="shrink-0 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{item.workspace}</span>
                    <span className="shrink-0 tabular-nums text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                      {formatRelativeTime(item.updatedAt)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      {loading && <StateBlock kind="loading" title="加载中…" compact />}
      {loadError && <StateBlock kind="error" title="加载失败" compact />}
      {copyError && <div role="alert" className="py-2 text-center text-[length:var(--type-caption)] text-[var(--color-danger-text)]">{copyError}</div>}
      {hasMore && !loading && (
        <button
          type="button"
          onClick={() => { setLoading(true); load(items.length) }}
          className="mt-1 w-full py-2 text-[length:var(--type-caption)] text-[var(--color-link)] hover:bg-[var(--color-layer)]"
        >
          加载更多
        </button>
      )}
      {ctx.renderMenu}
    </div>
  )
}
