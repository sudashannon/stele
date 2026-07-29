import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { SemanticSearch } from './SemanticSearch'
import { searchSemantic, type SemanticSearchResult } from '../api/client'

vi.mock('../api/client', () => ({
  searchSemantic: vi.fn(),
}))

afterEach(() => {
  vi.restoreAllMocks()
})

function buildResults(): SemanticSearchResult[] {
  return [
    { id: 'match-1', title: 'Matching Doc', workspace: 'ws-a', type: 'design', similarity: 1 },
    { id: 'unrelated-1', title: 'Unrelated Doc', workspace: 'ws-b', type: 'spec', similarity: 0.4 },
  ]
}

describe('SemanticSearch', () => {
  it('calls the search-semantic API and renders ranked results as the user types', async () => {
    vi.mocked(searchSemantic).mockResolvedValue(buildResults())

    render(<SemanticSearch onNodeClick={() => {}} />)

    const input = await waitFor(() => screen.getByLabelText('语义搜索') as HTMLInputElement)
    fireEvent.change(input, { target: { value: 'reset my password' } })

    await waitFor(() => expect(screen.getByText('Matching Doc')).toBeTruthy(), { timeout: 2000 })
    expect(screen.getByText('100%')).toBeTruthy()
    expect(screen.getByText('Unrelated Doc')).toBeTruthy()
    expect(searchSemantic).toHaveBeenCalledWith('reset my password', 0)

    // The higher-similarity item's result row must precede the lower one.
    const rows = screen.getAllByRole('button').filter((b) => b.textContent?.includes('Doc'))
    expect(rows[0].textContent).toContain('Matching Doc')
  })

  it('calls onNodeClick with the component id when a result is clicked', async () => {
    vi.mocked(searchSemantic).mockResolvedValue(buildResults())
    const onNodeClick = vi.fn()

    render(<SemanticSearch onNodeClick={onNodeClick} />)
    const input = await waitFor(() => screen.getByLabelText('语义搜索') as HTMLInputElement)
    fireEvent.change(input, { target: { value: 'reset my password' } })

    const resultButton = await waitFor(() => screen.getByText('Matching Doc'), { timeout: 2000 })
    fireEvent.click(resultButton)
    expect(onNodeClick).toHaveBeenCalledWith('match-1')
  })

  it('shows a load error when the search request fails', async () => {
    vi.mocked(searchSemantic).mockRejectedValue(new Error('boom'))

    render(<SemanticSearch onNodeClick={() => {}} />)
    const input = await waitFor(() => screen.getByLabelText('语义搜索') as HTMLInputElement)
    fireEvent.change(input, { target: { value: 'reset my password' } })

    await waitFor(() => expect(screen.getByText('搜索失败')).toBeTruthy(), { timeout: 2000 })
  })

  it('clears results when the query is emptied', async () => {
    vi.mocked(searchSemantic).mockResolvedValue(buildResults())

    render(<SemanticSearch onNodeClick={() => {}} />)
    const input = await waitFor(() => screen.getByLabelText('语义搜索') as HTMLInputElement)

    fireEvent.change(input, { target: { value: 'reset my password' } })
    await waitFor(() => expect(screen.getByText('Matching Doc')).toBeTruthy(), { timeout: 2000 })

    fireEvent.change(input, { target: { value: '' } })
    await waitFor(() => expect(screen.queryByText('Matching Doc')).toBeFalsy())
  })

  it('renders the filename when it differs from the document title', async () => {
    const filename = '2026-07-14-rx101-orin-bsp-build-system-research.md'
    vi.mocked(searchSemantic).mockResolvedValue([{
      id: `/workspace/knowledge/${filename}`,
      title: '结论摘要',
      workspace: 'miao',
      type: 'knowledge',
      similarity: 1,
    }])

    render(<SemanticSearch onNodeClick={() => {}} />)
    const input = await waitFor(() => screen.getByLabelText('语义搜索') as HTMLInputElement)
    fireEvent.change(input, { target: { value: filename } })

    await waitFor(() => expect(screen.getByText(filename)).toBeTruthy(), { timeout: 2000 })
    expect(screen.getByText('结论摘要')).toBeTruthy()
  })
})
