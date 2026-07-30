import { useEffect, useRef } from 'react'

export interface ShortcutDef {
  /** Key to match (e.g. 'k', '1', 'Escape') */
  key: string
  /** Whether Ctrl (or Cmd on macOS) is required */
  ctrlOrCmd: boolean
  /** Whether Shift is required */
  shift?: boolean
  /** Whether Alt is required */
  alt?: boolean
  /** Human-readable label for display in palette */
  label: string
  /** Handler to run when shortcut fires */
  run: () => void
  /** Whether to prevent browser default */
  preventDefault?: boolean
}

function matchesShortcut(event: KeyboardEvent, shortcut: ShortcutDef): boolean {
  const ctrlOrCmd = event.ctrlKey || event.metaKey
  if (shortcut.ctrlOrCmd !== ctrlOrCmd) return false
  if ((shortcut.shift ?? false) !== event.shiftKey) return false
  if ((shortcut.alt ?? false) !== event.altKey) return false
  return event.key.toLowerCase() === shortcut.key.toLowerCase()
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName.toLowerCase()
  return tag === 'input' || tag === 'textarea' || tag === 'select' || target.isContentEditable
}

/**
 * Format shortcut for display.
 * Detects macOS at runtime to show ⌘ instead of Ctrl.
 */
export function formatShortcut(shortcut: ShortcutDef): string {
  const isMac =
    typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
  const parts: string[] = []
  if (shortcut.ctrlOrCmd) parts.push(isMac ? '⌘' : 'Ctrl')
  if (shortcut.alt) parts.push(isMac ? '⌥' : 'Alt')
  if (shortcut.shift) parts.push('Shift')
  const keyMap: Record<string, string> = {
    arrowup: '↑',
    arrowdown: '↓',
    arrowleft: '←',
    arrowright: '→',
    escape: 'Esc',
    ' ': 'Space',
  }
  parts.push(keyMap[shortcut.key.toLowerCase()] ?? shortcut.key.toUpperCase())
  return parts.join(' + ')
}

/**
 * Global keyboard shortcuts registry.
 * Call once in App.tsx; registers handle keydown at the document level.
 * Shortcuts are evaluated in registration order; first match wins.
 * Input/textarea/select/editor targets are ignored to avoid stealing
 * typing focus.
 */
export function useKeyboardShortcuts(shortcuts: ShortcutDef[]) {
  const shortcutsRef = useRef(shortcuts)
  shortcutsRef.current = shortcuts

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (isEditableTarget(event.target)) return

      for (const shortcut of shortcutsRef.current) {
        if (!matchesShortcut(event, shortcut)) continue
        if (shortcut.preventDefault !== false) event.preventDefault()
        shortcut.run()
        break
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])
}

/**
 * Build the all-shortcuts reference list shown in the ? mode of the palette.
 */
export function buildShortcutList(shortcuts: ShortcutDef[]): ShortcutDef[] {
  return shortcuts.filter((shortcut) => !!shortcut.label)
}
