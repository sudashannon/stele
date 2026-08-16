import React, { useEffect, useState, useCallback } from 'react'
import { deleteDocuments, fetchLintIssues, fixDeadLinks } from '../api/client'
import type { LintIssue } from '../api/types'
import type { FixDeadLinkRequest } from '../api/client'
import { Icon } from './icons'
import { StateBlock } from './StateBlock'
const POLL_INTERVAL_MS = 3000
const MAX_POLL_ATTEMPTS = 20

// Parses a dead-link detail string into { oldPath, suggestionPath } if it
// has a "possibly archived as" or "possibly at" suggestion.
function parseDeadLinkSuggestion(detail: string): { oldPath: string; newPath: string } | null {
  const m = detail.match(/^link to (.+?) has no matching component; possibly (?:archived as|at) (.+)$/)
  if (!m) return null
  return { oldPath: m[1], newPath: m[2] }
}

/** Map a lint rule to its severity level for the rule-summary table. */
function ruleSeverity(rule: string): 'warn' | 'danger' {
  // dead-link is the only rule that denotes a broken navigation edge;
  // every other rule is a documentation hygiene warning.
  if (rule === 'dead-link') return 'danger'
  return 'warn'
}

// What each low-quality signal means, in the reader's terms. The rule reports the
// measurements too, so a row can be judged before anything is deleted.
const SIGNAL_LABELS: Record<string, string> = {
  short: '过短',
  unstructured: '无结构',
  'unfilled-outline': '骨架未填',
  'placeholder-dense': '占位符密集',
  'unresolved-in-finished': '已完成仍有待办',
}

export function LintPanel({ onOpen }: { onOpen?: (path: string) => void }) {
  const [issues, setIssues] = useState<LintIssue[]>([])
  const [gaveUp, setGaveUp] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [fixing, setFixing] = useState(false)
  const [fixError, setFixError] = useState<string | null>(null)
  const [expandedRules, setExpandedRules] = useState<Set<string>>(new Set())
  // Deletion is destructive, so the chosen paths are held apart from the
  // dead-link fix selection and confirmed before anything is sent.
  const [doomed, setDoomed] = useState<Set<string>>(new Set())
  const [confirming, setConfirming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteNote, setDeleteNote] = useState<string | null>(null)

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

  const toggleDoomed = (path: string) => {
    setDoomed((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
    setConfirming(false)
  }

  const handleDelete = useCallback(async () => {
    if (doomed.size === 0) return
    setDeleting(true)
    setDeleteNote(null)
    try {
      const result = await deleteDocuments([...doomed])
      const parts: string[] = []
      if (result.deleted.length > 0) {
        parts.push(`已移入回收站 ${result.deleted.length} 篇（${result.trash}）`)
      }
      if (result.failed.length > 0) {
        const reasons = [...new Set(result.failed.map((f) => f.reason))]
        parts.push(`${result.failed.length} 篇未删除：${reasons.join('；')}`)
      }
      setDeleteNote(parts.join(' · ') || '没有文档被删除')
      setDoomed(new Set())
      setConfirming(false)
      // The index rebuilds server-side after a delete; re-read so the list stops
      // showing rows whose files are gone.
      setIssues(await fetchLintIssues())
    } catch (e) {
      setDeleteNote(e instanceof Error ? e.message : '删除失败')
    } finally {
      setDeleting(false)
    }
  }, [doomed])

  const toggleRuleExpand = (rule: string) => {
    setExpandedRules((prev) => {
      const next = new Set(prev)
      if (next.has(rule)) next.delete(rule)
      else next.add(rule)
      return next
    })
  }

  if (issues.length === 0) {
    if (!gaveUp) {
      return <StateBlock kind="loading" title="索引构建中…" compact />
    }
    return <StateBlock kind="empty" title="未发现问题" compact />
  }

  const groups = new Map<string, LintIssue[]>()
  for (const issue of issues) {
    const list = groups.get(issue.rule)
    if (list) list.push(issue)
    else groups.set(issue.rule, [issue])
  }

  const hasDeadLinkFix = selected.size > 0

  return (
    <div>
      {fixError && (
        <div className="mb-2 border border-[var(--color-warn-text)] px-2 py-1 text-[var(--color-warn-text)]">{fixError}</div>
      )}
      {deleteNote && (
        <div
          data-testid="lint-delete-note"
          className="mb-2 border border-[var(--color-border)] bg-[var(--color-layer)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)]"
        >
          {deleteNote}
        </div>
      )}
      {doomed.size > 0 && (
        <div
          data-testid="lint-delete-bar"
          className="mb-2 flex flex-wrap items-center justify-end gap-2 text-[length:var(--type-caption)]"
        >
          {confirming ? (
            <>
              {/* Says where the files go, because "delete" that is really a move
                  should not read as an unlink. */}
              <span className="text-[var(--color-text-secondary)]">
                将 {doomed.size} 篇移入回收站（可恢复），确定？
              </span>
              <button
                type="button"
                onClick={() => setConfirming(false)}
                className="border border-[var(--color-border)] px-2 py-0.5 text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
              >
                取消
              </button>
              <button
                type="button"
                data-testid="lint-delete-confirm"
                onClick={handleDelete}
                disabled={deleting}
                className="border border-[var(--color-danger)] bg-[var(--color-danger-subtle)] px-2 py-0.5 font-medium text-[var(--color-danger-text)] hover:bg-[var(--color-layer)] disabled:opacity-50"
              >
                {deleting ? '删除中…' : '确认移入回收站'}
              </button>
            </>
          ) : (
            <button
              type="button"
              data-testid="lint-delete-request"
              onClick={() => setConfirming(true)}
              className="border border-[var(--color-border)] px-2 py-0.5 text-[var(--color-danger-text)] hover:bg-[var(--color-layer)]"
            >
              删除选中 ({doomed.size})
            </button>
          )}
        </div>
      )}
      {hasDeadLinkFix && (
        <div className="mb-2 flex justify-end">
          <button
            type="button"
            onClick={handleFix}
            disabled={fixing}
            className="shrink-0 border border-[var(--color-accent)] bg-[var(--color-accent-subtle)] px-2 py-0.5 text-[length:var(--type-caption)] text-[var(--color-accent)] hover:bg-[var(--color-layer)] disabled:opacity-50"
          >
            {fixing ? '修复中…' : `修复选中 (${selected.size})`}
          </button>
        </div>
      )}
      <div className="overflow-x-auto">
        <table className="w-full border-collapse bg-[var(--color-surface)]">
          <thead className="sticky top-0 z-10">
            <tr>
              <th className="py-1 px-2 text-left bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]">
                规则
              </th>
              <th className="py-1 px-2 text-left bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]" style={{ width: 80 }}>
                严重度
              </th>
              <th className="py-1 px-2 text-right bg-[var(--color-layer)] border-b border-[var(--color-border)] text-[length:var(--type-caption)] font-semibold text-[var(--color-text-secondary)]" style={{ width: 80 }}>
                数量
              </th>
            </tr>
          </thead>
          <tbody>
            {[...groups.entries()].map(([rule, items]) => {
              const severity = ruleSeverity(rule)
              const sevLabel = severity === 'danger' ? '严重' : '警告'
              const isExpanded = expandedRules.has(rule)
              return (
                <React.Fragment key={rule}>
                  <tr
                    className="cursor-pointer border-t border-[var(--color-border-subtle)] hover:bg-[var(--color-layer)]"
                    onClick={() => toggleRuleExpand(rule)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleRuleExpand(rule) } }}
                  >
                    <td className="py-1 px-2 font-mono text-[length:var(--type-body)] font-medium whitespace-nowrap">
                      {rule}
                    </td>
                    <td className={
                      'py-1 px-2 text-[length:var(--type-caption)] ' +
                      (severity === 'danger'
                        ? 'text-[var(--color-danger-text)]'
                        : 'text-[var(--color-warn-text)]')
                    }>
                      {sevLabel}
                    </td>
                    <td className="py-1 px-2 text-right font-mono tabular-nums text-[length:var(--type-body)]">
                      {items.length}
                    </td>
                  </tr>
                  {isExpanded && (
                    <tr>
                      <td colSpan={3} className="px-2 py-1">
                        <div className="space-y-1">
                          {rule === 'low-quality' && (
                            <div className="flex items-center justify-between pb-1 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">
                              <span>勾选后可移入回收站（可恢复）。上游导入的文档删除后，下次导入会再次出现。</span>
                              <button
                                type="button"
                                data-testid="lint-select-all-lowquality"
                                onClick={() => {
                                  const paths = items.map((i) => i.componentId)
                                  const allChosen = paths.every((p) => doomed.has(p))
                                  setDoomed((prev) => {
                                    const next = new Set(prev)
                                    for (const p of paths) {
                                      if (allChosen) next.delete(p)
                                      else next.add(p)
                                    }
                                    return next
                                  })
                                  setConfirming(false)
                                }}
                                className="shrink-0 border border-[var(--color-border)] px-2 py-0.5 text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
                              >
                                全选/取消
                              </button>
                            </div>
                          )}
                          {items.map((i, idx) => {
                            const suggestion = parseDeadLinkSuggestion(i.detail)
                            if (i.lowQuality) {
                              return (
                                <LowQualityDetail
                                  key={idx}
                                  issue={i}
                                  onOpen={onOpen}
                                  checked={doomed.has(i.componentId)}
                                  onToggle={() => toggleDoomed(i.componentId)}
                                />
                              )
                            }
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
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              )
            })}
          </tbody>
        </table>
      </div>
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
      <div className="flex items-center min-w-0 max-w-[var(--measure)] text-[length:var(--type-caption)] leading-[var(--leading-caption)] text-[var(--color-text-secondary)] pl-1" title={decodedDetail}>
        {hasSuggestion && <input type="checkbox" checked={checked} onChange={onToggle} className="shrink-0 mr-1" />}
        <span className="truncate">{decodedDetail}</span>
        {sourceButton}
      </div>
    )
  }
  const [, prefix, path, suffix, suggestion] = match
  return (
    <div className="flex items-center min-w-0 max-w-[var(--measure)] text-[length:var(--type-caption)] leading-[var(--leading-caption)] text-[var(--color-text-secondary)] pl-1 flex-wrap gap-x-1" title={decodedDetail}>
      {hasSuggestion && <input type="checkbox" checked={checked} onChange={onToggle} className="shrink-0 mr-1" />}
      <span className="shrink-0 whitespace-nowrap">{prefix}</span>
      <span className="min-w-0 truncate font-mono" dir="rtl" style={{ textAlign: 'left' }}>
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

// A low-quality row shows the measurements behind the verdict, not just the
// verdict. The rule is a heuristic - while it was being calibrated, three
// versions of one signal flagged the best-written documents in the corpus - so
// the numbers have to be visible before anyone deletes anything.
function LowQualityDetail({
  issue, onOpen, checked, onToggle,
}: {
  issue: LintIssue
  onOpen?: (path: string) => void
  checked: boolean
  onToggle: () => void
}) {
  const lq = issue.lowQuality!
  const name = issue.componentId.split('/').pop() ?? issue.componentId
  return (
    <div
      data-testid="lint-lowquality-row"
      className="flex items-center gap-2 py-0.5 text-[length:var(--type-caption)]"
    >
      <input
        type="checkbox"
        checked={checked}
        onChange={onToggle}
        aria-label={`选择 ${name} 以删除`}
        className="shrink-0"
      />
      <button
        type="button"
        onClick={() => onOpen?.(issue.componentId)}
        title={issue.componentId}
        className="min-w-0 flex-1 truncate text-left text-[var(--color-link)] hover:underline"
      >
        {name}
      </button>
      <div className="flex shrink-0 flex-wrap items-center gap-1">
        {lq.signals.map((s) => (
          <span
            key={s}
            className="border border-[var(--color-border)] bg-[var(--color-layer)] px-1 text-[var(--color-text-secondary)]"
          >
            {SIGNAL_LABELS[s] ?? s}
          </span>
        ))}
        {lq.imported && (
          <span className="border border-[var(--color-border)] bg-[var(--color-layer)] px-1 text-[var(--color-text-tertiary)]">
            上游导入
          </span>
        )}
      </div>
      <span className="shrink-0 font-mono tabular-nums text-[var(--color-text-secondary)]">
        {lq.chars} 字 · {lq.headings} 标题
        {lq.emptyHeadings > 0 && ` · ${lq.emptyHeadings} 空`}
        {lq.placeholders > 0 && ` · ${lq.placeholders} 占位`}
      </span>
    </div>
  )
}
