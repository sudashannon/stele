import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createShareLink, revokeShareLink } from '../api/client'
import { ShareModal } from './ShareModal'

vi.mock('../api/client', () => ({
  createShareLink: vi.fn(),
  revokeShareLink: vi.fn(),
}))

const lanUrl = 'http://10.0.28.45:8989/share/token'

function renderModal(onClose = vi.fn()) {
  return { onClose, ...render(<ShareModal path="/workspace/knowledge/doc.md" workspace="miao" onClose={onClose} />) }
}

async function createLink() {
  fireEvent.click(screen.getByTestId('share-create-btn'))
  return await screen.findByTestId('share-link-input') as HTMLInputElement
}

beforeEach(() => {
  vi.mocked(createShareLink).mockReset()
  vi.mocked(revokeShareLink).mockReset()
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  })
  Object.defineProperty(document, 'execCommand', {
    configurable: true,
    value: vi.fn().mockReturnValue(false),
  })
})

afterEach(() => {
  Reflect.deleteProperty(document, 'execCommand')
})

describe('ShareModal', () => {
  it('inherits dialog semantics and keyboard close behavior from Modal', async () => {
    const { onClose } = renderModal()

    const dialog = screen.getByRole('dialog', { name: '分享文档' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId('modal-close')))

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('uses the backend LAN URL and reports successful creation', async () => {
    vi.mocked(createShareLink).mockResolvedValue({ url: lanUrl })
    renderModal()

    const input = await createLink()

    expect(input.value).toBe(lanUrl)
    expect(input.readOnly).toBe(true)
    expect(createShareLink).toHaveBeenCalledWith('/workspace/knowledge/doc.md', 'miao', 3600)
    expect(screen.getByRole('status').textContent).toContain('分享链接已创建')
  })

  it('clears the old link and token before regenerating', async () => {
    vi.mocked(createShareLink).mockResolvedValueOnce({ url: lanUrl })
    const { promise, resolve } = Promise.withResolvers<{ url: string }>()
    vi.mocked(createShareLink).mockReturnValueOnce(promise)
    renderModal()
    await createLink()

    fireEvent.click(screen.getByRole('button', { name: '重新生成' }))

    expect(screen.queryByTestId('share-link-input')).toBeNull()
    expect((screen.getByTestId('share-create-btn') as HTMLButtonElement).disabled).toBe(true)
    resolve({ url: 'http://10.0.28.45:8989/share/new-token' })
    expect((await screen.findByTestId('share-link-input') as HTMLInputElement).value).toContain('new-token')
  })

  it('reports an explicit error when the response URL has no share token', async () => {
    vi.mocked(createShareLink).mockResolvedValue({ url: 'http://10.0.28.45:8989/not-a-share' })
    renderModal()

    fireEvent.click(screen.getByTestId('share-create-btn'))

    expect((await screen.findByRole('alert')).textContent).toContain('无法从分享链接解析撤销令牌')
    expect(screen.queryByTestId('share-link-input')).toBeNull()
  })

  it('reports creation failure and re-enables the create action', async () => {
    vi.mocked(createShareLink).mockRejectedValue(new Error('创建服务不可用'))
    renderModal()

    fireEvent.click(screen.getByTestId('share-create-btn'))

    expect((await screen.findByRole('alert')).textContent).toContain('创建服务不可用')
    expect((screen.getByTestId('share-create-btn') as HTMLButtonElement).disabled).toBe(false)
  })

  it('reports copy success and failure', async () => {
    vi.mocked(createShareLink).mockResolvedValue({ url: lanUrl })
    renderModal()
    await createLink()

    fireEvent.click(screen.getByTestId('share-copy-btn'))
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(lanUrl))
    expect(screen.getByRole('status').textContent).toContain('链接已复制')

    vi.mocked(navigator.clipboard.writeText).mockRejectedValueOnce(new Error('剪贴板权限被拒绝'))
    fireEvent.click(screen.getByTestId('share-copy-btn'))
    expect((await screen.findByRole('alert')).textContent).toContain('无法复制到剪贴板')
  })

  it('falls back to execCommand when Clipboard API access is unavailable', async () => {
    vi.mocked(createShareLink).mockResolvedValue({ url: lanUrl })
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    vi.mocked(document.execCommand).mockReturnValue(true)
    renderModal()
    await createLink()

    fireEvent.click(screen.getByTestId('share-copy-btn'))

    await waitFor(() => expect(document.execCommand).toHaveBeenCalledWith('copy'))
    expect(screen.getByRole('status').textContent).toContain('链接已复制')
    expect(document.querySelector('textarea[aria-hidden="true"]')).toBeNull()
  })

  it('cancels revoke confirmation without calling the API', async () => {
    vi.mocked(createShareLink).mockResolvedValue({ url: lanUrl })
    renderModal()
    await createLink()

    fireEvent.click(screen.getByTestId('share-revoke-btn'))
    expect(screen.getByRole('dialog', { name: '确认撤销分享' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    expect(revokeShareLink).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: '分享文档' })).toBeTruthy()
  })

  it('revokes only after confirmation and reports success', async () => {
    vi.mocked(createShareLink).mockResolvedValue({ url: lanUrl })
    vi.mocked(revokeShareLink).mockResolvedValue()
    renderModal()
    await createLink()

    fireEvent.click(screen.getByTestId('share-revoke-btn'))
    fireEvent.click(screen.getByTestId('share-revoke-confirm-btn'))

    await waitFor(() => expect(revokeShareLink).toHaveBeenCalledWith('token'))
    expect((await screen.findByRole('status')).textContent).toContain('分享已撤销')
    expect(screen.queryByTestId('share-link-input')).toBeNull()
  })

  it('reports revoke failure and keeps the link available', async () => {
    vi.mocked(createShareLink).mockResolvedValue({ url: lanUrl })
    vi.mocked(revokeShareLink).mockRejectedValue(new Error('撤销服务不可用'))
    renderModal()
    await createLink()

    fireEvent.click(screen.getByTestId('share-revoke-btn'))
    fireEvent.click(screen.getByTestId('share-revoke-confirm-btn'))

    expect((await screen.findByRole('alert')).textContent).toContain('撤销服务不可用')
    expect((screen.getByTestId('share-link-input') as HTMLInputElement).value).toBe(lanUrl)
  })
})
