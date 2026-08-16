import GithubSlugger from 'github-slugger'

export interface MarkdownFrontmatter {
  /** The exact text between the opening and closing frontmatter delimiters. */
  raw: string
  fields: Record<string, string>
}

export interface MarkdownHeading {
  id: string
  text: string
  level: number
  startLine: number
  endLine: number
}

export interface MarkdownDocumentModel {
  raw: string
  frontmatter: MarkdownFrontmatter | null
  body: string
  /** The 1-based source line on which `body` starts after frontmatter removal. */
  bodyStartLine: number
  headings: MarkdownHeading[]
}

interface ParsedFrontmatter {
  frontmatter: MarkdownFrontmatter
  body: string
  bodyStartLine: number
}

const openingDelimiter = /^---\r?\n/
const closingDelimiter = /\n---[ \t]*(?:\r?\n|$)/g
const fieldLine = /^([A-Za-z_][A-Za-z0-9_-]*)[ \t]*:[ \t]*(.*)$/
const markdownEscape = /\\([!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~])/g

function sourceLineAt(raw: string, offset: number): number {
  let line = 1
  for (let index = 0; index < offset; index += 1) {
    if (raw[index] === '\n') line += 1
  }
  return line
}

function parseScalar(value: string): string | null {
  const trimmed = value.trim()
  if (trimmed === '') return ''

  const quote = trimmed[0]
  if (quote === '"' || quote === "'") {
    if (trimmed.length < 2 || trimmed.at(-1) !== quote) return null
    return trimmed.slice(1, -1)
  }

  // Collections, anchors, and block scalars need YAML parsing. Reject the
  // entire block instead of presenting partially trustworthy metadata.
  if (/^[\[\]{},&*!|>]/.test(trimmed)) return null
  return trimmed
}

function parseFields(frontmatter: string): Record<string, string> {
  const fields: Record<string, string> = {}
  for (const line of frontmatter.split(/\r?\n/)) {
    if (/^[ \t]*(?:#.*)?$/.test(line)) continue
    const match = fieldLine.exec(line)
    if (!match || Object.hasOwn(fields, match[1])) return {}

    const value = parseScalar(match[2])
    if (value === null) return {}
    fields[match[1]] = value
  }
  return fields
}

function parseLeadingFrontmatter(raw: string): ParsedFrontmatter | null {
  const opening = openingDelimiter.exec(raw)
  if (!opening) return null

  closingDelimiter.lastIndex = opening[0].length
  const closing = closingDelimiter.exec(raw)
  if (!closing || closing.index === undefined) return null

  const closingLineStart = closing.index + 1
  const frontmatterEnd = raw[closingLineStart - 2] === '\r' ? closingLineStart - 1 : closingLineStart
  const delimiterEnd = closing.index + closing[0].length
  const body = raw.slice(delimiterEnd).trimStart()
  const bodyOffset = raw.length - body.length

  return {
    frontmatter: {
      raw: raw.slice(opening[0].length, frontmatterEnd),
      fields: parseFields(raw.slice(opening[0].length, frontmatterEnd)),
    },
    body,
    bodyStartLine: sourceLineAt(raw, bodyOffset),
  }
}

function replaceInlineCode(text: string, segments: string[]): string {
  return text.replace(/(`+)([\s\S]*?)\1/g, (_match, _ticks: string, code: string) => {
    const token = `\uE000${segments.length}\uE001`
    segments.push(code)
    return token
  })
}

function removeEmphasis(text: string): string {
  // Underscores inside words are ordinary text, so markers must be separated
  // from word characters just as they are in Markdown's emphasis rules.
  const marker = /(^|[^\p{L}\p{N}_])(\*{1,3}|_{1,3}|~~)(?=\S)([\s\S]*?\S)\2(?=$|[^\p{L}\p{N}_])/gu
  let stripped = text
  for (let pass = 0; pass < 3; pass += 1) {
    stripped = stripped.replace(marker, '$1$3')
  }
  return stripped
}

/**
 * Produces the text ReactMarkdown renders inside an inline heading. This is
 * deliberately small rather than a second Markdown parser: it covers the
 * inline forms that affect navigation labels while GitHubSlugger owns IDs.
 */
export function stripMarkdownInline(text: string): string {
  const protectedSegments: string[] = []
  let stripped = text.replace(markdownEscape, (_match, character: string) => {
    const token = `\uE000${protectedSegments.length}\uE001`
    protectedSegments.push(character)
    return token
  })
  stripped = replaceInlineCode(stripped, protectedSegments)
  stripped = stripped
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/!?\[([^\]]*)\]\[[^\]]*\]/g, '$1')
  stripped = removeEmphasis(stripped)

  return stripped.replace(/\uE000(\d+)\uE001/g, (_match, index: string) => protectedSegments[Number(index)]).trim()
}

function fenceOpening(line: string): { marker: '`' | '~'; length: number } | null {
  const match = /^(?: {0,3})(`{3,}|~{3,})/.exec(line)
  if (!match) return null
  return { marker: match[1][0] as '`' | '~', length: match[1].length }
}

function isFenceClosing(line: string, fence: { marker: '`' | '~'; length: number }): boolean {
  const marker = fence.marker === '`' ? '`' : '~'
  return new RegExp(`^ {0,3}${marker}{${fence.length},}[ \\t]*$`).test(line)
}

function extractHeadings(body: string, bodyStartLine: number): MarkdownHeading[] {
  const slugger = new GithubSlugger()
  const headings: MarkdownHeading[] = []
  let fence: { marker: '`' | '~'; length: number } | null = null

  for (const [index, line] of body.split(/\r?\n/).entries()) {
    if (fence) {
      if (isFenceClosing(line, fence)) fence = null
      continue
    }

    const opening = fenceOpening(line)
    if (opening) {
      fence = opening
      continue
    }

    const match = /^(?: {0,3})(#{1,6})(?:[ \t]+(.*?)|[ \t]*)$/.exec(line)
    if (!match) continue

    const content = (match[2] ?? '').replace(/[ \t]+#+[ \t]*$/, '')
    const text = stripMarkdownInline(content)
    const startLine = bodyStartLine + index
    headings.push({
      id: slugger.slug(text),
      text,
      level: match[1].length,
      startLine,
      endLine: startLine,
    })
  }

  return headings
}

export function parseMarkdownDocument(raw: string): MarkdownDocumentModel {
  const parsedFrontmatter = parseLeadingFrontmatter(raw)
  const body = parsedFrontmatter?.body ?? raw
  const bodyStartLine = parsedFrontmatter?.bodyStartLine ?? 1

  return {
    raw,
    frontmatter: parsedFrontmatter?.frontmatter ?? null,
    body,
    bodyStartLine,
    headings: extractHeadings(body, bodyStartLine),
  }
}
