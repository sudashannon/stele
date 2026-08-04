import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ScoredAction, UseCommandPaletteReturn } from '../hooks/useCommandPalette'
import { highlightMatches } from '../hooks/useCommandPalette'
import { formatShortcut } from '../hooks/useKeyboardShortcuts'
import type { ShortcutDef } from '../hooks/useKeyboardShortcuts'
import { Modal } from './Modal'
import { Icon } from './icons'
import type { IconName } from './icons'

interface Props {
  palette: UseCommandPaletteReturn
  shortcuts?: ShortcutDef[]
}
const ACTION_ICONS: Readonly<Record<string, IconName>> = {
  'new-todo': 'check',
  bookmarks: 'bookmark',
  settings: 'settings',
  refresh: 'refresh',
  'nav-changes': 'changes',
  'nav-todos': 'todos',
  'nav-graph': 'graph',
  'nav-timeline': 'timeline',
  'nav-search': 'search',
  'nav-recent': 'recent',
  'nav-lint': 'lint',
  'nav-report': 'report',
  'nav-calendar': 'calendar',
}


/** Group actions by category, preserving scored order within each group. */
function groupByCategory(items: ScoredAction[]): Map<string, ScoredAction[]> {
  const groups = new Map<string, ScoredAction[]>()
  for (const item of items) {
    const cat = item.action.category ?? 'Other'
    const list = groups.get(cat)
    if (list) {
      list.push(item)
    } else {
      groups.set(cat, [item])
    }
  }
  return groups
}

export function CommandPalette({ palette, shortcuts }: Props) {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const { query, setQuery, results, closePalette } = palette
  const orderedResults = useMemo(
    () => Array.from(groupByCategory(results).values()).flat(),
    [results],
  )

  useEffect(() => {
    if (palette.open) setSelectedIndex(0)
  }, [palette.open])

  useEffect(() => {
    setSelectedIndex((current) => Math.min(current, Math.max(orderedResults.length - 1, 0)))
  }, [orderedResults.length])

  useEffect(() => {
    document.getElementById(`palette-option-${selectedIndex}`)?.scrollIntoView?.({ block: 'nearest' })
  }, [selectedIndex])

  const selectAction = useCallback((action: ScoredAction['action']) => {
    action.run()
    closePalette()
  }, [closePalette])

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLInputElement>) => {
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setSelectedIndex((current) =>
          Math.max(Math.min(current + 1, orderedResults.length - 1), 0),
        )
        return
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        setSelectedIndex((current) => Math.max(current - 1, 0))
        return
      }
      if (event.key === 'Enter' && orderedResults.length > 0) {
        event.preventDefault()
        selectAction(orderedResults[selectedIndex].action)
      }
    },
    [orderedResults, selectAction, selectedIndex],
  )

  const isShortcutMode = query.startsWith('?')

  if (!palette.open) return null

  return (
    <Modal
      title="命令面板"
      hideTitle
      onClose={closePalette}
      width="max-w-xl"
      data-testid="command-palette"
    >
      <div className="flex flex-col bg-[var(--color-surface)]">
        <div className="flex items-center gap-3 border-b border-[var(--color-border)] px-4 py-3">
          <Icon name="search" className="shrink-0 text-[var(--color-text-secondary)]" />
          <input
            type="text"
            role="combobox"
            aria-label="搜索命令"
            aria-expanded="true"
            aria-controls="command-palette-results"
            aria-activedescendant={
              !isShortcutMode && orderedResults.length > 0
                ? `palette-option-${selectedIndex}`
                : undefined
            }
            value={query}
            onChange={(event) => {
              setQuery(event.target.value)
              setSelectedIndex(0)
            }}
            onKeyDown={onKeyDown}
            placeholder={isShortcutMode ? '快捷键速查…' : '搜索命令…  (Ctrl+K 开关)'}
            className="flex-1 border-none bg-transparent text-[length:var(--type-body)] text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
            autoComplete="off"
            spellCheck={false}
          />
          <kbd className="shrink-0 border border-[var(--color-border)] bg-[var(--color-bg)] px-1.5 py-0.5 font-mono text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            Esc
          </kbd>
        </div>

        <div
          id="command-palette-results"
          className="max-h-[400px] overflow-y-auto p-2"
          role={isShortcutMode ? 'list' : 'listbox'}
          aria-label={isShortcutMode ? '快捷键' : '命令'}
        >
          {isShortcutMode && shortcuts ? (
            <ShortcutList shortcuts={shortcuts} />
          ) : orderedResults.length === 0 ? (
            <div className="py-6 text-center text-[length:var(--type-body)] text-[var(--color-text-secondary)]">
              {query ? '无匹配命令' : '输入关键词搜索…'}
            </div>
          ) : (
            <ActionList
              results={orderedResults}
              selectedIdx={selectedIndex}
              onSelectedIndexChange={setSelectedIndex}
              onSelect={selectAction}
            />
          )}
        </div>

        <div className="flex items-center gap-4 border-t border-[var(--color-border)] px-4 py-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
          <span className="inline-flex items-center gap-1">
            <Icon name="chevron-down" />
            导航
          </span>
          <span>Enter 执行</span>
          <span>Esc 关闭</span>
          <span className="ml-auto">? 快捷键</span>
        </div>
      </div>
    </Modal>
  )
}

// ── Action list with category groups ────────────────────────────────────────

function iconForAction(id: string, category: string): IconName {
  return ACTION_ICONS[id] ?? (category === 'Navigation' ? 'chevron-right' : 'changes')
}

function ActionList({
  results,
  selectedIdx,
  onSelectedIndexChange,
  onSelect,
}: {
  results: ScoredAction[]
  selectedIdx: number
  onSelectedIndexChange: (index: number) => void
  onSelect: (action: ScoredAction['action']) => void
}) {
  const groups = groupByCategory(results)
  let globalIdx = 0
  const rows: React.ReactNode[] = []
  const entries = [...groups.entries()]

  entries.forEach(([category, items], gi) => {
    if (gi > 0 && items.length > 0) {
      rows.push(
        <div
          key={`sep-${gi}`}
          className="mx-1 my-1 border-t"
          style={{ borderColor: 'var(--palette-separator)' }}
        />,
      )
    }
    rows.push(
      <div
        key={`cat-${category}`}
        className="text-[length:var(--type-micro)] font-semibold px-2 py-1 uppercase tracking-wider"
        style={{ color: 'var(--color-text-tertiary)' }}
      >
        {category}
      </div>,
    )
    for (const item of items) {
      const idx = globalIdx++
      const isSelected = idx === selectedIdx
      rows.push(
        <div
          id={`palette-option-${idx}`}
          key={item.action.id}
          data-palette-idx={idx}
          role="option"
          aria-selected={isSelected}
          className="flex cursor-pointer items-center gap-3 px-3 py-2 text-[length:var(--type-body)]"
          style={{
            backgroundColor: isSelected ? 'var(--palette-highlight)' : 'transparent',
            color: 'var(--color-text-primary)',
          }}
          onClick={() => onSelect(item.action)}
          onMouseEnter={() => onSelectedIndexChange(idx)}
        >
          <span className="w-5 shrink-0 text-center text-[var(--color-text-secondary)]">
            <Icon name={iconForAction(item.action.id, item.action.category)} />
          </span>
          <span className="flex-1">
            <span
              dangerouslySetInnerHTML={{
                __html: highlightMatches(
                  item.action.label,
                  item.labelIndices,
                ),
              }}
            />
            {item.action.subtitle && (
              <span
                className="block text-[length:var(--type-caption)]"
                style={{ color: 'var(--color-text-secondary)' }}
                dangerouslySetInnerHTML={{
                  __html: highlightMatches(
                    item.action.subtitle,
                    item.subtitleIndices,
                  ),
                }}
              />
            )}
          </span>
          {item.action.shortcut && (
            <kbd className="shrink-0 border border-[var(--color-border)] bg-[var(--color-bg)] px-1.5 py-0.5 font-mono text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
              {item.action.shortcut}
            </kbd>
          )}
        </div>,
      )
    }
  })

  return <div>{rows}</div>
}

// ── Shortcut reference list (mode "?") ─────────────────────────────────────

function ShortcutList({ shortcuts }: { shortcuts: ShortcutDef[] }) {
  return (
    <div>
      <div
        className="text-[length:var(--type-micro)] font-semibold px-2 py-1 uppercase tracking-wider"
        style={{ color: 'var(--color-text-tertiary)' }}
      >
        快捷键
      </div>
      {shortcuts.map((s, i) => (
        <div
          key={i}
          className="flex items-center justify-between px-3 py-2 text-[length:var(--type-body)]"
          role="listitem"
          style={{ color: 'var(--color-text-primary)' }}
        >
          <span>{s.label}</span>
          <kbd className="shrink-0 border border-[var(--color-border)] bg-[var(--color-bg)] px-1.5 py-0.5 font-mono text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            {formatShortcut(s)}
          </kbd>
        </div>
      ))}
    </div>
  )
}
