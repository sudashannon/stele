import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MarkdownEditor } from './MarkdownEditor'

afterEach(() => vi.restoreAllMocks())

function artifactResponse(content: string, etag = '"version-1"'): Response {
  return {
    ok: true,
    text: async () => content,
    headers: new Headers({ ETag: etag }),
  } as Response
}

describe('MarkdownEditor', () => {
  it('saves changed source and adopts the returned ETag', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(artifactResponse('# Original'))
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ etag: '"version-2"', path: '/x/design.md', bytes: 9 }),
      } as Response)

    render(<MarkdownEditor path="/x/design.md" workspace="docs" onClose={vi.fn()} />)
    const editor = await screen.findByRole('textbox', { name: 'Markdown 源码' })
    await waitFor(() => expect((editor as HTMLTextAreaElement).value).toBe('# Original'))

    fireEvent.change(editor, { target: { value: '# Changed' } })
    await waitFor(() => expect((screen.getByTestId('markdown-editor-save') as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByTestId('markdown-editor-save'))

    await waitFor(() => expect(screen.getByRole('status').textContent).toContain('已保存'))
    expect(fetchSpy).toHaveBeenLastCalledWith(
      expect.stringContaining('/api/artifact?'),
      expect.objectContaining({
        method: 'PUT',
        headers: expect.objectContaining({ 'If-Match': '"version-1"' }),
        body: '# Changed',
      }),
    )
    expect((screen.getByTestId('markdown-editor-save') as HTMLButtonElement).disabled).toBe(true)
  })

  it('asks before returning from dirty source editing', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(artifactResponse('# Original'))
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const onClose = vi.fn()

    render(<MarkdownEditor path="/x/design.md" onClose={onClose} />)
    const editor = await screen.findByRole('textbox', { name: 'Markdown 源码' })
    await waitFor(() => expect((editor as HTMLTextAreaElement).value).toBe('# Original'))
    fireEvent.change(editor, { target: { value: '# Changed' } })
    fireEvent.click(screen.getByTestId('markdown-editor-close'))

    expect(confirm).toHaveBeenCalledTimes(1)
    expect(onClose).not.toHaveBeenCalled()
    confirm.mockReturnValue(true)
    fireEvent.click(screen.getByTestId('markdown-editor-close'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('retains unsaved source after a stale ETag and can reload the server version', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(artifactResponse('# Original'))
      .mockResolvedValueOnce({
        ok: false,
        status: 412,
        headers: new Headers({ ETag: '"version-2"' }),
        json: async () => ({ error: 'artifact changed' }),
      } as Response)
      .mockResolvedValueOnce(artifactResponse('# Server version', '"version-2"'))

    render(<MarkdownEditor path="/x/design.md" onClose={vi.fn()} />)
    const editor = await screen.findByRole('textbox', { name: 'Markdown 源码' })
    await waitFor(() => expect((editor as HTMLTextAreaElement).value).toBe('# Original'))
    fireEvent.change(editor, { target: { value: '# My unsaved source' } })
    await waitFor(() => expect((screen.getByTestId('markdown-editor-save') as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByTestId('markdown-editor-save'))

    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('artifact changed'))
    expect((editor as HTMLTextAreaElement).value).toBe('# My unsaved source')
    expect((screen.getByTestId('markdown-editor-save') as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByTestId('markdown-editor-reload-server'))
    await waitFor(() => expect((editor as HTMLTextAreaElement).value).toBe('# Server version'))
  })
})
