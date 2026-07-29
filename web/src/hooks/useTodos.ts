import { useState, useCallback, useEffect } from 'react'
import { fetchTodos, createTodo, updateTodo, deleteTodo, type TodoQueryParams } from '../api/client'
import type { Todo, TodoCounts, CreateTodoInput, UpdateTodoInput, TodoListResponse } from '../api/types'

export interface UseTodosReturn {
  todos: Todo[]
  counts: TodoCounts | null
  revision: number
  writable: boolean
  loading: boolean
  error: string | null
  createTodo: (input: CreateTodoInput) => Promise<Todo>
  updateTodo: (id: string, patch: UpdateTodoInput) => Promise<Todo>
  deleteTodo: (id: string) => Promise<void>
  refetch: (params?: TodoQueryParams) => Promise<void>
}

export function useTodos(): UseTodosReturn {
  const [data, setData] = useState<TodoListResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async (params?: TodoQueryParams) => {
    setLoading(true)
    setError(null)
    try {
      const result = await fetchTodos(params)
      setData(result)
    } catch (e) {
      setError(e instanceof Error ? e.message : '获取待办失败')
    } finally {
      setLoading(false)
    }
  }, [])

  // Initial fetch on mount
  useEffect(() => {
    refetch()
  }, [refetch])

  // SSE refetch is registered by the App-level useWikiEvents caller,
  // not here — avoids duplicate EventSource connections.

  const create = useCallback(async (input: CreateTodoInput): Promise<Todo> => {
    try {
      const todo = await createTodo(input)
      await refetch()
      return todo
    } catch (e) {
      const msg = e instanceof Error ? e.message : '创建待办失败'
      setError(msg)
      throw e
    }
  }, [refetch])

  const update = useCallback(async (id: string, patch: UpdateTodoInput): Promise<Todo> => {
    try {
      const todo = await updateTodo(id, patch)
      await refetch()
      return todo
    } catch (e) {
      const msg = e instanceof Error ? e.message : '更新待办失败'
      setError(msg)
      throw e
    }
  }, [refetch])

  const remove = useCallback(async (id: string): Promise<void> => {
    try {
      await deleteTodo(id)
      await refetch()
    } catch (e) {
      const msg = e instanceof Error ? e.message : '删除待办失败'
      setError(msg)
      throw e
    }
  }, [refetch])

  return {
    todos: data?.items ?? [],
    counts: data?.counts ?? null,
    revision: data?.revision ?? 0,
    writable: data === null ? true : data.writable,
    loading,
    error,
    createTodo: create,
    updateTodo: update,
    deleteTodo: remove,
    refetch,
  }
}
