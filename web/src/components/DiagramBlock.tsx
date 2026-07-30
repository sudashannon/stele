import { useEffect, useRef, useState } from 'react'
import type mermaidApi from 'mermaid'

let mermaidIdCounter = 0
let mermaidModulePromise: Promise<{ default: typeof mermaidApi }> | null = null
let mermaidInitialized = false

async function loadMermaid() {
  mermaidModulePromise ??= import('mermaid')
    .then((module) => {
      if (!mermaidInitialized) {
        module.default.initialize({ startOnLoad: false, theme: 'neutral' })
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
  return <div className="flex justify-center" dangerouslySetInnerHTML={{ __html: svg }} />
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
      <div className="text-[var(--type-caption)] font-medium text-[var(--color-danger)]">{message}</div>
      {detail && (
        <div className="font-mono text-[var(--type-caption)] break-all text-[var(--color-text-secondary)]">
          {detail}
        </div>
      )}
      {onReload && (
        <button
          type="button"
          onClick={onReload}
          className="border border-[var(--color-border)] bg-[var(--color-surface)] px-2 py-1 text-[var(--type-caption)] text-[var(--color-text-primary)] hover:bg-[var(--color-layer)]"
        >
          刷新页面
        </button>
      )}
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
