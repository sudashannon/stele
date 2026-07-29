type View = 'changes' | 'todos' | 'graph' | 'timeline' | 'search' | 'recent' | 'lint' | 'report' | 'shares' | 'calendar'

interface SideRailProps {
  view: View
  onSelect: (v: View) => void
  onOpenSettings?: () => void
  onToggleBookmarks?: () => void
  bookmarkPanelOpen?: boolean
  onOpenPalette?: () => void
  zoomPercent?: string
  todoCount?: number
}
const items: { key: View; label: string; icon: string }[] = [
  { key: 'changes', label: '变更仪表盘', icon: '📋' },
  { key: 'todos', label: '待办', icon: '✅' },
  { key: 'graph', label: '知识图谱', icon: '🧭' },
  { key: 'timeline', label: '时间线', icon: '📆' },
  { key: 'search', label: '语义搜索', icon: '🔍' },
  { key: 'recent', label: '最近更新', icon: '🕐' },
  { key: 'lint', label: '文档健康', icon: '🩺' },
  { key: 'report', label: '报告', icon: '📊' },
  { key: 'shares', label: '分享', icon: '🔗' },
  { key: 'calendar', label: '日历', icon: '📅' },
]

export function SideRail({ view, onSelect, onOpenSettings, onToggleBookmarks, bookmarkPanelOpen, onOpenPalette, zoomPercent, todoCount }: SideRailProps) {
  return (
    <nav
      className="h-full w-[52px] shrink-0 bg-white/55 backdrop-blur-[22px] border-r border-[var(--color-border)] flex flex-col items-center gap-1 py-3 shadow-sm"
      aria-label="主导航"
    >
      {items.map((item) => {
        const active = view === item.key
        return (
          <button
            key={item.key}
            type="button"
            aria-label={item.label}
            onClick={() => onSelect(item.key)}
            title={item.label}
            className={
              'w-[38px] h-[38px] grid place-items-center text-[17px] relative ' +
              (active
                ? 'bg-[var(--color-accent)] text-white shadow-md'
                : 'text-[var(--color-text-secondary)] hover:bg-[var(--palette-highlight)]')
            }
          >
            <span aria-hidden="true">{item.icon}</span>
            {item.key === 'todos' && todoCount !== undefined && todoCount > 0 && (
              <span
                data-testid="side-rail-todo-badge"
                className="absolute -top-0.5 -right-0.5 min-w-[16px] h-4 flex items-center justify-center rounded-full bg-[var(--color-danger)] text-white text-[9px] font-bold leading-none px-1"
              >
                {todoCount >= 100 ? '99+' : todoCount}
              </span>
            )}
          </button>
        )
      })}

      <div className="flex-1" />

      <button
        type="button"
        aria-label="收藏夹"
        onClick={onToggleBookmarks}
        disabled={!onToggleBookmarks}
        title={onToggleBookmarks ? (bookmarkPanelOpen ? '关闭收藏夹' : '打开收藏夹') : '即将推出'}
        className={
          'w-[38px] h-[38px] grid place-items-center text-[17px] ' +
          (bookmarkPanelOpen
            ? 'bg-[var(--color-accent)] text-white shadow-md'
            : onToggleBookmarks
              ? 'text-[var(--color-text-secondary)] hover:bg-[var(--palette-highlight)]'
              : 'text-[var(--color-text-tertiary)] cursor-not-allowed')
        }
      >
        <span aria-hidden="true">⭐</span>
      </button>

      <button
        type="button"
        aria-label="命令面板"
        onClick={onOpenPalette}
        title="命令面板 (Ctrl+K)"
        className="w-[38px] h-[38px] grid place-items-center text-[17px] text-[var(--color-text-secondary)] hover:bg-[var(--palette-highlight)]"
      >
        <span aria-hidden="true">⌨️</span>
      </button>

      <button
        type="button"
        aria-label="设置"
        onClick={onOpenSettings}
        disabled={!onOpenSettings}
        title={onOpenSettings ? '设置' : '即将推出'}
        className={
          'w-[38px] h-[38px] grid place-items-center text-[17px] ' +
          (onOpenSettings ? 'text-[var(--color-text-secondary)] hover:bg-[var(--palette-highlight)]' : 'text-[var(--color-text-tertiary)] cursor-not-allowed')
        }
      >
        <span aria-hidden="true">⚙️</span>
      </button>

      {zoomPercent && (
        <div className="text-[10px] text-[var(--color-text-tertiary)] text-center pb-1 select-none tabular-nums">
          {zoomPercent}
        </div>
      )}
    </nav>
  )
}
