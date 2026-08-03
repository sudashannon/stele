import { describe, expect, it } from 'vitest'
import { TYPE_COLORS, TYPE_BADGE_STYLES, typeBadgeClass } from './graphPalette'

// Every indexed component type must be visually distinguishable. `knowledge`,
// `report` and `artifact` all fell through to the same grey fallback, so the
// calendar showed knowledge and report identically and the timeline's document
// layer was one colour for 249 of its 320 recent entries.
const INDEXED_TYPES = [
  'change', 'proposal', 'design', 'tasks', 'spec', 'plan',
  'knowledge', 'report', 'artifact', 'diagram', 'session',
]

describe('type palette', () => {
  it('assigns a fill to every indexed type', () => {
    const missing = INDEXED_TYPES.filter((type) => !TYPE_COLORS[type])
    expect(missing).toEqual([])
  })

  it('keeps those fills distinct', () => {
    const fills = INDEXED_TYPES.map((type) => TYPE_COLORS[type])
    expect(new Set(fills).size).toBe(fills.length)
  })

  it('assigns a badge style to every indexed type', () => {
    const missing = INDEXED_TYPES.filter((type) => !TYPE_BADGE_STYLES[type])
    expect(missing).toEqual([])
    // An unknown type still gets a readable badge rather than nothing.
    expect(typeBadgeClass('brand-new-type')).toContain('bg-')
  })

  it('keeps badge styles distinct so a badge names its type', () => {
    const styles = INDEXED_TYPES.map((type) => TYPE_BADGE_STYLES[type])
    expect(new Set(styles).size).toBe(styles.length)
  })
})
