import { renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { buildShortcutList, formatShortcut, useKeyboardShortcuts } from './useKeyboardShortcuts'

describe('useKeyboardShortcuts', () => {
  it('runs the first matching shortcut and prevents the browser default', () => {
    const first = vi.fn()
    const second = vi.fn()
    renderHook(() =>
      useKeyboardShortcuts([
        { key: '1', ctrlOrCmd: true, label: '第一个', run: first },
        { key: '1', ctrlOrCmd: true, label: '第二个', run: second },
      ]),
    )

    const event = new KeyboardEvent('keydown', { key: '1', ctrlKey: true, cancelable: true })
    const prevented = vi.spyOn(event, 'preventDefault')
    document.dispatchEvent(event)

    expect(first).toHaveBeenCalledTimes(1)
    expect(second).not.toHaveBeenCalled()
    expect(prevented).toHaveBeenCalledTimes(1)
  })

  it('ignores keydown events from editable targets', () => {
    const run = vi.fn()
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', ctrlOrCmd: true, label: '命令面板', run }]),
    )

    const input = document.createElement('input')
    document.body.appendChild(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true }))

    const editable = document.createElement('div')
    Object.defineProperty(editable, 'isContentEditable', { value: true })
    document.body.appendChild(editable)
    editable.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true }))

    expect(run).not.toHaveBeenCalled()
  })

  it('supports shifted shortcuts such as Ctrl++', () => {
    const run = vi.fn()
    renderHook(() =>
      useKeyboardShortcuts([
        { key: '+', ctrlOrCmd: true, shift: true, label: '放大', run },
      ]),
    )

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: '+', ctrlKey: true, shiftKey: true, bubbles: true }),
    )

    expect(run).toHaveBeenCalledTimes(1)
  })

  it('formats shortcuts and filters the displayed shortcut list', () => {
    expect(
      formatShortcut({ key: 'arrowdown', ctrlOrCmd: true, label: '下一项', run: () => {} }),
    ).toBe('Ctrl + ↓')

    expect(
      buildShortcutList([
        { key: 'k', ctrlOrCmd: true, label: '命令面板', run: () => {} },
        { key: '=', ctrlOrCmd: true, label: '', run: () => {} },
      ]),
    ).toHaveLength(1)
  })
})
