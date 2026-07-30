import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { revokeShareLink } from '../api/client'
import { ShareList } from './ShareList'

vi.mock('../api/client', () => ({ revokeShareLink: vi.fn() }))

const share = {
  token: 'share-token',
  path: '/knowledge/design.md',
  workspace: 'docs',
  expires_at: '2099-01-02T00:00:00Z',
  created_at: '2026-07-30T08:00:00Z',
  url: 'http://10.0.0.8:8989/share/share-token',
}

function mockList(entries: unknown[] = [share]) {
  vi.mocked(fetch).mockResolvedValue({ ok: true, status: 200, json: async () => entries } as Response)
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  })
  Object.defineProperty(document, 'execCommand', {
    configurable: true,
    value: vi.fn().mockReturnValue(false),
  })
  vi.mocked(revokeShareLink).mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
  Reflect.deleteProperty(document, 'execCommand')
})

describe('ShareList', () => {
  it('shows readable workspace and time labels', async () => {
    mockList()
    render(<ShareList />)

    expect(await screen.findByText('design.md')).toBeTruthy()
    expect(screen.getByText('Workspace：docs')).toBeTruthy()
    expect(screen.getByText(/创建$/)).toBeTruthy()
    expect(screen.getByText(/过期$/)).toBeTruthy()
  })

  it('reports list loading failure instead of presenting it as empty', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 503 } as Response)
    render(<ShareList />)

    expect((await screen.findByRole('alert')).textContent).toContain('加载分享列表失败 (503)')
  })

  it('reports copy success and failure', async () => {
    mockList()
    render(<ShareList />)
    const copy = await screen.findByRole('button', { name: /复制 design.md/ })

    fireEvent.click(copy)
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(share.url))
    expect(screen.getByRole('status').textContent).toContain('链接已复制')

    vi.mocked(navigator.clipboard.writeText).mockRejectedValueOnce(new Error('复制被拒绝'))
    fireEvent.click(copy)
    expect((await screen.findByRole('alert')).textContent).toContain('无法复制到剪贴板')
  })

  it('falls back to execCommand when the Clipboard API is unavailable on LAN HTTP', async () => {
    mockList()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    vi.mocked(document.execCommand).mockReturnValue(true)
    render(<ShareList />)

    fireEvent.click(await screen.findByRole('button', { name: /复制 design.md/ }))

    await waitFor(() => expect(document.execCommand).toHaveBeenCalledWith('copy'))
    expect(screen.getByRole('status').textContent).toContain('链接已复制')
    expect(document.querySelector('textarea[aria-hidden="true"]')).toBeNull()
  })

  it('cancels revoke confirmation without calling the API', async () => {
    mockList()
    render(<ShareList />)

    fireEvent.click(await screen.findByRole('button', { name: /撤销 design.md/ }))
    expect(screen.getByRole('dialog', { name: '确认撤销分享' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    expect(revokeShareLink).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('revokes after confirmation and reports success', async () => {
    mockList()
    vi.mocked(revokeShareLink).mockResolvedValue()
    render(<ShareList />)

    fireEvent.click(await screen.findByRole('button', { name: /撤销 design.md/ }))
    fireEvent.click(screen.getByTestId('share-list-revoke-confirm-btn'))

    await waitFor(() => expect(revokeShareLink).toHaveBeenCalledWith('share-token'))
    expect(screen.getByRole('status').textContent).toContain('分享已撤销')
    expect(screen.queryByText('design.md')).toBeNull()
  })

  it('cannot dismiss the confirmation while revoke is in flight', async () => {
    mockList()
    const { promise, resolve } = Promise.withResolvers<void>()
    vi.mocked(revokeShareLink).mockReturnValue(promise)
    render(<ShareList />)

    fireEvent.click(await screen.findByRole('button', { name: /撤销 design.md/ }))
    fireEvent.click(screen.getByTestId('share-list-revoke-confirm-btn'))
    await waitFor(() => expect(screen.queryByTestId('modal-close')).toBeNull())
    const dialog = screen.getByRole('dialog', { name: '确认撤销分享' })

    fireEvent.keyDown(document, { key: 'Escape' })
    fireEvent.mouseDown(dialog.parentElement!)
    expect(screen.getByRole('dialog', { name: '确认撤销分享' })).toBeTruthy()

    resolve()
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '确认撤销分享' })).toBeNull())
  })

  it('keeps revoke confirmation open after failure and allows an in-place retry', async () => {
    mockList()
    vi.mocked(revokeShareLink)
      .mockRejectedValueOnce(new Error('撤销失败'))
      .mockResolvedValueOnce()
    render(<ShareList />)

    fireEvent.click(await screen.findByRole('button', { name: /撤销 design.md/ }))
    const confirm = screen.getByTestId('share-list-revoke-confirm-btn')
    fireEvent.click(confirm)

    expect((await screen.findByRole('alert')).textContent).toContain('撤销失败')
    expect(screen.getByText('design.md')).toBeTruthy()
    expect(screen.getByRole('dialog', { name: '确认撤销分享' })).toBeTruthy()

    fireEvent.click(confirm)
    await waitFor(() => expect(revokeShareLink).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '确认撤销分享' })).toBeNull())
    expect(screen.getByRole('status').textContent).toContain('分享已撤销')
    expect(screen.queryByText('design.md')).toBeNull()
  })
})
