import { useCallback, useEffect, useId, useRef } from 'react'
import { createPortal } from 'react-dom'
import { Icon } from './icons'

/**
 * Modal is the single overlay primitive for the app. Before it existed,
 * ShareModal, the Settings wrapper in App.tsx and GuardButton's confirm sheet
 * were each a bare `fixed inset-0` div: no `aria-modal`, no Tab containment,
 * and no focus restoration, so keyboard users could Tab into the page behind
 * the overlay and never got their focus back after closing.
 *
 * Behaviour contract:
 *  - Renders through a portal so it escapes any transformed/overflow ancestor.
 *  - `role="dialog" aria-modal="true"`, labelled by the rendered title.
 *  - Escape closes; clicking the scrim closes (opt out with `dismissible`).
 *  - Focus moves to the first focusable child on open, is trapped inside while
 *    open, and returns to the element that had focus before opening.
 *  - Sharp corners and token colors only — see styles.css radii note.
 */

const FOCUSABLE =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

let bodyScrollLockCount = 0
let bodyOverflowBeforeLock = ''

function lockBodyScroll() {
  if (bodyScrollLockCount === 0) {
    bodyOverflowBeforeLock = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  bodyScrollLockCount += 1
}

function unlockBodyScroll() {
  bodyScrollLockCount = Math.max(0, bodyScrollLockCount - 1)
  if (bodyScrollLockCount === 0) {
    document.body.style.overflow = bodyOverflowBeforeLock
  }
}

export interface ModalProps {
  /** Accessible name. Rendered as the dialog header unless `hideTitle`. */
  title: string
  onClose: () => void
  children: React.ReactNode
  /** Escape / scrim-click close. Default true. */
  dismissible?: boolean
  /** Keep the title as the accessible name but do not render a header row. */
  hideTitle?: boolean
  /** Tailwind max-width class for the panel. Default `max-w-md`. */
  width?: string
  'data-testid'?: string
}

export function Modal({
  title,
  onClose,
  children,
  dismissible = true,
  hideTitle = false,
  width = 'max-w-md',
  'data-testid': testId,
}: ModalProps) {
  const panelRef = useRef<HTMLDivElement | null>(null)
  const restoreRef = useRef<HTMLElement | null>(null)
  const titleId = useId()

  const focusable = useCallback((): HTMLElement[] => {
    const panel = panelRef.current
    if (!panel) return []
    return Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (el) => !el.hidden && el.getAttribute('aria-hidden') !== 'true',
    )
  }, [])

  // Capture the opener, move focus in, and hand focus back on unmount. Runs
  // once per mount: the modal is conditionally rendered by its parent, so
  // mount/unmount is the open/close boundary.
  useEffect(() => {
    restoreRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const first = focusable()[0] ?? panelRef.current
    first?.focus()
    return () => {
      if (restoreRef.current?.isConnected) restoreRef.current.focus()
    }
  }, [focusable])

  useEffect(() => {
    lockBodyScroll()
    return unlockBodyScroll
  }, [])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dismissible) {
        event.stopPropagation()
        onClose()
        return
      }
      if (event.key !== 'Tab') return
      const items = focusable()
      if (items.length === 0) {
        event.preventDefault()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      // Wrap at both ends, and pull focus back in if it has escaped the panel.
      if (!event.shiftKey && (active === last || !panelRef.current?.contains(active))) {
        event.preventDefault()
        first.focus()
      } else if (event.shiftKey && (active === first || !panelRef.current?.contains(active))) {
        event.preventDefault()
        last.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown, true)
    return () => document.removeEventListener('keydown', onKeyDown, true)
  }, [dismissible, focusable, onClose])

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-[var(--palette-bg)] p-4"
      onMouseDown={(e) => {
        if (dismissible && e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        data-testid={testId}
        onMouseDown={(e) => e.stopPropagation()}
        className={`flex max-h-[85vh] w-full ${width} flex-col border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-overlay)]`}
      >
        {hideTitle ? (
          <h2 id={titleId} className="sr-only">
            {title}
          </h2>
        ) : (
          <div className="flex shrink-0 items-center justify-between border-b border-[var(--color-border)] px-4 py-3">
            <h2 id={titleId} className="text-sm font-semibold text-[var(--color-text-primary)]">
              {title}
            </h2>
            {dismissible && (
              <button
                type="button"
                onClick={onClose}
                aria-label="关闭"
                data-testid="modal-close"
                className="px-1 text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
              >
                <Icon name="close" />
              </button>
            )}
          </div>
        )}
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      </div>
    </div>,
    document.body,
  )
}
