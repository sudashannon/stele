import { useEffect, useState, useCallback } from 'react'
import { fetchLintIssues, fixDeadLinks } from '../api/client'
import type { LintIssue } from '../api/types'
import type { FixDeadLinkRequest } from '../api/client'
import { Icon } from './icons'

const POLL_INTERVAL_MS = 3000
const MAX_POLL_ATTEMPTS = 20

// Parses a dead-link detail string into { oldPath, suggestionPath } if it
// has a "possibly archived as" or "possibly at" suggestion.
function parseDeadLinkSuggestion(detail: string): { oldPath: string; newPath: string } | null {
  const m = detail.match(/^link to (.+?) has no matching component; possibly (?:archived as|at) (.+)$/)
  if (!m) return null
  return { oldPath: m[1], newPath: m[2] }
}

export function LintPanel({ onOpen }: { onOpen?: (path: string) => void }) {
  const [issues, setIssues] = useState<LintIssue[]>([])
  const [gaveUp, setGaveUp] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [fixing, setFixing] = useState(false)
  const [fixError, setFixError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let attempts = 0
    let timer: number | undefined

    const poll = () => {
      fetchLintIssues()
        .then((data) => {
          if (cancelled) return
          if (data.length > 0) {
            setIssues(data)
            return
          }
          setIssues([])
          attempts += 1
          if (attempts >= MAX_POLL_ATTEMPTS) {
            setGaveUp(true)
            return
          }
          timer = window.setTimeout(poll, POLL_INTERVAL_MS)
        })
        .catch(() => {
          if (cancelled) return
          setIssues([])
          setGaveUp(true)
        })
    }

    poll()

    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  const handleFix = useCallback(async () => {
    const reqs: FixDeadLinkRequest[] = []
    for (const key of selected) {
      const [sourceId, oldPath] = key.split('\x00')
      const issue = issues.find((i) => {
        if (i.componentId !== sourceId || i.rule !== 'dead-link') return false
        const p = parseDeadLinkSuggestion(i.detail)
        return p !== null && p.oldPath === oldPath
      })
      reqs.push({ sourceId, oldPath: parseDeadLinkSuggestion(issue!.detail)!.oldPath, newPath: parseDeadLinkSuggestion(issue!.detail)!.newPath })
    }
    if (reqs.length === 0) return
    setFixError(null)

    setFixing(true)
    try {
      const results = await fixDeadLinks(reqs)
      const failures = results.filter((result) => !result.fixed)
      const reasons = [...new Set(failures.map((result) => result.error ?? result.sourceId))]
      setFixError(failures.length > 0 ? `${failures.length} 项未修复：${reasons.join('；')}` : null)
      setIssues(await fetchLintIssues())
      setSelected(new Set())
    } catch (e) {
      setFixError(e instanceof Error ? e.message : '修复失败')
      console.error('fix failed:', e)
    } finally {
      setFixing(false)
    }
  }, [selected, issues])

  const toggleSelect = (sourceId: string, oldPath: string) => {
    const key = sourceId + '\x00' + oldPath
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  if (issues.length === 0) {
    if (!gaveUp) {
      return <div className="text-xs text-[var(--color-text-secondary)] animate-pulse">索引构建中…</div>
    }
    return <div className="text-xs text-[var(--color-text-secondary)]">未发现问题</div>
  }

  const groups = new Map<string, LintIssue[]>()
  for (const issue of issues) {
    const list = groups.get(issue.rule)
    if (list) list.push(issue)
    else groups.set(issue.rule, [issue])
  }

  return (
    <div className="space-y-4 text-xs">
      {fixError && <div className="border border-[var(--color-warn)] px-2 py-1 text-[var(--color-warn)]">{fixError}</div>}
      {[...groups.entries()].map(([rule, items]) => (
        <section key={rule}>
          <div className="sticky top-0 mb-1 flex items-center gap-2 border-b border-[var(--color-border)] bg-[var(--color-surface)] py-1">
            <span className="shrink-0 text-[var(--color-warn)] font-mono font-semibold whitespace-nowrap">{rule}</span>
            <span className="text-[var(--color-text-secondary)]">({items.length})</span>
            {rule === 'dead-link' && selected.size > 0 && (
              <button
                type="button"
                onClick={handleFix}
                disabled={fixing}
                className="ml-auto shrink-0 border border-[var(--color-accent)] bg-[var(--color-accent-subtle)] px-2 py-0.5 text-[var(--type-caption)] text-[var(--color-accent)] hover:bg-[var(--color-layer)] disabled:opacity-50"
              >
                {fixing ? '修复中…' : `修复选中 (${selected.size})`}
              </button>
            )}
          </div>
          <div className="space-y-1">
            {items.map((i, idx) => {
              const suggestion = parseDeadLinkSuggestion(i.detail)
              return (
                <LintDetail
                  key={idx}
                  detail={i.detail}
                  componentId={i.componentId}
                  onOpen={onOpen}
                  hasSuggestion={!!suggestion}
                  checked={suggestion ? selected.has(i.componentId + '\x00' + suggestion.oldPath) : false}
                  onToggle={suggestion ? () => toggleSelect(i.componentId, suggestion.oldPath) : undefined}
                />
              )
            })}
          </div>
        </section>
      ))}
    </div>
  )
}

function safeDecode(text: string): string {
  try { return decodeURIComponent(text) } catch { return text }
}

const DEAD_LINK_DETAIL_RE = /^(link to )(.+?)( has no matching component)(; possibly (?:archived as|at) .+)?$/

function LintDetail({
  detail, componentId, onOpen, hasSuggestion, checked, onToggle,
}: {
  detail: string; componentId: string; onOpen?: (path: string) => void
  hasSuggestion?: boolean; checked?: boolean; onToggle?: () => void
}) {
  const decodedDetail = safeDecode(detail)
  const match = detail.match(DEAD_LINK_DETAIL_RE)

  const sourceButton = onOpen && componentId ? (
    <button
      type="button"
      onClick={() => onOpen(componentId)}
      className="ml-1 shrink-0 text-[var(--color-accent)] hover:text-[var(--color-accent-hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]"
      title={`打开来源: ${componentId}`}
      aria-label={`打开来源: ${componentId}`}
    >
      <Icon name="open" size={14} />
    </button>
  ) : null

  if (!match) {
    return (
      <div className="flex items-center min-w-0 text-[var(--color-text-secondary)] pl-1" title={decodedDetail}>
        {hasSuggestion && <input type="checkbox" checked={checked} onChange={onToggle} className="shrink-0 mr-1" />}
        <span className="truncate">{decodedDetail}</span>
        {sourceButton}
      </div>
    )
  }
  const [, prefix, path, suffix, suggestion] = match
  return (
    <div className="flex items-center min-w-0 text-[var(--color-text-secondary)] pl-1 flex-wrap gap-x-1" title={decodedDetail}>
      {hasSuggestion && <input type="checkbox" checked={checked} onChange={onToggle} className="shrink-0 mr-1" />}
      <span className="shrink-0 whitespace-nowrap">{prefix}</span>
      <span className="min-w-0 truncate" dir="rtl" style={{ textAlign: 'left' }}>
        {safeDecode(path)}
      </span>
      <span className="shrink-0 whitespace-nowrap">{suffix}</span>
      {suggestion && (
        <span className="shrink-0 whitespace-nowrap text-[var(--color-accent)]">{suggestion}</span>
      )}
      {sourceButton}
    </div>
  )
}
