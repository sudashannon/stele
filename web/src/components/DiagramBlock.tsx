
import { useEffect, useRef, useState } from 'react'

let mermaidIdCounter = 0
let mermaidModulePromise: Promise<typeof import('mermaid')> | null = null
let mermaidInitialized = false

async function loadMermaid() {
  mermaidModulePromise ??= import('mermaid').then((module) => {
    if (!mermaidInitialized) {
      module.default.initialize({ startOnLoad: false, theme: 'neutral' })
      mermaidInitialized = true
    }
    return module
  })
  return mermaidModulePromise
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
  return <div className="flex justify-center" dangerouslySetInnerHTML={{ __html: svg }} />
}

function DiagramFallback({ code, message }: { code: string; message: string }) {
  return (
    <div className="space-y-2" role="status" aria-live="polite">
      <div className="text-[var(--type-caption)] font-medium text-[var(--color-danger)]">{message}</div>
      <pre className="max-h-[200px] overflow-auto bg-[var(--color-layer)] p-3 font-mono text-[var(--type-caption)] whitespace-pre-wrap text-[var(--color-text-primary)]">
        {code}
      </pre>
    </div>
  )
}

function DiagramLoading({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 text-[var(--type-caption)] text-[var(--color-text-secondary)]" role="status">
      <span>{label}</span>
    </div>
  )
}

function MermaidRenderer({ code }: { code: string }) {
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
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
          setError('Mermaid 图表解析失败，已显示源码。')
          return
        }
        setSvg(svg)
      })
      .catch(() => {
        if (!cancelled) setError('Mermaid 图表渲染失败，已显示源码。')
      })

    return () => {
      cancelled = true
    }
  }, [code])

  if (error) return <DiagramFallback code={code} message={error} />
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
