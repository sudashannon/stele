import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { BookmarkPanel } from './BookmarkPanel'
import type { Bookmark } from '../api/types'

describe('BookmarkPanel', () => {
  it('shows an empty state when there are no bookmarks', () => {
    render(<BookmarkPanel bookmarks={[]} onOpen={vi.fn()} onRemove={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByText(/尚无收藏/)).toBeTruthy()
  })

  it('renders each bookmark with its type badge and title', () => {
    const bookmarks: Bookmark[] = [
      { path: '/x/design.md', title: 'design.md', type: 'md', starredAt: '2026-01-01T00:00:00Z' },
      { path: '/x/proposal.md', title: 'proposal.md', type: 'md', starredAt: '2026-01-02T00:00:00Z' },
    ]
    render(<BookmarkPanel bookmarks={bookmarks} onOpen={vi.fn()} onRemove={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByText('design.md')).toBeTruthy()
    expect(screen.getByText('proposal.md')).toBeTruthy()
  })

  it('calls onOpen with the path when a bookmark row is clicked', () => {
    const onOpen = vi.fn()
    const bookmarks: Bookmark[] = [
      { path: '/x/design.md', title: 'design.md', type: 'md', starredAt: '2026-01-01T00:00:00Z' },
    ]
    render(<BookmarkPanel bookmarks={bookmarks} onOpen={onOpen} onRemove={vi.fn()} onClose={vi.fn()} />)
    fireEvent.click(screen.getByText('design.md'))
    expect(onOpen).toHaveBeenCalledWith('/x/design.md')
  })

  it('calls onRemove with the path when the remove button is clicked', () => {
    const onRemove = vi.fn()
    const bookmarks: Bookmark[] = [
      { path: '/x/design.md', title: 'design.md', type: 'md', starredAt: '2026-01-01T00:00:00Z' },
    ]
    render(<BookmarkPanel bookmarks={bookmarks} onOpen={vi.fn()} onRemove={onRemove} onClose={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: '移除 design.md' }))
    expect(onRemove).toHaveBeenCalledWith('/x/design.md')
  })

  it('calls onClose when the close button is clicked', () => {
    const onClose = vi.fn()
    render(<BookmarkPanel bookmarks={[]} onOpen={vi.fn()} onRemove={vi.fn()} onClose={onClose} />)
    fireEvent.click(screen.getByRole('button', { name: '关闭收藏' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
