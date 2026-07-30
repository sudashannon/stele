import { useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSlug from 'rehype-slug'
import GithubSlugger from 'github-slugger'
import { fetchArtifactContent, fetchCachedSummary, summarizeDocument } from '../api/client'
import { DiagramBlock } from './DiagramBlock'
import { ShareModal } from './ShareModal'
import { Icon } from './icons'

function getDiagramLanguage(className?: string): 'mermaid' | 'plantuml' | null {
  if (className === 'language-mermaid') return 'mermaid'
  if (className === 'language-plantuml') return 'plantuml'
  return null
}

function extractTitle(rawText: string, fallbackPath: string | null): string {
  if (!rawText) return fallbackPath ? (fallbackPath.split('/').pop() ?? '') : ''
  if (rawText.startsWith('---')) {
    const end = rawText.indexOf('\n---', 3)
    if (end !== -1) {
      const fm = rawText.slice(3, end)
      const match = fm.match(/^title:\s*(.+)$/m)
      if (match) return match[1].trim().replace(/^["']|["']$/g, '')
    }
  }
  const body = stripFrontmatter(rawText)
  const heading = body.match(/^#\s+(.+)$/m)
  if (heading) return heading[1].trim()
  return fallbackPath ? (fallbackPath.split('/').pop() ?? '') : ''
}

function stripFrontmatter(text: string): string {
  if (text.startsWith('---\n') || text.startsWith('---\r\n')) {
    const end = text.indexOf('\n---', 3)
    if (end !== -1) return text.slice(end + 4).trimStart()
  }
  return text
}

interface TocEntry {
  id: string
  text: string
  level: number
}

function extractToc(markdown: string): TocEntry[] {
  const slugger = new GithubSlugger()
  const entries: TocEntry[] = []
  let inFence = false
  for (const line of markdown.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence
      continue
    }
    if (inFence) continue
    const match = /^(#{1,3})\s+(.+?)\s*#*\s*$/.exec(line)
    if (!match) continue
    const text = match[2]
      .replace(/`([^`]+)`/g, '$1')
      .replace(/\*\*([^*]+)\*\*/g, '$1')
      .replace(/\*([^*]+)\*/g, '$1')
      .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
      .trim()
    entries.push({ id: slugger.slug(text), text, level: match[1].length })
  }
  return entries
}

function isExternalHref(href: string) {
  return /^(https?:|data:|mailto:|tel:|#|\/)/i.test(href)
}

function resolveArtifactHref(docPath: string | null, href: string | undefined, workspace?: string) {
  if (!href) return href
  if (!docPath || isExternalHref(href)) return href
  const base = docPath.split('/').slice(0, -1).filter(Boolean)
  const parts = href.split('/').filter((part) => part && part !== '.')
  for (const part of parts) {
    if (part === '..') base.pop()
    else base.push(part)
  }
  const absPath = '/' + base.join('/')
  const params = new URLSearchParams({ path: absPath })
  if (workspace) params.set('workspace', workspace)
  return '/api/artifact?' + params.toString()
}

const markdownComponents: Components = {
  h1: ({ node, ...rest }) => <h1 className="mb-3 mt-5 text-2xl font-bold" {...rest} />,
  h2: ({ node, ...rest }) => <h2 className="mb-2 mt-5 text-xl font-semibold" {...rest} />,
  h3: ({ node, ...rest }) => <h3 className="mb-2 mt-4 text-lg font-semibold" {...rest} />,
  p: ({ node, ...rest }) => <p className="mb-3 leading-7" {...rest} />,
  ul: ({ node, ...rest }) => <ul className="mb-3 list-disc pl-6" {...rest} />,
  ol: ({ node, ...rest }) => <ol className="mb-3 list-decimal pl-6" {...rest} />,
  li: ({ node, ...rest }) => <li className="mb-1" {...rest} />,
  blockquote: ({ node, ...rest }) => (
    <blockquote
      className="mb-3 border-l-4 border-[var(--color-border)] py-1 pl-4 italic text-[var(--color-text-secondary)]"
      {...rest}
    />
  ),
  hr: ({ node, ...rest }) => <hr className="my-6 border-[var(--color-border)]" {...rest} />,
  table: ({ node, ...rest }) => (
    <div className="mb-4 overflow-x-auto">
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
    const language = getDiagramLanguage(className)
    if (language) return <DiagramBlock language={language} code={String(children).replace(/\n$/, '')} />
    return (
      <code className="break-words bg-[var(--color-bg)] px-1 py-0.5 font-mono text-sm" {...rest}>
        {children}
      </code>
    )
  },
  pre: ({ node, ...rest }) => (
    <pre className="mb-3 overflow-x-auto break-words bg-[var(--color-bg)] p-4 font-mono text-sm whitespace-pre-wrap" {...rest} />
  ),
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
}

type SummaryState =
  | { status: 'idle' }
  | { status: 'loading' }
  // `auto` marks a summary restored from the cache on open rather than one the
  // user just asked for: it must appear silently instead of yanking the scroll.
  | { status: 'ready'; text: string; auto?: boolean }
  | { status: 'error'; message: string }

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
}: Props) {
  const [content, setContent] = useState<string | null>(body ?? null)
  const [error, setError] = useState<string | null>(null)
  const [zoomed, setZoomed] = useState<{ src: string; alt: string } | null>(null)
  const [shareOpen, setShareOpen] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [summary, setSummary] = useState<SummaryState>({ status: 'idle' })
  const scrollRef = useRef<HTMLDivElement>(null)
  const summaryRef = useRef<HTMLElement>(null)
  const fetchRequestRef = useRef(0)
  const summaryRequestRef = useRef(0)
  const docTitle = path ? extractTitle(content ?? '', path) : ''

  useEffect(() => {
    if (body !== undefined) {
      fetchRequestRef.current += 1
      setContent(stripFrontmatter(body))
      setError(null)
      setZoomed(null)
      return
    }
    if (!path) return
    const requestId = ++fetchRequestRef.current
    setContent(null)
    setError(null)
    setZoomed(null)
    fetchArtifactContent(path, workspace)
      .then((text) => {
        if (fetchRequestRef.current === requestId) setContent(stripFrontmatter(text))
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
    if (!path) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (zoomed) setZoomed(null)
      else onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [path, onClose, zoomed])

  const toc = useMemo(() => (content ? extractToc(content) : []), [content])

  const components = useMemo<Components>(
    () => ({
      ...markdownComponents,
      a: ({ node, href, ...rest }) => (
        <a
          {...rest}
          href={resolveArtifactHref(path, href, workspace)}
          className="text-[var(--color-accent)] underline"
          target="_blank"
          rel="noreferrer"
        />
      ),
      img: ({ node, src, alt, ...rest }) => {
        const resolvedSrc = typeof src === 'string' ? resolveArtifactHref(path, src, workspace) : src
        return (
          <img
            {...rest}
            src={resolvedSrc}
            alt={alt}
            className="max-w-full cursor-zoom-in"
            onClick={() => typeof resolvedSrc === 'string' && setZoomed({ src: resolvedSrc, alt: alt ?? '' })}
          />
        )
      },
    }),
    [path, workspace],
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

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--color-surface)] shadow-[var(--shadow-1)]" role="region" aria-label={filename}>
      <header className="sticky top-0 z-10 flex flex-col gap-3 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-6 py-3">
        <div className="flex items-center justify-between gap-4">
          <div className="min-w-0 text-sm" title={path ?? undefined}>
            <div className="flex items-center gap-2">
              <span className="font-semibold text-[var(--color-text-primary)]">{displayTitle}</span>
              <span className="border border-[var(--color-border)] bg-[var(--color-layer)] px-1.5 py-0.5 text-[var(--type-caption)] text-[var(--color-text-secondary)]">
                只读文档
              </span>
            </div>
            {displayTitle !== filename && (
              <span className="mt-1 block truncate text-[var(--type-caption)] font-normal text-[var(--color-text-secondary)]">{path}</span>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {path && (
              <button
                type="button"
                data-testid="summary-btn"
                onClick={() => void handleSummarize()}
                disabled={summary.status === 'loading'}
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Icon name="info" />
                <span>{summary.status === 'loading' ? '摘要生成中…' : '生成摘要'}</span>
              </button>
            )}
            {onToggleStar && path && (
              <button
                type="button"
                aria-label={isStarred ? '取消收藏' : '收藏'}
                aria-pressed={!!isStarred}
                onClick={() => onToggleStar(path, filename)}
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
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
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
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
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
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
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
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
                className="flex items-center gap-1 border border-[var(--color-border)] px-2 py-1.5 text-[var(--type-caption)] text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
              >
                <Icon name="share" />
                <span>分享</span>
              </button>
            )}
            <button
              type="button"
              aria-label="关闭"
              onClick={onClose}
              className="flex items-center gap-1 border border-[var(--color-border)] px-3 py-1.5 text-[var(--type-caption)] font-medium text-[var(--color-accent)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
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
                    'shrink-0 whitespace-nowrap border px-2.5 py-1 text-[var(--type-caption)] ' +
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
        {toc.length > 1 && (
          <nav
            data-testid="markdown-toc"
            aria-label="文档目录"
            className="hidden w-60 shrink-0 overflow-y-auto border-r border-[var(--color-border)] px-3 py-6 lg:block"
          >
            <div className="mb-2 px-2 text-[var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">目录</div>
            <ul className="space-y-0.5">
              {toc.map((entry, index) => (
                <li key={`${entry.id}-${index}`}>
                  <button
                    type="button"
                    onClick={() => jumpTo(entry.id)}
                    className="w-full truncate px-2 py-1 text-left text-[var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)] hover:text-[var(--color-accent)]"
                    style={{ paddingLeft: `${(entry.level - 1) * 12 + 8}px` }}
                    title={entry.text}
                  >
                    {entry.text}
                  </button>
                </li>
              ))}
            </ul>
          </nav>
        )}
        <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto">
          <div className="mx-auto max-w-5xl px-6 py-8 text-base leading-relaxed">
            {summary.status !== 'idle' && (
              <section
                ref={summaryRef}
                data-testid="markdown-summary"
                role="status"
                aria-live="polite"
                className="mb-6 border border-[var(--color-border)] bg-[var(--color-layer)] p-4"
              >
                <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-[var(--color-text-primary)]">
                  <Icon name="info" />
                  <span>文档摘要</span>
                </div>
                {summary.status === 'loading' && (
                  <div className="flex items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                    <Icon name="spinner" size={14} className="animate-spin" />
                    <span>正在生成摘要，请稍候…</span>
                  </div>
                )}
                {summary.status === 'error' && <div className="text-[length:var(--type-caption)] text-[var(--color-danger)]">{summary.message}</div>}
                {summary.status === 'ready' && <p className="whitespace-pre-wrap text-[length:var(--type-caption)] text-[var(--color-text-primary)]">{summary.text}</p>}
              </section>
            )}
            {error && <div className="text-[var(--type-caption)] text-[var(--color-danger)]">{error}</div>}
            {!error && content === null && <div className="text-[var(--type-caption)] text-[var(--color-text-secondary)]">加载中…</div>}
            {!error && content !== null && (
              <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSlug]} components={components}>
                {content}
              </ReactMarkdown>
            )}
          </div>
        </div>
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
