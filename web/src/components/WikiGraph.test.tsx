import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import cytoscape from 'cytoscape'
import { WikiGraph } from './WikiGraph'

const fetchCommunityOverviewMock = vi.hoisted(() => vi.fn())
const searchSemanticMock = vi.hoisted(() => vi.fn())

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client')
  return {
    ...actual,
    fetchCommunityOverview: fetchCommunityOverviewMock,
    searchSemantic: searchSemanticMock,
  }
})

const mockCy = {
  on: vi.fn(),
  one: vi.fn(),
  fit: vi.fn(),
  destroy: vi.fn(),
  nodes: vi.fn(() => ({
    removeClass: vi.fn(() => ({ removeClass: vi.fn() })),
    forEach: vi.fn(),
  })),
  batch: vi.fn((fn: () => void) => fn()),
}

vi.mock('cytoscape', () => ({
  default: vi.fn(() => mockCy),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
  fetchCommunityOverviewMock.mockReset()
  searchSemanticMock.mockReset()
})

function mockGraphResponse(
  components: unknown[],
  edges: unknown[] = [],
  communities?: Record<string, number>,
  communityLabels?: Record<string, string>,
) {
  return {
    ok: true,
    json: async () => ({ components, edges, communities, communityLabels }),
  } as Response
}

describe('WikiGraph', () => {
  it('fetches components+edges, initializes cytoscape with mapped elements, wires tap-to-click, and destroys on unmount', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
      ),
    )
    const onNodeClick = vi.fn()
    const { container, unmount } = render(<WikiGraph onNodeClick={onNodeClick} />)
    await waitFor(() => expect(container.querySelector('[data-testid="wiki-graph-canvas"]')).toBeTruthy())

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalled())
    const call = vi.mocked(cytoscape).mock.calls[0][0] as unknown as {
      elements: Array<{ data: { id: string; source?: string; target?: string; kind?: string } }>
      style: Array<{ selector: string; style: Record<string, unknown> }>
      layout: { name: string }
    }
    expect(call.elements).toEqual([
      { data: { id: '/x/a.md', label: 'A', color: 'rgb(234, 145, 31)', commColor: 'rgb(255, 255, 255)' } },
      { data: { id: '/x/b.md', label: 'B', color: 'rgb(36, 161, 72)', commColor: 'rgb(255, 255, 255)' } },
      { data: { id: 'e0', source: '/x/a.md', target: '/x/b.md', kind: 'references', color: 'rgb(36, 161, 72)' } },
    ])
    expect(JSON.stringify(call.elements)).not.toMatch(/var\(|color-mix\(/)
    expect(JSON.stringify(call.style)).not.toMatch(/var\(|color-mix\(/)
    expect(call.layout.name).toBe('cose')

    expect(mockCy.on).toHaveBeenCalledWith('tap', 'node', expect.any(Function))
    const tapHandler = mockCy.on.mock.calls.find((entry) => entry[0] === 'tap')![2] as (event: {
      target: { id: () => string; data: (name: string) => string; addClass: (name: string) => void; removeClass: (name: string) => void }
    }) => void
    await act(async () => {
      tapHandler({
        target: {
          id: () => '/x/a.md',
          data: (name: string) => (name === 'label' ? 'A' : ''),
          addClass: vi.fn(),
          removeClass: vi.fn(),
        },
      })
    })
    expect(onNodeClick).toHaveBeenCalledWith('/x/a.md')
    expect(screen.getByText(/已选中：/)).toBeTruthy()

    unmount()
    expect(mockCy.destroy).toHaveBeenCalled()
  })

  it('uses a bounded non-iterative layout for large connected graphs', async () => {
    const components = Array.from({ length: 251 }, (_, index) => ({
      id: `/x/${index}.md`,
      type: 'spec',
      title: `Node ${index}`,
      path: `/x/${index}.md`,
      workspace: 'miao',
    }))
    const edges = components.slice(1).map((component, index) => ({
      from: components[index].id,
      to: component.id,
      kind: 'references',
      source: 'markdown-link',
    }))
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockGraphResponse(components, edges))

    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)?.[0] as unknown as {
        layout: { name: string; animate: boolean }
      }
      expect(call.layout).toMatchObject({ name: 'concentric', animate: false })
    })
  })

  it('renders a type legend once components load', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
        { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
      ]),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('wiki-graph-legend')).toBeTruthy())
    expect(screen.getByText('spec')).toBeTruthy()
    expect(screen.getByText('plan')).toBeTruthy()
  })

  it('sorts edgeless nodes by type and falls back to grid layout when there are zero edges', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        { id: '/x/diagram.md', type: 'diagram', title: 'D', path: '/x/diagram.md', workspace: 'miao' },
        { id: '/x/change.md', type: 'change', title: 'C', path: '/x/change.md', workspace: 'miao' },
        { id: '/x/plan.md', type: 'plan', title: 'P', path: '/x/plan.md', workspace: 'miao' },
      ]),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalled())
    const call = vi.mocked(cytoscape).mock.calls[0][0] as unknown as {
      elements: Array<{ data: { id: string } }>
      layout: { name: string }
    }
    expect(call.elements.map((element) => element.data.id)).toEqual(['/x/change.md', '/x/plan.md', '/x/diagram.md'])
    expect(call.layout.name).toBe('grid')
  })

  it('defaults to connected-only view, explains hidden isolated nodes, and lets the user toggle back to all nodes', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
          { id: '/x/isolated.md', type: 'artifact', title: 'Isolated', path: '/x/isolated.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalledTimes(1))
    const firstCall = vi.mocked(cytoscape).mock.calls[0][0] as unknown as {
      elements: Array<{ data: { id: string } }>
    }
    expect(firstCall.elements.map((element) => element.data.id)).toEqual(['/x/a.md', '/x/b.md', 'e0'])
    expect(screen.getByTestId('wiki-graph-visibility-summary').textContent).toContain('仅关联视图隐藏 1 个孤立节点')

    const toggle = screen.getByLabelText('仅显示有关联的节点') as HTMLInputElement
    expect(toggle.checked).toBe(true)
    act(() => toggle.click())

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalledTimes(2))
    const secondCall = vi.mocked(cytoscape).mock.calls[1][0] as unknown as {
      elements: Array<{ data: { id: string } }>
    }
    expect(secondCall.elements.map((element) => element.data.id)).toEqual(['/x/a.md', '/x/b.md', '/x/isolated.md', 'e0'])
    expect(screen.getByTestId('wiki-graph-visibility-summary').textContent).toContain('显示 3 / 3 节点')
  })

  it('excludes vector, bm25, and tag edges from rendered connectivity and keeps tag-only nodes hidden in connected-only mode', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
          { id: '/x/c.md', type: 'artifact', title: 'C', path: '/x/c.md', workspace: 'miao' },
          { id: '/x/d.md', type: 'diagram', title: 'D', path: '/x/d.md', workspace: 'miao' },
        ],
        [
          { from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' },
          { from: '/x/b.md', to: '/x/c.md', kind: 'similar', source: 'vector' },
          { from: '/x/a.md', to: '/x/c.md', kind: 'search-hit', source: 'bm25' },
          { from: '/x/c.md', to: '/x/d.md', kind: 'shares-tag:alpha', source: 'tag', weight: 0.4 },
        ],
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalledTimes(1))
    const call = vi.mocked(cytoscape).mock.calls[0][0] as unknown as {
      elements: Array<{ data: { id: string; source?: string; target?: string; kind?: string } }>
      layout: { name: string }
    }
    expect(call.elements).toEqual([
      { data: { id: '/x/a.md', label: 'A', color: 'rgb(234, 145, 31)', commColor: 'rgb(255, 255, 255)' } },
      { data: { id: '/x/b.md', label: 'B', color: 'rgb(36, 161, 72)', commColor: 'rgb(255, 255, 255)' } },
      { data: { id: 'e0', source: '/x/a.md', target: '/x/b.md', kind: 'references', color: 'rgb(36, 161, 72)' } },
    ])
    expect(call.elements.some((element) => element.data.source === '/x/b.md' && element.data.target === '/x/c.md')).toBe(false)
    expect(call.elements.some((element) => element.data.source === '/x/a.md' && element.data.target === '/x/c.md')).toBe(false)
    expect(call.elements.some((element) => element.data.source === '/x/c.md' && element.data.target === '/x/d.md')).toBe(false)
    expect(call.elements.some((element) => element.data.kind?.startsWith('shares-tag:'))).toBe(false)
    expect(call.layout.name).toBe('cose')
    expect(screen.getByTestId('wiki-graph-visibility-summary').textContent).toContain('仅关联视图隐藏 2 个孤立节点')
  })

  it('shows a hover tooltip with the node title and connected-edge highlight on mouseover', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A标题', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)
    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalled())

    expect(screen.queryByTestId('wiki-graph-tooltip')).toBeNull()

    const connectedEdges = { addClass: vi.fn(), removeClass: vi.fn() }
    const fakeNode = {
      addClass: vi.fn(),
      removeClass: vi.fn(),
      connectedEdges: vi.fn(() => connectedEdges),
      renderedPosition: vi.fn(() => ({ x: 42, y: 24 })),
      data: vi.fn(() => 'A标题'),
    }
    const mouseoverHandler = mockCy.on.mock.calls.find((entry) => entry[0] === 'mouseover')![2] as (event: {
      target: typeof fakeNode
    }) => void
    const mouseoutHandler = mockCy.on.mock.calls.find((entry) => entry[0] === 'mouseout')![2] as (event: {
      target: typeof fakeNode
    }) => void

    act(() => mouseoverHandler({ target: fakeNode }))
    await waitFor(() => expect(screen.getByTestId('wiki-graph-tooltip').textContent).toBe('A标题'))
    expect(fakeNode.addClass).toHaveBeenCalledWith('hovered')
    expect(connectedEdges.addClass).toHaveBeenCalledWith('highlighted')

    act(() => mouseoutHandler({ target: fakeNode }))
    await waitFor(() => expect(screen.queryByTestId('wiki-graph-tooltip')).toBeNull())
    expect(connectedEdges.removeClass).toHaveBeenCalledWith('highlighted')
  })

  it('shows an indexing message while polling, then a genuine empty-state message once polling gives up', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockGraphResponse([]))
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByText('索引构建中…')).toBeTruthy()
    expect(cytoscape).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(20 * 3000)
    })

    expect(screen.getByText(/索引为空，请先注册工作区并重建（POST \/api\/wiki\/rebuild）/)).toBeTruthy()
    expect(cytoscape).not.toHaveBeenCalled()
  })

  it('auto-populates once a later poll returns data, without manual view-switching', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const graphResponses = [
      mockGraphResponse([]),
      mockGraphResponse([]),
      mockGraphResponse([{ id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' }]),
    ]
    let graphCallIndex = 0
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(() => {
      const response = graphResponses[Math.min(graphCallIndex, graphResponses.length - 1)]
      graphCallIndex += 1
      return Promise.resolve(response)
    })
    render(<WikiGraph onNodeClick={vi.fn()} />)

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
    await waitFor(() => expect(screen.getByTestId('wiki-graph-legend')).toBeTruthy())
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('retries a transient graph fetch failure and recovers without leaving the view', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValue(
        mockGraphResponse([{ id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' }]),
      )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(screen.getByText('索引构建中…')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    await waitFor(() => expect(screen.getByTestId('wiki-graph-legend')).toBeTruthy())
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('stops polling and does not update state after unmount', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockGraphResponse([]))
    const { unmount } = render(<WikiGraph onNodeClick={vi.fn()} />)

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

  it('passes community labels into filters and legends', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
          { id: '/x/c.md', type: 'artifact', title: 'C', path: '/x/c.md', workspace: 'miao' },
        ],
        [
          { from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' },
          { from: '/x/a.md', to: '/x/c.md', kind: 'similar', source: 'embedding' },
        ],
        { '/x/a.md': 0, '/x/b.md': 0, '/x/c.md': 1 },
        { '0': '发布流程', '1': '索引维护' },
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalled())
    const call = vi.mocked(cytoscape).mock.calls[0][0] as unknown as {
      elements: Array<{ data: { id: string; commColor?: string; kind?: string } }>
    }
    expect(call.elements.find((element) => element.data.id === '/x/a.md')?.data.commColor).toBe('rgb(15, 98, 254)')
    expect(call.elements.find((element) => element.data.id === '/x/b.md')?.data.commColor).toBe('rgb(15, 98, 254)')
    expect(call.elements.find((element) => element.data.id === '/x/c.md')?.data.commColor).toBe('rgb(36, 161, 72)')

    await waitFor(() => expect(screen.getByTestId('wiki-graph-community-legend')).toBeTruthy())
    expect(screen.getAllByText('发布流程').length).toBeGreaterThan(0)
    expect(screen.getAllByText('索引维护').length).toBeGreaterThan(0)
  })

  it('filters nodes to a single community when its label chip is clicked, and clears on a second click', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
          { id: '/x/c.md', type: 'artifact', title: 'C', path: '/x/c.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
        { '/x/a.md': 0, '/x/b.md': 0, '/x/c.md': 1 },
        { '0': '主图谱', '1': '孤立资料' },
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(screen.getAllByTestId('community-chip').length).toBe(2))
    const communityButton = screen.getAllByTestId('community-chip').find((element) => element.textContent?.includes('孤立资料'))!

    fireEvent.click(communityButton)
    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)![0] as unknown as {
        elements: Array<{ data: { id: string } }>
      }
      expect(call.elements.map((element) => element.data.id)).toEqual(['/x/c.md'])
    })

    fireEvent.click(communityButton)
    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)![0] as unknown as {
        elements: Array<{ data: { id: string } }>
      }
      expect(call.elements.map((element) => element.data.id)).toEqual(expect.arrayContaining(['/x/a.md', '/x/b.md']))
    })
  })

  it('filters nodes by workspace chip toggle', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'alpha' },
        { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'beta' },
      ]),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(screen.getAllByTestId('workspace-chip').length).toBe(2))
    const alphaChip = screen.getAllByTestId('workspace-chip').find((element) => element.textContent === 'alpha')!

    fireEvent.click(alphaChip)
    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)![0] as unknown as {
        elements: Array<{ data: { id: string } }>
      }
      expect(call.elements.map((element) => element.data.id)).toEqual(['/x/b.md'])
    })

    fireEvent.click(screen.getByTestId('filter-reset'))
    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)![0] as unknown as {
        elements: Array<{ data: { id: string } }>
      }
      expect(call.elements.map((element) => element.data.id)).toEqual(
        expect.arrayContaining(['/x/a.md', '/x/b.md']),
      )
    })
  })
  it('clears the selected-node notice when filtering rebuilds the graph', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'alpha' },
        { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'beta' },
      ]),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(screen.getAllByTestId('workspace-chip')).toHaveLength(2))
    const tapHandler = mockCy.on.mock.calls.filter((entry) => entry[0] === 'tap').at(-1)![2] as (event: {
      target: {
        id: () => string
        data: (name: string) => string
        addClass: (name: string) => void
        removeClass: (name: string) => void
      }
    }) => void
    act(() => {
      tapHandler({
        target: {
          id: () => '/x/a.md',
          data: () => 'A',
          addClass: vi.fn(),
          removeClass: vi.fn(),
        },
      })
    })
    expect(screen.getByText(/已选中：/).textContent).toContain('A')

    const alphaChip = screen.getAllByTestId('workspace-chip').find((element) => element.textContent === 'alpha')!
    fireEvent.click(alphaChip)

    await waitFor(() => expect(screen.queryByText(/已选中：/)).toBeNull())
  })


  it('loads community overview body after showing a loading state', async () => {
    const pendingOverview = deferred<string>()
    fetchCommunityOverviewMock.mockReturnValueOnce(pendingOverview.promise)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
        { '/x/a.md': 0, '/x/b.md': 0 },
        { '0': '发布流程' },
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(screen.getAllByTestId('community-chip').length).toBe(1))
    fireEvent.click(screen.getByTestId('community-chip'))
    fireEvent.click(screen.getByRole('button', { name: /社区综述/ }))

    expect(screen.getByText('正在加载社区综述…')).toBeTruthy()
    pendingOverview.resolve('这是社区综述正文')

    await waitFor(() => expect(screen.getByTestId('community-overview-body').textContent).toContain('这是社区综述正文'))
    expect(fetchCommunityOverviewMock).toHaveBeenCalledWith(0)
  })

  it('shows community overview errors', async () => {
    fetchCommunityOverviewMock.mockRejectedValueOnce(new Error('该社区成员少于 3 个，未生成综述'))
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
        { '/x/a.md': 0, '/x/b.md': 0 },
        { '0': '发布流程' },
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('community-chip')).toBeTruthy())
    fireEvent.click(screen.getByTestId('community-chip'))
    fireEvent.click(screen.getByRole('button', { name: /社区综述/ }))

    await waitFor(() => expect(screen.getByText('该社区成员少于 3 个，未生成综述')).toBeTruthy())
  })

  it('ignores stale overview responses when switching communities', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    fetchCommunityOverviewMock
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
          { id: '/x/c.md', type: 'artifact', title: 'C', path: '/x/c.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
        { '/x/a.md': 0, '/x/b.md': 0, '/x/c.md': 1 },
        { '0': '发布流程', '1': '索引维护' },
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(screen.getAllByTestId('community-chip').length).toBe(2))
    const chips = screen.getAllByTestId('community-chip')
    const firstChip = chips.find((element) => element.textContent?.includes('发布流程'))!
    const secondChip = chips.find((element) => element.textContent?.includes('索引维护'))!

    fireEvent.click(firstChip)
    fireEvent.click(screen.getByRole('button', { name: /社区综述/ }))
    fireEvent.click(secondChip)

    await waitFor(() => expect(fetchCommunityOverviewMock).toHaveBeenNthCalledWith(2, 1))
    second.resolve('第二个社区正文')
    await waitFor(() => expect(screen.getByTestId('community-overview-body').textContent).toContain('第二个社区正文'))

    first.resolve('第一个社区正文')
    await act(async () => {
      await Promise.resolve()
    })
    expect(screen.getByTestId('community-overview-body').textContent).not.toContain('第一个社区正文')
  })

  it('refetches the graph when the SSE hook fires a graph-updated event', async () => {
    class MockEventSource {
      static instance: MockEventSource | null = null
      listeners: Record<string, Array<() => void>> = {}
      constructor() {
        MockEventSource.instance = this
      }
      addEventListener(type: string, cb: () => void) {
        ;(this.listeners[type] ??= []).push(cb)
      }
      close() {}
    }
    vi.stubGlobal('EventSource', MockEventSource)

    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(() =>
      Promise.resolve(
        mockGraphResponse([{ id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' }]),
      ),
    )
    render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalled())
    const callsBeforeEvent = fetchMock.mock.calls.length

    await act(async () => {
      MockEventSource.instance!.listeners['graph-updated']?.forEach((cb) => cb())
    })

    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(callsBeforeEvent))
    vi.unstubAllGlobals()
  })
  it('clears the previous semantic-search highlight as soon as a new search starts', async () => {
    const firstSearch = deferred<Array<{ id: string }>>()
    const secondSearch = deferred<Array<{ id: string }>>()
    searchSemanticMock
      .mockReturnValueOnce(firstSearch.promise)
      .mockReturnValueOnce(secondSearch.promise)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
      ]),
    )
    const node = {
      id: () => '/x/a.md',
      toggleClass: vi.fn(),
    }
    const nodes = {
      removeClass: vi.fn(),
      forEach: vi.fn((callback: (current: typeof node) => void) => callback(node)),
    }
    nodes.removeClass.mockReturnValue(nodes)
    mockCy.nodes.mockReturnValue(nodes)

    render(<WikiGraph onNodeClick={vi.fn()} />)
    const search = await screen.findByLabelText('图谱语义搜索')
    fireEvent.change(search, { target: { value: 'first' } })
    await waitFor(() => expect(searchSemanticMock).toHaveBeenCalledWith('first', 10, expect.any(AbortSignal)))

    firstSearch.resolve([{ id: '/x/a.md' }])
    await waitFor(() => expect(node.toggleClass).toHaveBeenCalledWith('search-match', true))
    nodes.removeClass.mockClear()

    fireEvent.change(search, { target: { value: 'second' } })
    expect(nodes.removeClass).toHaveBeenNthCalledWith(1, 'search-match')
    expect(nodes.removeClass).toHaveBeenNthCalledWith(2, 'search-dim')
  })

})
