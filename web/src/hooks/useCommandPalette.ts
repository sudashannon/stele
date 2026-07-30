import { useState, useMemo, useCallback } from 'react'

// ── Command Action ──────────────────────────────────────────────────────────

export interface CommandAction {
  id: string
  label: string
  subtitle?: string
  shortcut?: string
  /** Category for grouping in the palette */
  category: string
  /** Optional legacy icon identifier; the palette maps actions to shared icons. */
  icon?: string
  run: () => void
}

// ── Fuzzy Match ─────────────────────────────────────────────────────────────
// Lightweight fuzzy search with relevance scoring and match indices for
// character-level highlighting in the palette UI.
// Ported from Worklog's command-palette.svelte.ts

export interface FuzzyMatchResult {
  score: number
  indices: number[]
}

/** Fuzzy-match `query` against `text`. Returns score + matched char indices. */
export function fuzzyMatch(
  query: string,
  text: string,
): FuzzyMatchResult | null {
  const q = query.toLowerCase().trim()
  const t = text.toLowerCase().trim()

  if (!q) return { score: 1000, indices: [] }
  if (q.length > t.length) return null

  // 1. Exact match
  if (t === q) {
    return {
      score: 1000,
      indices: Array.from({ length: q.length }, (_, i) => i),
    }
  }

  // 2. Prefix match
  if (t.startsWith(q)) {
    return {
      score: 800,
      indices: Array.from({ length: q.length }, (_, i) => i),
    }
  }

  // 3. Substring (anywhere)
  const subIdx = t.indexOf(q)
  if (subIdx !== -1) {
    return {
      score: subIdx === 0 ? 800 : 600,
      indices: Array.from({ length: q.length }, (_, i) => subIdx + i),
    }
  }

  // 4. Fuzzy — scan left-to-right matching chars with gaps
  const indices: number[] = []
  let textIdx = 0
  let gaps = 0
  let consecutive = true

  for (let qi = 0; qi < q.length; qi++) {
    const qc = q[qi]
    while (textIdx < t.length && t[textIdx] !== qc) {
      textIdx++
    }
    if (textIdx >= t.length) return null

    if (indices.length > 0 && textIdx !== indices[indices.length - 1] + 1) {
      consecutive = false
      gaps += textIdx - indices[indices.length - 1] - 1
    }
    indices.push(textIdx)
    textIdx++
  }

  if (gaps > q.length * 3) return null

  const score = consecutive ? 500 : Math.max(300, 500 - gaps * 10)
  return { score, indices }
}

/** Wrap matched characters in `<mark>` tags for highlighting. */
export function highlightMatches(text: string, indices: number[]): string {
  if (!indices || indices.length === 0) return escapeHtml(text)
  const chars: string[] = []
  let lastIdx = 0
  const sorted = [...new Set(indices)].sort((a, b) => a - b)

  for (const idx of sorted) {
    if (idx >= text.length) continue
    if (idx > lastIdx) {
      chars.push(escapeHtml(text.slice(lastIdx, idx)))
    }
    chars.push(`<mark class="palette-match">${escapeHtml(text[idx])}</mark>`)
    lastIdx = idx + 1
  }
  if (lastIdx < text.length) {
    chars.push(escapeHtml(text.slice(lastIdx)))
  }
  return chars.join('')
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

// ── Command Registry Hook ───────────────────────────────────────────────────

export interface ScoredAction {
  action: CommandAction
  score: number
  labelIndices: number[]
  subtitleIndices: number[]
}

export interface UseCommandPaletteReturn {
  /** All registered commands */
  actions: CommandAction[]
  /** Filtered + scored results for the current query */
  results: ScoredAction[]
  /** Whether the palette is open */
  open: boolean
  /** Current search query */
  query: string
  /** Set search query */
  setQuery: (q: string) => void
  /** Open the palette */
  openPalette: () => void
  /** Close the palette */
  closePalette: () => void
  /** Toggle palette open/close */
  togglePalette: () => void
}

const CATEGORY_ORDER: Record<string, number> = {
  Navigation: 1,
  Actions: 2,
  Commands: 3,
}

export function useCommandPalette(
  actions: CommandAction[],
): UseCommandPaletteReturn {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')

  const openPalette = useCallback(() => {
    setOpen(true)
    setQuery('')
  }, [])

  const closePalette = useCallback(() => {
    setOpen(false)
    setQuery('')
  }, [])

  const togglePalette = useCallback(() => {
    if (open) {
      closePalette()
    } else {
      openPalette()
    }
  }, [open, openPalette, closePalette])

  const results = useMemo<ScoredAction[]>(() => {
    const q = query.trim().toLowerCase()
    let scored: ScoredAction[] = actions.map((action) => {
      const labelResult = fuzzyMatch(query, action.label)
      const subtitleResult =
        action.subtitle ? fuzzyMatch(query, action.subtitle) : null
      const score = Math.max(
        labelResult?.score ?? 0,
        subtitleResult?.score ?? 0,
      )
      return {
        action,
        score,
        labelIndices: labelResult?.indices ?? [],
        subtitleIndices: subtitleResult?.indices ?? [],
      }
    })

    // Filter: show all when empty query, else only matches
    if (q) {
      scored = scored.filter((s) => s.score > 0)
    }

    // Sort: score desc, then category order, then label alpha
    scored.sort((a, b) => {
      if (b.score !== a.score) return b.score - a.score
      const catA = CATEGORY_ORDER[a.action.category] ?? 99
      const catB = CATEGORY_ORDER[b.action.category] ?? 99
      if (catA !== catB) return catA - catB
      return a.action.label.localeCompare(b.action.label)
    })

    return scored
  }, [query, actions])

  return {
    actions,
    results,
    open,
    query,
    setQuery,
    openPalette,
    closePalette,
    togglePalette,
  }
}
