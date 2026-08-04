import { useEffect, useRef, useState } from 'react'
import type mermaidApi from 'mermaid'

let mermaidIdCounter = 0
let mermaidModulePromise: Promise<{ default: typeof mermaidApi }> | null = null
let mermaidInitialized = false

async function loadMermaid() {
  mermaidModulePromise ??= import('mermaid')
    .then((module) => {
      if (!mermaidInitialized) {
        // `useMaxWidth` is mermaid's default and it caps each diagram at its own
        // intrinsic width, so a small graph stayed small no matter how wide the
        // reading column got. Turning it off per diagram type makes mermaid emit
        // a plain viewBox, and DiagramSvg then scales it with CSS.
        module.default.initialize({
          startOnLoad: false,
          theme: 'neutral',
          flowchart: { useMaxWidth: false },
          sequence: { useMaxWidth: false },
          gantt: { useMaxWidth: false },
          class: { useMaxWidth: false },
          state: { useMaxWidth: false },
          journey: { useMaxWidth: false },
          er: { useMaxWidth: false },
        })
        mermaidInitialized = true
      }
      return module
    })
    .catch((reason: unknown) => {
      // Drop the rejected promise so a later block can retry instead of
      // replaying the same failure for the rest of the tab's lifetime.
      mermaidModulePromise = null
      throw reason
    })
  return mermaidModulePromise
}

// Hashed chunks are served `immutable`, so a tab that stayed open across a
// redeploy keeps requesting chunk names the server no longer has. The import
// then fails with a fetch/module-script error that has nothing to do with the
// diagram source, and only a reload can recover it.
function isStaleBundleError(reason: unknown): boolean {
  const message = reason instanceof Error ? reason.message : String(reason)
  return /dynamically imported module|module script failed|Failed to fetch/i.test(message)
}

function describeReason(reason: unknown): string {
  const message = reason instanceof Error ? reason.message : String(reason)
  return message.length > 200 ? `${message.slice(0, 200)}…` : message
}

// Kroki's diagram-by-URL endpoints expect the source deflate-compressed and
// then base64url-encoded: https://docs.kroki.io/kroki/setup/encode-diagram/
async function encodeForKroki(source: string): Promise<string> {
  const data = new TextEncoder().encode(source)
  const cs = new CompressionStream('deflate')
  const writer = cs.writable.getWriter()
  writer.write(data)
  writer.close()
  const compressed = await new Response(cs.readable).arrayBuffer()
  return btoa(String.fromCharCode(...new Uint8Array(compressed)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
}

function DiagramSvg({ svg }: { svg: string }) {
  const hostRef = useRef<HTMLDivElement | null>(null)

  // Normalise the emitted SVG in the DOM rather than by rewriting its markup,
  // because the markup arrives from the renderer as a string.
  //
  // Two separate failures are being corrected here, and the second was caused by
  // the first attempt at the first:
  //
  // 1. Both mermaid and kroki ship absolute `width`/`height` plus an inline
  //    `max-width`, pinning the drawing to its intrinsic size so it sat stranded
  //    in the corner of a wide container. And the emitted viewBox is not always
  //    tight around the drawing, which strands it in the top-left even after the
  //    absolute sizes are gone. So the viewBox is recomputed from the rendered
  //    content's own bounding box, which is the only authority on where the
  //    drawing actually is.
  // 2. `width:100%; height:auto` alone then blew up tall narrow diagrams: a
  //    317x1614 flowchart became 1172x5962 on screen. Height must be bounded, so
  //    the SVG is fitted inside both constraints and `preserveAspectRatio`
  //    letterboxes rather than distorts.
  useEffect(() => {
    const el = hostRef.current?.querySelector('svg')
    if (!el) return
    el.removeAttribute('width')
    el.removeAttribute('height')
    el.style.removeProperty('max-width')
    el.setAttribute('preserveAspectRatio', 'xMidYMid meet')

    // Measure the svg element itself: getBBox on an <svg> returns the union of
    // all its children. Measuring `querySelector('g')` instead took only the
    // FIRST group, which on a sequence diagram is a single actor box — that
    // cropped the viewBox to a 267x73 fragment of a 1168x501 drawing.
    let box: DOMRect | null = null
    try {
      const b = (el as SVGGraphicsElement).getBBox()
      if (b.width > 0 && b.height > 0) box = b as DOMRect
    } catch {
      // getBBox throws on a detached or display:none subtree; the emitted viewBox
      // is then the best available information.
    }
    if (box) {
      const pad = Math.max(4, Math.min(box.width, box.height) * 0.02)
      el.setAttribute(
        'viewBox',
        `${box.x - pad} ${box.y - pad} ${box.width + pad * 2} ${box.height + pad * 2}`,
      )
    }

    const vb = el.getAttribute('viewBox')?.split(/[\s,]+/).map(Number)
    const ratio = vb && vb.length === 4 && vb[2] > 0 && vb[3] > 0 ? vb[3] / vb[2] : null
    el.style.width = '100%'
    el.style.height = 'auto'
    // Bound the height so a tall diagram stays on one screen. Only tall shapes
    // need it; a wide one never reaches the cap.
    el.style.maxHeight = ratio !== null && ratio > 1 ? '78vh' : ''
  }, [svg])

  return <div ref={hostRef} className="w-full" dangerouslySetInnerHTML={{ __html: svg }} />
}

function DiagramFallback({
  code,
  message,
  detail,
  onReload,
}: {
  code: string
  message: string
  detail?: string
  onReload?: () => void
}) {
  return (
    <div className="space-y-2" role="status" aria-live="polite">
      <div className="text-[length:var(--type-caption)] font-medium text-[var(--color-danger-text)]">{message}</div>
      {detail && (
        <div className="font-mono text-[length:var(--type-caption)] break-all text-[var(--color-text-secondary)]">
          {detail}
        </div>
      )}
      {onReload && (
        <button
          type="button"
          onClick={onReload}
          className="border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
        >
          刷新页面
        </button>
      )}
      <pre className="max-h-[200px] overflow-auto bg-[var(--color-layer)] p-3 font-mono text-[length:var(--type-caption)] whitespace-pre-wrap text-[var(--color-text-primary)]">
        {code}
      </pre>
    </div>
  )
}

function DiagramLoading({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]" role="status">
      <span>{label}</span>
    </div>
  )
}

interface DiagramError {
  message: string
  detail?: string
  stale?: boolean
}

function MermaidRenderer({ code }: { code: string }) {
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<DiagramError | null>(null)
  const idRef = useRef('')
  if (!idRef.current) idRef.current = `mermaid-${++mermaidIdCounter}`

  useEffect(() => {
    let cancelled = false
    setSvg(null)
    setError(null)

    loadMermaid()
      .then(({ default: mermaid }) => mermaid.render(idRef.current, code))
      .then(({ svg }) => {
        if (cancelled) return
        if (svg.includes('Syntax error') || svg.includes('Parse error')) {
          setError({ message: 'Mermaid 图表解析失败，已显示源码。' })
          return
        }
        setSvg(svg)
      })
      .catch((reason: unknown) => {
        if (cancelled) return
        console.error('mermaid render failed', reason)
        setError(
          isStaleBundleError(reason)
            ? {
                message: '图表渲染器加载失败：面板已更新，请刷新页面。',
                detail: describeReason(reason),
                stale: true,
              }
            : { message: 'Mermaid 图表渲染失败，已显示源码。', detail: describeReason(reason) },
        )
      })

    return () => {
      cancelled = true
    }
  }, [code])

  if (error)
    return (
      <DiagramFallback
        code={code}
        message={error.message}
        detail={error.detail}
        onReload={error.stale ? () => window.location.reload() : undefined}
      />
    )
  if (svg === null) return <DiagramLoading label="正在加载 Mermaid 渲染器…" />
  return <DiagramSvg svg={svg} />
}

function PlantUmlRenderer({ code }: { code: string }) {
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    setSvg(null)
    setError(null)

    encodeForKroki(code)
      .then((encoded) => fetch(`https://kroki.io/plantuml/svg/${encoded}`, { signal: controller.signal }))
      .then((res) => {
        if (!res.ok) throw new Error(`kroki request failed: ${res.status}`)
        return res.text()
      })
      .then((nextSvg) => {
        if (!cancelled) setSvg(nextSvg)
      })
      .catch((reason: unknown) => {
        const aborted =
          typeof reason === 'object' &&
          reason !== null &&
          'name' in reason &&
          reason.name === 'AbortError'
        if (!cancelled && !controller.signal.aborted && !aborted) {
          setError('PlantUML 图表渲染失败，已显示源码。')
        }
      })

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [code])

  if (error) return <DiagramFallback code={code} message={error} />
  if (svg === null) return <DiagramLoading label="正在加载图表…" />
  return <DiagramSvg svg={svg} />
}

interface Props {
  language: 'mermaid' | 'plantuml'
  code: string
}

export function DiagramBlock({ language, code }: Props) {
  return (
    <div className="mb-3 overflow-x-auto border border-[var(--color-border-subtle)] bg-[var(--color-layer)] p-4">
      {language === 'mermaid' ? <MermaidRenderer code={code} /> : <PlantUmlRenderer code={code} />}
    </div>
  )
}
