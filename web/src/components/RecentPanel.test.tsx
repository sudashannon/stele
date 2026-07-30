import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { RecentPanel } from './RecentPanel'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RecentPanel', () => {
  it('renders fetched items sorted newest first with type badge, workspace, and relative time', async () => {
    const now = Date.now()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [
        {
          id: '/x/a.md',
          title: 'A doc',
          type: 'spec',
          workspace: 'miao',
          updatedAt: new Date(now - 2 * 3600_000).toISOString(),
          path: '/x/a.md',
        },
        {
          id: '/x/b.md',
          title: 'B doc',
          type: 'design',
          workspace: 'miao',
          updatedAt: new Date(now - 30 * 60_000).toISOString(),
          path: '/x/b.md',
        },
      ],
    } as Response)

    render(<RecentPanel />)
    await waitFor(() => expect(screen.getByText('A doc')).toBeTruthy())
    expect(screen.getByText('B doc')).toBeTruthy()
    expect(screen.getByText('spec')).toBeTruthy()
    expect(screen.getByText('design')).toBeTruthy()
    expect(screen.getAllByText('miao').length).toBe(2)
    expect(screen.getByText('2小时前')).toBeTruthy()
    expect(screen.getByText('30分钟前')).toBeTruthy()
    // Each document type must be visually distinguishable; both badges used to
    // render the same accent fill, so the badge carried no information.
    const specBadge = screen.getByText('spec')
    const designBadge = screen.getByText('design')
    expect(specBadge.className).not.toBe(designBadge.className)
    expect(specBadge.className).not.toContain('bg-[var(--color-accent)]')
    expect(designBadge.className).not.toContain('bg-[var(--color-accent)]')
    const itemButtons = screen.getAllByRole('button')
    expect(itemButtons[0].textContent).toContain('B doc')
    expect(itemButtons[1].textContent).toContain('A doc')
  })

  it('calls onOpen with the item path when clicked', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [
        {
          id: '/x/a.md',
          title: 'A doc',
          type: 'spec',
          workspace: 'miao',
          updatedAt: new Date().toISOString(),
          path: '/x/a.md',
        },
      ],
    } as Response)
    const onOpen = vi.fn()
    render(<RecentPanel onOpen={onOpen} />)
    await waitFor(() => expect(screen.getByText('A doc')).toBeTruthy())
    fireEvent.click(screen.getByText('A doc'))
    expect(onOpen).toHaveBeenCalledWith('/x/a.md')
  })

  it('uses native button semantics for keyboard activation', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [{
        id: '/x/keyboard.md',
        title: 'Keyboard doc',
        type: 'spec',
        workspace: 'miao',
        updatedAt: new Date().toISOString(),
        path: '/x/keyboard.md',
      }],
    } as Response)
    const onOpen = vi.fn()
    render(<RecentPanel onOpen={onOpen} />)

    const item = await screen.findByRole('button', { name: /Keyboard doc/ })
    expect(item.tagName).toBe('BUTTON')
    expect(item.getAttribute('type')).toBe('button')
    fireEvent.click(item)

    expect(onOpen).toHaveBeenCalledOnce()
    expect(onOpen).toHaveBeenCalledWith('/x/keyboard.md')
  })

  it('groups today, yesterday, and older items while keeping stable order for ties', async () => {
    const today = new Date()
    today.setHours(10, 0, 0, 0)
    const yesterday = new Date(today)
    yesterday.setDate(yesterday.getDate() - 1)
    const older = new Date(today)
    older.setDate(older.getDate() - 2)
    const tiedTime = today.toISOString()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [
        { id: 'tie-a', title: 'Tie A', type: 'spec', workspace: 'alpha', updatedAt: tiedTime, path: '/tie-a' },
        { id: 'old', title: 'Older', type: 'design', workspace: 'gamma', updatedAt: older.toISOString(), path: '/old' },
        { id: 'tie-b', title: 'Tie B', type: 'spec', workspace: 'beta', updatedAt: tiedTime, path: '/tie-b' },
        { id: 'yesterday', title: 'Yesterday item', type: 'design', workspace: 'delta', updatedAt: yesterday.toISOString(), path: '/yesterday' },
      ],
    } as Response)

    render(<RecentPanel />)
    await screen.findByText('Tie A')

    expect(screen.getByRole('heading', { name: '今天' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: '昨天' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: '更早' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: '今天' }).id).toBe('recent-today')
    expect(screen.getByRole('heading', { name: '昨天' }).id).toBe('recent-yesterday')
    expect(screen.getByRole('heading', { name: '更早' }).id).toBe('recent-older')
    const labels = screen.getAllByRole('button').map((button) => button.textContent)
    expect(labels[0]).toContain('Tie A')
    expect(labels[1]).toContain('Tie B')
    expect(labels[2]).toContain('Yesterday item')
    expect(labels[3]).toContain('Older')
    expect(screen.getByText('alpha')).toBeTruthy()
    expect(screen.getByText('beta')).toBeTruthy()
  })

  it('shows an empty-state message when there are no recent items', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [],
    } as Response)
    render(<RecentPanel />)
    await waitFor(() => expect(screen.getByText('暂无最近变更')).toBeTruthy())
  })

  it('shows a load error message when the fetch fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    } as Response)
    render(<RecentPanel />)
    await waitFor(() => expect(screen.getByText('加载失败')).toBeTruthy())
  })

  it('falls back to execCommand when the Clipboard API is unavailable', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [{
        id: '/x/copy.md',
        title: 'Copy doc',
        type: 'spec',
        workspace: 'miao',
        updatedAt: new Date().toISOString(),
        path: '/x/copy.md',
      }],
    } as Response)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    const execCommand = vi.fn().mockReturnValue(true)
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
    render(<RecentPanel />)

    const item = await screen.findByRole('button', { name: /Copy doc/ })
    fireEvent.contextMenu(item)
    fireEvent.click(await screen.findByRole('menuitem', { name: '复制路径' }))

    await waitFor(() => expect(execCommand).toHaveBeenCalledWith('copy'))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('reports an accessible error when both clipboard strategies fail', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => [{
        id: '/x/copy-fail.md',
        title: 'Copy failure doc',
        type: 'spec',
        workspace: 'miao',
        updatedAt: new Date().toISOString(),
        path: '/x/copy-fail.md',
      }],
    } as Response)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) },
    })
    Object.defineProperty(document, 'execCommand', { configurable: true, value: vi.fn().mockReturnValue(false) })
    render(<RecentPanel />)

    const item = await screen.findByRole('button', { name: /Copy failure doc/ })
    fireEvent.contextMenu(item)
    fireEvent.click(await screen.findByRole('menuitem', { name: '复制标题' }))

    expect((await screen.findByRole('alert')).textContent).toContain('复制失败，请手动复制')
  })
})
