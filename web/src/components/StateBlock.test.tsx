import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { StateBlock } from './StateBlock'

describe('StateBlock', () => {
  it('renders loading state with role="status"', () => {
    render(<StateBlock kind="loading" title="加载中…" />)
    expect(screen.getByRole('status')).toBeTruthy()
    expect(screen.getByText('加载中…')).toBeTruthy()
  })

  it('renders error state with role="alert"', () => {
    render(<StateBlock kind="error" title="加载失败" />)
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByText('加载失败')).toBeTruthy()
  })

  it('renders empty state without status or alert role', () => {
    render(<StateBlock kind="empty" title="暂无数据" />)
    expect(screen.queryByRole('status')).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByText('暂无数据')).toBeTruthy()
  })

  it('passes testId to the root element', () => {
    render(<StateBlock kind="error" title="错误" testId="my-error" />)
    expect(screen.getByTestId('my-error')).toBeTruthy()
  })

  it('renders action button and fires onClick', () => {
    const onClick = vi.fn()
    render(
      <StateBlock
        kind="empty"
        title="暂无数据"
        action={{ label: '去创建', onClick }}
      />,
    )
    const button = screen.getByRole('button', { name: '去创建' })
    expect(button).toBeTruthy()
    fireEvent.click(button)
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('does not render action button in loading state', () => {
    render(
      <StateBlock
        kind="loading"
        title="加载中…"
        action={{ label: '重试', onClick: vi.fn() }}
      />,
    )
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('renders hints in empty state', () => {
    render(
      <StateBlock
        kind="empty"
        title="暂无数据"
        hints={['搜索变更', '搜索文档', 'Ctrl + K']}
      />,
    )
    expect(screen.getByText('搜索变更')).toBeTruthy()
    expect(screen.getByText('搜索文档')).toBeTruthy()
    expect(screen.getByText('Ctrl + K')).toBeTruthy()
  })

  it('renders compact variant as inline row', () => {
    const { container } = render(
      <StateBlock kind="loading" title="加载中…" compact />,
    )
    // compact uses flex + items-center (inline row), default uses flex-col + max-width
    const root = container.firstElementChild!
    expect(root.className).toContain('items-center')
    expect(root.className).not.toContain('max-w-')
  })

  it('renders detail text in non-compact mode', () => {
    render(<StateBlock kind="empty" title="暂无数据" detail="这里还没有内容" />)
    expect(screen.getByText('这里还没有内容')).toBeTruthy()
  })
})
