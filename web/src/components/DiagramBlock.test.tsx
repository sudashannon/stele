import { act, render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import mermaid from 'mermaid'
import { DiagramBlock } from './DiagramBlock'

vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn(),
  },
}))

afterEach(() => vi.restoreAllMocks())

describe('DiagramBlock', () => {
  it('does not initialize mermaid at module load and lazy-loads it only for mermaid blocks', async () => {
    expect(mermaid.initialize).not.toHaveBeenCalled()
    vi.mocked(mermaid.render).mockResolvedValue({
      svg: '<svg data-testid="fake-mermaid-svg"></svg>',
      diagramType: 'flowchart-v2',
    })

    const { container } = render(<DiagramBlock language="mermaid" code="graph TD;A-->B" />)

    await waitFor(() => expect(mermaid.initialize).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(container.querySelector('[data-testid="fake-mermaid-svg"]')).toBeTruthy(),
    )
    expect(mermaid.render).toHaveBeenCalledWith(expect.stringMatching(/^mermaid-/), 'graph TD;A-->B')
  })

  it('shows a visible fallback when mermaid.render fails', async () => {
    vi.mocked(mermaid.render).mockRejectedValue(new Error('parse error'))

    render(<DiagramBlock language="mermaid" code="invalid mermaid syntax" />)

    await waitFor(() => expect(screen.getByText('Mermaid 图表渲染失败，已显示源码。')).toBeTruthy())
    expect(screen.getByText('invalid mermaid syntax')).toBeTruthy()
  })

  it('renders a plantuml diagram by fetching the SVG from Kroki', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      text: async () => '<svg data-testid="fake-kroki-svg"></svg>',
    } as Response)

    const { container } = render(
      <DiagramBlock language="plantuml" code={'@startuml\nAlice -> Bob\n@enduml'} />,
    )

    await waitFor(() =>
      expect(container.querySelector('[data-testid="fake-kroki-svg"]')).toBeTruthy(),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('https://kroki.io/plantuml/svg/'),
      { signal: expect.any(AbortSignal) },
    )
  })

  it('shows a visible fallback when the Kroki request fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network error'))

    render(<DiagramBlock language="plantuml" code="@startuml Alice -> Bob @enduml" />)

    await waitFor(() => expect(screen.getByText('PlantUML 图表渲染失败，已显示源码。')).toBeTruthy())
    expect(screen.getByText('@startuml Alice -> Bob @enduml')).toBeTruthy()
  })

  it('aborts PlantUML requests when code changes and on unmount', async () => {
    const requests: Array<PromiseWithResolvers<Response>> = []
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(() => {
      const request = Promise.withResolvers<Response>()
      requests.push(request)
      return request.promise
    })

    const { rerender, unmount } = render(
      <DiagramBlock language="plantuml" code="@startuml A -> B @enduml" />,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const firstSignal = fetchMock.mock.calls[0][1]?.signal

    rerender(<DiagramBlock language="plantuml" code="@startuml C -> D @enduml" />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(firstSignal?.aborted).toBe(true)

    const secondSignal = fetchMock.mock.calls[1][1]?.signal
    unmount()
    expect(secondSignal?.aborted).toBe(true)
  })

  it('does not show a failure fallback for an aborted PlantUML request', async () => {
    const request = Promise.withResolvers<Response>()
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => request.promise)
    render(<DiagramBlock language="plantuml" code="@startuml A -> B @enduml" />)
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled())

    const abortError = new Error('aborted')
    abortError.name = 'AbortError'
    await act(async () => request.reject(abortError))

    expect(screen.queryByText('PlantUML 图表渲染失败，已显示源码。')).toBeNull()
    expect(screen.getByText('正在加载图表…')).toBeTruthy()
  })
})
