import { act, render, screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { MarkdownViewer } from './MarkdownViewer'

afterEach(() => vi.restoreAllMocks())

if (typeof globalThis.CSS === 'undefined') {
  Object.defineProperty(globalThis, 'CSS', {
    value: { escape: (value: string) => value.replace(/[^a-zA-Z0-9_-]/g, (ch) => `\\${ch}`) },
    writable: true,
  })
}

describe('MarkdownViewer', () => {
  it('renders nothing when path is null', () => {
    const { container } = render(<MarkdownViewer path={null} onClose={vi.fn()} />)
    expect(container.firstChild).toBeNull()
  })

  it('fetches and renders markdown content for the given path', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      text: async () => '# Hello\n\nSome **body** text.',
    } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())
    expect(screen.getByText('body')).toBeTruthy()
    expect(screen.getByText('只读文档')).toBeTruthy()
  })

  it('strips a leading YAML frontmatter block before rendering', async () => {
    const raw = '---\ncomet_change: foo\nrole: technical-design\ncanonical_spec: openspec\n---\n# Real Title\n\nBody.'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => raw } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Real Title' })).toBeTruthy())
    expect(screen.queryByText('comet_change: foo')).toBeNull()
    expect(screen.queryByText('canonical_spec: openspec')).toBeNull()
  })

  it('calls onClose when the close button is clicked', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Hello' } as Response)

    const onClose = vi.fn()
    render(<MarkdownViewer path="/x/design.md" onClose={onClose} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('does not render a star button when onToggleStar is omitted', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Hello' } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())

    expect(screen.queryByRole('button', { name: '收藏' })).toBeNull()
    expect(screen.queryByRole('button', { name: '取消收藏' })).toBeNull()
  })

  it('shows an unstarred button and calls onToggleStar with path and filename', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Hello' } as Response)

    const onToggleStar = vi.fn()
    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} onToggleStar={onToggleStar} isStarred={false} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())

    const starButton = screen.getByRole('button', { name: '收藏' })
    expect(starButton.textContent).toContain('收藏')
    fireEvent.click(starButton)
    expect(onToggleStar).toHaveBeenCalledWith('/x/design.md', 'design.md')
  })

  it('shows a filled star and aria-pressed when isStarred is true', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Hello' } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} onToggleStar={vi.fn()} isStarred={true} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())

    const starButton = screen.getByRole('button', { name: '取消收藏' })
    expect(starButton.textContent).toContain('已收藏')
    expect(starButton.getAttribute('aria-pressed')).toBe('true')
  })

  it('calls summarizeDocument and renders the returned summary', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/wiki/summarize')) {
        return { ok: true, json: async () => ({ summary: '这是一段摘要。' }) } as Response
      }
      return { ok: true, text: async () => '# Hello' } as Response
    })

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())

    fireEvent.click(screen.getByTestId('summary-btn'))

    await waitFor(() => expect(screen.getByTestId('markdown-summary').textContent).toContain('这是一段摘要。'))
  })

  it('scrolls the summary into view and announces it, since it renders above the body', async () => {
    const scrollIntoView = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoView
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/wiki/summarize')) {
        return { ok: true, json: async () => ({ summary: '摘要正文' }) } as Response
      }
      return { ok: true, text: async () => '# Hello\n\nbody' } as Response
    })

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())
    scrollIntoView.mockClear()

    fireEvent.click(screen.getByTestId('summary-btn'))

    // Pressing the button while scrolled into the body used to change nothing
    // on screen: the block lands at the top of the scroll container.
    const section = await waitFor(() => screen.getByTestId('markdown-summary'))
    expect(scrollIntoView).toHaveBeenCalled()
    expect(section.getAttribute('role')).toBe('status')
    expect(section.getAttribute('aria-live')).toBe('polite')
    await waitFor(() => expect(section.textContent).toContain('摘要正文'))
  })

  it('restores an already cached summary when the document opens, without generating one', async () => {
    const calls: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      calls.push(url)
      if (url.includes('/api/wiki/summary?')) {
        return { ok: true, status: 200, json: async () => ({ summary: '缓存里的摘要' }) } as Response
      }
      return { ok: true, text: async () => '# Hello\n\nbody' } as Response
    })

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByTestId('markdown-summary').textContent).toContain('缓存里的摘要'))
    // The read-only endpoint answers the probe; the generating one is untouched.
    expect(calls.some((url) => url.includes('/api/wiki/summary?'))).toBe(true)
    expect(calls.some((url) => url.includes('/api/wiki/summarize'))).toBe(false)
  })

  it('keeps the summary hidden when nothing is cached yet', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/wiki/summary?')) {
        return { ok: false, status: 204, json: async () => ({}) } as Response
      }
      return { ok: true, text: async () => '# Hello\n\nbody' } as Response
    })

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())
    expect(screen.queryByTestId('markdown-summary')).toBeNull()
    expect(screen.getByTestId('summary-btn').textContent).toContain('生成摘要')
  })

  it('clears the previous summary and ignores its stale response when refreshing document content', async () => {
    let contentRequest = 0
    const summaryResponse = Promise.withResolvers<Response>()
    vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      const url = String(input)
      if (url.includes('/api/wiki/summarize')) {
        return summaryResponse.promise
      }
      // The viewer also probes the read-only summary cache on open; it must not
      // be counted as a content fetch.
      if (url.includes('/api/wiki/summary?')) {
        return Promise.resolve({ ok: false, status: 204, json: async () => ({}) } as Response)
      }
      contentRequest += 1
      return Promise.resolve({
        ok: true,
        text: async () => contentRequest === 1 ? '# Old document' : '# Refreshed document',
      } as Response)
    })

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Old document' })).toBeTruthy())
    fireEvent.click(screen.getByTestId('summary-btn'))
    await waitFor(() => expect(screen.getByTestId('markdown-summary').textContent).toContain('正在生成摘要'))

    fireEvent.click(screen.getByTestId('refresh-btn'))

    await waitFor(() => expect(screen.queryByTestId('markdown-summary')).toBeNull())
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Refreshed document' })).toBeTruthy())
    await act(async () => {
      summaryResponse.resolve({ ok: true, json: async () => ({ summary: '旧正文的摘要' }) } as Response)
      await summaryResponse.promise
    })
    await waitFor(() => expect(screen.queryByText('旧正文的摘要')).toBeNull())
  })

  it('clears the previous summary and ignores stale summary responses when switching documents', async () => {
    let resolveFirst!: (value: Response) => void
    let resolveSecond!: (value: Response) => void
    vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      const url = String(input)
      if (url.includes('/api/wiki/summarize?id=%2Fx%2Fa.md')) {
        return new Promise((resolve) => {
          resolveFirst = resolve
        }) as Promise<Response>
      }
      if (url.includes('/api/wiki/summarize?id=%2Fx%2Fb.md')) {
        return new Promise((resolve) => {
          resolveSecond = resolve
        }) as Promise<Response>
      }
      return Promise.resolve({ ok: true, text: async () => '# Doc' } as Response)
    })

    const { rerender } = render(<MarkdownViewer path="/x/a.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Doc' })).toBeTruthy())

    fireEvent.click(screen.getByTestId('summary-btn'))
    await waitFor(() => expect(screen.getByTestId('markdown-summary').textContent).toContain('正在生成摘要'))

    rerender(<MarkdownViewer path="/x/b.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.queryByTestId('markdown-summary')).toBeNull())

    resolveFirst({ ok: true, json: async () => ({ summary: '旧摘要' }) } as Response)
    await waitFor(() => expect(screen.queryByText('旧摘要')).toBeNull())

    fireEvent.click(screen.getByTestId('summary-btn'))
    resolveSecond({ ok: true, json: async () => ({ summary: '新摘要' }) } as Response)
    await waitFor(() => expect(screen.getByTestId('markdown-summary').textContent).toContain('新摘要'))
    expect(screen.queryByText('旧摘要')).toBeNull()
  })

  it('renders a GFM table as real table markup, not raw pipe text', async () => {
    const table = '| A | B |\n| --- | --- |\n| 1 | 2 |'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => table } as Response)

    const { container } = render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByText('A')).toBeTruthy())
    expect(container.querySelector('table')).not.toBeNull()
    expect(container.querySelectorAll('td').length).toBe(2)
  })

  it('rebases relative image paths to the artifact API under the current markdown directory', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '![架构图](diagrams/arch.png)' } as Response)

    const { container } = render(
      <MarkdownViewer
        path="/repo/knowledge/2026-07-15-nvstream-middleware-design.md"
        workspace="rx101"
        onClose={vi.fn()}
      />,
    )

    await waitFor(() => expect(container.querySelector('img')).not.toBeNull())
    const img = container.querySelector('img')!
    expect(img.getAttribute('src')).toBe('/api/artifact?path=%2Frepo%2Fknowledge%2Fdiagrams%2Farch.png&workspace=rx101')
  })

  it('rebases relative file links to the artifact API under the current markdown directory', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '[查看 SVG 源图](diagrams/arch.svg)' } as Response)

    render(
      <MarkdownViewer
        path="/repo/knowledge/2026-07-15-nvstream-middleware-design.md"
        workspace="rx101"
        onClose={vi.fn()}
      />,
    )

    const link = await screen.findByRole('link', { name: '查看 SVG 源图' })
    expect(link.getAttribute('href')).toBe('/api/artifact?path=%2Frepo%2Fknowledge%2Fdiagrams%2Farch.svg&workspace=rx101')
  })

  it('calls onClose when Escape is pressed', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Hello' } as Response)

    const onClose = vi.fn()
    render(<MarkdownViewer path="/x/design.md" onClose={onClose} />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('renders an artifact switcher when multiple artifacts are given, highlights the current one, and switches in place on click', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      const text = url.includes('design.md') ? '# 设计文档' : '# 任务清单'
      return { ok: true, text: async () => text } as Response
    })

    const artifacts = [
      { path: '/x/design.md', label: '设计文档' },
      { path: '/x/tasks.md', label: '任务清单' },
    ]
    const onClose = vi.fn()
    const onSelectArtifact = vi.fn()
    render(<MarkdownViewer path="/x/design.md" artifacts={artifacts} onSelectArtifact={onSelectArtifact} onClose={onClose} />)

    await waitFor(() => expect(screen.getByText('设计文档')).toBeTruthy())

    const switcher = screen.getByTestId('artifact-switcher')
    const currentButton = screen.getAllByText('设计文档').find((el) => switcher.contains(el))!
    const otherButton = screen.getAllByText('任务清单').find((el) => switcher.contains(el))!
    expect(currentButton.getAttribute('aria-current')).toBe('true')
    expect(otherButton.getAttribute('aria-current')).toBe('false')

    otherButton.click()
    expect(onSelectArtifact).toHaveBeenCalledWith('/x/tasks.md')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('does not render the switcher with fewer than two artifacts', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Hello' } as Response)

    render(
      <MarkdownViewer
        path="/x/design.md"
        artifacts={[{ path: '/x/design.md', label: '设计文档' }]}
        onSelectArtifact={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Hello' })).toBeTruthy())
    expect(screen.queryByTestId('artifact-switcher')).toBeNull()
  })

  it('renders a TOC nav listing heading text and jumps to the heading on click', async () => {
    Element.prototype.scrollIntoView = vi.fn()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# A\n\nintro\n\n## B\n\nmiddle\n\n### C\n\nend' } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    const nav = await screen.findByTestId('markdown-toc')

    expect(screen.getByRole('button', { name: 'A' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'B' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'C' })).toBeTruthy()

    const entryB = screen.getAllByText('B').find((el) => nav.contains(el))
    expect(entryB).toBeTruthy()
    entryB!.click()

    expect(Element.prototype.scrollIntoView).toHaveBeenCalledTimes(1)
  })

  it('does not render a TOC nav for zero or exactly one heading', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => 'just a paragraph, no headings at all' } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByText(/just a paragraph/)).toBeTruthy())
    expect(screen.queryByTestId('markdown-toc')).toBeNull()
  })

  it('still does not render a TOC nav with a single heading', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Only Heading\n\nbody text' } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('body text')).toBeTruthy())
    expect(screen.queryByTestId('markdown-toc')).toBeNull()
  })

  it('strips inline markdown from a heading to produce a clean TOC label', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '# Title\n\n## `foo` bar\n\nbody' } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await screen.findByTestId('markdown-toc')

    expect(screen.getByRole('button', { name: 'foo bar' })).toBeTruthy()
    expect(screen.queryByText('`foo` bar')).toBeNull()
  })

  it('opens an image lightbox on click, closes on overlay click, and does not close when clicking the enlarged image', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '![diagram](http://x/a.svg)\n\nsome text' } as Response)

    render(<MarkdownViewer path="/x/design.md" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('some text')).toBeTruthy())

    // The thumbnail is wrapped in a real button so the zoom has a keyboard route;
    // the zoom affordance therefore lives on the control, not on the <img>.
    const thumb = screen.getByRole('img', { name: 'diagram' })
    const zoomControl = thumb.closest('button')
    expect(zoomControl).toBeTruthy()
    expect(zoomControl!.className).toContain('cursor-zoom-in')
    expect(screen.queryByTestId('image-lightbox')).toBeNull()

    fireEvent.click(zoomControl!)

    const lightbox = screen.getByTestId('image-lightbox')
    expect(lightbox.getAttribute('role')).toBe('dialog')
    const enlarged = screen.getAllByRole('img', { name: 'diagram' }).find((el) => lightbox.contains(el))
    expect(enlarged).toBeTruthy()
    expect(enlarged!.getAttribute('src')).toBe('http://x/a.svg')

    fireEvent.click(enlarged!)
    expect(screen.queryByTestId('image-lightbox')).not.toBeNull()
    fireEvent.click(lightbox)
    expect(screen.queryByTestId('image-lightbox')).toBeNull()
  })

  it('closes the lightbox on Escape without closing the viewer, then closes the viewer on the next Escape', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => '![diagram](http://x/a.svg)\n\nsome text' } as Response)

    const onClose = vi.fn()
    render(<MarkdownViewer path="/x/design.md" onClose={onClose} />)
    await waitFor(() => expect(screen.getByText('some text')).toBeTruthy())

    fireEvent.click(screen.getByRole('img', { name: 'diagram' }))
    expect(screen.getByTestId('image-lightbox')).toBeTruthy()

    fireEvent(window, new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(screen.queryByTestId('image-lightbox')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()

    fireEvent(window, new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('renders a create-todo button and calls onCreateTodo when clicked', async () => {
    const markdown = '# Test Doc\n\nSome content.'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => markdown } as Response)
    const onCreateTodo = vi.fn()
    render(<MarkdownViewer path="/x/test.md" onClose={() => {}} onCreateTodo={onCreateTodo} />)
    const btn = await screen.findByTestId('create-todo-btn')
    fireEvent.click(btn)
    expect(onCreateTodo).toHaveBeenCalledTimes(1)
  })

  it('does not render create-todo button when onCreateTodo is omitted', async () => {
    const markdown = '# Test Doc\n\nSome content.'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, text: async () => markdown } as Response)
    render(<MarkdownViewer path="/x/test.md" onClose={() => {}} />)
    await screen.findByRole('button', { name: '关闭' })
    expect(screen.queryByTestId('create-todo-btn')).toBeNull()
  })
})
