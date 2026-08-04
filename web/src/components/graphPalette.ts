/**
 * CSS-facing graph palette. Keep this module free of Cytoscape imports so
 * timeline, calendar and filter views do not pull the graph renderer into
 * their lazy chunks merely to color a legend.
 */
// Node/badge fill per document type. Every indexed type needs an entry: the
// calendar and the timeline fall back to --color-text-secondary for anything
// missing, and `knowledge` (the largest population by far) plus `report` used to
// land there together with `artifact`, so three types rendered as the same grey
// and the colour carried no information.
export const TYPE_COLORS: Record<string, string> = {
  change: 'var(--color-accent)',
  proposal: 'color-mix(in srgb, var(--color-accent) 45%, var(--color-danger))',
  design: 'color-mix(in srgb, var(--color-success) 55%, var(--color-accent))',
  tasks: 'var(--color-warn)',
  spec: 'color-mix(in srgb, var(--color-warn) 70%, var(--color-danger))',
  plan: 'var(--color-success)',
  knowledge: 'var(--color-teal)',
  report: 'color-mix(in srgb, var(--color-purple) 55%, var(--color-accent))',
  artifact: 'var(--color-text-secondary)',
  diagram: 'var(--color-danger)',
  session: 'var(--color-purple)',
}

/**
 * Node shape per document type for the knowledge graph. When community colour
 * occupies the fill channel (max 8 hues + neutral tail, per the categorical cap),
 * type is encoded as shape — a preattentive channel that does not compete with
 * the community ramp. Cytoscape's built-in shape set supports 12 distinct shapes
 * out of the box, enough for all 11 indexed types.
 *
 * The shape keys match Cytoscape's `shape` style property values exactly.
 */
export const TYPE_SHAPES: Record<string, string> = {
  change: 'triangle',
  proposal: 'diamond',
  design: 'pentagon',
  tasks: 'rectangle',
  spec: 'hexagon',
  plan: 'star',
  knowledge: 'ellipse',
  report: 'round-rectangle',
  artifact: 'vee',
  diagram: 'heptagon',
  session: 'octagon',
}

/** Shape icon IDs used in the graph legend — kept in a separate record so the
 *  legend can reference a stable visual without coupling to Cytoscape's shape
 *  name strings. */
export const TYPE_SHAPE_ORDER: string[] = [
  'change',
  'proposal',
  'design',
  'tasks',
  'spec',
  'plan',
  'knowledge',
  'report',
  'artifact',
  'diagram',
  'session',
]
/** Maximum distinct categorical community colours before collapsing to --viz-rest. */
export const COMMUNITY_CATEGORICAL_LIMIT = 8

export const COMMUNITY_COLORS = [
  'var(--viz-1)',
  'var(--viz-2)',
  'var(--viz-3)',
  'var(--viz-4)',
  'var(--viz-5)',
  'var(--viz-6)',
  'var(--viz-7)',
  'var(--viz-8)',
]
export const COMMUNITY_REST_COLOR = 'var(--viz-rest)'

/** Colour for a community at a given 0-based rank by weight.
 *  Ranks 0–7 map to --viz-1…--viz-8; rank ≥ 8 returns --viz-rest. */
export function communityColor(rank: number): string {
  return rank < COMMUNITY_COLORS.length ? COMMUNITY_COLORS[rank] : COMMUNITY_REST_COLOR
}

/**
 * Document-type badge styling for the list views (最近更新 / 语义搜索). Both
 * lists hard-coded `bg-[var(--color-accent)]`, so every type rendered in the
 * same blue and the badge carried no information. Pairs follow the existing
 * PHASE_STYLES convention in ChangeExplorer — a subtle tint behind a saturated
 * glyph color — and never use --color-warn as a text color (see the token note
 * in styles.css); amber text uses --color-warn-text instead.
 */
export const TYPE_BADGE_STYLES: Record<string, string> = {
  change: 'bg-[var(--color-accent-subtle)] text-[var(--color-accent)]',
  proposal: 'bg-[var(--color-purple-subtle)] text-[var(--color-purple)]',
  design: 'bg-[var(--color-layer)] text-[var(--color-teal)]',
  tasks: 'bg-[var(--color-warn-subtle)] text-[var(--color-warn-text)]',
  spec: 'bg-[var(--color-danger-subtle)] text-[var(--color-danger-text)]',
  plan: 'bg-[var(--color-success-subtle)] text-[var(--color-success-text)]',
  knowledge: 'bg-[var(--color-layer-accent)] text-[var(--color-text-primary)]',
  report: 'bg-[var(--color-layer)] text-[var(--color-accent)]',
  artifact: 'bg-[var(--color-layer)] text-[var(--color-text-secondary)]',
  diagram: 'bg-[var(--color-layer)] text-[var(--color-danger-text)]',
  // Shares the purple identity of the graph node color, but pairs it with the
  // plain layer background used by design/report/diagram so the badge never
  // reads as `proposal` (which owns the purple-subtle background).
  session: 'bg-[var(--color-layer)] text-[var(--color-purple)]',
}

const FALLBACK_TYPE_BADGE = 'bg-[var(--color-layer)] text-[var(--color-text-secondary)]'

export function typeBadgeClass(type: string): string {
  return TYPE_BADGE_STYLES[type] ?? FALLBACK_TYPE_BADGE
}
