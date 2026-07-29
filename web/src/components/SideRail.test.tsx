import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { SideRail } from './SideRail'

describe('SideRail', () => {
  it('renders navigation buttons for all views', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)
    expect(screen.getByRole('button', { name: '变更仪表盘' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '知识图谱' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '时间线' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '语义搜索' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '最近更新' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '文档健康' })).toBeTruthy()
  })

  it('renders active view with blue background', () => {
    render(<SideRail view="graph" onSelect={() => {}} />)
    const graphBtn = screen.getByRole('button', { name: '知识图谱' })
    expect(graphBtn.className).toContain('bg-\[var(--color-accent)\]')
  })

  it('calls onSelect with the view key when a button is clicked', () => {
    const onSelect = vi.fn()
    render(<SideRail view="changes" onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button', { name: '文档健康' }))
    expect(onSelect).toHaveBeenCalledWith('lint')
  })

  it('renders a disabled settings button when no handler provided', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)
    const settings = screen.getByRole('button', { name: '设置' }) as HTMLButtonElement
    expect(settings.disabled).toBe(true)
  })

  it('renders enabled settings when handler provided', () => {
    render(<SideRail view="changes" onSelect={() => {}} onOpenSettings={() => {}} />)
    const settings = screen.getByRole('button', { name: '设置' }) as HTMLButtonElement
    expect(settings.disabled).toBe(false)
  })

  it('renders a disabled bookmark button when onToggleBookmarks is omitted', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)
    const star = screen.getByRole('button', { name: '收藏夹' }) as HTMLButtonElement
    expect(star.disabled).toBe(true)
  })

  it('calls onToggleBookmarks when the bookmark button is clicked', () => {
    const onToggleBookmarks = vi.fn()
    render(<SideRail view="changes" onSelect={() => {}} onToggleBookmarks={onToggleBookmarks} />)
    fireEvent.click(screen.getByRole('button', { name: '收藏夹' }))
    expect(onToggleBookmarks).toHaveBeenCalledTimes(1)
  })

  it('marks the bookmark button as active when panel is open', () => {
    render(<SideRail view="changes" onSelect={() => {}} onToggleBookmarks={() => {}} bookmarkPanelOpen={true} />)
    const star = screen.getByRole('button', { name: '收藏夹' })
    expect(star.className).toContain('bg-\[var(--color-accent)\]')
  })

  it('renders command palette button', () => {
    render(<SideRail view="changes" onSelect={() => {}} onOpenPalette={() => {}} />)
    expect(screen.getByRole('button', { name: '命令面板' })).toBeTruthy()
  })

  it('calls onOpenPalette when command palette button is clicked', () => {
    const onOpenPalette = vi.fn()
    render(<SideRail view="changes" onSelect={() => {}} onOpenPalette={onOpenPalette} />)
    fireEvent.click(screen.getByRole('button', { name: '命令面板' }))
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
    const badge = screen.getByTestId('side-rail-todo-badge')
    expect(badge).toBeTruthy()
    expect(badge.textContent).toBe('5')
  })

  it('caps todo badge at 99+ when count is 100 or more', () => {
    render(<SideRail view="changes" onSelect={() => {}} todoCount={150} />)
    const badge = screen.getByTestId('side-rail-todo-badge')
    expect(badge.textContent).toBe('99+')
  })

  it('does not render todo badge when todoCount is zero', () => {
    render(<SideRail view="changes" onSelect={() => {}} todoCount={0} />)
    expect(screen.queryByTestId('side-rail-todo-badge')).toBeNull()
  })

  it('does not render todo badge when todoCount is undefined', () => {
    render(<SideRail view="changes" onSelect={() => {}} />)
    expect(screen.queryByTestId('side-rail-todo-badge')).toBeNull()
  })
})
