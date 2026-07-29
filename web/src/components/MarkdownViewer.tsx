import { useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSlug from 'rehype-slug'
import GithubSlugger from 'github-slugger'
import { fetchArtifactContent } from '../api/client'
import { DiagramBlock } from './DiagramBlock'
import { ShareModal } from './ShareModal'

// Fenced code blocks carry their language as `language-xxx` in the code
// element's className (e.g. ```mermaid -> "language-mermaid"). Only these two
// languages should render as diagrams; everything else stays plain code.
function getDiagramLanguage(className?: string): 'mermaid' | 'plantuml' | null {
  if (className === 'language-mermaid') return 'mermaid'
  if (className === 'language-plantuml') return 'plantuml'
  return null
}


// extractTitle reads the document title from either YAML frontmatter
// (title: "...") or the first "# " ATX heading. Falls back to filename.
function extractTitle(rawText: string, fallbackPath: string | null): string {
  if (!rawText) return fallbackPath ? (fallbackPath.split("/").pop() ?? "") : ""
  // Try frontmatter title field
  if (rawText.startsWith("---")) {
    const end = rawText.indexOf("\n---", 3)
    if (end !== -1) {
      const fm = rawText.slice(3, end)
      const m = fm.match(/^title:\s*(.+)$/m)
      if (m) return m[1].trim().replace(/^["']|["']$/g, "")
    }
  }
  // Try first "# " heading in the content (after stripping frontmatter)
  const body = stripFrontmatter(rawText)
  const heading = body.match(/^#\s+(.+)$/m)
  if (heading) return heading[1].trim()
  return fallbackPath ? (fallbackPath.split("/").pop() ?? "") : ""
}

// Strips a leading YAML frontmatter block (---\n...\n---). Real design-doc
// artifacts from the API start with this metadata, but it's noise inside the
// rendered document.
function stripFrontmatter(text: string): string {
  if (text.startsWith('---\n') || text.startsWith('---\r\n')) {
    const end = text.indexOf('\n---', 3)
    if (end !== -1) {
      return text.slice(end + 4).trimStart()
    }
  }
  return text
}

interface TocEntry {
  id: string
  text: string
  level: number
}

// Parses ATX headings (#, ##, ###) from markdown into a TOC, assigning each the
// same slug id that rehype-slug produces at render time (both use GithubSlugger
// traversing in document order, so a fresh slugger here matches the rendered
// heading ids — including duplicate-heading disambiguation like "-1"). Fenced
// code blocks are skipped so a commented "# foo" inside ```…``` isn't a heading.
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
    const m = /^(#{1,3})\s+(.+?)\s*#*\s*$/.exec(line)
    if (!m) continue
    // Strip inline markdown emphasis/code/links from the visible label.
    const text = m[2]
      .replace(/`([^`]+)`/g, '$1')
      .replace(/\*\*([^*]+)\*\*/g, '$1')
      .replace(/\*([^*]+)\*/g, '$1')
      .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
      .trim()
    entries.push({ id: slugger.slug(text), text, level: m[1].length })
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
  h1: ({ node, ...rest }) => <h1 className="text-2xl font-bold mt-5 mb-3" {...rest} />,
  h2: ({ node, ...rest }) => <h2 className="text-xl font-semibold mt-5 mb-2" {...rest} />,
  h3: ({ node, ...rest }) => <h3 className="text-lg font-semibold mt-4 mb-2" {...rest} />,
  p: ({ node, ...rest }) => <p className="mb-3 leading-7" {...rest} />,
  ul: ({ node, ...rest }) => <ul className="list-disc pl-6 mb-3" {...rest} />,
  ol: ({ node, ...rest }) => <ol className="list-decimal pl-6 mb-3" {...rest} />,
  li: ({ node, ...rest }) => <li className="mb-1" {...rest} />,
  blockquote: ({ node, ...rest }) => (
    <blockquote
      className="border-l-4 border-[var(--color-border)] pl-4 py-1 mb-3 text-[var(--color-text-secondary)] italic"
      {...rest}
    />
  ),
  hr: ({ node, ...rest }) => <hr className="my-6 border-[var(--color-border)]" {...rest} />,
  table: ({ node, ...rest }) => (
    <div className="overflow-x-auto mb-4">
      <table className="border-collapse w-full text-left" {...rest} />
    </div>
  ),
  thead: ({ node, ...rest }) => <thead className="bg-[var(--color-bg)]" {...rest} />,
  tbody: ({ node, ...rest }) => <tbody {...rest} />,
  tr: ({ node, ...rest }) => <tr className="border-b border-[var(--color-border)]" {...rest} />,
  th: ({ node, ...rest }) => (
    <th className="border border-[var(--color-border)] px-3 py-2 font-semibold whitespace-nowrap" {...rest} />
  ),
  td: ({ node, ...rest }) => <td className="border border-[var(--color-border)] px-3 py-2 align-top" {...rest} />,
  // inline case inside <code> only, and the block case inside <pre><code>.
  code: ({ node, className, children, ...rest }) => {
    const language = getDiagramLanguage(className)
    if (language) {
      return <DiagramBlock language={language} code={String(children).replace(/\n$/, '')} />
    }
    return (
      <code className="bg-[var(--color-bg)] px-1 py-0.5 font-mono text-sm break-words" {...rest}>
        {children}
      </code>
    )
  },
  pre: ({ node, ...rest }) => (
    <pre
      className="bg-[var(--color-bg)] p-4 overflow-x-auto font-mono text-sm mb-3 whitespace-pre-wrap break-words"
      {...rest}
    />
  ),
}

interface Artifact {
  path: string
  label: string
}

interface Props {
  path: string | null
  // Renders this markdown string directly instead of fetching `path` from
  // the artifact API — used by ReportView for generated report bodies that
  // never touch disk under a change's artifact tree. When set, `path` is
  // still used as the header title but no fetch/effect runs.
  body?: string
  artifacts?: Artifact[]
  workspace?: string
  onSelectArtifact?: (path: string) => void
  onClose: () => void
  // Bookmark star toggle: both are optional so callers that don't wire
  // bookmarking (e.g. ReportView's generated-body viewer) simply omit them
  // and the star button doesn't render.
  onToggleStar?: (path: string, title: string) => void
  isStarred?: boolean
  onNavigateToChange?: (changeName: string) => void
  onCreateTodo?: () => void
}

export function MarkdownViewer({ path, body, artifacts, workspace, onSelectArtifact, onClose, onToggleStar, isStarred, onNavigateToChange, onCreateTodo }: Props) {
  const [content, setContent] = useState<string | null>(body ?? null)
  const [error, setError] = useState(false)
  const [zoomed, setZoomed] = useState<{ src: string; alt: string } | null>(null)
  const [shareOpen, setShareOpen] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const docTitle = path ? extractTitle(content ?? "", path) : ""
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (body !== undefined) {
      setContent(stripFrontmatter(body))
      setError(false)
      setZoomed(null)
      return
    }
    if (!path) return
    setContent(null)
    setError(false)
    setZoomed(null)
    fetchArtifactContent(path, workspace)
      .then((text) => setContent(stripFrontmatter(text)))
      .catch(() => setError(true))
  }, [path, body, workspace])

  // Escape closes the lightbox first (if open), otherwise the viewer — so a
  // user zooming an image can dismiss just the overlay without losing the doc.
  useEffect(() => {
    if (!path) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (zoomed) setZoomed(null)
      else onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [path, onClose, zoomed])

  const toc = useMemo(() => (content ? extractToc(content) : []), [content])

  // Override img/link so relative markdown assets resolve against the current
  // document's directory via the artifact API; images still open in a lightbox.
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

  if (!path && body === undefined) return null

  const filename = path ? path.split('/').pop() ?? path : '报告'
  const displayTitle = docTitle && docTitle !== filename ? docTitle : filename
  const changeName = useMemo(() => {
    if (!path) return null
    const m = path.match(/\/changes\/([^\/]+)\//)
    return m ? m[1] : null
  }, [path])

  const jumpTo = (id: string) => {
    const el = scrollRef.current?.querySelector(`#${CSS.escape(id)}`)
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <div
      className="h-full min-h-0 flex flex-col bg-white shadow-sm"
      role="region"
      aria-label={filename}
    >
      <header className="sticky top-0 z-10 bg-white border-b border-[var(--color-border)] px-6 py-3 flex flex-col gap-2">
        <div className="flex items-center justify-between gap-4">
          <div className="text-sm min-w-0" title={path ?? undefined}>
            <span className="font-semibold text-[var(--color-text-primary)]">{displayTitle}</span>
            {displayTitle !== filename && (
              <span className="text-[var(--color-text-secondary)] ml-2 text-xs font-normal truncate">{path}</span>
            )}
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {onToggleStar && path && (
              <button
                type="button"
                aria-label={isStarred ? '取消收藏' : '收藏'}
                aria-pressed={!!isStarred}
                onClick={() => onToggleStar(path, filename)}
                className="shrink-0 text-lg leading-none px-2 py-1.5 border border-[var(--color-border)] hover:bg-[var(--palette-highlight)] hover:border-[var(--color-accent)]"
              >
                {isStarred ? '⭐' : '☆'}
              </button>
            )}
            {path && (
              <button
                type="button"
                aria-label="刷新"
                onClick={() => { setContent(null); setRefreshKey(k => k + 1) }}
                data-testid="refresh-btn"
                className="shrink-0 text-lg leading-none px-2 py-1.5 border border-[var(--color-border)] hover:bg-[var(--palette-highlight)] hover:border-[var(--color-accent)]"
              >
                🔄
              </button>
            )}
            {onCreateTodo && (
              <button
                type="button"
                aria-label="添加待办"
                data-testid="create-todo-btn"
                onClick={onCreateTodo}
                className="shrink-0 text-lg leading-none px-2 py-1.5 border border-[var(--color-border)] hover:bg-[var(--palette-highlight)] hover:border-[var(--color-accent)]"
                title="添加待办"
              >
                📋
              </button>
            )}
            {changeName && onNavigateToChange && (
              <button
                type="button"
                aria-label="跳转到变更"
                onClick={() => onNavigateToChange(changeName)}
                data-testid="navigate-change-btn"
                className="shrink-0 text-lg leading-none px-2 py-1.5 border border-[var(--color-border)] hover:bg-[var(--palette-highlight)] hover:border-[var(--color-accent)]"
                title="跳转到变更视图"
              >
                📋
              </button>
            )}
            {path && (
              <button
                type="button"
                aria-label="分享"
                onClick={() => setShareOpen(true)}
                data-testid="share-open-btn"
                className="shrink-0 text-lg leading-none px-2 py-1.5 border border-[var(--color-border)] hover:bg-[var(--palette-highlight)] hover:border-[var(--color-accent)]"
              >
                🔗
              </button>
            )}
            <button
              type="button"
              onClick={onClose}
              className="shrink-0 text-sm font-medium px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--palette-highlight)] hover:border-[var(--color-accent)]"
            >
              ✕ 关闭
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
                    'shrink-0 text-xs px-2.5 py-1 rounded-full border whitespace-nowrap ' +
                    (active
                      ? 'bg-[var(--color-accent)] text-white border-[var(--color-accent)]'
                      : 'text-[var(--color-text-primary)] border-[var(--color-border)] hover:border-[var(--color-accent)] hover:text-[var(--color-accent)]')
                  }
                >
                  {artifact.label}
                </button>
              )
            })}
          </div>
        )}
      </header>
      <div className="flex-1 min-h-0 flex">
        {toc.length > 1 && (
          <nav
            data-testid="markdown-toc"
            aria-label="文档目录"
            className="hidden lg:block w-60 shrink-0 overflow-y-auto border-r border-[var(--color-border)] py-6 px-3"
          >
            <div className="text-xs font-semibold text-[var(--color-text-secondary)] px-2 mb-2">目录</div>
            <ul className="space-y-0.5">
              {toc.map((entry, i) => (
                <li key={`${entry.id}-${i}`}>
                  <button
                    type="button"
                    onClick={() => jumpTo(entry.id)}
                    className="w-full text-left text-xs text-[var(--color-text-primary)] hover:text-[var(--color-accent)] hover:bg-[var(--palette-highlight)] px-2 py-1 truncate"
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
          <div className="max-w-5xl mx-auto px-6 py-8 text-base leading-relaxed">
            {error && <div className="text-[var(--color-danger)]">加载失败</div>}
            {!error && content === null && <div className="text-[var(--color-text-secondary)]">加载中…</div>}
            {!error && content !== null && (
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                rehypePlugins={[rehypeSlug]}
                components={components}
              >
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
          className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-8 cursor-zoom-out"
        >
          <img
            src={zoomed.src}
            alt={zoomed.alt}
            className="max-w-[92vw] max-h-[92vh] object-contain"
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}

      {shareOpen && (
        <ShareModal path={path} workspace={workspace} onClose={() => setShareOpen(false)} />
      )}
    </div>
  )
}
