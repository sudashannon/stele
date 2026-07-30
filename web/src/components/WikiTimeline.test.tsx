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
    await waitFor(() => expect(screen.getByText('暂无变更数据')).toBeTruthy())
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
    expect(screen.getAllByTestId('wiki-timeline-bar')).toHaveLength(2)
    expect(screen.getAllByText('comet-panel').length).toBeGreaterThan(0)
    expect(screen.getAllByText('other-repo').length).toBeGreaterThan(0)
    expect(screen.getAllByText('交付节奏').length).toBeGreaterThan(0)
    expect(screen.getAllByText('质量治理').length).toBeGreaterThan(0)
    expect(screen.queryByText('无关社区')).toBeNull()
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

    await waitFor(() => expect(screen.getAllByTestId('wiki-timeline-community-legend-item')).toHaveLength(12))
    expect(screen.getByTestId('wiki-timeline-community-overflow').textContent).toContain('另有 2 个社区')
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
    expect(bar.style.getPropertyValue('--timeline-bar-border')).toBe('var(--color-accent)')
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
    expect(screen.getByTestId('graph-filter-summary').textContent).toContain('显示 1 / 2 条变更')
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
