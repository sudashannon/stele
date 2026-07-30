import { act, render, screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { SemanticSearch } from './SemanticSearch'
import { searchSemantic, type SemanticSearchResult } from '../api/client'

vi.mock('../api/client', () => ({
  searchSemantic: vi.fn(),
}))

afterEach(() => {
  vi.clearAllMocks()
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
    expect(searchSemantic).toHaveBeenCalledWith(
      'reset my password',
      expect.any(Number),
      expect.any(AbortSignal),
    )
    expect(vi.mocked(searchSemantic).mock.calls[0][1]).toBeGreaterThanOrEqual(1)

    // The higher-similarity item's result row must precede the lower one.
    const rows = screen.getAllByRole('button').filter((b) => b.textContent?.includes('Doc'))
    expect(rows[0].textContent).toContain('Matching Doc')
  })

  it('renders the frontmatter tags carried by a result', async () => {
    vi.mocked(searchSemantic).mockResolvedValue([
      { id: 'tagged', title: 'PCIe Endpoint Mode', workspace: 'rx101', type: 'knowledge', similarity: 0.9, tags: ['RX101', 'PCIe'] },
      { id: 'untagged', title: 'No Tags Doc', workspace: 'rx101', type: 'knowledge', similarity: 0.5 },
    ])

    render(<SemanticSearch onNodeClick={() => {}} />)
    fireEvent.change(screen.getByLabelText('语义搜索'), { target: { value: 'pcie' } })

    await waitFor(() => expect(screen.getByText('PCIe Endpoint Mode')).toBeTruthy(), { timeout: 2000 })
    const tagGroups = screen.getAllByTestId('search-result-tags')
    // Only the tagged result renders a tag row.
    expect(tagGroups).toHaveLength(1)
    expect(tagGroups[0].textContent).toContain('RX101')
    expect(tagGroups[0].textContent).toContain('PCIe')
  })

  it('advertises the tag: filter in the placeholder', () => {
    vi.mocked(searchSemantic).mockResolvedValue([])
    render(<SemanticSearch onNodeClick={() => {}} />)
    expect((screen.getByLabelText('语义搜索') as HTMLInputElement).placeholder).toContain('tag:')
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

    await waitFor(
      () => expect(screen.getByText('语义搜索暂不可用，请稍后重试')).toBeTruthy(),
      { timeout: 2000 },
    )
  })

  it('aborts the previous request and ignores a stale response during rapid input', async () => {
    const first = Promise.withResolvers<SemanticSearchResult[]>()
    const second = Promise.withResolvers<SemanticSearchResult[]>()
    vi.mocked(searchSemantic)
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)

    render(<SemanticSearch onNodeClick={() => {}} />)
    const input = screen.getByLabelText('语义搜索')
    fireEvent.change(input, { target: { value: 'first' } })
    await waitFor(() => expect(searchSemantic).toHaveBeenCalledTimes(1), { timeout: 2000 })
    const firstSignal = vi.mocked(searchSemantic).mock.calls[0][2]

    fireEvent.change(input, { target: { value: 'second' } })
    await waitFor(() => expect(searchSemantic).toHaveBeenCalledTimes(2), { timeout: 2000 })
    expect(firstSignal?.aborted).toBe(true)

    second.resolve([{
      id: 'latest',
      title: 'Latest result',
      workspace: 'ws',
      type: 'design',
      similarity: 0.9,
    }])
    await waitFor(() => expect(screen.getByText('Latest result')).toBeTruthy())

    await act(async () => {
      first.resolve([{
        id: 'stale',
        title: 'Stale result',
        workspace: 'ws',
        type: 'design',
        similarity: 1,
      }])
      await first.promise
    })
    expect(screen.queryByText('Stale result')).toBeNull()
    expect(screen.getByText('Latest result')).toBeTruthy()
    expect(vi.mocked(searchSemantic).mock.calls.every(([, topK]) => topK !== undefined && topK >= 1)).toBe(true)
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
