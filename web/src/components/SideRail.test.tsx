import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SIDE_RAIL_ITEMS, SideRail } from './SideRail'

describe('SideRail', () => {
  it('renders navigation buttons for all views in the declared order', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)

    const nav = screen.getByRole('navigation', { name: '主导航' })
    const navButtons = within(nav)
      .getAllByRole('button')
      .slice(0, SIDE_RAIL_ITEMS.length)
      .map((button) => button.getAttribute('aria-label'))

    expect(navButtons).toEqual(SIDE_RAIL_ITEMS.map((item) => item.label))
  })

  it('marks the active view with aria-current and keeps its label available for hover', () => {
    render(<SideRail view="graph" onSelect={() => {}} />)

    const graphButton = screen.getByRole('button', { name: '知识图谱' })
    expect(graphButton.getAttribute('aria-current')).toBe('page')
    expect(screen.getByText('知识图谱')).toBeTruthy()
  })

  it('titles every numbered view with its own Ctrl shortcut', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)

    // Derived from the item list so a reorder cannot leave a title behind.
    for (const item of SIDE_RAIL_ITEMS) {
      const title = screen.getByRole('button', { name: item.label }).getAttribute('title') ?? ''
      if (item.shortcutKey === undefined) {
        expect(title).toBe(item.label)
        continue
      }
      expect(title).toContain('Ctrl')
      expect(title).toContain(String(item.shortcutKey))
    }
  })

  it('calls onSelect with the view key when a navigation button is clicked', () => {
    const onSelect = vi.fn()
    render(<SideRail view="changes" onSelect={onSelect} />)

    fireEvent.click(screen.getByRole('button', { name: '文档健康' }))

    expect(onSelect).toHaveBeenCalledWith('lint')
  })

  it('renders a disabled settings button when no handler is provided', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)

    expect((screen.getByRole('button', { name: '设置' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('renders enabled settings when a handler is provided', () => {
    render(<SideRail view="changes" onSelect={() => {}} onOpenSettings={() => {}} />)

    expect((screen.getByRole('button', { name: '设置' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('renders a disabled bookmark button when onToggleBookmarks is omitted', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)

    expect((screen.getByRole('button', { name: '收藏夹' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('calls onToggleBookmarks when the bookmark button is clicked', () => {
    const onToggleBookmarks = vi.fn()
    render(
      <SideRail
        view="changes"
        onSelect={() => {}}
        onToggleBookmarks={onToggleBookmarks}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '收藏夹' }))

    expect(onToggleBookmarks).toHaveBeenCalledTimes(1)
  })

  it('marks the bookmark toggle with aria-pressed instead of aria-current', () => {
    const { rerender } = render(
      <SideRail
        view="changes"
        onSelect={() => {}}
        onToggleBookmarks={() => {}}
        bookmarkPanelOpen={false}
      />,
    )

    const bookmarkButton = screen.getByRole('button', { name: '收藏夹' })
    expect(bookmarkButton.getAttribute('aria-pressed')).toBe('false')
    expect(bookmarkButton.getAttribute('aria-current')).toBeNull()

    rerender(
      <SideRail
        view="changes"
        onSelect={() => {}}
        onToggleBookmarks={() => {}}
        bookmarkPanelOpen={true}
      />,
    )
    expect(bookmarkButton.getAttribute('aria-pressed')).toBe('true')
    expect(bookmarkButton.getAttribute('aria-current')).toBeNull()
    expect(screen.getByText('收藏夹')).toBeTruthy()
  })

  it('disables the unavailable command palette action with an accurate title', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)

    const commandPalette = screen.getByRole('button', { name: '命令面板' }) as HTMLButtonElement
    expect(commandPalette.disabled).toBe(true)
    expect(commandPalette.title).toBe('命令面板不可用')
  })

  it('renders the command palette with a distinct glyph and opens it', () => {
    const onOpenPalette = vi.fn()
    render(
      <SideRail view="search" onSelect={() => {}} onOpenPalette={onOpenPalette} />,
    )

    const semanticSearch = screen.getByRole('button', { name: '语义搜索' })
    const commandPalette = screen.getByRole('button', { name: '命令面板' })
    expect(semanticSearch.querySelector('svg')?.classList.contains('lucide-search')).toBe(true)
    expect(commandPalette.querySelector('svg')?.classList.contains('lucide-command')).toBe(true)

    fireEvent.click(commandPalette)
    expect(onOpenPalette).toHaveBeenCalledTimes(1)
  })

  it('renders zoom indicator when zoomPercent is provided', () => {
    render(<SideRail view="changes" onSelect={() => {}} zoomPercent="90%" />)

    expect(screen.getByText('90%')).toBeTruthy()
  })

  it('does not render zoom indicator when zoomPercent is omitted', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)

    expect(screen.queryByText('100%')).toBeNull()
  })

  it('renders todo badge when todoCount is provided and greater than zero', () => {
    render(<SideRail view="changes" onSelect={() => {}} todoCount={5} />)

    expect(screen.getByTestId('side-rail-todo-badge').textContent).toBe('5')
  })

  it('caps todo badge at 99+ when count is 100 or more', () => {
    render(<SideRail view="changes" onSelect={() => {}} todoCount={150} />)

    expect(screen.getByTestId('side-rail-todo-badge').textContent).toBe('99+')
  })

  it('does not render todo badge when todoCount is zero or undefined', () => {
    const { rerender } = render(<SideRail view="changes" onSelect={() => {}} todoCount={0} />)
    expect(screen.queryByTestId('side-rail-todo-badge')).toBeNull()

    rerender(<SideRail view="changes" onSelect={() => {}} />)
    expect(screen.queryByTestId('side-rail-todo-badge')).toBeNull()
  })

  it('paints the active view with the accent fill and never keeps the idle surface fill', () => {
    render(<SideRail view="graph" onSelect={() => {}} />)

    const active = screen.getByRole('button', { name: '知识图谱' })
    expect(active.className).toContain('bg-[var(--color-accent)]')
    expect(active.className).toContain('text-[var(--color-text-on-color)]')
    expect(active.className).not.toContain('bg-[var(--color-surface)]')

    const idle = screen.getByRole('button', { name: '时间线' })
    expect(idle.className).toContain('bg-[var(--color-surface)]')
    expect(idle.className).not.toContain('bg-[var(--color-accent)]')
  })

  it('paints a pressed rail action the same way as an active view', () => {
    render(
      <SideRail view="changes" onSelect={() => {}} onToggleBookmarks={() => {}} bookmarkPanelOpen />,
    )

    const bookmarks = screen.getByRole('button', { name: '收藏夹' })
    expect(bookmarks.getAttribute('aria-pressed')).toBe('true')
    expect(bookmarks.className).toContain('bg-[var(--color-accent)]')
    expect(bookmarks.className).not.toContain('bg-[var(--color-surface)]')
  })

  it('reveals the rail label only on hover/focus and layers it above in-view overlays', () => {
    render(<SideRail view="graph" onSelect={() => {}} />)

    const hint = screen.getByText('知识图谱')
    // A bare text-[var(--type-caption)] compiles to a color utility and wins
    // over the color below it, leaving white-on-white text.
    expect(hint.className).toContain('text-[length:var(--type-caption)]')
    expect(hint.className).not.toContain('text-[var(--type-caption)]')
    expect(hint.className).toContain('text-[var(--color-text-primary)]')
    // Never pinned open for the active view: the bubble spills past the 4.25rem
    // rail and used to sit on top of the view beneath it.
    expect(hint.className).toContain('opacity-0')
    expect(hint.className).toContain('group-hover:opacity-100')
    expect(hint.className).toContain('group-focus-visible:opacity-100')
    // Above graph/timeline overlays (z-10/z-20), below the bookmark popover.
    expect(hint.className).toContain('z-30')
  })

  it('does not pin the label open for a pressed rail action either', () => {
    render(
      <SideRail view="changes" onSelect={() => {}} onToggleBookmarks={() => {}} bookmarkPanelOpen />,
    )

    const labels = screen.getAllByText('收藏夹')
    const hint = labels.find((element) => element.tagName === 'SPAN')!
    expect(hint.className).toContain('opacity-0')
    expect(hint.className).not.toContain(' opacity-100')
  })
})
