import { useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import {
  streamChat,
  fetchChatSession,
  fetchChangeDetail,
  type ChatSessionMessage,
} from '../api/client'
import { Icon } from './icons'
import { SearchableCombobox, type SearchableComboboxOption } from './SearchableCombobox'

interface ChatMessage {
  role: 'user' | 'assistant' | 'error'
  text: string
  thinking?: string
}

function toChatMessage(msg: ChatSessionMessage): ChatMessage {
  const role = msg.role === 'user' ? 'user' : 'assistant'
  let text = ''
  let thinking = ''
  for (const block of msg.content ?? []) {
    if (block.type === 'thinking') thinking += block.thinking ?? ''
    else text += block.text ?? ''
  }
  return thinking ? { role, text, thinking } : { role, text }
}

function rankContextFile(path: string, workspace?: string) {
  if (!workspace) return 1
  return path.includes(`/${workspace}/`) || path.startsWith(`${workspace}/`) ? 0 : 1
}

export function ChatBubble({ changeName, workspace, documentPath, componentId }: {
  changeName?: string
  workspace?: string
  documentPath?: string
  componentId?: string
}) {
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [contextFiles, setContextFiles] = useState<string[]>([])
  const [selectedFiles, setSelectedFiles] = useState<string[]>([])
  const [contextPanelOpen, setContextPanelOpen] = useState(true)
  const [graphMode, setGraphMode] = useState(true)
  const [comboboxValue, setComboboxValue] = useState('')
  const messagesRef = useRef<HTMLDivElement>(null)
  const userActedRef = useRef(false)
  const launcherRef = useRef<HTMLButtonElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const wasOpenRef = useRef(false)

  useEffect(() => {
    if (open) {
      inputRef.current?.focus()
      wasOpenRef.current = true
    } else if (wasOpenRef.current) {
      launcherRef.current?.focus()
      wasOpenRef.current = false
    }
  }, [open])
  const effectiveChange = changeName || (documentPath ? documentPath.split(/[\\/]/).pop() || documentPath : '')
  const sessionKey = componentId || effectiveChange

  useEffect(() => {
    let cancelled = false
    if (!sessionKey) return
    fetchChatSession(sessionKey)
      .then((session) => {
        if (cancelled || userActedRef.current) return
        setMessages((session.messages ?? []).map(toChatMessage))
      })
      .catch(() => {
        // 加载历史失败不阻塞聊天：静默保持空会话，用户仍可正常发送新消息。
      })
    return () => {
      cancelled = true
    }
  }, [sessionKey])

  useEffect(() => {
    if (!changeName) return
    let cancelled = false
    fetchChangeDetail(changeName, workspace)
      .then((detail) => {
        if (cancelled) return
        const paths = Array.from(new Set(
          (detail.phases ?? [])
            .flatMap((phase) => phase.artifacts ?? [])
            .filter((artifact) => artifact.exists)
            .map((artifact) => artifact.path || artifact.file),
        ))
        setContextFiles(paths)
      })
      .catch(() => {
        // 产物清单加载失败不阻塞聊天：静默保持空的上下文文件选择器。
      })
    return () => {
      cancelled = true
    }
  }, [changeName, workspace])

  const contextOptions = useMemo<SearchableComboboxOption[]>(
    () => contextFiles
      .filter((path) => !selectedFiles.includes(path))
      .sort((left, right) => {
        const rankDiff = rankContextFile(left, workspace) - rankContextFile(right, workspace)
        if (rankDiff !== 0) return rankDiff
        return left.localeCompare(right, 'zh-CN')
      })
      .map((path) => ({
        value: path,
        label: path.split('/').pop() || path,
        description: path,
        group: rankContextFile(path, workspace) === 0 ? '当前工作区' : '其他文档',
        keywords: path,
      })),
    [contextFiles, selectedFiles, workspace],
  )

  useEffect(() => {
    if (messagesRef.current) messagesRef.current.scrollTop = messagesRef.current.scrollHeight
  }, [messages])

  function toggleContextFile(path: string) {
    setSelectedFiles((prev) => (prev.includes(path) ? prev.filter((item) => item !== path) : [...prev, path]))
  }

  async function handleSend() {
    const text = input.trim()
    if (!text || sending) return
    userActedRef.current = true

    // The document open in the viewer is what the user is looking at, so it is
    // always part of the prompt. Routing context through `selectedFiles` alone
    // silently dropped it whenever a change happened to be selected: that list
    // starts empty and is only filled by the picker below, so the model
    // answered "您没有提供任何文档内容" while a document was on screen.
    const filesToSend = [...new Set([...(documentPath ? [documentPath] : []), ...selectedFiles])]
    setMessages((prev) => [...prev, { role: 'user', text }])
    setInput('')
    setSending(true)
    setMessages((prev) => [...prev, { role: 'assistant', text: '', thinking: '' }])

    try {
      await streamChat(
        effectiveChange,
        text,
        filesToSend,
        (event) => {
          setMessages((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (last?.role !== 'assistant') return prev
            if (event.type === 'thinking') {
              next[next.length - 1] = { ...last, thinking: (last.thinking ?? '') + (event.content ?? '') }
            } else if (event.type === 'delta') {
              next[next.length - 1] = { ...last, text: last.text + (event.content ?? '') }
            }
            return next
          })
        },
        graphMode,
        componentId,
      )
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setMessages((prev) => [...prev, { role: 'error', text: message }])
    } finally {
      setSending(false)
    }
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void handleSend()
    }
  }

  return (
    <>
      <button
        ref={launcherRef}
        data-testid="chat-bubble-button"
        aria-label="打开聊天"
        onClick={() => setOpen(true)}
        className="fixed bottom-4 right-4 flex h-10 w-10 items-center justify-center border border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)] shadow-[var(--shadow-2)]"
      >
        <Icon name="chat" />
      </button>
      {open && (
        <section
          data-testid="chat-overlay"
          role="dialog"
          aria-label={effectiveChange ? `Chat · ${effectiveChange}` : '聊天'}
          className="fixed bottom-20 right-4 flex h-[min(80vh,720px)] w-[440px] flex-col border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-overlay)]"
        >
          <div className="flex items-center justify-between border-b border-[var(--color-border)] px-3 py-3">
            <span className="text-[length:var(--type-body)] leading-[var(--leading-body)] font-semibold text-[var(--color-text-primary)]">Chat · {effectiveChange}</span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                data-testid="chat-graph-mode-toggle"
                aria-pressed={graphMode}
                onClick={() => setGraphMode((value) => !value)}
                title="图谱模式：将当前变更在知识图谱中的关联上下文注入对话"
                className={
                  'flex items-center gap-1 border px-2 py-1 text-[length:var(--type-caption)] font-medium ' +
                  (graphMode
                    ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-color)]'
                    : 'border-[var(--color-border)] bg-[var(--color-layer)] text-[var(--color-text-secondary)]')
                }
              >
                <Icon name="graph" />
                <span>图谱模式</span>
              </button>
              <button
                type="button"
                data-testid="chat-overlay-close"
                aria-label="关闭聊天"
                onClick={() => setOpen(false)}
                className="text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
              >
                <Icon name="close" />
              </button>
            </div>
          </div>
          <div data-testid="chat-messages" ref={messagesRef} className="flex-1 space-y-2 overflow-y-auto p-3 text-[length:var(--type-body)]">
            {messages.map((msg, index) => (
              <div
                key={index}
                data-testid={`chat-msg-${msg.role}`}
                className={
                  'max-w-[85%] px-3 py-2 text-[length:var(--type-caption)] ' +
                  (msg.role === 'user'
                    ? 'ml-auto bg-[var(--color-accent)] text-[var(--color-text-on-color)] whitespace-pre-wrap'
                    : msg.role === 'error'
                      ? 'border border-[var(--color-danger)] bg-[var(--color-danger-subtle)] text-[var(--color-danger-text)]'
                      : 'bg-[var(--color-layer)] text-[var(--color-text-primary)]')
                }
              >
                {msg.role === 'assistant' ? (
                  <>
                    {msg.thinking && !msg.text && (
                      <div className="mb-1 text-[length:var(--type-caption)] italic text-[var(--color-text-secondary)]">思考中：{msg.thinking}</div>
                    )}
                    <div className="prose prose-sm max-w-none [&_p]:my-1">
                      <ReactMarkdown>{msg.text}</ReactMarkdown>
                    </div>
                  </>
                ) : (
                  msg.text
                )}
              </div>
            ))}
          </div>
          {(documentPath || contextFiles.length > 0) && (
            <div className="border-t border-[var(--color-border)]">
              {documentPath && (
                <div
                  data-testid="chat-current-document"
                  className="flex items-center gap-1.5 px-3 py-2 text-[length:var(--type-caption)] text-[var(--color-text-secondary)]"
                >
                  <Icon name="check" size={12} className="shrink-0 text-[var(--color-success-text)]" />
                  <span className="shrink-0">当前文档</span>
                  <span className="truncate text-[var(--color-text-primary)]" title={documentPath}>
                    {documentPath.split(/[\\/]/).pop()}
                  </span>
                </div>
              )}
              {contextFiles.length > 0 && (
                <button
                  type="button"
                  data-testid="context-panel-toggle"
                  onClick={() => setContextPanelOpen((value) => !value)}
                  className="flex w-full items-center justify-between border-t border-[var(--color-border-subtle)] px-3 py-2 text-[length:var(--type-caption)] font-medium text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
                >
                  <span>
                    追加文档
                    {selectedFiles.length > 0 ? ` (已选 ${selectedFiles.length}/${contextFiles.length})` : ` (${contextFiles.length})`}
                  </span>
                  <Icon name={contextPanelOpen ? 'chevron-down' : 'chevron-right'} />
                </button>
              )}
              {contextPanelOpen && contextFiles.length > 0 && (
                <div className="space-y-2 px-3 pb-3">
                  <div className="space-y-2">
                    <label htmlFor="chat-context-combobox" className="block text-[length:var(--type-caption)] font-medium text-[var(--color-text-secondary)]">
                      添加文档
                    </label>
                    <SearchableCombobox
                      data-testid="context-file-combobox"
                      options={contextOptions}
                      value={comboboxValue}
                      onChange={(value) => {
                        toggleContextFile(value)
                        setComboboxValue('')
                      }}
                      placeholder="搜索并添加文档…"
                      ariaLabel="搜索并添加上下文文档"
                      emptyText="无可添加文档"
                      maxResults={8}
                    />
                  </div>
                  {selectedFiles.length > 0 && (
                    <div data-testid="context-file-list" className="flex flex-wrap gap-1.5">
                      {selectedFiles.map((path) => (
                        <button
                          key={path}
                          type="button"
                          data-testid={`context-file-chip-${path}`}
                          onClick={() => toggleContextFile(path)}
                          className="flex items-center gap-1 border border-[var(--color-accent)] bg-[var(--color-accent-subtle)] px-2 py-1 text-[length:var(--type-caption)] text-[var(--color-accent)]"
                          title={path}
                        >
                          <span className="max-w-[220px] truncate">{path.split('/').pop()}</span>
                          <Icon name="close" size={14} />
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
          <div className="flex gap-2 border-t border-[var(--color-border)] p-3">
            <textarea
              ref={inputRef}
              data-testid="chat-input"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="询问关于此变更的问题…"
              className="h-10 flex-1 resize-none border border-[var(--color-border)] bg-[var(--color-surface)] p-2 text-[length:var(--type-body)]"
            />
            <button
              type="button"
              data-testid="chat-send"
              onClick={() => void handleSend()}
              disabled={sending || !input.trim()}
              className="border border-[var(--color-accent)] bg-[var(--color-accent)] px-3 text-[length:var(--type-body)] text-[var(--color-text-on-color)] disabled:opacity-50"
            >
              发送
            </button>
          </div>
        </section>
      )}
    </>
  )
}
