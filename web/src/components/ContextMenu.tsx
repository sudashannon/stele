
import { useEffect, useRef, useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { Icon, isIconName, type IconName } from './icons'

export interface ContextMenuItem {
  id: string
  label: string
  icon?: string
  disabled?: boolean
  danger?: boolean
  run: () => void
}

interface Props {
  items: ContextMenuItem[]
  x: number
  y: number
  onClose: () => void
}

function resolveIconName(item: ContextMenuItem): IconName | null {
  if (item.icon && isIconName(item.icon)) return item.icon

  if (item.id.startsWith('open')) return 'open'
  if (item.id.startsWith('copy')) return 'copy'
  if (item.id.includes('remove') || item.id.includes('delete') || item.id.includes('trash')) return 'trash'
  if (item.id.includes('share')) return 'share'
  if (item.id.includes('refresh')) return 'refresh'
  if (item.id.includes('close')) return 'close'
  if (item.id.includes('check') || item.id.includes('confirm')) return 'check'
  return null
}

/**
 * Carbon-style right-click context menu.
 * Renders via portal at the cursor position, auto-flips to stay on screen.
 */
export function ContextMenu({ items, x, y, onClose }: Props) {
  const menuRef = useRef<HTMLDivElement>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])
  const [position, setPosition] = useState<{ x: number; y: number }>({ x, y })
  const [mounted, setMounted] = useState(false)
  const enabledIndexes = items.flatMap((item, index) => (item.disabled ? [] : [index]))
  const [activeIndex, setActiveIndex] = useState(enabledIndexes[0] ?? -1)

  useEffect(() => {
    setActiveIndex(enabledIndexes[0] ?? -1)
  }, [items.length, x, y])

  // `mounted` gates the first render (the portal measures itself before it is
  // positioned), so itemRefs are still empty when this effect first runs.
  // Without `mounted` in the deps it never re-ran and focus stayed on the
  // trigger, making the ArrowUp/ArrowDown/Enter handling below unreachable.
  useEffect(() => {
    if (activeIndex >= 0) itemRefs.current[activeIndex]?.focus()
  }, [activeIndex, mounted])

  // Auto-position: ensure menu stays within viewport
  useEffect(() => {
    setMounted(true)
    if (!menuRef.current) return

    const rect = menuRef.current.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight

    let posX = x
    let posY = y

    if (x + rect.width > vw) posX = x - rect.width
    if (y + rect.height > vh) posY = y - rect.height

    posX = Math.max(4, Math.min(posX, vw - rect.width - 4))
    posY = Math.max(4, Math.min(posY, vh - rect.height - 4))

    setPosition({ x: posX, y: posY })
  }, [x, y, items.length])

  // Close on outside click or Escape
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose()
    }
    function onClick(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setTimeout(onClose, 0)
      }
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onClick)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onClick)
    }
  }, [onClose])

  const moveActive = useCallback(
    (direction: 1 | -1) => {
      if (enabledIndexes.length === 0) return
      const current = enabledIndexes.indexOf(activeIndex)
      const nextIndex = current === -1
        ? enabledIndexes[0]
        : enabledIndexes[(current + direction + enabledIndexes.length) % enabledIndexes.length]
      setActiveIndex(nextIndex)
    },
    [activeIndex, enabledIndexes],
  )

  if (!mounted) return null

  return createPortal(
    <div
      ref={menuRef}
      role="menu"
      aria-orientation="vertical"
      className="fixed z-50 min-w-[160px] border border-[var(--color-border)] bg-[var(--color-surface)] py-1 shadow-[var(--shadow-2)]"
      style={{ left: position.x, top: position.y }}
      onKeyDown={(event) => {
        if (event.key === 'ArrowDown') {
          event.preventDefault()
          moveActive(1)
        } else if (event.key === 'ArrowUp') {
          event.preventDefault()
          moveActive(-1)
        } else if (event.key === 'Home') {
          event.preventDefault()
          setActiveIndex(enabledIndexes[0] ?? -1)
        } else if (event.key === 'End') {
          event.preventDefault()
          setActiveIndex(enabledIndexes.at(-1) ?? -1)
        } else if (event.key === 'Tab') {
          onClose()
        }
      }}
    >
      {items.map((item, index) => {
        const iconName = resolveIconName(item)
        return (
          <button
            key={item.id}
            ref={(node) => {
              itemRefs.current[index] = node
            }}
            type="button"
            role="menuitem"
            tabIndex={index === activeIndex ? 0 : -1}
            disabled={item.disabled}
            onMouseEnter={() => {
              if (!item.disabled) setActiveIndex(index)
            }}
            onClick={() => {
              item.run()
              onClose()
            }}
            className={
              'flex w-full items-center gap-2 px-3 py-2 text-left text-[var(--type-caption)] transition-colors enabled:hover:bg-[var(--color-layer)] enabled:focus-visible:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:opacity-40 ' +
              (item.danger ? 'text-[var(--color-danger)]' : 'text-[var(--color-text-primary)]')
            }
          >
            {iconName ? <Icon name={iconName} className="shrink-0" /> : <span className="w-4 shrink-0" aria-hidden="true" />}
            <span className="flex-1">{item.label}</span>
          </button>
        )
      })}
    </div>,
    document.body,
  )
}

/**
 * Hook that returns context menu state and handlers for attaching to elements.
 *
 * Usage:
 *   const ctx = useContextMenu()
 *   return <div onContextMenu={ctx.onContextMenu}>...</div>
 *          {ctx.menuProps && <ContextMenu {...ctx.menuProps} />}
 */
export function useContextMenu() {
  const [menuProps, setMenuProps] = useState<{
    items: ContextMenuItem[]
    x: number
    y: number
  } | null>(null)

  const onContextMenu = useCallback(
    (items: ContextMenuItem[]) => (event: React.MouseEvent) => {
      event.preventDefault()
      event.stopPropagation()
      setMenuProps({ items, x: event.clientX, y: event.clientY })
    },
    [],
  )

  const closeMenu = useCallback(() => setMenuProps(null), [])

  const renderMenu = menuProps ? (
    <ContextMenu
      items={menuProps.items}
      x={menuProps.x}
      y={menuProps.y}
      onClose={closeMenu}
    />
  ) : null

  return { onContextMenu, renderMenu, closeMenu }
}
