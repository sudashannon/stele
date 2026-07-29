import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createShareLink, revokeShareLink } from '../api/client'
import { ShareModal } from './ShareModal'

vi.mock('../api/client', () => ({
  createShareLink: vi.fn(),
  revokeShareLink: vi.fn(),
}))

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ShareModal', () => {
  it('uses the backend LAN URL without replacing it with the browser origin', async () => {
    const lanUrl = 'http://10.0.28.45:8989/share/token'
    vi.mocked(createShareLink).mockResolvedValue({ url: lanUrl })
    vi.mocked(revokeShareLink).mockResolvedValue()

    render(<ShareModal path="/workspace/knowledge/doc.md" workspace="miao" onClose={vi.fn()} />)
    fireEvent.click(screen.getByTestId('share-create-btn'))

    const input = await waitFor(() => screen.getByTestId('share-link-input') as HTMLInputElement)
    expect(input.value).toBe(lanUrl)
    expect(input.readOnly).toBe(true)
    expect(createShareLink).toHaveBeenCalledWith('/workspace/knowledge/doc.md', 'miao', 3600)
  })
})
