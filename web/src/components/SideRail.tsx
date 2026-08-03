import { Icon, type IconName } from './icons'
import { formatShortcut, type ShortcutDef } from '../hooks/useKeyboardShortcuts'

export type SideRailView =
  | 'changes'
  | 'todos'
  | 'graph'
  | 'timeline'
  | 'search'
  | 'recent'
  | 'lint'
  | 'report'
  | 'sessions'
  | 'shares'
  | 'calendar'

interface SideRailItem {
  key: SideRailView
  label: string
  icon: IconName
  shortcutKey?: number
}

interface SideRailAction {
  label: string
  icon: IconName
  onClick?: () => void
  disabled?: boolean
  pressed?: boolean
  title: string
}

export interface SideRailProps {
  view: SideRailView
  onSelect: (v: SideRailView) => void
  onOpenSettings?: () => void
  onToggleBookmarks?: () => void
  bookmarkPanelOpen?: boolean
  onOpenPalette?: () => void
  zoomPercent?: string
  todoCount?: number
}

export const SIDE_RAIL_ITEMS: SideRailItem[] = [
  { key: 'changes', label: '变更仪表盘', icon: 'changes', shortcutKey: 1 },
  { key: 'todos', label: '待办', icon: 'todos', shortcutKey: 2 },
  { key: 'graph', label: '知识图谱', icon: 'graph', shortcutKey: 3 },
  { key: 'timeline', label: '时间线', icon: 'timeline', shortcutKey: 4 },
  { key: 'search', label: '语义搜索', icon: 'search', shortcutKey: 5 },
  { key: 'recent', label: '最近更新', icon: 'recent', shortcutKey: 6 },
  { key: 'lint', label: '文档健康', icon: 'lint', shortcutKey: 7 },
  { key: 'report', label: '报告', icon: 'report', shortcutKey: 8 },
  { key: 'sessions', label: 'Agent 会话', icon: 'sessions', shortcutKey: 9 },
  { key: 'shares', label: '分享', icon: 'share' },
  { key: 'calendar', label: '日历', icon: 'calendar' },
]

function shortcutTitle(key: number): string {
  const shortcut: ShortcutDef = {
    key: String(key),
    ctrlOrCmd: true,
    label: '',
    run: () => {},
  }
  return formatShortcut(shortcut)
}

// Base classes deliberately carry NO background / border-color / text-color:
// Tailwind resolves competing utilities by stylesheet order, not by the order
// they appear in the class string, so a shared `bg-[var(--color-surface)]` in
// the base beat the active branch's `bg-[var(--color-accent)]` and left the
// selected rail button white — with white `--color-text-on-color` glyphs on
// top, the active icon vanished. Each state now supplies its own complete set.
const RAIL_BUTTON_BASE =
  'group relative h-10 w-10 border transition-colors focus-visible:z-10 focus-visible:border-[var(--color-accent)] focus-visible:outline-none'
const RAIL_BUTTON_ACTIVE =
  'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)] shadow-[var(--shadow-1)]'
const RAIL_BUTTON_IDLE =
  'border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-secondary)] hover:border-[var(--color-border-hover)] hover:bg-[var(--color-layer)]'
const RAIL_BUTTON_DISABLED =
  'cursor-not-allowed border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-text-tertiary)]'

export function navButtonClass(active: boolean): string {
  return [RAIL_BUTTON_BASE, active ? RAIL_BUTTON_ACTIVE : RAIL_BUTTON_IDLE].join(' ')
}

export function actionButtonClass(active: boolean, disabled: boolean): string {
  const state = disabled ? RAIL_BUTTON_DISABLED : active ? RAIL_BUTTON_ACTIVE : RAIL_BUTTON_IDLE
  return [RAIL_BUTTON_BASE, state].join(' ')
}

// Hover/focus-only label for the icon-only rail. It is NOT pinned open for the
// active view: the rail is 4.25rem wide, so the bubble necessarily spills into
// the content column, where a permanently visible label overlapped the view
// beneath it. `z-30` keeps it above in-view overlays (graph panels and
// tooltips at z-10/z-20) while staying under the bookmark popover (z-40) and
// modals (z-50), which previously painted over it and cut the label mid-word.
function RailHint({ label }: { label: string }) {
  return (
    <span
      className={
        // `text-[length:…]` is required: `text-[var(--type-caption)]` compiles
        // to a *color* utility (Tailwind cannot tell a size token from a color
        // one), which overrode the color below and left the hint inheriting the
        // button's white text — an empty white tooltip next to the active item.
        'pointer-events-none absolute left-full top-1/2 z-30 ml-2 -translate-y-1/2 whitespace-nowrap border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] font-medium leading-none text-[var(--color-text-primary)] opacity-0 shadow-[var(--shadow-1)] transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100'
      }
    >
      {label}
    </span>
  )
}

function RailAction({ label, icon, onClick, disabled = false, pressed, title }: SideRailAction) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-pressed={pressed}
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={actionButtonClass(Boolean(pressed), disabled)}
    >
      <span className="grid h-full w-full place-items-center">
        <Icon name={icon} size={16} />
      </span>
      <RailHint label={label} />
    </button>
  )
}

export function SideRail({
  view,
  onSelect,
  onOpenSettings,
  onToggleBookmarks,
  bookmarkPanelOpen,
  onOpenPalette,
  zoomPercent,
  todoCount,
}: SideRailProps) {
  return (
    <nav
      className="flex h-full w-[4.25rem] shrink-0 flex-col items-center gap-2 border-r border-[var(--color-border)] bg-[var(--color-surface)] px-[var(--spacing-03)] py-[var(--spacing-04)]"
      aria-label="主导航"
    >
      {SIDE_RAIL_ITEMS.map((item) => {
        const active = view === item.key
        const title = item.shortcutKey
          ? `${item.label} (${shortcutTitle(item.shortcutKey)})`
          : item.label

        return (
          <button
            key={item.key}
            type="button"
            aria-label={item.label}
            aria-current={active ? 'page' : undefined}
            onClick={() => onSelect(item.key)}
            title={title}
            className={navButtonClass(active)}
          >
            <span className="grid h-full w-full place-items-center">
              <Icon name={item.icon} size={16} />
            </span>
            <RailHint label={item.label} />
            {item.key === 'todos' && todoCount !== undefined && todoCount > 0 && (
              <span
                data-testid="side-rail-todo-badge"
                className="absolute -right-1 -top-1 min-w-[1.25rem] border border-[var(--color-danger)] bg-[var(--color-danger)] px-[0.1875rem] py-[0.0625rem] text-center text-[length:var(--type-caption)] font-semibold leading-none text-[var(--color-text-on-color)]"
              >
                {todoCount >= 100 ? '99+' : todoCount}
              </span>
            )}
          </button>
        )
      })}

      <div className="flex-1" />

      <RailAction
        label="收藏夹"
        icon="bookmark"
        onClick={onToggleBookmarks}
        disabled={!onToggleBookmarks}
        pressed={Boolean(bookmarkPanelOpen)}
        title={
          onToggleBookmarks
            ? `${bookmarkPanelOpen ? '关闭' : '打开'}收藏夹 (${formatShortcut({ key: 'b', ctrlOrCmd: true, label: '', run: () => {} })})`
            : '即将推出'
        }
      />

      <RailAction
        label="命令面板"
        icon="command"
        onClick={onOpenPalette}
        disabled={!onOpenPalette}
        title={onOpenPalette
          ? `命令面板 (${formatShortcut({ key: 'k', ctrlOrCmd: true, label: '', run: () => {} })})`
          : '命令面板不可用'}
      />

      <RailAction
        label="设置"
        icon="settings"
        onClick={onOpenSettings}
        disabled={!onOpenSettings}
        title={onOpenSettings ? '设置' : '即将推出'}
      />

      {zoomPercent && (
        <div className="pb-[var(--spacing-02)] text-center text-[length:var(--type-caption)] text-[var(--color-text-secondary)] tabular-nums select-none">
          {zoomPercent}
        </div>
      )}
    </nav>
  )
}
