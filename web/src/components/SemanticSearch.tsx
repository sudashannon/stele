import { useEffect, useMemo, useRef, useState } from 'react'
import { searchSemantic } from '../api/client'
import type { SemanticSearchResult } from '../api/client'
import { TYPE_COLORS } from './WikiGraph'

const DEBOUNCE_MS = 300
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
  const debounceRef = useRef<number | undefined>(undefined)

  useEffect(() => {
    if (debounceRef.current !== undefined) window.clearTimeout(debounceRef.current)
    const trimmed = query.trim()
    if (trimmed === '') {
      setResults([])
      setLoadError(false)
      setPage(0)
      return
    }
    debounceRef.current = window.setTimeout(() => {
      searchSemantic(trimmed, 0)
        .then((data) => {
          setResults(data)
          setLoadError(false)
          setPage(0)
          setTypeFilter(null)
        })
        .catch(() => {
          setResults([])
          setLoadError(true)
        })
    }, DEBOUNCE_MS)
    return () => {
      if (debounceRef.current !== undefined) window.clearTimeout(debounceRef.current)
    }
  }, [query])

  const totalPages = Math.ceil(filteredResults.length / PAGE_SIZE)
  const pageResults = useMemo(
    () => filteredResults.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE),
    [filteredResults, page],
  )

  return (
    <div className="space-y-3 text-xs">
      <input
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="按含义、标题或文件名搜索…"
        aria-label="语义搜索"
        className="w-full border border-[var(--color-border)] px-3 py-2 text-sm outline-none focus:border-[var(--color-accent)]"
      />
      {loadError && <div className="text-[var(--color-danger)]">搜索失败</div>}
      {!loadError && query.trim() !== '' && results.length === 0 && (
        <div className="text-[var(--color-text-secondary)]">无匹配结果</div>
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
                className={`rounded-full px-2.5 py-0.5 text-[11px] font-medium transition-colors ${
                  typeFilter === null
                    ? 'bg-[var(--color-accent)] text-white'
                    : 'bg-[var(--color-bg)] text-[var(--color-text-secondary)] hover:bg-[var(--color-border)]'
                }`}
              >全部</button>
              {types.map((t) => (
                <button
                  key={t}
                  type="button"
                  aria-pressed={typeFilter === t}
                  onClick={() => { setTypeFilter(typeFilter === t ? null : t); setPage(0) }}
                  className={`rounded-full px-2.5 py-0.5 text-[11px] font-medium transition-colors ${
                    typeFilter === t
                      ? 'text-white'
                      : 'bg-[var(--color-bg)] text-[var(--color-text-secondary)] hover:bg-[var(--color-border)]'
                  }`}
                  style={typeFilter === t ? { backgroundColor: TYPE_COLORS[t] ?? 'var(--color-text-secondary)' } : undefined}
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
                <span
                  className="shrink-0 px-1.5 py-0.5 text-[10px] font-medium text-white"
                  style={{ backgroundColor: TYPE_COLORS[item.type] ?? 'var(--color-text-secondary)' }}
                >
                  {item.type}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{item.title}</span>
                  {filename !== item.title && (
                    <span className="block truncate text-[11px] text-[var(--color-text-secondary)]">{filename}</span>
                  )}
                </span>
                <span className="shrink-0 text-[var(--color-text-secondary)]">{item.workspace}</span>
                <span className="shrink-0 tabular-nums text-[var(--color-accent)]">
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
            ← 上一页
          </button>
          <span className="text-[var(--color-text-secondary)]">
            {page + 1} / {totalPages}
          </span>
          <button
            type="button"
            disabled={page >= totalPages - 1}
            onClick={() => setPage(page + 1)}
            className="border border-[var(--color-border)] px-2 py-1 disabled:opacity-30"
          >
            下一页 →
          </button>
        </div>
      )}
    </div>
  )
}
