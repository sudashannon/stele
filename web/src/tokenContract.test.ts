import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * Guards the two token-contract bugs that shipped in production.
 *
 * Neither is observable in jsdom: Tailwind's stylesheet is not loaded during
 * tests, so a rendered element's computed font-size cannot be asserted. The
 * defect lives in what Tailwind compiles the class string into, which is why
 * these assertions read source. SideRail.test.tsx already guards the first bug
 * at a single call site; this generalises both to the whole tree.
 */

const SRC = join(dirname(fileURLToPath(import.meta.url)))

const sourceFiles = (dir: string): string[] =>
  readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) return sourceFiles(path)
    return path.endsWith('.tsx') && !path.includes('.test.') ? [path] : []
  })

const relative = (path: string) => path.slice(SRC.length + 1)

describe('type token usage', () => {
  it('never uses the bare text-[var(--type-*)] form', () => {
    // `text-[var(--type-caption)]` compiles to `color: var(--type-caption)`,
    // because Tailwind cannot tell a size token from a colour one. `color: .75rem`
    // is invalid at computed-value time, so the declaration becomes `unset` and
    // the element loses its font-size AND — since this rule sorts after the
    // colour utilities — its intended colour, inheriting from its ancestor
    // instead. 81 sites across 13 files shipped this way.
    const offenders = sourceFiles(SRC).flatMap((path) => {
      const hits = readFileSync(path, 'utf8').match(/text-\[var\(--type-[a-z-]+\)\]/g)
      return hits ? [`${relative(path)}: ${hits.length}`] : []
    })
    expect(offenders, 'use text-[length:var(--type-*)] so Tailwind emits font-size').toEqual([])
  })

  it('only references custom properties that styles.css declares', () => {
    // `--color-warning` (the declared token is `--color-warn`) and
    // `--color-overlay` were referenced but never declared, so they resolved to
    // unset and those elements silently inherited a parent colour.
    const stylesheet = readFileSync(join(SRC, 'styles.css'), 'utf8')
    const declared = new Set(Array.from(stylesheet.matchAll(/^\s*(--[a-z0-9-]+)\s*:/gm), (m) => m[1]))
    // Set inline on the element that consumes it, so it is never in the stylesheet.
    const setInJsx = new Set(['--timeline-bar-border'])

    const missing = new Map<string, Set<string>>()
    for (const path of sourceFiles(SRC)) {
      for (const [, name] of readFileSync(path, 'utf8').matchAll(/var\((--[a-z0-9-]+)/g)) {
        if (declared.has(name) || setInJsx.has(name)) continue
        const bucket = missing.get(name) ?? new Set<string>()
        bucket.add(relative(path))
        missing.set(name, bucket)
      }
    }
    expect(
      Object.fromEntries(Array.from(missing, ([name, files]) => [name, Array.from(files)])),
      'an undeclared custom property renders as an inherited value, not an error',
    ).toEqual({})
  })
})
