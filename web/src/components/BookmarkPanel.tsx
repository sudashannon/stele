
import type { Bookmark } from '../api/types'
import { Icon } from './icons'
import { StateBlock } from './StateBlock'
interface BookmarkPanelProps {
  bookmarks: Bookmark[]
  onOpen: (path: string) => void
  onRemove: (path: string) => void
  onClose: () => void
}

// Compact side popover listing starred docs. Rendered by App.tsx as an
// absolute overlay next to SideRail when bookmarkPanelOpen is true.
export function BookmarkPanel({ bookmarks, onOpen, onRemove, onClose }: BookmarkPanelProps) {
  return (
    <div
      data-testid="bookmark-panel"
      role="region"
      aria-label="收藏"
      className="flex max-h-[70vh] w-80 flex-col overflow-hidden border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-overlay)]"
    >
      <div className="flex items-center justify-between border-b border-[var(--color-border)] px-4 py-3">
        <div className="text-[length:var(--type-body)] leading-[var(--leading-body)] font-semibold text-[var(--color-text-primary)]">收藏</div>
        <button
          type="button"
          aria-label="关闭收藏"
          onClick={onClose}
          className="text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
        >
          <Icon name="close" />
        </button>
      </div>
      <div className="flex-1 overflow-y-auto">
        {bookmarks.length === 0 ? (
          <StateBlock
            kind="empty"
            title="尚无收藏"
            detail="可在文档工具栏中点击“收藏”保存当前文档。"
            compact
          />
        ) : (
          <ul className="divide-y divide-[var(--color-border)]">
            {bookmarks.map((bookmark) => (
              <li key={bookmark.path} className="flex items-center hover:bg-[var(--color-layer)]">
                {/* The row's padding lives on the button, not the <li>, so the
                 * whole row surface opens the document instead of leaving a
                 * dead strip along the edges. */}
                <button
                  type="button"
                  onClick={() => onOpen(bookmark.path)}
                  aria-label={`打开 ${bookmark.title}`}
                  className="flex min-w-0 flex-1 items-center gap-2 py-2.5 pl-4 pr-2 text-left"
                >
                  <span className="shrink-0 border border-[var(--color-accent)] bg-[var(--color-accent-subtle)] px-1.5 py-0.5 text-[length:var(--type-caption)] font-medium uppercase text-[var(--color-accent)]">
                    {bookmark.type}
                  </span>
                  <span className="truncate text-[length:var(--type-body)] leading-[var(--leading-body)] text-[var(--color-text-primary)]" title={bookmark.title}>
                    {bookmark.title}
                  </span>
                </button>
                <button
                  type="button"
                  aria-label={`移除 ${bookmark.title}`}
                  onClick={() => onRemove(bookmark.path)}
                  className="shrink-0 self-stretch px-4 text-[var(--color-text-tertiary)] hover:text-[var(--color-danger-text)]"
                >
                  <Icon name="trash" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
