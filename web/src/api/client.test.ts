import { describe, it, expect, vi, afterEach } from 'vitest'
import {
  fetchChanges,
  fetchWorkspaces,
  addWorkspace,
  fetchChangesWithMeta,
  fetchWikiIndex,
  fetchWikiGraph,
  fetchWikiLint,
  fetchChangeDetail,
  fetchArtifactContent,
  streamChat,
  fetchChatSession,
  fetchChatConfig,
  updateChatConfig,
  fetchChatProviders,
  createShareLink,
  fetchTodos,
  createTodo,
  updateTodo,
  deleteTodo,
} from './client'
import type { ChatStreamEvent } from './client'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('fetchChanges', () => {
  it('unwraps the {changes, dir} envelope into a bare array', async () => {
    const mockResponse = {
      dir: '../miao/openspec',
      changes: [
        {
          name: 'rx101-system-sw-architecture',
          workflow: 'full',
          phase: 'build',
          archived: false,
          tasksCompleted: 19,
          tasksTotal: 31,
          verifyResult: 'pending',
          createdAt: '2026-05-29',
          artifacts: {},
          visualized: true,
          designReviewed: true,
          verifyReviewed: false,
          verifiedAt: '',
          buildMode: 'subagent-driven-development',
          reviewMode: 'standard',
          tddMode: 'direct',
          autoTransition: true,
          stateWarning: '',
        },
      ],
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchChanges()
    expect(result).toHaveLength(1)
    expect(result[0].name).toBe('rx101-system-sw-architecture')
    expect(result[0].tasksTotal).toBe(31)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchChanges()).rejects.toThrow()
  })

  it('resolves to [] when changes is null (nil slice JSON-encodes as null)', async () => {
    const mockResponse = { changes: null, dir: '../miao/openspec' }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchChanges()
    expect(result).toEqual([])
  })
})

describe('fetchWorkspaces', () => {
  it('returns the parsed workspace list', async () => {
    const mockResponse = [
      { alias: 'miao', path: '/x/miao', color: '#0063f8' },
      { alias: 'wan2_2_deploy', path: '/x/wan', color: '#16a34a' },
    ]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchWorkspaces()
    expect(result).toEqual(mockResponse)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchWorkspaces()).rejects.toThrow()
  })
})

describe('addWorkspace', () => {
  it('POSTs the workspace config as JSON', async () => {
    const cfg = { alias: 'new-ws', path: '/x/new', color: '#0063f8' }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)

    await addWorkspace(cfg)

    expect(fetchSpy).toHaveBeenCalledWith('/api/workspaces', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg),
    })
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 409 } as Response)
    await expect(addWorkspace({ alias: 'miao', path: '/x/miao', color: '#0063f8' })).rejects.toThrow()
  })
})

describe('fetchChangesWithMeta', () => {
  it('returns the full envelope, preserving failedWorkspaces', async () => {
    const mockResponse = {
      changes: [
        {
          name: 'rx101-system-sw-architecture',
          workflow: 'full',
          phase: 'build',
          archived: false,
          tasksCompleted: 19,
          tasksTotal: 31,
          verifyResult: 'pending',
          createdAt: '2026-05-29',
          artifacts: {},
          visualized: true,
          designReviewed: true,
          verifyReviewed: false,
          verifiedAt: '',
          buildMode: 'subagent-driven-development',
          reviewMode: 'standard',
          tddMode: 'direct',
          autoTransition: true,
          workspace: 'miao',
        },
      ],
      failedWorkspaces: ['broken-ws'],
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchChangesWithMeta()
    expect(result.changes).toHaveLength(1)
    expect(result.changes[0].name).toBe('rx101-system-sw-architecture')
    expect(result.failedWorkspaces).toEqual(['broken-ws'])
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchChangesWithMeta()).rejects.toThrow()
  })
})

describe('fetchWikiIndex', () => {
  it('GETs /api/wiki/index and returns the component array', async () => {
    const mockResponse = [
      { id: 'change:foo', type: 'change', title: 'Foo', path: 'openspec/changes/foo', workspace: 'miao' },
    ]
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchWikiIndex()
    expect(fetchSpy).toHaveBeenCalledWith('/api/wiki/index')
    expect(result).toEqual(mockResponse)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchWikiIndex()).rejects.toThrow()
  })
})

describe('fetchWikiGraph', () => {
  it('GETs /api/wiki/graph and returns components + edges', async () => {
    const mockResponse = {
      components: [
        { id: 'change:foo', type: 'change', title: 'Foo', path: 'openspec/changes/foo', workspace: 'miao' },
      ],
      edges: [{ from: 'change:foo', to: 'design:foo', kind: 'implements', source: 'yaml' }],
    }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchWikiGraph()
    expect(fetchSpy).toHaveBeenCalledWith('/api/wiki/graph')
    expect(result).toEqual(mockResponse)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchWikiGraph()).rejects.toThrow()
  })
})

describe('fetchWikiLint', () => {
  it('GETs /api/wiki/lint and returns the issue array', async () => {
    const mockResponse = [{ rule: 'orphan-node', componentId: 'change:foo', detail: '无反向链接' }]
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchWikiLint()
    expect(fetchSpy).toHaveBeenCalledWith('/api/wiki/lint')
    expect(result).toEqual(mockResponse)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchWikiLint()).rejects.toThrow()
  })
})

describe('fetchChangeDetail', () => {
  it('appends a ?workspace= query param when a workspace is given', async () => {
    const mockResponse = {
      name: 'rx101-x',
      workflow: 'full',
      phase: 'build',
      archived: false,
      tasksCompleted: 0,
      tasksTotal: 0,
      verifyResult: '',
      createdAt: '',
      phases: [],
    }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response)

    const result = await fetchChangeDetail('rx101-x', 'rx101')
    expect(fetchSpy).toHaveBeenCalledWith('/api/changes/rx101-x?workspace=rx101')
    expect(result).toEqual(mockResponse)
  })

  it('omits the workspace query param when no workspace is given', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({}),
    } as Response)

    await fetchChangeDetail('rx101-x')
    expect(fetchSpy).toHaveBeenCalledWith('/api/changes/rx101-x')
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 404 } as Response)
    await expect(fetchChangeDetail('missing-change')).rejects.toThrow()
  })
})

describe('fetchArtifactContent', () => {
  it('includes workspace=<ws> in the query string when a workspace is given', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      text: async () => '# content',
    } as Response)

    const result = await fetchArtifactContent('/p', 'ws')
    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const requestedUrl = new URL(fetchSpy.mock.calls[0][0] as string, 'http://localhost')
    expect(requestedUrl.pathname).toBe('/api/artifact')
    expect(requestedUrl.searchParams.get('path')).toBe('/p')
    expect(requestedUrl.searchParams.get('workspace')).toBe('ws')
    expect(result).toBe('# content')
  })

  it('omits the workspace param entirely when no workspace is given', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      text: async () => '# content',
    } as Response)

    await fetchArtifactContent('/p')
    const requestedUrl = new URL(fetchSpy.mock.calls[0][0] as string, 'http://localhost')
    expect(requestedUrl.searchParams.has('workspace')).toBe(false)
    expect(requestedUrl.searchParams.get('path')).toBe('/p')
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchArtifactContent('/p')).rejects.toThrow()
  })
})

describe('streamChat', () => {
  it('throws with the error-body message when res.ok is false, WITHOUT reading the body stream', async () => {
    const getReader = vi.fn()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
      json: async () => ({ message: '请先在设置中配置 API Key' }),
      body: { getReader },
    } as unknown as Response)

    const onEvent = vi.fn()
    await expect(streamChat('foo-change', 'hi', [], onEvent)).rejects.toThrow('请先在设置中配置 API Key')
    expect(getReader).not.toHaveBeenCalled()
    expect(onEvent).not.toHaveBeenCalled()
  })

  it('falls back to statusText when the error body has neither message nor error', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: async () => ({}),
    } as Response)

    await expect(streamChat('foo-change', 'hi', [], vi.fn())).rejects.toThrow('Internal Server Error')
  })

  it('POSTs change/message/context_files and parses thinking/delta/done SSE frames into onEvent calls', async () => {
    const frames = [
      'data: {"type":"thinking","content":"分析中"}\n\n',
      'data: {"type":"delta","content":"结论：A"}\n\n',
      'data: {"type":"delta","content":"B"}\n\n',
      'data: {"type":"done"}\n\n',
    ]
    const encoder = new TextEncoder()
    let call = 0
    const reader = {
      read: vi.fn(async () => {
        if (call < frames.length) {
          const chunk = encoder.encode(frames[call])
          call += 1
          return { done: false, value: chunk }
        }
        return { done: true, value: undefined }
      }),
    }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      body: { getReader: () => reader },
    } as unknown as Response)

    const events: ChatStreamEvent[] = []
    await streamChat('foo-change', '你好', ['proposal.md'], (e) => events.push(e))

    expect(fetchSpy).toHaveBeenCalledWith('/api/chat/message', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        change: 'foo-change',
        message: '你好',
        context_files: ['proposal.md'],
        includeGraph: false,
      }),
    })
    expect(events).toEqual([
      { type: 'thinking', content: '分析中' },
      { type: 'delta', content: '结论：A' },
      { type: 'delta', content: 'B' },
      { type: 'done', content: undefined },
    ])
  })

  it('skips malformed SSE lines instead of throwing', async () => {
    const encoder = new TextEncoder()
    const chunks = [
      encoder.encode('data: {not json}\n\n'),
      encoder.encode('data: {"type":"delta","content":"ok"}\n\n'),
    ]
    let call = 0
    const reader = {
      read: vi.fn(async () => {
        if (call < chunks.length) {
          const value = chunks[call]
          call += 1
          return { done: false, value }
        }
        return { done: true, value: undefined }
      }),
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      body: { getReader: () => reader },
    } as unknown as Response)

    const events: ChatStreamEvent[] = []
    await streamChat('foo-change', 'hi', [], (e) => events.push(e))
    expect(events).toEqual([{ type: 'delta', content: 'ok' }])
  })
})

describe('fetchChatSession', () => {
  it('GETs /api/chat/session with the change query param and returns the parsed session', async () => {
    const mockSession = {
      change: 'rx101-x',
      messages: [
        { role: 'user', content: [{ type: 'text', text: '之前的问题' }] },
        { role: 'assistant', content: [{ type: 'text', text: '之前的回答' }] },
      ],
      context_files: ['proposal.md'],
      usage: { total_input: 10, total_output: 20 },
      created_at: '2026-07-01T00:00:00Z',
      updated_at: '2026-07-02T00:00:00Z',
    }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockSession,
    } as Response)

    const result = await fetchChatSession('rx101-x')

    expect(fetchSpy).toHaveBeenCalledWith('/api/chat/session?change=' + encodeURIComponent('rx101-x'))
    expect(result).toEqual(mockSession)
  })

  it('encodes special characters in the change name', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ change: 'a/b', messages: [], context_files: [], usage: { total_input: 0, total_output: 0 }, created_at: '', updated_at: '' }),
    } as Response)

    await fetchChatSession('a/b')

    expect(fetchSpy).toHaveBeenCalledWith('/api/chat/session?change=' + encodeURIComponent('a/b'))
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchChatSession('rx101-x')).rejects.toThrow()
  })
})

describe('fetchChatConfig', () => {
  it('GETs /api/chat/config and returns the parsed config', async () => {
    const mockConfig = {
      active_provider: 'anthropic',
      providers: {
        anthropic: {
          api_key: 'sk-c****umMM',
          api_base: '',
          model: 'claude-3-5-sonnet',
          temperature: 0.7,
          max_tokens: 4096,
          thinking: 'auto',
        },
      },
    }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockConfig,
    } as Response)

    const result = await fetchChatConfig()

    expect(fetchSpy).toHaveBeenCalledWith('/api/chat/config')
    expect(result).toEqual(mockConfig)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchChatConfig()).rejects.toThrow()
  })
})

describe('updateChatConfig', () => {
  it('PUTs the partial config patch as JSON and returns the merged config', async () => {
    const patch = { active_provider: 'anthropic', providers: { anthropic: { model: 'claude-3-opus' } } }
    const mockConfig = { active_provider: 'anthropic', providers: { anthropic: { api_key: '', api_base: '', model: 'claude-3-opus', temperature: 0.7, max_tokens: 4096, thinking: 'auto' } } }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockConfig,
    } as Response)

    const result = await updateChatConfig(patch)

    expect(fetchSpy).toHaveBeenCalledWith('/api/chat/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    expect(result).toEqual(mockConfig)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(updateChatConfig({})).rejects.toThrow()
  })
})

describe('fetchChatProviders', () => {
  it('GETs /api/chat/providers and returns the parsed list', async () => {
    const mockProviders = {
      active: 'anthropic',
      providers: [{ name: 'anthropic', models: ['claude-3-5-sonnet', 'claude-3-opus'], supports_images: true }],
    }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => mockProviders,
    } as Response)

    const result = await fetchChatProviders()

    expect(fetchSpy).toHaveBeenCalledWith('/api/chat/providers')
    expect(result).toEqual(mockProviders)
  })

  it('throws on non-OK response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 500 } as Response)
    await expect(fetchChatProviders()).rejects.toThrow()
  })
})

describe('createShareLink', () => {
  it('preserves the public LAN URL returned by the backend', async () => {
    const response = { url: 'http://10.0.28.45:8989/share/token' }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => response,
    } as Response)

    const result = await createShareLink('/workspace/knowledge/doc.md', 'miao', 3600)

    expect(fetchSpy).toHaveBeenCalledWith('/api/share/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        path: '/workspace/knowledge/doc.md',
        workspace: 'miao',
        ttl: 3600,
      }),
    })
    expect(result).toEqual(response)
  })
})

// ── Todo API ─────────────────────────────────────────────────────────────────
describe('fetchTodos', () => {
  it('calls GET /api/todos and returns the parsed response', async () => {
    const response = {
      items: [{ id: 'a1', workspace: 'ws1', title: 'Test', notes: '', status: 'open', priority: 'normal', dueAt: null, change: null, wikiRefs: [], metadata: { source: 'ui' as const }, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', completedAt: null }],
      counts: { total: 1, open: 1, inProgress: 0, done: 0 },
      revision: 42,
      writable: true,
    }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => response,
    } as Response)
    const result = await fetchTodos()
    expect(fetchSpy).toHaveBeenCalledWith('/api/todos')
    expect(result).toEqual(response)
  })

  it('appends query params for status, workspace, change, wikiComponentId, and q', async () => {
    const response = { items: [], counts: { total: 0, open: 0, inProgress: 0, done: 0 }, revision: 0, writable: true }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => response,
    } as Response)
    await fetchTodos({ status: 'open', workspace: 'ws1', q: 'urgent' })
    const url = (fetchSpy.mock.calls[0][0] as string)
    expect(url).toContain('/api/todos?')
    expect(url).toContain('status=open')
    expect(url).toContain('workspace=ws1')
    expect(url).toContain('q=urgent')
  })

  it('throws on non-ok response with backend error message', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: 'store unreachable' }),
    } as Response)
    await expect(fetchTodos()).rejects.toThrow('store unreachable')
  })
})

describe('fetchTodos normalization', () => {
  it('fills in omitted Go omitempty defaults via normalizeTodo', async () => {
    const sparse = { id: 'sp1', workspace: 'ws1', title: 'Sparse', status: 'open', priority: 'normal', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' }
    const response = { items: [sparse], counts: { total: 1, open: 1, inProgress: 0, done: 0 }, revision: 1, writable: true }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, json: async () => response } as Response)
    const result = await fetchTodos()
    expect(result.items[0].notes).toBe('')
    expect(result.items[0].dueAt).toBeNull()
    expect(result.items[0].change).toBeNull()
    expect(result.items[0].wikiRefs).toEqual([])
    expect(result.items[0].completedAt).toBeNull()
  })
  it('POSTs to /api/todos with JSON body and returns the created todo', async () => {
    const todo = { id: 'new1', workspace: 'ws1', title: 'New todo', notes: '', status: 'open', priority: 'normal', dueAt: null, change: null, wikiRefs: [], metadata: { source: 'ui' as const }, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', completedAt: null }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => todo,
    } as Response)
    const input = { workspace: 'ws1', title: 'New todo' }
    const result = await createTodo(input)
    expect(fetchSpy).toHaveBeenCalledWith('/api/todos', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
    expect(result).toEqual(todo)
  })
})

describe('updateTodo', () => {
  it('PATCHes /api/todos/:id with JSON body and returns the updated todo', async () => {
    const todo = { id: 'a1', workspace: 'ws1', title: 'Updated', notes: '', status: 'in_progress' as const, priority: 'high' as const, dueAt: null, change: null, wikiRefs: [], metadata: { source: 'ui' as const }, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', completedAt: null }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => todo,
    } as Response)
    const patch = { status: 'in_progress' as const, priority: 'high' as const }
    const result = await updateTodo('a1', patch)
    expect(fetchSpy).toHaveBeenCalledWith('/api/todos/a1', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    expect(result).toEqual(todo)
  })

  it('URL-encodes the todo id in the path', async () => {
    const todo = { id: 'a/b', workspace: 'ws1', title: 'Slash', notes: '', status: 'open', priority: 'normal', dueAt: null, change: null, wikiRefs: [], metadata: { source: 'ui' as const }, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', completedAt: null }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => todo,
    } as Response)
    await updateTodo('a/b', { title: 'Slash' })
    expect(fetchSpy).toHaveBeenCalledWith('/api/todos/a%2Fb', expect.any(Object))
  })
})

describe('deleteTodo', () => {
  it('DELETEs /api/todos/:id', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
    } as Response)
    await deleteTodo('a1')
    expect(fetchSpy).toHaveBeenCalledWith('/api/todos/a1', { method: 'DELETE' })
  })

  it('throws on non-ok response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({ error: 'not found' }),
    } as Response)
    await expect(deleteTodo('missing')).rejects.toThrow('not found')
  })
})
