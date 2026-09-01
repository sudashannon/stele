import { describe, expect, it } from 'vitest'
import { parseMarkdownDocument, stripMarkdownInline } from './markdownDocument'

describe('parseMarkdownDocument', () => {
  it('separates a leading simple frontmatter block and preserves the source body position', () => {
    const raw = '---\ntitle: "Viewer document"\nstatus: draft\n---\n\n# Body\n'

    const document = parseMarkdownDocument(raw)

    expect(document.frontmatter).toEqual({
      raw: 'title: "Viewer document"\nstatus: draft\n',
      fields: { title: 'Viewer document', status: 'draft' },
    })
    expect(document.body).toBe('# Body\n')
    expect(document.bodyStartLine).toBe(6)
    expect(document.headings).toEqual([
      { id: 'body', text: 'Body', level: 1, startLine: 6, endLine: 6 },
    ])
  })

  it('keeps scalar fields when a sibling line needs YAML parsing', () => {
    const raw = '---\ntitle: Valid\ntags: [not-a-scalar]\n---\n# Body'

    const document = parseMarkdownDocument(raw)

    expect(document.frontmatter).toEqual({
      raw: 'title: Valid\ntags: [not-a-scalar]\n',
      fields: { title: 'Valid' },
    })
    expect(document.body).toBe('# Body')
  })

  it('skips nested block values while keeping their sibling scalars', () => {
    const raw = '---\ntitle: Nested\ndate: 2026-08-31\nsources:\n  - id: claim.a\n    resource: doc://x/y.md\n---\n# Body'

    const document = parseMarkdownDocument(raw)

    expect(document.frontmatter?.fields).toEqual({
      title: 'Nested',
      date: '2026-08-31',
    })
    expect(document.body).toBe('# Body')
  })

  it('ignores headings inside backtick and tilde fenced code blocks', () => {
    const raw = '# Visible\n```ts\n## Backtick fence\n```\n~~~\n### Tilde fence\n~~~\n## Also visible'

    expect(parseMarkdownDocument(raw).headings).toEqual([
      { id: 'visible', text: 'Visible', level: 1, startLine: 1, endLine: 1 },
      { id: 'also-visible', text: 'Also visible', level: 2, startLine: 8, endLine: 8 },
    ])
  })

  it('uses complete document order for duplicate IDs, including headings outside the TOC range', () => {
    const raw = '# Same\n#### Same\n# Same'

    expect(parseMarkdownDocument(raw).headings).toEqual([
      { id: 'same', text: 'Same', level: 1, startLine: 1, endLine: 1 },
      { id: 'same-1', text: 'Same', level: 4, startLine: 2, endLine: 2 },
      { id: 'same-2', text: 'Same', level: 1, startLine: 3, endLine: 3 },
    ])
  })

  it('accepts headings indented by at most three spaces and reports raw source lines', () => {
    const raw = 'Intro\n\n   ### Indented **heading** with `code` and [a link](https://example.com) ###\n    ## Not a heading\n# Last'

    expect(parseMarkdownDocument(raw).headings).toEqual([
      {
        id: 'indented-heading-with-code-and-a-link',
        text: 'Indented heading with code and a link',
        level: 3,
        startLine: 3,
        endLine: 3,
      },
      { id: 'last', text: 'Last', level: 1, startLine: 5, endLine: 5 },
    ])
  })
})

describe('stripMarkdownInline', () => {
  it('uses rendered inline text for navigation labels', () => {
    expect(stripMarkdownInline('**Bold** `code` and [link](https://example.com)')).toBe('Bold code and link')
  })
})
