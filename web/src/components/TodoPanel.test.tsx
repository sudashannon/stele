import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { TodoPanel } from './TodoPanel'
import type { Todo } from '../api/types'

function makeTodo(overrides: Partial<Todo> = {}): Todo {
  return {
    id: 't1',
    workspace: 'ws1',
    title: 'Test todo',
    notes: '',
    status: 'open',
    priority: 'normal',
    dueAt: null,
    change: null,
    wikiRefs: [],
    metadata: { source: 'ui' as const },
    createdAt: '2026-07-30T00:00:00Z',
    updatedAt: '2026-07-30T00:00:00Z',
    completedAt: null,
    ...overrides,
  }
}

function buildLocalISO(offsetDays: number): string {
  const d = new Date()
  d.setHours(12, 0, 0, 0)
  d.setDate(d.getDate() + offsetDays)
  return d.toISOString()
}

function todayISO(): string {
  return buildLocalISO(0)
}

function yesterdayISO(): string {
  return buildLocalISO(-1)
}

function tomorrowISO(): string {
  return buildLocalISO(1)
}

function nextWeekISO(): string {
  return buildLocalISO(7)
}

const baseProps = {
  todos: [] as Todo[],
  counts: { total: 0, open: 0, inProgress: 0, done: 0 },
  writable: true,
  loading: false,
  error: null,
  onCreate: vi.fn(),
  onUpdate: vi.fn(),
  onDelete: vi.fn(),
  workspaces: [],
  wikiComponents: [],
  onNavigateWiki: vi.fn(),
  onNavigateChange: vi.fn(),
  onDraftConsumed: vi.fn(),
}

describe('TodoPanel grouping', () => {
  it('groups overdue items in the overdue section when dueAt is before today', () => {
    const overdueTodo = makeTodo({ id: 'o1', title: 'Overdue task', dueAt: yesterdayISO() })
    render(<TodoPanel {...baseProps} todos={[overdueTodo]} />)
    const group = screen.getByTestId('todo-group-overdue')
    expect(group).toBeTruthy()
    expect(group.textContent).toContain('Overdue task')
  })

  it('groups today items in the today section', () => {
    const todayTodo = makeTodo({ id: 't1', title: 'Today task', dueAt: todayISO() })
    render(<TodoPanel {...baseProps} todos={[todayTodo]} />)
    const group = screen.getByTestId('todo-group-today')
    expect(group).toBeTruthy()
    expect(group.textContent).toContain('Today task')
  })

  it('groups tomorrow items in the tomorrow section', () => {
    const tomorrowTodo = makeTodo({ id: 'tm1', title: 'Tomorrow task', dueAt: tomorrowISO() })
    render(<TodoPanel {...baseProps} todos={[tomorrowTodo]} />)
    const group = screen.getByTestId('todo-group-tomorrow')
    expect(group).toBeTruthy()
    expect(group.textContent).toContain('Tomorrow task')
  })

  it('groups later items in the later section', () => {
    const laterTodo = makeTodo({ id: 'l1', title: 'Later task', dueAt: nextWeekISO() })
    render(<TodoPanel {...baseProps} todos={[laterTodo]} />)
    const group = screen.getByTestId('todo-group-later')
    expect(group).toBeTruthy()
    expect(group.textContent).toContain('Later task')
  })

  it('groups items without dueAt in the undated section', () => {
    const undatedTodo = makeTodo({ id: 'u1', title: 'Undated task', dueAt: null })
    render(<TodoPanel {...baseProps} todos={[undatedTodo]} />)
    const group = screen.getByTestId('todo-group-undated')
    expect(group).toBeTruthy()
    expect(group.textContent).toContain('Undated task')
  })

  it('groups done items in the done section regardless of dueAt', () => {
    const doneTodo = makeTodo({ id: 'd1', title: 'Done task', status: 'done', dueAt: todayISO() })
    render(<TodoPanel {...baseProps} todos={[doneTodo]} />)
    const group = screen.getByTestId('todo-group-done')
    expect(group).toBeTruthy()
    expect(group.textContent).toContain('Done task')
  })
})

describe('TodoPanel context prefill', () => {
  it('shows change prefill chip when draftChange is provided', () => {
    render(
      <TodoPanel
        {...baseProps}
        draftChange={{ workspace: 'ws1', name: 'my-change' }}
      />,
    )
    expect(screen.getByTestId('todo-qc-change').textContent).toBe('ws1/my-change')
  })

  it('shows wiki ref prefill chip when draftWikiRef is provided', () => {
    render(
      <TodoPanel
        {...baseProps}
        draftWikiRef={{ componentId: 'comp-1', workspace: 'ws1', titleSnapshot: 'My Doc' }}
      />,
    )
    expect(screen.getByTestId('todo-qc-wikiref-comp-1').textContent).toBe('My Doc')
  })

  it('calls onDraftConsumed when draft is provided', () => {
    const onDraftConsumed = vi.fn()
    render(
      <TodoPanel
        {...baseProps}
        onDraftConsumed={onDraftConsumed}
        draftChange={{ workspace: 'ws1', name: 'my-change' }}
      />,
    )
    expect(onDraftConsumed).toHaveBeenCalledTimes(1)
  })
})

describe('TodoPanel read-only', () => {
  it('shows read-only banner when writable is false', () => {
    render(<TodoPanel {...baseProps} writable={false} />)
    expect(screen.getByTestId('todo-readonly-banner')).toBeTruthy()
  })

  it('does not show read-only banner when writable is true', () => {
    render(<TodoPanel {...baseProps} writable={true} />)
    expect(screen.queryByTestId('todo-readonly-banner')).toBeNull()
  })

  it('hides quick capture input when writable is false', () => {
    render(<TodoPanel {...baseProps} writable={false} />)
    expect(screen.queryByTestId('todo-quick-capture')).toBeNull()
  })
})

describe('TodoPanel detail panel', () => {
  it('shows detail panel when a todo row is clicked', () => {
    const todo = makeTodo({ id: 'sel1', title: 'Selected todo' })
    render(<TodoPanel {...baseProps} todos={[todo]} />)
    expect(screen.queryByTestId('todo-detail')).toBeNull()
    fireEvent.click(screen.getByText('Selected todo'))
    expect(screen.getByTestId('todo-detail')).toBeTruthy()
  })

  it('clears change relation when clear button is clicked', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeTodo({ id: 'cc1', title: 'With change', change: null }))
    const todo = makeTodo({
      id: 'cc1',
      title: 'With change',
      change: { workspace: 'ws1', name: 'my-change' },
    })
    render(<TodoPanel {...baseProps} todos={[todo]} onUpdate={onUpdate} />)
    fireEvent.click(screen.getByText('With change'))
    fireEvent.click(screen.getByTestId('todo-clear-change'))
    expect(onUpdate).toHaveBeenCalledWith('cc1', { change: null })
  })

  it('removes wiki ref when remove button is clicked', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeTodo({ id: 'wr1', title: 'With wiki ref', wikiRefs: [] }))
    const todo = makeTodo({
      id: 'wr1',
      title: 'With wiki ref',
      wikiRefs: [{ componentId: 'comp-a', workspace: 'ws1', titleSnapshot: 'Doc A' }],
    })
    render(<TodoPanel {...baseProps} todos={[todo]} onUpdate={onUpdate} />)
    fireEvent.click(screen.getByText('With wiki ref'))
    fireEvent.click(screen.getByTestId('todo-remove-wikiref-comp-a'))
    expect(onUpdate).toHaveBeenCalledWith('wr1', { wikiRefs: [] })
  })

  it('shows current-workspace documents newest-first and groups undated documents separately', () => {
    const linkedRef = { componentId: 'linked', workspace: 'ws1', titleSnapshot: 'Already linked' }
    const todo = makeTodo({ id: 'docs1', title: 'Attach docs', wikiRefs: [linkedRef] })
    const onUpdate = vi.fn().mockResolvedValue(todo)
    render(
      <TodoPanel
        {...baseProps}
        todos={[todo]}
        onUpdate={onUpdate}
        wikiComponents={[
          { id: 'old', type: 'design', title: 'Old doc', path: '/old.md', workspace: 'ws1', updatedAt: '2026-07-01T00:00:00Z' },
          { id: 'recent', type: 'proposal', title: 'Recent doc', path: '/recent.md', workspace: 'ws1', updatedAt: '2026-07-29T00:00:00Z' },
          { id: 'undated', type: 'spec', title: 'Undated doc', path: '/undated.md', workspace: 'ws1' },
          { id: 'linked', type: 'tasks', title: 'Already linked', path: '/linked.md', workspace: 'ws1', updatedAt: '2026-07-30T00:00:00Z' },
          { id: 'other-workspace', type: 'plan', title: 'Other workspace', path: '/other.md', workspace: 'ws2', updatedAt: '2026-07-31T00:00:00Z' },
        ]}
      />,
    )

    fireEvent.click(screen.getByText('Attach docs'))
    const select = screen.getByTestId('todo-detail-wiki-select') as HTMLSelectElement
    expect(Array.from(select.querySelectorAll('optgroup'), (group) => group.label)).toEqual(['最近更新', '其他文档'])
    expect(Array.from(select.options, (option) => option.value)).toEqual(['', 'recent', 'old', 'undated'])
    expect(select.options[1].textContent).toContain('proposal: Recent doc')

    fireEvent.change(select, { target: { value: 'recent' } })
    expect(onUpdate).toHaveBeenCalledWith('docs1', {
      wikiRefs: [
        linkedRef,
        { componentId: 'recent', workspace: 'ws1', titleSnapshot: 'Recent doc' },
      ],
    })
  })
})

describe('TodoPanel daily progress', () => {
  it('shows today completion ratio in the header strip', () => {
    const todos = [
      makeTodo({ id: 'a', title: 'A', status: 'done', dueAt: todayISO() }),
      makeTodo({ id: 'b', title: 'B', status: 'open', dueAt: todayISO() }),
    ]
    render(<TodoPanel {...baseProps} todos={todos} />)
    const strip = screen.getByTestId('todo-progress-strip')
    expect(strip).toBeTruthy()
  })

  it('hides progress strip when no todos are due today', () => {
    render(<TodoPanel {...baseProps} todos={[makeTodo({ dueAt: nextWeekISO() })]} />)
    expect(screen.queryByTestId('todo-progress-strip')).toBeNull()
  })
})

describe('TodoPanel empty state', () => {
  it('renders empty state when no todos are provided', () => {
    render(<TodoPanel {...baseProps} todos={[]} />)
    expect(screen.getByTestId('todo-empty-state')).toBeTruthy()
  })
})

describe('TodoPanel mutation disablement when writable is false', () => {
  it('disables the detail title input when writable is false', () => {
    const todo = makeTodo({ id: 'ro1', title: 'Read-only todo' })
    render(<TodoPanel {...baseProps} todos={[todo]} writable={false} />)
    fireEvent.click(screen.getByText('Read-only todo'))
    const titleInput = screen.getByTestId('todo-detail-title') as HTMLInputElement
    expect(titleInput.disabled).toBe(true)
  })

  it('disables the detail notes textarea when writable is false', () => {
    const todo = makeTodo({ id: 'ro2', title: 'Read-only', notes: 'Some notes' })
    render(<TodoPanel {...baseProps} todos={[todo]} writable={false} />)
    fireEvent.click(screen.getByText('Read-only'))
    const notesTextarea = screen.getByTestId('todo-detail-notes') as HTMLTextAreaElement
    expect(notesTextarea.disabled).toBe(true)
  })

  it('disables the detail due-date input when writable is false', () => {
    const todo = makeTodo({ id: 'ro3', title: 'With date', dueAt: todayISO() })
    render(<TodoPanel {...baseProps} todos={[todo]} writable={false} />)
    fireEvent.click(screen.getByText('With date'))
    const dateInput = screen.getByTestId('todo-detail-duedate') as HTMLInputElement
    expect(dateInput.disabled).toBe(true)
  })

  it('hides the delete button in detail when writable is false', () => {
    const todo = makeTodo({ id: 'ro4', title: 'No delete' })
    render(<TodoPanel {...baseProps} todos={[todo]} writable={false} />)
    fireEvent.click(screen.getByText('No delete'))
    expect(screen.queryByTestId('todo-delete-btn')).toBeNull()
  })

  it('hides clear-change button in detail when writable is false', () => {
    const todo = makeTodo({ id: 'ro5', title: 'Has change', change: { workspace: 'ws1', name: 'ch' } })
    render(<TodoPanel {...baseProps} todos={[todo]} writable={false} />)
    fireEvent.click(screen.getByText('Has change'))
    expect(screen.queryByTestId('todo-clear-change')).toBeNull()
  })
})

describe('TodoPanel quick capture workspace', () => {
  it('renders workspace selector in quick capture area', () => {
    render(<TodoPanel {...baseProps} workspaces={[{ alias: 'ws1', path: '/a', color: '#000' }]} />)
    expect(screen.getByTestId('todo-qc-workspace')).toBeTruthy()
  })

  it('disables add button and shows tooltip when no workspace selected', () => {
    render(<TodoPanel {...baseProps} workspaces={[]} />)
    const btn = screen.getByTestId('todo-quick-capture-btn') as HTMLButtonElement
    // Workspace defaults to empty; button disabled
    expect(btn.disabled).toBe(true)
  })

  it('selects draft change workspace on mount', () => {
    render(
      <TodoPanel
        {...baseProps}
        workspaces={[{ alias: 'ws2', path: '/b', color: '#000' }]}
        draftChange={{ workspace: 'ws2', name: 'ch1' }}
      />,
    )
    const wsSelect = screen.getByTestId('todo-qc-workspace') as HTMLSelectElement
    expect(wsSelect.value).toBe('ws2')
  })

  it('uses defaultWorkspace when no draft', () => {
    render(
      <TodoPanel
        {...baseProps}
        workspaces={[{ alias: 'ws1', path: '/a', color: '#000' }, { alias: 'ws2', path: '/b', color: '#000' }]}
        defaultWorkspace="ws2"
      />,
    )
    const wsSelect = screen.getByTestId('todo-qc-workspace') as HTMLSelectElement
    expect(wsSelect.value).toBe('ws2')
  })
})

describe('TodoPanel detail change and wiki association', () => {
  it('shows change select when no change is linked', () => {
    const todo = makeTodo({ id: 'nc1', title: 'No change' })
    render(<TodoPanel {...baseProps} todos={[todo]} changes={[{ name: 'ch1', workspace: 'ws1', workflow: 'full', phase: 'build', archived: false, tasksCompleted: 0, tasksTotal: 0, verifyResult: 'pending', createdAt: '', artifacts: {}, visualized: false, designReviewed: false, verifyReviewed: false, verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false }]} />)
    fireEvent.click(screen.getByText('No change'))
    expect(screen.getByTestId('todo-detail-change-select')).toBeTruthy()
  })

  it('shows wiki add select in detail', () => {
    const todo = makeTodo({ id: 'wp1', title: 'Wiki picker' })
    const comps = [{ id: 'comp-a', workspace: 'ws1', title: 'Doc A', type: 'spec' as const, path: '/a/doc.md', updatedAt: '2026-01-01T00:00:00Z' }]
    render(<TodoPanel {...baseProps} todos={[todo]} wikiComponents={comps} />)
    fireEvent.click(screen.getByText('Wiki picker'))
    expect(screen.getByTestId('todo-detail-wiki-select')).toBeTruthy()
  })

  it('appends a wiki ref on select', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeTodo({ id: 'wp2', title: 'Wiki adder' }))
    const todo = makeTodo({ id: 'wp2', title: 'Wiki adder' })
    const comps = [{ id: 'comp-b', workspace: 'ws1', title: 'Doc B', type: 'spec' as const, path: '/b/doc.md', updatedAt: '2026-01-01T00:00:00Z' }]
    render(<TodoPanel {...baseProps} todos={[todo]} wikiComponents={comps} onUpdate={onUpdate} />)
    fireEvent.click(screen.getByText('Wiki adder'))
    const wikiSelect = screen.getByTestId('todo-detail-wiki-select') as HTMLSelectElement
    fireEvent.change(wikiSelect, { target: { value: 'comp-b' } })
    expect(onUpdate).toHaveBeenCalledWith('wp2', { wikiRefs: [{ componentId: 'comp-b', workspace: 'ws1', titleSnapshot: 'Doc B' }] })
  })
})

describe('TodoPanel editor — local datetime', () => {
  it('renders due-date as datetime-local input', () => {
    const todo = makeTodo({ id: 'dt1', title: 'Date todo', dueAt: '2026-07-15T14:30:00Z' })
    render(<TodoPanel {...baseProps} todos={[todo]} />)
    fireEvent.click(screen.getByText('Date todo'))
    const dateInput = screen.getByTestId('todo-detail-duedate') as HTMLInputElement
    expect(dateInput.type).toBe('datetime-local')
  })
})

describe('TodoPanel mutation error banner', () => {
  it('shows inline error banner when error is set and items exist', () => {
    const todo = makeTodo({ id: 'e1', title: 'Error item' })
    render(<TodoPanel {...baseProps} todos={[todo]} error="创建失败: 网络错误" />)
    const banner = screen.getByTestId('todo-mutation-error')
    expect(banner).toBeTruthy()
    expect(banner.textContent).toContain('网络错误')
    // Items still visible
    expect(screen.getByText('Error item')).toBeTruthy()
  })

  it('shows full error page when error is set and no items exist', () => {
    render(<TodoPanel {...baseProps} todos={[]} error="获取待办失败" />)
    expect(screen.getByTestId('todo-panel-error')).toBeTruthy()
    expect(screen.queryByTestId('todo-mutation-error')).toBeNull()
  })
})

describe('TodoPanel — failed create retains input', () => {
  it('keeps quick capture text and context on create failure', async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error('fail'))
    render(
      <TodoPanel
        {...baseProps}
        onCreate={onCreate}
        workspaces={[{ alias: 'ws1', path: '/a', color: '#000' }]}
        defaultWorkspace="ws1"
      />,
    )
    const input = screen.getByTestId('todo-quick-capture') as HTMLInputElement
    fireEvent.change(input, { target: { value: 'my todo' } })
    fireEvent.click(screen.getByTestId('todo-quick-capture-btn'))
    // Confirm the handler was invoked, then assert input retained
    await waitFor(() => expect(onCreate).toHaveBeenCalled())
    expect((screen.getByTestId('todo-quick-capture') as HTMLInputElement).value).toBe('my todo')
  })
})

describe('TodoPanel — async workspace fill', () => {
  it('fills qcWorkspace from defaultWorkspace when initial value is empty', () => {
    render(
      <TodoPanel
        {...baseProps}
        workspaces={[{ alias: 'ws1', path: '/a', color: '#000' }]}
        defaultWorkspace="ws1"
      />,
    )
    const wsSelect = screen.getByTestId('todo-qc-workspace') as HTMLSelectElement
    expect(wsSelect.value).toBe('ws1')
  })

  it('does not overwrite qcWorkspace once user has selected', () => {
    // Provide initial default then a new workspace list that would overwrite;
    // user selection should stick
    const { rerender } = render(
      <TodoPanel
        {...baseProps}
        workspaces={[{ alias: 'ws1', path: '/a', color: '#000' }]}
        defaultWorkspace="ws1"
      />,
    )
    const wsSelect = screen.getByTestId('todo-qc-workspace') as HTMLSelectElement
    fireEvent.change(wsSelect, { target: { value: 'ws1' } })
    // Rerender with a new default — should not overwrite
    rerender(
      <TodoPanel
        {...baseProps}
        workspaces={[{ alias: 'ws1', path: '/a', color: '#000' }, { alias: 'ws2', path: '/b', color: '#000' }]}
        defaultWorkspace="ws2"
      />,
    )
    expect((screen.getByTestId('todo-qc-workspace') as HTMLSelectElement).value).toBe('ws1')
  })
})
