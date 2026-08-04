import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { WikiTimeline } from './WikiTimeline'
import { CalendarPanel } from './CalendarPanel'

function mockGraphResponse(
  components: unknown[],
  communities?: Record<string, number>,
  communityLabels?: Record<string, string>,
) {
  return {
    ok: true,
    json: async () => ({ components, edges: [], communities, communityLabels }),
  } as Response
}

function mockJsonResponse(data: unknown, ok = true) {
  return {
    ok,
    json: async () => data,
  } as Response
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('WikiTimeline', () => {
  it('shows a loading state, then an empty-state message when there are no changes', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockGraphResponse([]))
    render(<WikiTimeline />)
    expect(screen.getByText('加载中…')).toBeTruthy()
    await waitFor(() => expect(screen.getByText('当前口径（全部）没有数据')).toBeTruthy())
  })

  it('renders one bar per change component, grouped by workspace, and exposes community labels', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          {
            id: 'c1',
            type: 'change',
            title: 'Add timeline view',
            path: 'openspec/changes/c1',
            workspace: 'comet-panel',
            frontmatter: { created_at: '2026-01-01T00:00:00Z', phase: 'build' },
            updatedAt: '2026-01-05T00:00:00Z',
          },
          {
            id: 'c2',
            type: 'change',
            title: 'Fix lint rule',
            path: 'openspec/changes/c2',
            workspace: 'other-repo',
            frontmatter: { created_at: '2026-02-01T00:00:00Z', phase: 'verify' },
            updatedAt: '2026-02-03T00:00:00Z',
          },
          {
            id: 'p1',
            type: 'plan',
            title: 'Not a change',
            path: 'openspec/changes/c1/plan.md',
            workspace: 'comet-panel',
          },
        ],
        { c1: 0, c2: 1 },
        { '0': '交付节奏', '1': '质量治理', '2': '无关社区' },
      ),
    )
    render(<WikiTimeline />)

    await waitFor(() => expect(screen.getByTestId('wiki-timeline')).toBeTruthy())
    // The default scope draws documents alongside changes: drawing changes only
    // is what left the chart blank once the work moved to knowledge documents.
    expect(screen.getAllByTestId('wiki-timeline-bar')).toHaveLength(3)
    expect(screen.getAllByTestId('wiki-timeline-bar').filter((bar) => bar.dataset.kind === 'document')).toHaveLength(1)
    expect(screen.getAllByText('comet-panel').length).toBeGreaterThan(0)
    expect(screen.getAllByText('other-repo').length).toBeGreaterThan(0)
    expect(screen.getAllByText('交付节奏').length).toBeGreaterThan(0)
    expect(screen.getAllByText('质量治理').length).toBeGreaterThan(0)
    expect(screen.queryByText('无关社区')).toBeNull()
  })

  // The blank chart after mid-July was a scope problem: 320 documents and every
  // session were simply not drawn. Switching scope must isolate one layer, and an
  // empty layer must say where the work actually is.
  it('switches scope between changes, documents and sessions, and names what other scopes hold', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/wiki/sessions')) {
        return Promise.resolve(mockJsonResponse({
          sessions: [{
            id: 'sess-1',
            path: '/home/u/.omp/agent/sessions/-repo/a.jsonl',
            workspace: 'comet-panel',
            title: '排查图谱',
            cwd: '/repo',
            startedAt: '2026-02-01T00:00:00Z',
            updatedAt: '2026-02-04T00:00:00Z',
            userTurns: 3,
            toolCalls: { read: 9 },
            writes: [], edits: [], reads: [], intents: [],
            activity: { '2026-02-01': 12, '2026-02-04': 30 },
          }],
        }))
      }
      return Promise.resolve(mockGraphResponse(
        [
          {
            id: 'c1', type: 'change', title: 'Add timeline view', path: 'openspec/changes/c1',
            workspace: 'comet-panel', frontmatter: { created_at: '2026-01-01T00:00:00Z', phase: 'build' },
            updatedAt: '2026-01-05T00:00:00Z',
          },
          {
            id: 'k1', type: 'knowledge', title: 'Bandwidth analysis', path: '/repo/knowledge/a.md',
            workspace: 'comet-panel', updatedAt: '2026-02-03T00:00:00Z',
          },
        ],
        { c1: 0 },
      ))
    })
    render(<WikiTimeline />)

    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-bar').length).toBeGreaterThan(2))
    const bars = () => screen.queryAllByTestId('wiki-timeline-bar')
    const scopeButton = (label: string) =>
      screen.getAllByRole('button').find((button) => button.textContent?.startsWith(label))!

    // Session days are drawn from the per-day activity, one mark per active day.
    await waitFor(() => expect(bars().filter((bar) => bar.dataset.kind === 'session')).toHaveLength(2))

    fireEvent.click(scopeButton('变更'))
    await waitFor(() => expect(bars()).toHaveLength(1))
    expect(bars()[0].dataset.kind).toBe('change')

    fireEvent.click(scopeButton('文档'))
    await waitFor(() => expect(bars()).toHaveLength(1))
    expect(bars()[0].dataset.kind).toBe('document')
    expect(bars()[0].getAttribute('title')).toContain('文档：Bandwidth analysis')

    fireEvent.click(scopeButton('会话'))
    await waitFor(() => expect(bars()).toHaveLength(2))
    expect(bars()[0].getAttribute('title')).toContain('次活动')
  })

  it('tells the reader where the work is when the chosen scope is empty', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/wiki/sessions')) return Promise.resolve(mockJsonResponse({ sessions: [] }))
      return Promise.resolve(mockGraphResponse([
        {
          id: 'k1', type: 'knowledge', title: 'Only a document', path: '/repo/knowledge/a.md',
          workspace: 'comet-panel', updatedAt: '2026-02-03T00:00:00Z',
        },
      ]))
    })
    render(<WikiTimeline />)

    await waitFor(() => expect(screen.getByTestId('wiki-timeline')).toBeTruthy())
    const scopeButton = screen.getAllByRole('button').find((button) => button.textContent?.startsWith('变更'))!
    fireEvent.click(scopeButton)

    await waitFor(() => expect(screen.getByText('当前口径（变更）没有数据')).toBeTruthy())
    expect(screen.getByTestId('wiki-timeline-scope-hint').textContent).toContain('1 篇文档')
  })

  // A community filter cannot include sessions - they belong to no community -
  // so dropping them silently would look like the session layer went missing.
  it('says that a community filter excludes sessions', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/wiki/sessions')) {
        return Promise.resolve(mockJsonResponse({
          sessions: [{
            id: 'sess-1', path: '/x/a.jsonl', workspace: 'comet-panel', title: '会话', cwd: '/repo',
            startedAt: '2026-02-01T00:00:00Z', updatedAt: '2026-02-01T00:00:00Z', userTurns: 1,
            toolCalls: {}, writes: [], edits: [], reads: [], intents: [], activity: { '2026-02-01': 4 },
          }],
        }))
      }
      return Promise.resolve(mockGraphResponse(
        [{
          id: 'c1', type: 'change', title: 'A change', path: 'openspec/changes/c1', workspace: 'comet-panel',
          frontmatter: { created_at: '2026-01-01T00:00:00Z', phase: 'build' }, updatedAt: '2026-01-05T00:00:00Z',
        }],
        { c1: 0 },
        { '0': '交付节奏' },
      ))
    })
    render(<WikiTimeline />)

    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-bar').length).toBe(2))
    fireEvent.click(screen.getByRole('button', { name: /交付节奏/ }))

    await waitFor(() =>
      expect(screen.getByTestId('graph-filter-summary').textContent).toContain('社区筛选不含会话'),
    )
  })

  // The chart spans months and scrolls horizontally: opening at the oldest edge
  // is what made the recent weeks feel missing even after they were drawn.
  it('opens scrolled to the recent end of the chart', async () => {
    // jsdom reports zero for every layout box, so the scroll geometry has to be
    // stubbed: the contract is that a chart wider than its viewport does not
    // open pinned to its oldest edge.
    const clientWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
    const scrollWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollWidth')
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, value: 600 })
    Object.defineProperty(HTMLElement.prototype, 'scrollWidth', { configurable: true, value: 4000 })
    try {
      vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/wiki/sessions')) return Promise.resolve(mockJsonResponse({ sessions: [] }))
        return Promise.resolve(mockGraphResponse([
          {
            id: 'old', type: 'knowledge', title: '很久以前', path: '/repo/knowledge/old.md',
            workspace: 'comet-panel', updatedAt: '2026-01-05T00:00:00Z',
          },
          {
            id: 'recent', type: 'knowledge', title: '最近', path: '/repo/knowledge/new.md',
            workspace: 'comet-panel', updatedAt: new Date().toISOString(),
          },
        ]))
      })
      render(<WikiTimeline />)

      const chart = await waitFor(() => screen.getByTestId('wiki-timeline'))
      await waitFor(() => expect(chart.scrollLeft).toBeGreaterThan(0))
    } finally {
      if (clientWidth) Object.defineProperty(HTMLElement.prototype, 'clientWidth', clientWidth)
      if (scrollWidth) Object.defineProperty(HTMLElement.prototype, 'scrollWidth', scrollWidth)
    }
  })

  it('caps the community legend so the timeline keeps its vertical workspace', async () => {
    const components = Array.from({ length: 14 }, (_, index) => ({
      id: `c${index}`,
      type: 'change',
      title: `Change ${index}`,
      path: `openspec/changes/c${index}`,
      workspace: 'comet-panel',
      frontmatter: { created_at: `2026-01-${String(index + 1).padStart(2, '0')}T00:00:00Z`, phase: 'build' },
      updatedAt: `2026-01-${String(index + 2).padStart(2, '0')}T00:00:00Z`,
    }))
    const communities = Object.fromEntries(components.map((component, index) => [component.id, index]))
    const communityLabels = Object.fromEntries(components.map((_, index) => [String(index), `Community ${index}`]))
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(components, communities, communityLabels),
    )

    render(<WikiTimeline />)

    // The cap dropped from 12 to 8: past roughly eight hues colour stops
    // identifying anything, so the tail collapses to one neutral entry. With 14
    // communities that is 8 coloured entries and 6 merged into the bucket. The
    // bucket says 其他 rather than 另有 because those communities ARE drawn, in
    // grey — the same wording the graph legend uses.
    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-community-legend-item')).toHaveLength(8))
    expect(screen.getByTestId('wiki-timeline-community-overflow').textContent).toContain('其他 6 个社区')
  })

  it('shows the tooltip and places a focused top-edge bar tooltip below its single focus border', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          {
            id: 'c1',
            type: 'change',
            title: 'Add timeline view',
            path: 'openspec/changes/c1',
            workspace: 'comet-panel',
            frontmatter: { created_at: '2026-01-01T00:00:00Z', phase: 'build' },
            updatedAt: '2026-01-05T00:00:00Z',
          },
        ],
        { c1: 0 },
      ),
    )
    render(<WikiTimeline />)

    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-bar')).toHaveLength(1))
    fireEvent.mouseEnter(screen.getByTestId('wiki-timeline-bar'), { clientX: 10, clientY: 10 })
    expect(screen.getByTestId('wiki-timeline-tooltip').textContent).toContain('Add timeline view')
    expect(screen.getByTestId('wiki-timeline-tooltip').textContent).toContain('build')

    const bar = screen.getByTestId('wiki-timeline-bar') as HTMLButtonElement
    vi.spyOn(bar, 'getBoundingClientRect').mockReturnValue({
      left: 100,
      right: 140,
      top: 4,
      bottom: 20,
      width: 40,
      height: 16,
      x: 100,
      y: 4,
      toJSON: () => ({}),
    })
    fireEvent.focus(bar)

    const focusTooltip = screen.getByTestId('wiki-timeline-tooltip')
    expect(focusTooltip.className).not.toContain('-translate-y-full')
    expect(focusTooltip.style.top).toBe('30px')
    expect(bar.className).toContain('focus-visible:border-[var(--color-text-primary)]')
    expect(bar.style.boxShadow).toBe('')
    // The bar's accent border now comes from the community's rank on the --viz-*
    // ramp rather than a fixed accent, so assert that a ranked community yields
    // its ramp colour instead of pinning one token name.
    expect(bar.style.getPropertyValue('--timeline-bar-border')).toMatch(/^var\(--viz-[1-8]\)$/)
  })

  it('falls back to an empty component list when the fetch fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    render(<WikiTimeline />)
    await waitFor(() => expect(screen.getByText('加载时间线数据失败')).toBeTruthy())
  })

  it('filters bars by workspace chip toggle', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          {
            id: 'c1',
            type: 'change',
            title: 'Add timeline view',
            path: 'openspec/changes/c1',
            workspace: 'comet-panel',
            frontmatter: { created_at: '2026-01-01T00:00:00Z', phase: 'build' },
            updatedAt: '2026-01-05T00:00:00Z',
          },
          {
            id: 'c2',
            type: 'change',
            title: 'Fix lint rule',
            path: 'openspec/changes/c2',
            workspace: 'other-repo',
            frontmatter: { created_at: '2026-02-01T00:00:00Z', phase: 'verify' },
            updatedAt: '2026-02-03T00:00:00Z',
          },
        ],
        { c1: 0, c2: 1 },
      ),
    )
    render(<WikiTimeline />)

    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-bar')).toHaveLength(2))
    const chips = screen.getAllByTestId('workspace-chip')
    const otherRepoChip = chips.find((element) => element.textContent === 'other-repo')!
    fireEvent.click(otherRepoChip)

    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-bar')).toHaveLength(1))
    expect(screen.getByTestId('graph-filter-summary').textContent).toContain('显示 1 / 2 项（口径：全部）')
  })

  it('navigates on Enter and Space from a timeline bar button', async () => {
    const onOpen = vi.fn()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse(
        [
          {
            id: 'c1',
            type: 'change',
            title: 'Add timeline view',
            path: 'openspec/changes/c1',
            workspace: 'comet-panel',
            frontmatter: { created_at: '2026-01-01T00:00:00Z', phase: 'build' },
            updatedAt: '2026-01-05T00:00:00Z',
          },
        ],
        { c1: 0 },
      ),
    )
    render(<WikiTimeline onOpen={onOpen} />)

    const bar = await waitFor(() => screen.getByTestId('wiki-timeline-bar'))
    fireEvent.keyDown(bar, { key: 'Enter' })
    fireEvent.keyDown(bar, { key: ' ' })

    expect(onOpen).toHaveBeenNthCalledWith(1, 'openspec/changes/c1')
    expect(onOpen).toHaveBeenNthCalledWith(2, 'openspec/changes/c1')
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

    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      mockGraphResponse([
        {
          id: 'c1',
          type: 'change',
          title: 'A',
          path: 'openspec/changes/c1',
          workspace: 'comet-panel',
          frontmatter: { created_at: '2026-01-01T00:00:00Z' },
          updatedAt: '2026-01-05T00:00:00Z',
        },
      ]),
    )
    render(<WikiTimeline />)

    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-bar')).toHaveLength(1))
    const callsBeforeEvent = fetchMock.mock.calls.length

    await act(async () => {
      MockEventSource.instance!.listeners['graph-updated']?.forEach((cb) => cb())
    })

    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(callsBeforeEvent))
    vi.unstubAllGlobals()
  })
})

describe('CalendarPanel', () => {
  it('groups selected-day items by workspace and supports keyboard navigation', async () => {
    const onOpen = vi.fn()
    vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      const url = String(input)
      if (url.includes('/api/wiki/calendar/month?')) {
        const parsed = new URL(url, 'http://localhost')
        const year = Number(parsed.searchParams.get('year'))
        const month = Number(parsed.searchParams.get('month'))
        return Promise.resolve(
          mockJsonResponse({
            year,
            month,
            days: { [`${year}-${String(month).padStart(2, '0')}-15`]: month === 1 ? 2 : 0 },
          }),
        )
      }
      if (url.includes('/api/wiki/calendar/day?')) {
        return Promise.resolve(
          mockJsonResponse([
            {
              id: 'doc-1',
              title: '发布说明',
              type: 'artifact',
              workspace: 'comet-panel',
              path: 'wiki/release-note.md',
              updatedAt: '2026-01-15T10:00:00Z',
            },
            {
              id: 'doc-2',
              title: '验证记录',
              type: 'spec',
              workspace: 'docs-site',
              path: 'wiki/verify.md',
              updatedAt: '2026-01-15T11:00:00Z',
            },
          ]),
        )
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    render(<CalendarPanel onOpen={onOpen} />)

    let dayButton: HTMLElement | undefined
    await waitFor(() => {
      dayButton = screen.getAllByRole('button').find((button) => button.textContent?.startsWith('15'))
      expect(dayButton).toBeTruthy()
    })
    fireEvent.click(dayButton!)

    await waitFor(() => expect(screen.getByText('发布说明')).toBeTruthy())
    expect(screen.getByText('comet-panel')).toBeTruthy()
    expect(screen.getByText('docs-site')).toBeTruthy()

    const itemButton = screen.getByRole('button', { name: /发布说明/ })
    fireEvent.keyDown(itemButton, { key: 'Enter' })
    fireEvent.keyDown(itemButton, { key: ' ' })

    expect(onOpen).toHaveBeenNthCalledWith(1, 'wiki/release-note.md')
    expect(onOpen).toHaveBeenNthCalledWith(2, 'wiki/release-note.md')
  })

  it('marks days that have artifacts with a dot and names the count for screen readers', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      const url = String(input)
      if (url.includes('/api/wiki/calendar/month?')) {
        const parsed = new URL(url, 'http://localhost')
        const year = Number(parsed.searchParams.get('year'))
        const month = Number(parsed.searchParams.get('month'))
        return Promise.resolve(
          mockJsonResponse({
            year,
            month,
            days: { [`${year}-${String(month).padStart(2, '0')}-15`]: 3 },
          }),
        )
      }
      return Promise.resolve(mockJsonResponse([]))
    })

    render(<CalendarPanel onOpen={vi.fn()} />)

    const marked = await waitFor(() => {
      const dot = document.querySelector('[data-testid^=calendar-dot-]')
      expect(dot).toBeTruthy()
      return dot!
    })
    // The heat marker is a dot, not a raw count printed under the date.
    const dayButton = marked.closest('button')!
    expect(dayButton.textContent).toBe('15')
    expect(dayButton.getAttribute('aria-label')).toContain('3 个产物')
    expect(dayButton.getAttribute('title')).toBe('3 个产物')

    const emptyDay = screen
      .getAllByRole('button')
      .find((button) => button.getAttribute('aria-label')?.includes('16日，无产物'))
    expect(emptyDay).toBeTruthy()
    expect(emptyDay!.querySelector('[data-testid^=calendar-dot-]')).toBeNull()
  })
})
