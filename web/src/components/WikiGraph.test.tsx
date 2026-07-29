import { act, render, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import cytoscape from 'cytoscape'
import { WikiGraph, TYPE_COLORS, COMMUNITY_COLORS } from './WikiGraph'

const mockCy = {
  on: vi.fn(),
  one: vi.fn(),
  fit: vi.fn(),
  destroy: vi.fn(),
  nodes: vi.fn(() => ({
    removeClass: vi.fn().mockReturnThis(),
    forEach: vi.fn(),
  })),
  batch: vi.fn((fn: () => void) => fn()),
}
vi.mock('cytoscape', () => ({
  default: vi.fn(() => mockCy),
}))

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

function mockGraphResponse(components: unknown[], edges: unknown[] = [], communities?: Record<string, number>) {
  return { ok: true, json: async () => ({ components, edges, communities }) } as Response
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
    // Edges present -> force-directed layout reveals structure instead of the flat grid.
    expect(call.layout.name).toBe('cose')

    expect(mockCy.on).toHaveBeenCalledWith('tap', 'node', expect.any(Function))
    const tapHandler = mockCy.on.mock.calls.find((c) => c[0] === 'tap')![2] as (evt: {
      target: { id: () => string }
    }) => void
    tapHandler({ target: { id: () => '/x/a.md' } })
    expect(onNodeClick).toHaveBeenCalledWith('/x/a.md')

    unmount()
    expect(mockCy.destroy).toHaveBeenCalled()
  })

  it('renders a type legend once components load', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
        { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
      ]),
    )
    const { getByTestId, getByText } = render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(getByTestId('wiki-graph-legend')).toBeTruthy())
    expect(getByText('spec')).toBeTruthy()
    expect(getByText('plan')).toBeTruthy()
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
    expect(call.elements.map((el) => el.data.id)).toEqual(['/x/change.md', '/x/plan.md', '/x/diagram.md'])
    expect(call.layout.name).toBe('grid')
  })

  it('defaults to connected-only view and lets the user toggle back to all nodes', async () => {
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
    const { getByLabelText } = render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalledTimes(1))
    const firstCall = vi.mocked(cytoscape).mock.calls[0][0] as unknown as {
      elements: Array<{ data: { id: string } }>
    }
    // Default view excludes the isolated node so the relationship subgraph is front-and-center.
    expect(firstCall.elements.map((el) => el.data.id)).toEqual(['/x/a.md', '/x/b.md', 'e0'])

    const toggle = getByLabelText('仅显示有关联的节点') as HTMLInputElement
    expect(toggle.checked).toBe(true)
    act(() => toggle.click())

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalledTimes(2))
    const secondCall = vi.mocked(cytoscape).mock.calls[1][0] as unknown as {
      elements: Array<{ data: { id: string } }>
    }
    // Toggled off -> isolated node reappears alongside the connected pair.
    expect(secondCall.elements.map((el) => el.data.id)).toEqual(['/x/a.md', '/x/b.md', '/x/isolated.md', 'e0'])
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
    const { getByTestId, queryByTestId } = render(<WikiGraph onNodeClick={vi.fn()} />)
    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalled())

    expect(queryByTestId('wiki-graph-tooltip')).toBeNull()

    const connectedEdges = { addClass: vi.fn(), removeClass: vi.fn() }
    const fakeNode = {
      addClass: vi.fn(),
      removeClass: vi.fn(),
      connectedEdges: vi.fn(() => connectedEdges),
      renderedPosition: vi.fn(() => ({ x: 42, y: 24 })),
      data: vi.fn(() => 'A标题'),
    }
    const mouseoverHandler = mockCy.on.mock.calls.find((c) => c[0] === 'mouseover')![2] as (evt: {
      target: typeof fakeNode
    }) => void
    const mouseoutHandler = mockCy.on.mock.calls.find((c) => c[0] === 'mouseout')![2] as (evt: {
      target: typeof fakeNode
    }) => void

    act(() => mouseoverHandler({ target: fakeNode }))
    await waitFor(() => expect(getByTestId('wiki-graph-tooltip').textContent).toBe('A标题'))
    expect(fakeNode.addClass).toHaveBeenCalledWith('hovered')
    expect(connectedEdges.addClass).toHaveBeenCalledWith('highlighted')

    act(() => mouseoutHandler({ target: fakeNode }))
    await waitFor(() => expect(queryByTestId('wiki-graph-tooltip')).toBeNull())
    expect(connectedEdges.removeClass).toHaveBeenCalledWith('highlighted')
  })

  it('shows an indexing message while polling, then a genuine empty-state message once polling gives up', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockGraphResponse([]))
    const onNodeClick = vi.fn()
    const { getByText } = render(<WikiGraph onNodeClick={onNodeClick} />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(getByText('索引构建中…')).toBeTruthy()
    expect(cytoscape).not.toHaveBeenCalled()

    // 20 attempts total, 3s apart -> advance past the full poll window.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20 * 3000)
    })

    expect(getByText(/索引为空，请先注册工作区并重建（POST \/api\/wiki\/rebuild）/)).toBeTruthy()
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
    const { getByText, getByTestId } = render(<WikiGraph onNodeClick={vi.fn()} />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(getByText('索引构建中…')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(getByText('索引构建中…')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    await waitFor(() => expect(getByTestId('wiki-graph-legend')).toBeTruthy())
    expect(fetchMock).toHaveBeenCalledTimes(3)
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

  it('colors node borders by community and shows a community legend', async () => {
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
      ),
    )
    const { getByTestId } = render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(vi.mocked(cytoscape)).toHaveBeenCalled())
    const call = vi.mocked(cytoscape).mock.calls[0][0] as unknown as {
      elements: Array<{ data: { id: string; commColor?: string; kind?: string } }>
    }
    expect(call.elements.find((el) => el.data.id === '/x/a.md')?.data.commColor).toBe('rgb(15, 98, 254)')
    expect(call.elements.find((el) => el.data.id === '/x/b.md')?.data.commColor).toBe('rgb(15, 98, 254)')
    expect(call.elements.find((el) => el.data.id === '/x/c.md')?.data.commColor).toBe('rgb(36, 161, 72)')
    expect(call.elements.find((el) => el.data.kind === 'similar')).toBeTruthy()

    await waitFor(() => expect(getByTestId('wiki-graph-community-legend')).toBeTruthy())
    expect(getByTestId('wiki-graph-community-legend').textContent).toContain('#0')
    expect(getByTestId('wiki-graph-community-legend').textContent).toContain('#1')
  })

  it('filters nodes to a single community when its legend entry is clicked, and clears on a second click', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'miao' },
          { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'miao' },
          { id: '/x/c.md', type: 'artifact', title: 'C', path: '/x/c.md', workspace: 'miao' },
        ],
        [{ from: '/x/a.md', to: '/x/b.md', kind: 'references', source: 'markdown-link' }],
        { '/x/a.md': 0, '/x/b.md': 0, '/x/c.md': 1 },
      ),
    )
    const { getByTestId, getAllByTestId } = render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(getByTestId('wiki-graph-community-legend')).toBeTruthy())
    const legendItems = getAllByTestId('wiki-graph-community-legend-item')
    const communityOneButton = legendItems.find((el) => el.textContent?.includes('#1'))!

    act(() => communityOneButton.click())
    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)![0] as unknown as {
        elements: Array<{ data: { id: string } }>
      }
      expect(call.elements.map((el) => el.data.id)).toEqual(['/x/c.md'])
    })

    act(() => communityOneButton.click())
    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)![0] as unknown as {
        elements: Array<{ data: { id: string } }>
      }
      expect(call.elements.map((el) => el.data.id)).toEqual(
        expect.arrayContaining(['/x/a.md', '/x/b.md']),
      )
    })
  })

  it('filters nodes by workspace chip toggle', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        { id: '/x/a.md', type: 'spec', title: 'A', path: '/x/a.md', workspace: 'alpha' },
        { id: '/x/b.md', type: 'plan', title: 'B', path: '/x/b.md', workspace: 'beta' },
      ]),
    )
    const { getAllByTestId } = render(<WikiGraph onNodeClick={vi.fn()} />)

    await waitFor(() => expect(getAllByTestId('workspace-chip').length).toBe(2))
    const alphaChip = getAllByTestId('workspace-chip').find((el) => el.textContent === 'alpha')!

    act(() => alphaChip.click())
    await waitFor(() => {
      const call = vi.mocked(cytoscape).mock.calls.at(-1)![0] as unknown as {
        elements: Array<{ data: { id: string } }>
      }
      expect(call.elements.map((el) => el.data.id)).toEqual(['/x/b.md'])
    })
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
})
