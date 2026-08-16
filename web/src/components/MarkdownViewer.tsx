import { Children, isValidElement, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSlug from 'rehype-slug'
import { fetchArtifactContent, fetchCachedSummary, summarizeDocument } from '../api/client'
import { parseMarkdownDocument, type MarkdownDocumentModel } from './markdownDocument'
import { DiagramBlock } from './DiagramBlock'
import { ShareModal } from './ShareModal'
import { Icon } from './icons'
import { BacklinksPanel } from './BacklinksPanel'
import { StateBlock } from './StateBlock'

interface MarkdownNodePosition {
  position?: {
    start?: { line?: number }
    end?: { line?: number }
  }
}

// ReactMarkdown parses the body after frontmatter has been removed, so offset
// its AST positions back to the original file for source-level navigation.
function sourceAttributes(node: unknown, lineOffset: number): Record<string, string> {
  const position = (node as MarkdownNodePosition | null)?.position
  const start = position?.start?.line
  const end = position?.end?.line
  if (typeof start !== 'number' || typeof end !== 'number') return {}
  return {
    'data-source-start': String(start + lineOffset),
    'data-source-end': String(end + lineOffset),
  }
}

function resolveArtifactHref(docPath: string | null, href: string | undefined, workspace: string | undefined): string | undefined {
  if (!href || href.startsWith('#') || /^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(href)) return href
  if (!docPath) return href
  const base = docPath.slice(0, docPath.lastIndexOf('/') + 1)
  const target = href.startsWith('/') ? href : `${base}${href}`
  const params = new URLSearchParams({ path: target })
  if (workspace) params.set('workspace', workspace)
  return `/api/artifact?${params.toString()}`
}

type CopyHandler = (text: string, label: string) => void
const editableTextExtensions: Record<string, true> = {
  '.md': true,
  '.markdown': true,
  '.mdx': true,
  '.txt': true,
  '.json': true,
  '.yaml': true,
  '.yml': true,
  '.toml': true,
}

function isEditableTextArtifact(path: string | null): boolean {
  if (!path) return false
  const dot = path.lastIndexOf('.')
  return dot >= 0 && editableTextExtensions[path.slice(dot).toLowerCase()] === true
}


function textFromReactNode(node: ReactNode): string {
  return Children.toArray(node)
    .map((child) => {
      if (typeof child === 'string' || typeof child === 'number') return String(child)
      if (isValidElement<{ children?: ReactNode }>(child)) return textFromReactNode(child.props.children)
      return ''
    })
    .join('')
}

function languageFromClassName(className: string | undefined): string {
  return className?.startsWith('language-') ? className.slice('language-'.length) : 'text'
}

function createMarkdownComponents(lineOffset: number, onCopy: CopyHandler): Components {
  return {
    h1: ({ node, ...rest }) => <h1 className="mb-3 mt-5 text-2xl font-bold" {...rest} {...sourceAttributes(node, lineOffset)} />,
    h2: ({ node, ...rest }) => <h2 className="mb-2 mt-5 text-xl font-semibold" {...rest} {...sourceAttributes(node, lineOffset)} />,
    h3: ({ node, ...rest }) => <h3 className="mb-2 mt-4 text-lg font-semibold" {...rest} {...sourceAttributes(node, lineOffset)} />,
    h4: ({ node, ...rest }) => <h4 className="mb-2 mt-4 text-base font-semibold" {...rest} {...sourceAttributes(node, lineOffset)} />,
    h5: ({ node, ...rest }) => <h5 className="mb-2 mt-3 text-[length:var(--type-body)] font-semibold" {...rest} {...sourceAttributes(node, lineOffset)} />,
    h6: ({ node, ...rest }) => <h6 className="mb-2 mt-3 text-[length:var(--type-body)] font-medium" {...rest} {...sourceAttributes(node, lineOffset)} />,
    p: ({ node, ...rest }) => <p className="mb-3 text-[length:var(--type-body-short)] leading-[var(--leading-body-short)]" {...rest} {...sourceAttributes(node, lineOffset)} />,
    ul: ({ node, ...rest }) => <ul className="mb-3 list-disc pl-6" {...rest} {...sourceAttributes(node, lineOffset)} />,
    ol: ({ node, ...rest }) => <ol className="mb-3 list-decimal pl-6" {...rest} {...sourceAttributes(node, lineOffset)} />,
    li: ({ node, ...rest }) => <li className="mb-1" {...rest} {...sourceAttributes(node, lineOffset)} />,
    blockquote: ({ node, ...rest }) => (
      <blockquote
        className="mb-3 border-l-4 border-[var(--color-border)] py-1 pl-4 italic text-[var(--color-text-secondary)]"
        {...rest}
        {...sourceAttributes(node, lineOffset)}
      />
    ),
    hr: ({ node, ...rest }) => <hr className="my-6 border-[var(--color-border)]" {...rest} {...sourceAttributes(node, lineOffset)} />,
    table: ({ node, ...rest }) => (
      <div className="mb-4 overflow-x-auto" {...sourceAttributes(node, lineOffset)}>
        <table className="w-full border-collapse text-left" {...rest} />
      </div>
    ),
    thead: ({ node, ...rest }) => <thead className="bg-[var(--color-bg)]" {...rest} />,
    tbody: ({ node, ...rest }) => <tbody {...rest} />,
    tr: ({ node, ...rest }) => <tr className="border-b border-[var(--color-border)]" {...rest} />,
    th: ({ node, ...rest }) => (
      <th className="whitespace-nowrap border border-[var(--color-border)] px-3 py-2 font-semibold" {...rest} />
    ),
    td: ({ node, ...rest }) => <td className="border border-[var(--color-border)] px-3 py-2 align-top" {...rest} />,
    code: ({ node, className, children, ...rest }) => {
      const language = className === 'language-mermaid' ? 'mermaid' : className === 'language-plantuml' ? 'plantuml' : null
      if (language) return <DiagramBlock language={language} code={String(children).replace(/\n$/, '')} />
      return (
        <code className={`break-words bg-[var(--color-layer)] px-1 py-0.5 font-[var(--font-mono)] text-[length:var(--type-caption)] ${className ?? ''}`} {...rest}>
          {children}
        </code>
      )
    },
    pre: ({ node, children }) => {
      const code = Children.toArray(children)[0]
      const codeProps = isValidElement<{ className?: string; code?: string }>(code) ? code.props : undefined
      const className = codeProps?.className
      const source = codeProps?.code ?? textFromReactNode(children)
      return (
        <div className="mb-4 overflow-hidden border border-[var(--color-border)]" data-testid="markdown-code-block" {...sourceAttributes(node, lineOffset)}>
          <div className="flex items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-layer)] px-3 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
            <span className="font-[var(--font-mono)]">{languageFromClassName(className)}</span>
            <button
              type="button"
              onClick={() => onCopy(source, '代码')}
              className="border border-[var(--color-border)] px-2 py-0.5 hover:border-[var(--color-accent)] hover:text-[var(--color-accent)]"
            >
              复制
            </button>
          </div>
          <pre className="overflow-x-auto bg-[var(--color-layer)] p-4 font-[var(--font-mono)] text-[length:var(--type-caption)] whitespace-pre">{children}</pre>
        </div>
      )
    },
  }
}

interface Artifact {
  path: string
  label: string
}

interface Props {
  path: string | null
  body?: string
  artifacts?: Artifact[]
  workspace?: string
  onSelectArtifact?: (path: string) => void
  onClose: () => void
  onToggleStar?: (path: string, title: string) => void
  isStarred?: boolean
  onNavigateToChange?: (changeName: string) => void
  onCreateTodo?: () => void
  /** Switches a real artifact viewer to source editing. */
  onEdit?: () => void
  /** Opens an agent session from the 相关会话 block. */
  onOpenSession?: (sessionId: string) => void
}

type SummaryState =
  | { status: 'idle' }
  | { status: 'loading' }
  // `auto` marks a summary restored from the cache on open rather than one the
  // user just asked for: it must appear silently instead of yanking the scroll.
  | { status: 'ready'; text: string; auto?: boolean }
  | { status: 'error'; message: string }
type ViewerMode = 'rendered' | 'source'

interface SearchMatch {
  line: number
  snippet: string
}


export function MarkdownViewer({
  path,
  body,
  artifacts,
  workspace,
  onSelectArtifact,
  onClose,
  onToggleStar,
  isStarred,
  onNavigateToChange,
  onCreateTodo,
  onEdit,
  onOpenSession,
}: Props) {
  const [content, setContent] = useState<string | null>(body ?? null)
  const [error, setError] = useState<string | null>(null)
  const [zoomed, setZoomed] = useState<{ src: string; alt: string } | null>(null)
  const [shareOpen, setShareOpen] = useState(false)
  const [frontmatterOpen, setFrontmatterOpen] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [summary, setSummary] = useState<SummaryState>({ status: 'idle' })
  const [viewerMode, setViewerMode] = useState<ViewerMode>('rendered')
  const [tocOpen, setTocOpen] = useState(true)
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchMatchIndex, setSearchMatchIndex] = useState(0)
  const [copyFeedback, setCopyFeedback] = useState<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const summaryRef = useRef<HTMLElement>(null)
  const fetchRequestRef = useRef(0)
  const summaryRequestRef = useRef(0)
  const documentModel = useMemo<MarkdownDocumentModel | null>(
    () => (content === null ? null : parseMarkdownDocument(content)),
    [content],
  )
  const docTitle = path
    ? documentModel?.frontmatter?.fields.title?.trim() || documentModel?.headings.find((heading) => heading.level === 1)?.text || ''
    : ''
  async function copyText(text: string, label: string) {
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(text)
      setCopyFeedback(`${label}已复制`)
    } catch {
      setCopyFeedback(`${label}复制失败`)
    }
  }
  const searchMatches = useMemo<SearchMatch[]>(() => {
    if (!documentModel || searchQuery.trim() === '') return []
    const query = searchQuery.trim().toLocaleLowerCase()
    const matches: SearchMatch[] = []
    for (const [index, line] of documentModel.body.split(/\r?\n/).entries()) {
      const lowerLine = line.toLocaleLowerCase()
      let offset = 0
      while (offset < lowerLine.length) {
        const match = lowerLine.indexOf(query, offset)
        if (match < 0) break
        const start = Math.max(0, match - 28)
        const end = Math.min(line.length, match + query.length + 28)
        matches.push({ line: documentModel.bodyStartLine + index, snippet: line.slice(start, end).trim() })
        offset = match + Math.max(query.length, 1)
      }
    }
    return matches
  }, [documentModel, searchQuery])
  const [activeHeading, setActiveHeading] = useState<string | null>(null)

  useEffect(() => {
    if (body !== undefined) {
      fetchRequestRef.current += 1
      setContent(body)
      setFrontmatterOpen(false)
      setViewerMode('rendered')
      setSearchOpen(false)
      setSearchQuery('')
      setCopyFeedback(null)
      setError(null)
      setZoomed(null)
      return
    }
    if (!path) return
    const requestId = ++fetchRequestRef.current
    setContent(null)
    setFrontmatterOpen(false)
    setViewerMode('rendered')
    setSearchOpen(false)
    setSearchQuery('')
    setCopyFeedback(null)
    setError(null)
    setZoomed(null)
    fetchArtifactContent(path, workspace)
      .then((text) => {
        if (fetchRequestRef.current === requestId) setContent(text)
      })
      .catch((err) => {
        if (fetchRequestRef.current === requestId) setError(err instanceof Error ? err.message : '加载失败')
      })
  }, [path, body, workspace, refreshKey])

  useEffect(() => {
    summaryRequestRef.current += 1
    setSummary({ status: 'idle' })
  }, [path, body, refreshKey])

  // Restore an already-generated summary when the document opens. This probes
  // the read-only cache endpoint, never the generating one, so opening a
  // document costs nothing; a miss simply leaves the 生成摘要 button waiting.
  // No request id is minted here: the reset effect above owns the counter, so a
  // response that lands after the reader moved on is discarded.
  useEffect(() => {
    if (!path) return
    const requestId = summaryRequestRef.current
    const controller = new AbortController()
    fetchCachedSummary(path, controller.signal)
      .then((text) => {
        if (!text || summaryRequestRef.current !== requestId) return
        setSummary({ status: 'ready', text, auto: true })
      })
      .catch(() => {
        // A failed probe must stay silent — the manual button still works.
      })
    return () => controller.abort()
  }, [path, body, refreshKey])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (searchOpen) setSearchOpen(false)
      else if (zoomed) setZoomed(null)
      else onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [path, onClose, searchOpen, zoomed])

  const toc = useMemo(
    () => documentModel?.headings.filter((heading) => heading.level <= 3) ?? [],
    [documentModel],
  )
  const components = useMemo<Components>(
    () => ({
      ...createMarkdownComponents(Math.max(0, (documentModel?.bodyStartLine ?? 1) - 1), copyText),
      a: ({ node, href, ...rest }) => (
        <a
          {...rest}
          {...sourceAttributes(node, Math.max(0, (documentModel?.bodyStartLine ?? 1) - 1))}
          href={resolveArtifactHref(path, href, workspace)}
          className="text-[var(--color-accent)] underline"
          target="_blank"
          rel="noreferrer"
        />
      ),
      img: ({ node, src, alt, ...rest }) => {
        const resolvedSrc = typeof src === 'string' ? resolveArtifactHref(path, src, workspace) : src
        return (
          <button
            type="button"
            className="cursor-zoom-in"
            {...sourceAttributes(node, Math.max(0, (documentModel?.bodyStartLine ?? 1) - 1))}
            onClick={() => typeof resolvedSrc === 'string' && setZoomed({ src: resolvedSrc, alt: alt ?? '' })}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                if (typeof resolvedSrc === 'string') setZoomed({ src: resolvedSrc, alt: alt ?? '' })
              }
            }}
          >
            <img {...rest} src={resolvedSrc} alt={alt} className="max-w-full" />
          </button>
        )
      },
    }),
    [copyText, documentModel, path, workspace],
  )

  // The summary renders at the very top of the scroll container, so pressing
  // 生成摘要 while reading further down produced no visible change at all: the
  // "正在生成…" placeholder and the finished text both land off-screen and the
  // button label is the only hint. Pull the block into view as soon as it
  // exists. The optional call keeps jsdom (no scrollIntoView) happy.
  useEffect(() => {
    if (summary.status === 'idle') return
    if (summary.status === 'ready' && summary.auto) return
    summaryRef.current?.scrollIntoView?.({ block: 'start' })
  }, [summary])

  // Track which heading is in the viewport so the TOC rail can highlight the
  // active entry. IntersectionObserver reports every intersecting heading; we
  // pick the one whose top is closest to the scroll container's top edge.
  useEffect(() => {
    const container = scrollRef.current
    if (!container || !content) return
    // Highlighting the active entry is progressive enhancement: the TOC still
    // navigates without it. Environments without IntersectionObserver (jsdom,
    // older embedded webviews) must degrade, not throw and take the whole
    // document viewer down with them.
    if (typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .map((e) => ({ id: e.target.id, top: e.boundingClientRect.top }))
          .sort((a, b) => a.top - b.top)
        if (visible.length > 0) setActiveHeading(visible[0].id)
      },
      { root: container, rootMargin: '-10% 0px -70% 0px', threshold: 0 },
    )
    const headings = container.querySelectorAll('h1[id], h2[id], h3[id]')
    headings.forEach((h) => observer.observe(h))
    return () => observer.disconnect()
  }, [content])

  if (!path && body === undefined) return null

  const filename = path ? path.split('/').pop() ?? path : '报告'
  const displayTitle = docTitle && docTitle !== filename ? docTitle : filename
  const changeName = path ? path.match(/\/changes\/([^\/]+)\//)?.[1] ?? null : null

  async function handleSummarize() {
    if (!path) return
    const requestId = ++summaryRequestRef.current
    setSummary({ status: 'loading' })
    try {
      const text = await summarizeDocument(path)
      if (summaryRequestRef.current === requestId) setSummary({ status: 'ready', text })
    } catch (err) {
      if (summaryRequestRef.current === requestId) {
        setSummary({ status: 'error', message: err instanceof Error ? err.message : '摘要生成失败' })
      }
    }
  }

  function jumpTo(id: string) {
    const element = scrollRef.current?.querySelector(`#${CSS.escape(id)}`)
    if (element) element.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
  function jumpToSearchMatch(index: number) {
    if (searchMatches.length === 0) return
    const nextIndex = (index + searchMatches.length) % searchMatches.length
    setSearchMatchIndex(nextIndex)
    const line = searchMatches[nextIndex].line
    const elements = Array.from(scrollRef.current?.querySelectorAll<HTMLElement>('[data-source-start]') ?? [])
    const target = elements.find((element) => Number(element.dataset.sourceStart) >= line)
    target?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }

  async function copyTitleLink() {
    const heading = documentModel?.headings[0]
    if (!heading) return
    const url = `${window.location.href.split('#')[0]}#${heading.id}`
    await copyText(url, '标题链接')
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--color-surface)] shadow-[var(--shadow-1)]" role="region" aria-label={filename}>
      <header className="sticky top-0 z-10 flex flex-col gap-3 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-6 py-3">
        <div className="flex items-center justify-between gap-4">
          <div className="min-w-0 text-[length:var(--type-body)]" title={path ?? undefined}>
            <div className="flex items-center gap-2">
              <span className="font-semibold text-[var(--color-text-primary)]">{displayTitle}</span>
              <span className="border border-[var(--color-border)] bg-[var(--color-layer)] px-1.5 py-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                只读文档
              </span>
            </div>
            <div className="flex flex-wrap items-center gap-2 border-t border-[var(--color-border)] pt-2">
              {toc.length > 1 && (
                <button
                  type="button"
                  aria-pressed={tocOpen}
                  onClick={() => setTocOpen((value) => !value)}
                  className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)]"
                >
                  目录
                </button>
              )}
              <button
                type="button"
                aria-pressed={searchOpen}
                onClick={() => setSearchOpen((value) => !value)}
                data-testid="markdown-search-toggle"
                className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)]"
              >
                搜索
              </button>
              <button
                type="button"
                aria-pressed={viewerMode === 'source'}
                onClick={() => setViewerMode((value) => (value === 'source' ? 'rendered' : 'source'))}
                data-testid="markdown-source-toggle"
                className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)]"
              >
                {viewerMode === 'source' ? '阅读' : '源码'}
              </button>
              <button
                type="button"
                onClick={() => void copyTitleLink()}
                disabled={!documentModel?.headings.length}
                className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] disabled:opacity-50"
              >
                复制标题链接
              </button>
              {copyFeedback && <span role="status" className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{copyFeedback}</span>}
            </div>
            {searchOpen && (
              <div data-testid="markdown-search" className="mt-2 flex flex-wrap items-center gap-2">
                <input
                  autoFocus
                  type="search"
                  aria-label="搜索文档"
                  value={searchQuery}
                  onChange={(event) => {
                    setSearchQuery(event.target.value)
                    setSearchMatchIndex(0)
                  }}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') {
                      event.preventDefault()
                      jumpToSearchMatch(searchMatchIndex + (event.shiftKey ? -1 : 1))
                    }
                  }}
                  placeholder="搜索文档…"
                  className="min-w-40 flex-1 border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] outline-none focus:border-[var(--color-accent)]"
                />
                <span className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                  {searchMatches.length === 0 ? '无匹配' : `${searchMatchIndex + 1}/${searchMatches.length}`}
                </span>
                <button type="button" aria-label="上一个匹配" onClick={() => jumpToSearchMatch(searchMatchIndex - 1)} disabled={searchMatches.length === 0} className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] disabled:opacity-50">上一个</button>
                <button type="button" aria-label="下一个匹配" onClick={() => jumpToSearchMatch(searchMatchIndex + 1)} disabled={searchMatches.length === 0} className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)] disabled:opacity-50">下一个</button>
                <button type="button" aria-label="关闭搜索" onClick={() => setSearchOpen(false)} className="border border-[var(--color-border)] px-2 py-1 text-[length:var(--type-caption)]">关闭</button>
                {searchMatches.length > 0 && (
                  <div className="basis-full space-y-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                    <div className="font-semibold">匹配位置</div>
                    {searchMatches.slice(0, 5).map((match, index) => (
                      <button
                        key={`${match.line}-${index}`}
                        type="button"
                        onClick={() => jumpToSearchMatch(index)}
                        className="block max-w-full truncate text-left hover:text-[var(--color-accent)]"
                      >
                        L{match.line}: {match.snippet}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            {displayTitle !== filename && (
              <span className="mt-1 block truncate font-[var(--font-mono)] text-[length:var(--type-caption)] font-normal text-[var(--color-text-secondary)]">{path}</span>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {path && (
              <button
                type="button"
                data-testid="summary-btn"
                onClick={() => void handleSummarize()}
                disabled={summary.status === 'loading'}
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Icon name="info" />
                <span>{summary.status === 'loading' ? '摘要生成中…' : '生成摘要'}</span>
              </button>
            )}
            {isEditableTextArtifact(path) && onEdit && (
              <button
                type="button"
                aria-label="编辑"
                onClick={onEdit}
                data-testid="markdown-edit-btn"
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
              >
                <span>编辑</span>
              </button>
            )}
            {onToggleStar && path && (
              <button
                type="button"
                aria-label={isStarred ? '取消收藏' : '收藏'}
                aria-pressed={!!isStarred}
                onClick={() => onToggleStar(path, filename)}
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
              >
                <Icon name={isStarred ? 'star-filled' : 'star'} />
                <span>{isStarred ? '已收藏' : '收藏'}</span>
              </button>
            )}
            {path && (
              <button
                type="button"
                aria-label="刷新"
                onClick={() => {
                  setContent(null)
                  setRefreshKey((value) => value + 1)
                }}
                data-testid="refresh-btn"
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
              >
                <Icon name="refresh" />
                <span>刷新</span>
              </button>
            )}
            {onCreateTodo && (
              <button
                type="button"
                aria-label="添加待办"
                data-testid="create-todo-btn"
                onClick={onCreateTodo}
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
                title="添加待办"
              >
                <Icon name="todos" />
                <span>待办</span>
              </button>
            )}
            {changeName && onNavigateToChange && (
              <button
                type="button"
                aria-label="跳转到变更"
                onClick={() => onNavigateToChange(changeName)}
                data-testid="navigate-change-btn"
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
                title="跳转到变更视图"
              >
                <Icon name="open" />
                <span>变更</span>
              </button>
            )}
            {path && (
              <button
                type="button"
                aria-label="分享"
                onClick={() => setShareOpen(true)}
                data-testid="share-open-btn"
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
              >
                <Icon name="share" />
                <span>分享</span>
              </button>
            )}
            <button
              type="button"
              aria-label="关闭"
              onClick={onClose}
              className="flex items-center gap-1 border border-[var(--color-border)] px-3 py-1.5 text-[length:var(--type-caption)] font-medium text-[var(--color-accent)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
            >
              <Icon name="close" />
              <span>关闭</span>
            </button>
          </div>
        </div>
        {artifacts && artifacts.length > 1 && (
          <div data-testid="artifact-switcher" className="flex items-center gap-1.5 overflow-x-auto">
            {artifacts.map((artifact) => {
              const active = artifact.path === path
              return (
                <button
                  key={artifact.path}
                  type="button"
                  aria-current={active}
                  onClick={() => !active && onSelectArtifact?.(artifact.path)}
                  className={
                    'shrink-0 whitespace-nowrap border px-2.5 py-1 text-[length:var(--type-caption)] ' +
                    (active
                      ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
                      : 'border-[var(--color-border)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:text-[var(--color-accent)]')
                  }
                >
                  {artifact.label}
                </button>
              )
            })}
          </div>
        )}
      </header>
      <div className="flex min-h-0 flex-1">
        {tocOpen && toc.length > 1 && (
          <nav
            data-testid="markdown-toc"
            aria-label="文档目录"
            // Hidden below the app's `narrow` gate (1200px) so a cramped viewport
            // keeps full reading width. Uses the shared breakpoint rather than a
            // component-local one, so the system stays at two gates.
            className="toc-rail hidden w-48 shrink-0 overflow-y-auto border-r border-[var(--color-border)] px-3 py-6 min-[1200px]:block"
          >
            <div className="mb-2 px-2 text-[length:var(--type-caption)] font-semibold text-[var(--color-text-tertiary)]">目录</div>
            <ul className="space-y-0.5">
              {toc.map((entry, index) => {
                const isActive = entry.id === activeHeading
                return (
                <li key={`${entry.id}-${index}`}>
                  <button
                    type="button"
                    onClick={() => jumpTo(entry.id)}
                    className={
                      'w-full truncate px-2 py-1 text-left text-[length:var(--type-caption)] hover:bg-[var(--color-layer)] ' +
                      (isActive
                        ? 'font-medium text-[var(--color-accent)]'
                        : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]')
                    }
                    style={{ paddingLeft: `${(entry.level - 1) * 12 + 8}px` }}
                    title={entry.text}
                  >
                    {entry.text}
                  </button>
                </li>
                )
              })}
            </ul>
          </nav>
        )}
        <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto">
          {/* Fluid up to the ceiling in `--measure`, then centred so the small
              remainder becomes a symmetric margin. Centring only failed before
              because the column was FIXED and narrow, which split the leftover
              into two useless slivers; a column that tracks its container leaves
              a margin, not a void. */}
          <div className="mx-auto w-full max-w-[var(--measure)] px-8 py-8">
            {summary.status !== 'idle' && (
              <section
                ref={summaryRef}
                data-testid="markdown-summary"
                role="status"
                aria-live="polite"
                className="mb-6 border border-[var(--color-border)] bg-[var(--color-layer)] p-4"
              >
                <div className="mb-2 flex items-center gap-2 text-[length:var(--type-body)] font-semibold text-[var(--color-text-primary)]">
                  <Icon name="info" />
                  <span>文档摘要</span>
                </div>
                {summary.status === 'loading' && (
                  <div className="flex items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                    <Icon name="spinner" size={14} className="animate-spin" />
                    <span>正在生成摘要，请稍候…</span>
                  </div>
                )}
                {summary.status === 'error' && <div className="text-[length:var(--type-caption)] text-[var(--color-danger-text)]">{summary.message}</div>}
                {summary.status === 'ready' && <p className="whitespace-pre-wrap text-[length:var(--type-caption)] text-[var(--color-text-primary)]">{summary.text}</p>}
              </section>
            )}
            {documentModel?.frontmatter && (
              <section data-testid="frontmatter-card" className="mb-6 border border-[var(--color-border)] bg-[var(--color-layer)]">
                <button
                  type="button"
                  aria-expanded={frontmatterOpen}
                  onClick={() => setFrontmatterOpen((value) => !value)}
                  className="flex w-full items-center justify-between px-4 py-3 text-left text-[length:var(--type-caption)] font-semibold text-[var(--color-text-primary)] hover:bg-[var(--color-bg)]"
                >
                  <span>文档元数据</span>
                  <span className="font-normal text-[var(--color-text-tertiary)]">{frontmatterOpen ? '收起' : '展开'}</span>
                </button>
                {frontmatterOpen && (
                  <div className="border-t border-[var(--color-border)] px-4 py-3">
                    {Object.keys(documentModel.frontmatter.fields).length > 0 ? (
                      <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-[length:var(--type-caption)]">
                        {Object.entries(documentModel.frontmatter.fields).map(([key, value]) => (
                          <div key={key} className="contents">
                            <dt className="font-[var(--font-mono)] text-[var(--color-text-secondary)]">{key}</dt>
                            <dd className="break-words text-[var(--color-text-primary)]">{value}</dd>
                          </div>
                        ))}
                      </dl>
                    ) : (
                      <pre className="overflow-x-auto whitespace-pre-wrap font-[var(--font-mono)] text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                        {documentModel.frontmatter.raw}
                      </pre>
                    )}
                  </div>
                )}
              </section>
            )}
            {error && <StateBlock kind="error" title={error} compact />}
            {!error && content === null && <StateBlock kind="loading" title="加载中…" compact />}
            {!error && documentModel !== null && (
              viewerMode === 'source' ? (
                <section data-testid="markdown-source-view" className="border border-[var(--color-border)] bg-[var(--color-layer)]">
                  <div className="flex items-center justify-between border-b border-[var(--color-border)] px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                    <span>原始 Markdown · {documentModel.raw.split('\n').length} 行</span>
                    <button
                      type="button"
                      onClick={() => void copyText(documentModel.raw, '源码')}
                      className="border border-[var(--color-border)] px-2 py-1 hover:border-[var(--color-accent)] hover:text-[var(--color-accent)]"
                    >
                      复制全文
                    </button>
                  </div>
                  <div className="grid grid-cols-[auto_1fr] overflow-x-auto">
                    <pre data-testid="markdown-source-line-numbers" aria-hidden="true" className="select-none border-r border-[var(--color-border)] px-3 py-4 text-right font-[var(--font-mono)] text-[length:var(--type-caption)] leading-6 text-[var(--color-text-tertiary)]">
                      {documentModel.raw.split('\n').map((_, index) => `${index + 1}\n`)}
                    </pre>
                    <pre className="overflow-x-auto p-4 font-[var(--font-mono)] text-[length:var(--type-caption)] leading-6 whitespace-pre">
                      <code>
                        {documentModel.raw.split('\n').map((line, index, lines) => (
                          <span key={index} data-source-start={index + 1} data-source-end={index + 1}>
                            {line}
                            {index < lines.length - 1 ? '\n' : ''}
                          </span>
                        ))}
                      </code>
                    </pre>
                  </div>
                </section>
              ) : (
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSlug]} components={components}>
                  {documentModel.body}
                </ReactMarkdown>
              )
            )}
          </div>
        </div>
        {/* Right rail — this is what the leftover width is FOR.
            Capping the prose at a measure necessarily leaves space; centring the
            column merely split that space into two useless slivers, and
            left-aligning it merely moved the void to one side. A document's
            context belongs beside it: what cites it, what it cites, and which
            agent sessions touched it. `path` doubles as the wiki componentId, so
            this needs no new plumbing. Collapses at the shared `narrow` gate.

            BacklinksPanel renders the session list itself, so mounting
            SessionBacklinks beside it drew 「相关会话」 twice and fetched the
            session index twice. */}
        {!error && content !== null && path && (
          <aside
            data-testid="markdown-context-rail"
            aria-label="文档上下文"
            className="hidden w-[19rem] shrink-0 space-y-5 overflow-y-auto border-l border-[var(--color-border)] bg-[var(--color-bg)] px-4 py-6 min-[1200px]:block"
          >
            <div className="space-y-1">
              <div className="text-[length:var(--type-caption)] font-semibold text-[var(--color-text-tertiary)]">文档</div>
              {workspace && (
                <div className="text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                  工作区 <span className="font-[family-name:var(--font-mono)]">{workspace}</span>
                </div>
              )}
              <div className="break-all font-[family-name:var(--font-mono)] text-[length:var(--type-caption)] text-[var(--color-text-tertiary)]">
                {path}
              </div>
            </div>
            <BacklinksPanel componentId={path} onOpenSession={onOpenSession} />
          </aside>
        )}
      </div>
      {zoomed && (
        <div
          data-testid="image-lightbox"
          role="dialog"
          aria-modal="true"
          aria-label={zoomed.alt || '图片预览'}
          onClick={() => setZoomed(null)}
          className="fixed inset-0 z-50 flex cursor-zoom-out items-center justify-center bg-[var(--color-overlay)] p-8"
        >
          <img
            src={zoomed.src}
            alt={zoomed.alt}
            className="max-h-[92vh] max-w-[92vw] object-contain"
            onClick={(event) => event.stopPropagation()}
          />
        </div>
      )}
      {shareOpen && path && <ShareModal path={path} workspace={workspace} onClose={() => setShareOpen(false)} />}
    </div>
  )
}
