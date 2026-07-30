import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ChatBubble } from './ChatBubble'
import { streamChat, fetchChatSession, fetchChangeDetail } from '../api/client'

vi.mock('../api/client', () => ({
  streamChat: vi.fn(),
  fetchChatSession: vi.fn(),
  fetchChangeDetail: vi.fn(),
}))

describe('ChatBubble', () => {
  beforeEach(() => {
    vi.mocked(streamChat).mockReset()
    vi.mocked(fetchChatSession).mockReset()
    vi.mocked(fetchChangeDetail).mockReset()
    vi.mocked(fetchChatSession).mockResolvedValue({
      change: 'rx101-x',
      messages: [],
      context_files: [],
      usage: { total_input: 0, total_output: 0 },
      created_at: '',
      updated_at: '',
    })
    vi.mocked(fetchChangeDetail).mockResolvedValue({
      name: 'rx101-x',
      workflow: '',
      phase: '',
      archived: false,
      tasksCompleted: 0,
      tasksTotal: 0,
      verifyResult: '',
      createdAt: '',
      phases: [],
    })
  })

  it('opens as a labeled non-modal dialog and focuses the message input', async () => {
    render(<ChatBubble changeName="rx101-x" />)
    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('rx101-x'))
    expect(screen.queryByTestId('chat-overlay')).toBeNull()

    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    const dialog = screen.getByRole('dialog', { name: 'Chat · rx101-x' })
    expect(dialog.getAttribute('aria-modal')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId('chat-input')))
  })

  it('restores focus to the launcher when the dialog closes', async () => {
    render(<ChatBubble changeName="rx101-x" />)
    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('rx101-x'))
    const launcher = screen.getByTestId('chat-bubble-button')
    fireEvent.click(launcher)

    fireEvent.click(screen.getByTestId('chat-overlay-close'))

    expect(screen.queryByTestId('chat-overlay')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(launcher))
  })

  it('sends a message via streamChat and renders accumulated delta text', async () => {
    vi.mocked(streamChat).mockImplementation(async (change, message, contextFiles, onEvent) => {
      expect(change).toBe('rx101-x')
      expect(message).toBe('hello there')
      expect(contextFiles).toEqual([])
      onEvent({ type: 'thinking', content: 'pondering...' })
      onEvent({ type: 'delta', content: 'Hi ' })
      onEvent({ type: 'delta', content: 'there!' })
      onEvent({ type: 'done' })
    })

    render(<ChatBubble changeName="rx101-x" />)
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    const textarea = screen.getByTestId('chat-input') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'hello there' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-send'))
    })

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByTestId('chat-messages').textContent).toContain('Hi there!'))
    expect(screen.getByTestId('chat-messages').textContent).toContain('hello there')
  })

  it('always sends the open document as context, even when a change is selected', async () => {
    const seen: string[][] = []
    vi.mocked(streamChat).mockImplementation(async (_change, _message, contextFiles, onEvent) => {
      seen.push(contextFiles)
      onEvent({ type: 'done' })
    })

    render(<ChatBubble changeName="rx101-x" documentPath="/ws/miao/openspec/changes/rx101-x/proposal.md" />)
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    // The viewer's document is surfaced as attached context, not hidden behind
    // the manual picker (which starts empty and silently dropped it before).
    expect(screen.getByTestId('chat-current-document').textContent).toContain('proposal.md')

    fireEvent.change(screen.getByTestId('chat-input'), { target: { value: '这份文档讲什么' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-send'))
    })

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
    expect(seen[0]).toEqual(['/ws/miao/openspec/changes/rx101-x/proposal.md'])
  })

  it('defaults graph mode on and passes includeGraph=true to streamChat', async () => {
    vi.mocked(streamChat).mockImplementation(async (_change, _message, _contextFiles, onEvent, includeGraph) => {
      expect(includeGraph).toBe(true)
      onEvent({ type: 'done' })
    })

    render(<ChatBubble changeName="rx101-x" />)
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    const toggle = screen.getByTestId('chat-graph-mode-toggle')
    expect(toggle.getAttribute('aria-pressed')).toBe('true')

    const textarea = screen.getByTestId('chat-input') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'hi' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-send'))
    })

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
  })

  it('toggling graph mode off passes includeGraph=false to streamChat', async () => {
    vi.mocked(streamChat).mockImplementation(async (_change, _message, _contextFiles, onEvent, includeGraph) => {
      expect(includeGraph).toBe(false)
      onEvent({ type: 'done' })
    })

    render(<ChatBubble changeName="rx101-x" />)
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    const toggle = screen.getByTestId('chat-graph-mode-toggle')
    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-pressed')).toBe('false')

    const textarea = screen.getByTestId('chat-input') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'hi' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-send'))
    })

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
  })

  it('shows an error message instead of hanging when streamChat rejects', async () => {
    vi.mocked(streamChat).mockRejectedValue(new Error('Anthropic API key not configured'))

    render(<ChatBubble changeName="rx101-x" />)
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    const textarea = screen.getByTestId('chat-input') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'hi' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-send'))
    })

    await waitFor(() =>
      expect(screen.getByTestId('chat-messages').textContent).toContain('Anthropic API key not configured'),
    )
  })

  it('loads persisted history on mount and renders it before any send', async () => {
    vi.mocked(fetchChatSession).mockResolvedValue({
      change: 'rx101-x',
      messages: [
        { role: 'user', content: [{ type: 'text', text: '之前的问题' }] },
        { role: 'assistant', content: [{ type: 'text', text: '之前的回答' }] },
      ],
      context_files: [],
      usage: { total_input: 10, total_output: 20 },
      created_at: '2026-07-01T00:00:00Z',
      updated_at: '2026-07-02T00:00:00Z',
    })

    render(<ChatBubble changeName="rx101-x" />)

    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('rx101-x'))
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    await waitFor(() => expect(screen.getByTestId('chat-messages').textContent).toContain('之前的问题'))
    expect(screen.getByTestId('chat-messages').textContent).toContain('之前的回答')
  })

  it('renders an empty transcript for a fresh change with no persisted history', async () => {
    vi.mocked(fetchChatSession).mockResolvedValue({
      change: 'brand-new-change',
      messages: [],
      context_files: [],
      usage: { total_input: 0, total_output: 0 },
      created_at: '',
      updated_at: '',
    })

    render(<ChatBubble changeName="brand-new-change" />)
    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('brand-new-change'))
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    expect(screen.getByTestId('chat-messages').textContent).toBe('')
  })

  it('selects context files through the shared searchable combobox and passes them to streamChat', async () => {
    vi.mocked(fetchChangeDetail).mockResolvedValue({
      name: 'rx101-x',
      workflow: '',
      phase: '',
      archived: false,
      tasksCompleted: 0,
      tasksTotal: 0,
      verifyResult: '',
      createdAt: '',
      phases: [
        {
          key: 'design',
          label: 'Design',
          status: 'done',
          artifacts: [
            { file: 'design.md', label: 'Design', exists: true, path: 'openspec/changes/rx101-x/design.md' },
            { file: 'proposal.md', label: 'Proposal', exists: true, path: 'openspec/changes/rx101-x/proposal.md' },
            { file: 'tasks.md', label: 'Tasks', exists: false, path: 'openspec/changes/rx101-x/tasks.md' },
          ],
        },
      ],
    })
    vi.mocked(streamChat).mockImplementation(async (_change, _message, contextFiles, onEvent) => {
      expect(contextFiles).toEqual(['openspec/changes/rx101-x/design.md'])
      onEvent({ type: 'delta', content: 'ok' })
      onEvent({ type: 'done' })
    })

    render(<ChatBubble changeName="rx101-x" workspace="rx101" />)
    fireEvent.click(screen.getByTestId('chat-bubble-button'))

    await waitFor(() => expect(fetchChangeDetail).toHaveBeenCalledWith('rx101-x', 'rx101'))

    const combobox = await screen.findByRole('combobox', { name: '搜索并添加上下文文档' })
    fireEvent.focus(combobox)
    fireEvent.keyDown(combobox, { key: 'Enter' })

    await waitFor(() => expect(screen.getByTestId('context-file-chip-openspec/changes/rx101-x/design.md')).toBeTruthy())
    expect(screen.queryByText('tasks.md')).toBeNull()

    const textarea = screen.getByTestId('chat-input') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'what changed?' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-send'))
    })

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
  })

  it('uses a standalone document as chat context without fetching change detail', async () => {
    vi.mocked(streamChat).mockImplementation(async (change, message, contextFiles, onEvent) => {
      expect(change).toBe('design.md')
      expect(message).toBe('summarize this')
      expect(contextFiles).toEqual(['/docs/design.md'])
      onEvent({ type: 'done' })
    })

    render(<ChatBubble documentPath="/docs/design.md" />)

    await waitFor(() => expect(fetchChatSession).toHaveBeenCalledWith('design.md'))
    expect(fetchChangeDetail).not.toHaveBeenCalled()

    fireEvent.click(screen.getByTestId('chat-bubble-button'))
    const textarea = screen.getByTestId('chat-input') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'summarize this' } })
    await act(async () => {
      fireEvent.click(screen.getByTestId('chat-send'))
    })

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(1))
  })

  it('shows a labeled, collapsible context-file section that toggles visibility', async () => {
    vi.mocked(fetchChangeDetail).mockResolvedValue({
      name: 'rx101-x',
      workflow: '',
      phase: '',
      archived: false,
      tasksCompleted: 0,
      tasksTotal: 0,
      verifyResult: '',
      createdAt: '',
      phases: [
        {
          key: 'design',
          label: 'Design',
          status: 'done',
          artifacts: [
            { file: 'design.md', label: 'Design', exists: true, path: 'openspec/changes/rx101-x/design.md' },
          ],
        },
      ],
    })

    render(<ChatBubble changeName="rx101-x" />)
    fireEvent.click(screen.getByTestId('chat-bubble-button'))
    await waitFor(() => expect(fetchChangeDetail).toHaveBeenCalledWith('rx101-x', undefined))

    expect(screen.queryByTestId('context-file-list')).toBeNull()
    expect(await screen.findByRole('combobox', { name: '搜索并添加上下文文档' })).toBeTruthy()

    fireEvent.click(screen.getByTestId('context-panel-toggle'))
    expect(screen.queryByRole('combobox', { name: '搜索并添加上下文文档' })).toBeNull()

    fireEvent.click(screen.getByTestId('context-panel-toggle'))
    expect(screen.getByRole('combobox', { name: '搜索并添加上下文文档' })).toBeTruthy()
  })
})
