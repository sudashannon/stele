import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { Modal } from './Modal'

describe('Modal', () => {
  it('exposes dialog semantics and moves focus to the first control', async () => {
    render(
      <Modal title="确认操作" onClose={vi.fn()} data-testid="modal">
        <button type="button">第一个</button>
        <button type="button">第二个</button>
      </Modal>,
    )

    const dialog = screen.getByRole('dialog', { name: '确认操作' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: '关闭' })))
  })

  it('traps Tab at both ends', async () => {
    render(
      <Modal title="焦点测试" onClose={vi.fn()}>
        <button type="button">第一个</button>
        <button type="button">最后一个</button>
      </Modal>,
    )
    const close = screen.getByRole('button', { name: '关闭' })
    const last = screen.getByText('最后一个')
    await waitFor(() => expect(document.activeElement).toBe(close))

    last.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(document.activeElement).toBe(close)

    close.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(last)
  })

  it('closes on Escape and restores focus to the opener', async () => {
    const onClose = vi.fn()
    function Harness() {
      const [open, setOpen] = useState(false)
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>打开弹层</button>

          {open && <Modal title="可关闭" onClose={() => { onClose(); setOpen(false) }}>内容</Modal>}
        </>
      )
    }
    render(<Harness />)
    const opener = screen.getByText('打开弹层')
    opener.focus()
    fireEvent.click(opener)
    await screen.findByRole('dialog')

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(document.activeElement).toBe(opener))
  })

  it('does not restore focus when the opener has disconnected', async () => {
    const opener = document.createElement('button')
    document.body.appendChild(opener)
    opener.focus()
    const focus = vi.spyOn(opener, 'focus')
    const { unmount } = render(<Modal title="断连测试" onClose={vi.fn()}>内容</Modal>)
    await screen.findByRole('dialog')
    focus.mockClear()

    opener.remove()
    unmount()

    expect(focus).not.toHaveBeenCalled()
  })

  it('locks body scrolling until the last open modal closes', () => {
    document.body.style.overflow = 'clip'
    const { rerender, unmount } = render(
      <>
        <Modal title="第一个弹层" onClose={vi.fn()}>一</Modal>
        <Modal title="第二个弹层" onClose={vi.fn()}>二</Modal>
      </>,
    )
    expect(document.body.style.overflow).toBe('hidden')

    rerender(<Modal title="第一个弹层" onClose={vi.fn()}>一</Modal>)
    expect(document.body.style.overflow).toBe('hidden')

    unmount()
    expect(document.body.style.overflow).toBe('clip')
    document.body.style.overflow = ''
  })

  it('omits the visual header and focuses content when the title is hidden', async () => {
    render(
      <Modal title="命令面板" hideTitle onClose={vi.fn()}>
        <input aria-label="搜索命令" />
      </Modal>,
    )

    expect(screen.getByRole('dialog', { name: '命令面板' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: '关闭' })).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('textbox', { name: '搜索命令' })))
  })

  it('does not close a non-dismissible modal', () => {
    const onClose = vi.fn()
    render(<Modal title="阻塞弹层" onClose={onClose} dismissible={false}>内容</Modal>)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })
})
