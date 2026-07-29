import { useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import {
  streamChat,
  fetchChatSession,
  fetchChangeDetail,
  type ChatSessionMessage,
} from '../api/client'

interface ChatMessage {
  role: 'user' | 'assistant' | 'error'
  text: string
  thinking?: string
}

// Backend content blocks separate `text` and `thinking` blocks (see
// provider.ContentBlock); the component's ChatMessage flattens each kind
// into its own field, matching how handleSend already accumulates streamed
// deltas/thinking onto a single message.
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
  const messagesRef = useRef<HTMLDivElement>(null)
  // Guards against the history load resolving AFTER the user has already
  // sent a message (e.g. slow /api/chat/session, fast first keystroke):
  // without this, the late resolve would stomp the just-sent message with
  // the (now stale) persisted history.
  const userActedRef = useRef(false)
  const effectiveChange = changeName || (documentPath ? documentPath.split(/[\\/]/).pop() || documentPath : '')
  const sessionKey = componentId || effectiveChange

  // 会话按当前上下文隔离：App.tsx 以 viewerPath 作为 key，切换文档会整体
  // 重新挂载本组件（内存 state 清空），随后这里再从后端拉回对应会话历史。
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

  // 上下文文件注入：挂载时拉取该变更的产物清单，把已存在的产物文件路径
  // 作为可勾选的上下文文件展示；用户勾选后发送时会连同消息一起传给后端。
  useEffect(() => {
    if (!changeName) return
    let cancelled = false
    fetchChangeDetail(changeName, workspace)
      .then((detail) => {
        if (cancelled) return
        const paths = (detail.phases ?? [])
          .flatMap((phase) => phase.artifacts ?? [])
          .filter((artifact) => artifact.exists)
          .map((artifact) => artifact.path || artifact.file)
        setContextFiles(paths)
      })
      .catch(() => {
        // 产物清单加载失败不阻塞聊天：静默保持空的上下文文件选择器。
      })
    return () => {
      cancelled = true
    }
  }, [changeName, workspace])

  function toggleContextFile(path: string) {
    setSelectedFiles((prev) => (prev.includes(path) ? prev.filter((p) => p !== path) : [...prev, path]))
  }

  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight
    }
  }, [messages])

  async function handleSend() {
    const text = input.trim()
    if (!text || sending) return
    userActedRef.current = true

    // 变更上下文发送用户勾选的产物；独立文档始终发送当前文件路径。
    const filesToSend = changeName ? selectedFiles : documentPath ? [documentPath] : []
    setMessages((prev) => [...prev, { role: 'user', text }])
    setInput('')
    setSending(true)

    // Placeholder assistant message accumulates thinking/delta events in place.
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

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  return (
    <>
      <button
        data-testid="chat-bubble-button"
        onClick={() => setOpen(true)}
        className="fixed bottom-4 right-4 w-10 h-10 rounded-full bg-[var(--color-accent)] text-white shadow-lg flex items-center justify-center text-lg"
      >
        💬
      </button>
      {open && (
        <div
          data-testid="chat-overlay"
          className="fixed bottom-20 right-4 w-[440px] h-[min(80vh,720px)] bg-white shadow-2xl border border-[var(--color-border)] flex flex-col"
        >
          <div className="flex items-center justify-between p-3 border-b border-[var(--color-border)]">
            <span className="text-sm font-semibold">Chat · {effectiveChange}</span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                data-testid="chat-graph-mode-toggle"
                aria-pressed={graphMode}
                onClick={() => setGraphMode((v) => !v)}
                title="图谱模式：将当前变更在知识图谱中的关联上下文注入对话"
                className={
                  graphMode
                    ? 'text-xs rounded-full px-2 py-0.5 bg-[var(--color-accent)] text-white font-medium'
                    : 'text-xs rounded-full px-2 py-0.5 bg-[var(--color-bg)] text-[var(--color-text-secondary)] border border-[var(--color-border)]'
                }
              >
                📊 图谱模式
              </button>
              <button data-testid="chat-overlay-close" onClick={() => setOpen(false)}>
                ✕
              </button>
            </div>
          </div>
          <div
            data-testid="chat-messages"
            ref={messagesRef}
            className="flex-1 overflow-y-auto p-3 text-sm space-y-2"
          >
            {messages.map((msg, i) => (
              <div
                key={i}
                data-testid={`chat-msg-${msg.role}`}
                className={
                  msg.role === 'user'
                    ? 'bg-[var(--color-accent)] text-white px-3 py-2 ml-auto max-w-[85%] whitespace-pre-wrap'
                    : msg.role === 'error'
                      ? 'bg-red-50 text-red-700 border border-red-200 px-3 py-2 max-w-[85%]'
                      : 'bg-[var(--color-bg)] text-[var(--color-text-primary)] px-3 py-2 max-w-[85%]'
                }
              >
                {msg.role === 'assistant' ? (
                  <>
                    {msg.thinking && !msg.text && (
                      <div className="text-xs text-[var(--color-text-secondary)] italic mb-1">💭 {msg.thinking}</div>
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
          {contextFiles.length > 0 && (
            <div className="border-t border-[var(--color-border)]">
              <button
                type="button"
                data-testid="context-panel-toggle"
                onClick={() => setContextPanelOpen((v) => !v)}
                className="w-full flex items-center justify-between px-3 py-1.5 text-xs font-medium text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
              >
                <span>
                  上下文文件
                  {selectedFiles.length > 0 ? ` (已选 ${selectedFiles.length}/${contextFiles.length})` : ` (${contextFiles.length})`}
                </span>
                <span className="text-[10px]">{contextPanelOpen ? '收起 ▲' : '展开 ▼'}</span>
              </button>
              {contextPanelOpen && (
                <div
                  data-testid="context-file-list"
                  className="flex flex-wrap gap-1.5 px-3 pb-2 max-h-20 overflow-y-auto"
                >
                  {contextFiles.map((path) => {
                    const selected = selectedFiles.includes(path)
                    return (
                      <button
                        key={path}
                        type="button"
                        data-testid={`context-file-chip-${path}`}
                        aria-pressed={selected}
                        onClick={() => toggleContextFile(path)}
                        title={path}
                        className={
                          selected
                            ? 'text-xs rounded-full px-2 py-0.5 bg-[var(--color-accent)] text-white font-medium flex items-center gap-1'
                            : 'text-xs rounded-full px-2 py-0.5 bg-white text-[var(--color-text-secondary)] border border-[var(--color-border)] hover:border-[var(--color-accent)] hover:text-[var(--color-accent)] flex items-center gap-1'
                        }
                      >
                        {selected && <span aria-hidden="true">✓</span>}
                        {path.split('/').pop()}
                      </button>
                    )
                  })}
                </div>
              )}
            </div>
          )}
          <div className="p-3 border-t border-[var(--color-border)] flex gap-2">
            <textarea
              data-testid="chat-input"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="询问关于此变更的问题…"
              className="flex-1 resize-none border border-[var(--color-border)] p-2 text-sm h-10"
            />
            <button
              data-testid="chat-send"
              onClick={handleSend}
              disabled={sending || !input.trim()}
              className="bg-[var(--color-accent)] text-white px-3 text-sm disabled:opacity-50"
            >
              发送
            </button>
          </div>
        </div>
      )}
    </>
  )
}
