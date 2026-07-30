import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { ContextMenu } from './ContextMenu'

describe('ContextMenu', () => {
  it('renders icon buttons and runs the active item after keyboard navigation', async () => {
    const open = vi.fn()
    const copy = vi.fn()
    const close = vi.fn()

    render(
      <ContextMenu
        items={[
          { id: 'open', label: '打开', run: open },
          { id: 'copy-path', label: '复制路径', run: copy },
        ]}
        x={24}
        y={32}
        onClose={close}
      />,
    )

    const menu = await screen.findByRole('menu')
    const items = await screen.findAllByRole('menuitem')
    expect(items[0].querySelector('svg')).toBeTruthy()
    expect(items[1].querySelector('svg')).toBeTruthy()

    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    await waitFor(() => expect(document.activeElement).toBe(items[1]))

    fireEvent.click(items[1])
    expect(copy).toHaveBeenCalledTimes(1)
    expect(close).toHaveBeenCalledTimes(1)
  })
  it('accepts every icon exposed by the shared registry', async () => {
    render(
      <ContextMenu
        items={[{ id: 'custom-calendar', label: '日历', icon: 'calendar', run: vi.fn() }]}
        x={24}
        y={32}
        onClose={vi.fn()}
      />,
    )

    const item = await screen.findByRole('menuitem', { name: '日历' })
    expect(item.querySelector('svg')).toBeTruthy()
  })
  it('moves focus into the menu on open so the arrow-key handlers are reachable', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()

    render(
      <ContextMenu
        items={[
          { id: 'open', label: '打开', run: vi.fn() },
          { id: 'copy-path', label: '复制路径', run: vi.fn() },
        ]}
        x={24}
        y={32}
        onClose={vi.fn()}
      />,
    )

    const items = await screen.findAllByRole('menuitem')
    await waitFor(() => expect(document.activeElement).toBe(items[0]))
    expect(document.activeElement).not.toBe(trigger)
    trigger.remove()
  })


  it('closes on Escape from the keyboard path', async () => {
    const close = vi.fn()
    render(
      <ContextMenu
        items={[{ id: 'open', label: '打开', run: vi.fn() }]}
        x={12}
        y={18}
        onClose={close}
      />,
    )

    const item = await screen.findByRole('menuitem', { name: '打开' })
    fireEvent.keyDown(item, { key: 'Escape' })
    expect(close).toHaveBeenCalledTimes(1)
  })
})
