import { useEffect, useMemo, useRef, useState } from 'react'
import { searchSemantic } from '../api/client'
import type { SemanticSearchResult } from '../api/client'
import { Icon } from './icons'
import { StateBlock } from './StateBlock'
import { typeBadgeClass } from './graphPalette'

const DEBOUNCE_MS = 300
const SEARCH_LIMIT = 100
const PAGE_SIZE = 20

function componentFilename(id: string): string {
  const separator = Math.max(id.lastIndexOf('/'), id.lastIndexOf('\\'))
  return id.slice(separator + 1)
}

interface SemanticSearchProps {
  onNodeClick: (id: string) => void
}

export function SemanticSearch({ onNodeClick }: SemanticSearchProps) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SemanticSearchResult[]>([])
  const [loadError, setLoadError] = useState(false)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(0)
  const [typeFilter, setTypeFilter] = useState<string | null>(null)
  const types = useMemo(() => {
    const seen = new Set<string>()
    for (const r of results) seen.add(r.type)
    return Array.from(seen).sort()
  }, [results])
  const filteredResults = useMemo(
    () => typeFilter ? results.filter(r => r.type === typeFilter) : results,
    [results, typeFilter]
  )
  const requestIdRef = useRef(0)

  useEffect(() => {
    const requestId = ++requestIdRef.current
    const trimmed = query.trim()
    if (trimmed === '') {
      setResults([])
      setLoadError(false)
      setLoading(false)
      setPage(0)
      setTypeFilter(null)
      return
    }

    const controller = new AbortController()
    setResults([])
    setLoadError(false)
    setLoading(true)
    const timer = window.setTimeout(async () => {
      try {
        const data = await searchSemantic(trimmed, SEARCH_LIMIT, controller.signal)
        if (requestId !== requestIdRef.current) return
        setResults(data)
        setLoadError(false)
        setPage(0)
        setTypeFilter(null)
      } catch {
        if (controller.signal.aborted || requestId !== requestIdRef.current) return
        setResults([])
        setLoadError(true)
      } finally {
        if (requestId === requestIdRef.current) setLoading(false)
      }
    }, DEBOUNCE_MS)

    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [query])

  const totalPages = Math.ceil(filteredResults.length / PAGE_SIZE)
  const pageResults = useMemo(
    () => filteredResults.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE),
    [filteredResults, page],
  )

  return (
    <div className="space-y-3 text-[length:var(--type-body)]">
      <input
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="按含义、标题或文件名搜索…（tag:KMC 可按标签筛选）"
        aria-label="语义搜索"
        className="w-full border border-[var(--color-border)] px-3 py-2 text-[length:var(--type-body)] outline-none focus:border-[var(--color-accent)]"
      />
      {loading && <StateBlock kind="loading" title="搜索中…" compact />}
      {loadError && (
        <StateBlock
          kind="error"
          title="语义搜索暂不可用，请稍后重试"
          compact
        />
      )}
      {!loading && !loadError && query.trim() !== '' && results.length === 0 && (
        <StateBlock
          kind="empty"
          title="无匹配结果"
          hints={['搜索范围：变更 / 文档 / 会话记录', '快捷键：Ctrl + K 打开命令面板']}
        />
      )}
      {results.length > 0 && (
        <>
          <div className="text-[var(--color-text-secondary)]">共 {results.length} 条结果</div>
          {types.length > 1 && (
            <div className="flex flex-wrap gap-1.5">
              <button
                type="button"
                aria-pressed={typeFilter === null}
                onClick={() => { setTypeFilter(null); setPage(0) }}
                className={`px-2.5 py-0.5 text-[length:var(--type-caption)] font-medium transition-colors ${
                  typeFilter === null
                    ? 'bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
                    : 'bg-[var(--color-layer)] text-[var(--color-text-secondary)] hover:bg-[var(--color-border)]'
                }`}
              >全部</button>
              {types.map((t) => (
                <button
                  key={t}
                  type="button"
                  aria-pressed={typeFilter === t}
                  onClick={() => { setTypeFilter(typeFilter === t ? null : t); setPage(0) }}
                  className={`px-2.5 py-0.5 text-[length:var(--type-caption)] font-medium transition-colors ${
                    typeFilter === t
                      ? 'bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
                      : 'bg-[var(--color-layer)] text-[var(--color-text-secondary)] hover:bg-[var(--color-border)]'
                  }`}
                >{t}</button>
              ))}
            </div>
          )}
        </>
      )}
      <ul className="space-y-1.5">
        {pageResults.map((item) => {
          const filename = componentFilename(item.id)
          return (
            <li key={item.id}>
              <button
                type="button"
                onClick={() => onNodeClick(item.id)}
                className="w-full flex items-center gap-2 border border-[var(--color-border)] px-3 py-2 text-left hover:bg-[var(--color-bg)]"
              >
                <span className={`shrink-0 px-1.5 py-0.5 text-[length:var(--type-caption)] font-medium ${typeBadgeClass(item.type)}`}>
                  {item.type}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{item.title}</span>
                  {filename !== item.title && (
                    <span className="block truncate text-[length:var(--type-caption)] text-[var(--color-text-secondary)] font-mono">{filename}</span>
                  )}
                  {item.tags && item.tags.length > 0 && (
                    <span data-testid="search-result-tags" className="mt-1 flex flex-wrap gap-1">
                      {item.tags.map((tag) => (
                        <span
                          key={tag}
                          className="border border-[var(--color-border-subtle)] bg-[var(--color-layer)] px-1.5 py-0.5 text-[length:var(--type-caption)] leading-none text-[var(--color-text-secondary)]"
                        >
                          {tag}
                        </span>
                      ))}
                    </span>
                  )}
                </span>
                <span className="shrink-0 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{item.workspace}</span>
                <span className="shrink-0 tabular-nums font-mono text-[var(--color-accent)]">
                  {Math.round(Math.min(1, item.similarity) * 100)}%
                </span>
              </button>
            </li>
          )
        })}
      </ul>
      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2 pt-2">
          <button
            type="button"
            disabled={page === 0}
            onClick={() => setPage(page - 1)}
            className="border border-[var(--color-border)] px-2 py-1 disabled:opacity-30"
          >
            <Icon name="chevron-left" />
            上一页
          </button>
          <span className="text-[var(--color-text-secondary)] tabular-nums font-mono">
            {page + 1} / {totalPages}
          </span>
          <button
            type="button"
            disabled={page >= totalPages - 1}
            onClick={() => setPage(page + 1)}
            className="border border-[var(--color-border)] px-2 py-1 disabled:opacity-30"
          >
            下一页
            <Icon name="chevron-right" />
          </button>
        </div>
      )}
    </div>
  )
}
