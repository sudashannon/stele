import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createTodo, deleteTodo, fetchTodos, updateTodo } from '../api/client'
import type { Todo, TodoListResponse } from '../api/types'
import { useTodos } from './useTodos'

vi.mock('../api/client', () => ({
  fetchTodos: vi.fn(),
  createTodo: vi.fn(),
  updateTodo: vi.fn(),
  deleteTodo: vi.fn(),
}))

const todo: Todo = {
  id: 'todo-1',
  workspace: 'ws1',
  title: 'Stable callbacks',
  notes: '',
  status: 'open',
  priority: 'normal',
  dueAt: null,
  change: null,
  wikiRefs: [],
  metadata: { source: 'ui' },
  externalRef: null,
  createdAt: '2026-07-30T00:00:00Z',
  updatedAt: '2026-07-30T00:00:00Z',
  completedAt: null,
}

const response: TodoListResponse = {
  items: [todo],
  counts: { total: 1, open: 1, inProgress: 0, done: 0, blocked: 0, dropped: 0 },
  revision: 1,
  writable: true,
}

beforeEach(() => {
  vi.mocked(fetchTodos).mockReset().mockResolvedValue(response)
  vi.mocked(createTodo).mockReset().mockResolvedValue(todo)
  vi.mocked(updateTodo).mockReset().mockResolvedValue(todo)
  vi.mocked(deleteTodo).mockReset().mockResolvedValue(undefined)
})

describe('useTodos callback stability', () => {
  it('keeps mutation and refetch callbacks stable across render-state changes', async () => {
    const { result } = renderHook(() => useTodos())
    const callbacks = {
      createTodo: result.current.createTodo,
      updateTodo: result.current.updateTodo,
      deleteTodo: result.current.deleteTodo,
      refetch: result.current.refetch,
    }

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.createTodo).toBe(callbacks.createTodo)
    expect(result.current.updateTodo).toBe(callbacks.updateTodo)
    expect(result.current.deleteTodo).toBe(callbacks.deleteTodo)
    expect(result.current.refetch).toBe(callbacks.refetch)
  })

  it('does not rebuild callbacks when a refetch replaces list data', async () => {
    const { result } = renderHook(() => useTodos())
    await waitFor(() => expect(result.current.loading).toBe(false))
    const refetch = result.current.refetch
    const update = result.current.updateTodo

    vi.mocked(fetchTodos).mockResolvedValue({ ...response, revision: 2 })
    await act(async () => refetch())

    expect(result.current.revision).toBe(2)
    expect(result.current.refetch).toBe(refetch)
    expect(result.current.updateTodo).toBe(update)
  })

  it('ignores an older refetch response that resolves after a newer one', async () => {
    const { result } = renderHook(() => useTodos())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let resolveOld!: (value: TodoListResponse) => void
    const oldRequest = new Promise<TodoListResponse>((resolve) => {
      resolveOld = resolve
    })
    vi.mocked(fetchTodos)
      .mockReturnValueOnce(oldRequest)
      .mockResolvedValueOnce({ ...response, revision: 3 })

    let oldRefetch!: Promise<void>
    await act(async () => {
      oldRefetch = result.current.refetch()
      await result.current.refetch()
    })
    expect(result.current.revision).toBe(3)

    await act(async () => {
      resolveOld({ ...response, revision: 2 })
      await oldRefetch
    })
    expect(result.current.revision).toBe(3)
  })
})
