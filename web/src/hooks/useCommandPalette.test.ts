import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { fuzzyMatch, highlightMatches, useCommandPalette } from './useCommandPalette'
import type { CommandAction } from './useCommandPalette'

const actions: CommandAction[] = [
  { id: 'settings', label: '设置', subtitle: '应用偏好', category: 'Navigation', run: vi.fn() },
  { id: 'search', label: '语义搜索', subtitle: '搜索文档', category: 'Navigation', run: vi.fn() },
  { id: 'refresh', label: '刷新数据', category: 'Commands', run: vi.fn() },
]

describe('useCommandPalette', () => {
  it('opens, clears query on close, and toggles consistently', () => {
    const { result } = renderHook(() => useCommandPalette(actions))

    act(() => result.current.openPalette())
    expect(result.current.open).toBe(true)
    act(() => result.current.setQuery('搜索'))
    expect(result.current.query).toBe('搜索')
    act(() => result.current.closePalette())
    expect(result.current.open).toBe(false)
    expect(result.current.query).toBe('')
    act(() => result.current.togglePalette())
    expect(result.current.open).toBe(true)
  })

  it('filters and scores labels and subtitles for the current query', () => {
    const { result } = renderHook(() => useCommandPalette(actions))

    act(() => result.current.setQuery('文档'))

    expect(result.current.results.map((item) => item.action.id)).toEqual(['search'])
    expect(result.current.results[0].subtitleIndices).toHaveLength(2)
  })
})

describe('command palette matching helpers', () => {
  it('returns fuzzy match indices and escapes highlighted HTML', () => {
    expect(fuzzyMatch('abc', 'a-b-c')?.indices).toEqual([0, 2, 4])
    expect(highlightMatches('<b>', [0])).toBe('<mark class="palette-match">&lt;</mark>b&gt;')
  })
})
