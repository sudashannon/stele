import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useCommandPalette } from '../hooks/useCommandPalette'
import type { CommandAction } from '../hooks/useCommandPalette'
import type { ShortcutDef } from '../hooks/useKeyboardShortcuts'
import { CommandPalette } from './CommandPalette'

function PaletteHarness({ actions, shortcuts }: { actions: CommandAction[]; shortcuts?: ShortcutDef[] }) {
  const palette = useCommandPalette(actions)
  return (
    <>
      <button type="button" onClick={palette.openPalette}>打开命令</button>
      <CommandPalette palette={palette} shortcuts={shortcuts} />
    </>
  )
}

describe('CommandPalette', () => {
  it('shares selection between hover and keyboard Enter', async () => {
    const runAlpha = vi.fn()
    const runBeta = vi.fn()
    render(<PaletteHarness actions={[
      { id: 'alpha', label: 'Alpha', category: 'Commands', run: runAlpha },
      { id: 'beta', label: 'Beta', category: 'Commands', run: runBeta },
    ]} />)

    fireEvent.click(screen.getByRole('button', { name: '打开命令' }))
    const input = await screen.findByRole('combobox', { name: '搜索命令' })
    const beta = screen.getByRole('option', { name: /Beta/ })
    fireEvent.mouseEnter(beta)

    expect(beta.getAttribute('aria-selected')).toBe('true')
    expect(input.getAttribute('aria-activedescendant')).toBe(beta.id)
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(runBeta).toHaveBeenCalledTimes(1)
    expect(runAlpha).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog', { name: '命令面板' })).toBeNull()
  })

  it('exposes modal and listbox ARIA and restores opener focus on Escape', async () => {
    render(<PaletteHarness actions={[
      { id: 'alpha', label: 'Alpha', category: 'Commands', run: () => {} },
    ]} />)
    const opener = screen.getByRole('button', { name: '打开命令' })
    opener.focus()
    fireEvent.click(opener)

    const dialog = await screen.findByRole('dialog', { name: '命令面板' })
    const input = screen.getByRole('combobox', { name: '搜索命令' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(screen.getByRole('listbox', { name: '命令' })).toBeTruthy()
    await waitFor(() => expect(document.activeElement).toBe(input))

    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(document.activeElement).toBe(opener)
  })

  it('uses Arrow navigation as the same selected index exposed through ARIA', async () => {
    render(<PaletteHarness actions={[
      { id: 'alpha', label: 'Alpha', category: 'Commands', run: () => {} },
      { id: 'beta', label: 'Beta', category: 'Commands', run: () => {} },
    ]} />)
    fireEvent.click(screen.getByRole('button', { name: '打开命令' }))
    const input = await screen.findByRole('combobox', { name: '搜索命令' })

    fireEvent.keyDown(input, { key: 'ArrowDown' })

    expect(screen.getByRole('option', { name: /Beta/ }).getAttribute('aria-selected')).toBe('true')
  })

  it('scrolls the selected option after keyboard selection changes', async () => {
    render(<PaletteHarness actions={[
      { id: 'alpha', label: 'Alpha', category: 'Commands', run: () => {} },
      { id: 'beta', label: 'Beta', category: 'Commands', run: () => {} },
    ]} />)
    fireEvent.click(screen.getByRole('button', { name: '打开命令' }))
    const input = await screen.findByRole('combobox', { name: '搜索命令' })
    const beta = screen.getByRole('option', { name: /Beta/ })
    const scrollIntoView = vi.fn()
    beta.scrollIntoView = scrollIntoView

    fireEvent.keyDown(input, { key: 'ArrowDown' })

    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' })
  })

  it('uses list semantics for shortcut reference mode instead of an empty listbox contract', async () => {
    render(<PaletteHarness
      actions={[{ id: 'alpha', label: 'Alpha', category: 'Commands', run: () => {} }]}
      shortcuts={[{ key: 'k', ctrlOrCmd: true, label: '打开命令', run: () => {} }]}
    />)
    fireEvent.click(screen.getByRole('button', { name: '打开命令' }))
    const input = await screen.findByRole('combobox', { name: '搜索命令' })
    fireEvent.change(input, { target: { value: '?' } })

    expect(screen.getByRole('list', { name: '快捷键' })).toBeTruthy()
    expect(screen.getByRole('listitem').textContent).toContain('打开命令')
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(input.getAttribute('aria-activedescendant')).toBeNull()
  })
})
