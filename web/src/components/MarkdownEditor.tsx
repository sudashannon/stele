import { useEffect, useRef, useState } from 'react'
import { ArtifactRequestError, fetchArtifactText, saveArtifactText } from '../api/client'

interface Props {
  path: string
  workspace?: string
  /** Returns to the read-only viewer after the editor has handled dirty state. */
  onClose: () => void
  /** Keeps the shell from discarding dirty source through external navigation. */
  onDirtyChange?: (dirty: boolean) => void
}

type EditorState = 'loading' | 'ready' | 'saving' | 'error' | 'conflict'

export function MarkdownEditor({ path, workspace, onClose, onDirtyChange }: Props) {
  const [content, setContent] = useState('')
  const [savedContent, setSavedContent] = useState('')
  const [etag, setEtag] = useState<string | null>(null)
  const [state, setState] = useState<EditorState>('loading')
  const [message, setMessage] = useState<string | null>(null)
  const requestRef = useRef(0)
  const dirty = content !== savedContent

  useEffect(() => {
    onDirtyChange?.(dirty)
  }, [dirty, onDirtyChange])

  async function loadServerVersion() {
    const requestId = ++requestRef.current
    setState('loading')
    setMessage(null)
    try {
      const artifact = await fetchArtifactText(path, workspace)
      if (requestRef.current !== requestId) return
      setContent(artifact.content)
      setSavedContent(artifact.content)
      setEtag(artifact.etag)
      setState('ready')
    } catch (error) {
      if (requestRef.current !== requestId) return
      setState('error')
      setMessage(error instanceof Error ? error.message : '加载文档失败')
    }
  }

  useEffect(() => {
    void loadServerVersion()
    // Changing the selected document must replace the editor's source and ETag.
    // loadServerVersion tracks request order so a previous document cannot win.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, workspace])

  async function save() {
    if (!etag || !dirty || state === 'saving') return
    setState('saving')
    setMessage(null)
    try {
      const result = await saveArtifactText(path, content, etag, workspace)
      setEtag(result.etag)
      setSavedContent(content)
      setState('ready')
      setMessage('已保存')
    } catch (error) {
      if (error instanceof ArtifactRequestError && error.status === 412) {
        // A conflict must leave the textarea untouched so the author can decide
        // whether to copy changes or deliberately load the server version.
        setEtag(error.etag)
        setState('conflict')
        setMessage(error.message)
        return
      }
      setState('error')
      setMessage(error instanceof Error ? error.message : '保存失败')
    }
  }

  function closeEditor() {
    if (!dirty || window.confirm('有未保存的修改，确定放弃并返回阅读吗？')) {
      onDirtyChange?.(false)
      onClose()
    }
  }

  const filename = path.split('/').pop() ?? path
  const busy = state === 'loading' || state === 'saving'

  return (
    <section className="flex h-full min-h-0 flex-col bg-[var(--color-surface)] shadow-[var(--shadow-1)]" aria-label={`编辑 ${filename}`}>
      <header className="flex shrink-0 items-center justify-between gap-4 border-b border-[var(--color-border)] px-6 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="truncate font-semibold text-[var(--color-text-primary)]">{filename}</h2>
            <span className="border border-[var(--color-border)] bg-[var(--color-layer)] px-1.5 py-0.5 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">编辑中</span>
          </div>
          <p className="truncate font-[var(--font-mono)] text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{path}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            type="button"
            onClick={() => void save()}
            disabled={busy || state === 'conflict' || !dirty || !etag}
            data-testid="markdown-editor-save"
            className="border border-[var(--color-border)] px-3 py-1.5 text-[length:var(--type-caption)] font-medium text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)] disabled:cursor-not-allowed disabled:opacity-50"
          >
            {state === 'saving' ? '保存中…' : '保存'}
          </button>
          <button
            type="button"
            onClick={closeEditor}
            data-testid="markdown-editor-close"
            className="border border-[var(--color-border)] px-3 py-1.5 text-[length:var(--type-caption)] font-medium text-[var(--color-accent)] hover:border-[var(--color-accent)] hover:bg-[var(--color-layer)]"
          >
            返回阅读
          </button>
        </div>
      </header>
      {state === 'conflict' && (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--color-warning)] bg-[var(--color-layer)] px-6 py-3 text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
          <span>{message ?? '服务器版本已更新，未保存的修改仍保留在编辑器中。'}</span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => {
                setState('ready')
                setMessage('已保留我的修改；再次保存将以服务器当前版本为基础。')
              }}
              className="border border-[var(--color-border)] px-2 py-1 hover:border-[var(--color-accent)]"
            >
              保留我的修改
            </button>
            <button
              type="button"
              onClick={() => void loadServerVersion()}
              data-testid="markdown-editor-reload-server"
              className="border border-[var(--color-border)] px-2 py-1 hover:border-[var(--color-accent)]"
            >
              重新加载服务器版本
            </button>
          </div>
        </div>
      )}
      {state === 'error' && (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--color-warning)] bg-[var(--color-layer)] px-6 py-3 text-[length:var(--type-caption)] text-[var(--color-text-primary)]">
          <span>{message ?? '操作失败'}</span>
          <button type="button" onClick={() => void loadServerVersion()} className="border border-[var(--color-border)] px-2 py-1 hover:border-[var(--color-accent)]">重试加载</button>
        </div>
      )}
      {message && state === 'ready' && <p role="status" className="shrink-0 border-b border-[var(--color-border)] px-6 py-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]">{message}</p>}
      <textarea
        aria-label="Markdown 源码"
        value={content}
        onChange={(event) => {
          const nextContent = event.target.value
          setContent(nextContent)
          onDirtyChange?.(nextContent !== savedContent)
          if (state === 'conflict' || state === 'error') setState('ready')
          setMessage(null)
        }}
        disabled={state === 'loading'}
        spellCheck={false}
        className="min-h-0 flex-1 resize-none bg-[var(--color-surface)] p-6 font-[var(--font-mono)] text-[length:var(--type-caption)] leading-6 text-[var(--color-text-primary)] outline-none focus:bg-[var(--color-layer)] disabled:cursor-wait"
      />
    </section>
  )
}
