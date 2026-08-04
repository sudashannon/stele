import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { LintPanel } from './LintPanel'

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('LintPanel', () => {
  it('lists lint issues grouped by rule', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [
        { rule: 'orphan', componentId: '/x/orphan.md', detail: 'Orphan' },
        { rule: 'dead-link', componentId: '/x/src.md', detail: 'broken' },
      ],
    } as Response)
    render(<LintPanel />)
    await waitFor(() => expect(screen.getByText(/orphan/)).toBeTruthy())
    await waitFor(() => expect(screen.getByText(/dead-link/)).toBeTruthy())
  })

  it('renders long dead-link paths with the filename tail preserved via RTL truncation', async () => {
    const longPath = '/home/user/workspace/comet-panel/.comet/changes/some-very-long-change-name/design.md'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [
        { rule: 'dead-link', componentId: '/x/src.md', detail: `link to ${longPath} has no matching component` },
      ],
    } as Response)
    render(<LintPanel />)
    await waitFor(() => expect(screen.getByText(/dead-link/)).toBeTruthy())
    // Expand the dead-link rule row to reveal detail text
    fireEvent.click(screen.getByText(/dead-link/))
    await waitFor(() => expect(screen.getByText('link to')).toBeTruthy())
    expect(screen.getByText(longPath)).toBeTruthy()
    expect(screen.getByText('has no matching component')).toBeTruthy()
    const detailRow = screen.getByText(longPath)
    expect(detailRow.getAttribute('dir')).toBe('rtl')
    expect(detailRow.closest('[title]')?.getAttribute('title')).toBe(
      `link to ${longPath} has no matching component`,
    )
  })

  it('decodes percent-encoded CJK filenames in dead-link details', async () => {
    const encodedPath = '/x/LZ100_%E4%BA%A7%E7%BA%BF%E7%94%9F%E4%BA%A7.md'
    const decodedPath = decodeURIComponent(encodedPath)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [
        { rule: 'dead-link', componentId: '/x/src.md', detail: `link to ${encodedPath} has no matching component` },
      ],
    } as Response)
    render(<LintPanel />)
    await waitFor(() => expect(screen.getByText(/dead-link/)).toBeTruthy())
    // Expand the dead-link rule row to reveal detail text
    fireEvent.click(screen.getByText(/dead-link/))
    await waitFor(() => expect(screen.getByText('link to')).toBeTruthy())
    expect(screen.getByText(decodedPath)).toBeTruthy()
    expect(screen.queryByText(encodedPath)).toBeFalsy()
    const detailRow = screen.getByText(decodedPath)
    expect(detailRow.closest('[title]')?.getAttribute('title')).toBe(
      `link to ${decodedPath} has no matching component`,
    )
  })

  it('falls back to the raw string when the detail has a malformed percent-encoding', async () => {
    const malformedPath = '/x/broken-%E4%A7.md'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [
        { rule: 'dead-link', componentId: '/x/src.md', detail: `link to ${malformedPath} has no matching component` },
      ],
    } as Response)
    render(<LintPanel />)
    await waitFor(() => expect(screen.getByText(/dead-link/)).toBeTruthy())
    // Expand the dead-link rule row to reveal detail text
    fireEvent.click(screen.getByText(/dead-link/))
    await waitFor(() => expect(screen.getByText('link to')).toBeTruthy())
    expect(screen.getByText(malformedPath)).toBeTruthy()
  })

  it('shows an indexing message while polling, then a genuine clean-state message once polling gives up', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, json: async () => [] } as Response)
    render(<LintPanel />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByText('索引构建中…')).toBeTruthy()

    // 20 attempts total, 3s apart -> advance past the full poll window.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20 * 3000)
    })
    expect(screen.getByText('未发现问题')).toBeTruthy()
  })

  it('auto-populates once a later poll returns issues, without manual view-switching', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({ ok: true, json: async () => [] } as Response)
      .mockResolvedValueOnce({ ok: true, json: async () => [] } as Response)
      .mockResolvedValue({
        ok: true,
        json: async () => [{ rule: 'orphan', componentId: '/x/orphan.md', detail: 'Orphan' }],
      } as Response)
    render(<LintPanel />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByText('索引构建中…')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(screen.getByText('索引构建中…')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    await waitFor(() => expect(screen.getByText(/orphan/)).toBeTruthy())
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('stops polling and does not update state after unmount', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, json: async () => [] } as Response)
    const { unmount } = render(<LintPanel />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    const callsBeforeUnmount = fetchMock.mock.calls.length

    unmount()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60000)
    })
    expect(fetchMock.mock.calls.length).toBe(callsBeforeUnmount)
  })
})
