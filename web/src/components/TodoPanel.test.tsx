import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
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
    externalRef: null,
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
  counts: { total: 0, open: 0, inProgress: 0, done: 0, blocked: 0, dropped: 0 },
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

  it('groups blocked and dropped items in distinct terminal sections', () => {
    render(<TodoPanel {...baseProps} todos={[
      makeTodo({ id: 'b1', title: 'Blocked task', status: 'blocked' }),
      makeTodo({ id: 'x1', title: 'Dropped task', status: 'dropped' }),
    ]} />)
    expect(screen.getByTestId('todo-group-blocked').textContent).toContain('Blocked task')
    expect(screen.getByTestId('todo-group-dropped').textContent).toContain('Dropped task')
  })

  it('filters each extended status and renders OMP phase and blocker', () => {
    const blocked = makeTodo({
      id: 'omp-1',
      title: 'OMP blocked',
      status: 'blocked',
      metadata: { source: 'omp' },
      externalRef: { system: 'omp', sessionId: 'session', taskKey: '0:0', phase: 'build', blocker: 'waiting' },
    })
    const dropped = makeTodo({ id: 'drop-1', title: 'Dropped task', status: 'dropped' })
    render(<TodoPanel {...baseProps} todos={[blocked, dropped]} counts={{ total: 2, open: 0, inProgress: 0, done: 0, blocked: 1, dropped: 1 }} />)
    expect(screen.getByText(/1 个阻塞/).textContent).toContain('1 个放弃')

    const origin = screen.getByTestId('todo-omp-origin-omp-1')
    expect(origin.textContent).toContain('build')
    expect(origin.textContent).toContain('waiting')
    fireEvent.click(screen.getByTestId('todo-filter-blocked'))
    expect(screen.getByText('OMP blocked')).toBeTruthy()
    expect(screen.queryByText('Dropped task')).toBeNull()
    fireEvent.click(screen.getByText('OMP blocked'))
    expect(screen.getByTestId('todo-detail-omp-origin').textContent).toContain('session / 0:0')
    expect(screen.getByTestId('todo-status-dropped')).toBeTruthy()
  })

  // The projection records which session produced a todo. Resolving that id to
  // an indexed transcript is what turns the origin chip into the way back to the
  // work it came from; an unindexed session must stay inert rather than dangle.
  it('opens the source session from the origin chip when that session is indexed', () => {
    const projected = makeTodo({
      id: 'omp-2',
      title: 'From a session',
      metadata: { source: 'omp' },
      externalRef: { system: 'omp', sessionId: 'sess-uuid', taskKey: '0:1', phase: 'build', blocker: '' },
    })
    const onNavigateSession = vi.fn()
    const { unmount } = render(
      <TodoPanel
        {...baseProps}
        todos={[projected]}
        onNavigateSession={onNavigateSession}
        sessionPathById={{ 'sess-uuid': '/home/u/.omp/agent/sessions/-repo/a.jsonl' }}
      />,
    )

    const chip = screen.getByTestId('todo-omp-origin-omp-2')
    fireEvent.click(chip)
    expect(onNavigateSession).toHaveBeenCalledWith('/home/u/.omp/agent/sessions/-repo/a.jsonl')
    unmount()

    render(<TodoPanel {...baseProps} todos={[projected]} onNavigateSession={onNavigateSession} sessionPathById={{}} />)
    fireEvent.click(screen.getByTestId('todo-omp-origin-omp-2'))
    expect(onNavigateSession).toHaveBeenCalledTimes(1)
  })

  // A blocked todo's one-line reason is rarely enough; the session that produced
  // it holds the context. The detail panel is where a reader lands, so the jump
  // has to be there and not only on the list row.
  it('opens the source session from the detail panel, and says when it is not indexed', () => {
    const projected = makeTodo({
      id: 'omp-3',
      title: '定位首包超时根因',
      status: 'blocked',
      metadata: { source: 'omp' },
      externalRef: { system: 'omp', sessionId: 'sess-uuid', taskKey: '1:0', phase: '根因修复', blocker: '依赖板端历史 BSP' },
    })
    const onNavigateSession = vi.fn()
    const { unmount } = render(
      <TodoPanel
        {...baseProps}
        todos={[projected]}
        counts={{ total: 1, open: 0, inProgress: 0, done: 0, blocked: 1, dropped: 0 }}
        onNavigateSession={onNavigateSession}
        sessionPathById={{ 'sess-uuid': '/home/u/.omp/agent/sessions/-repo/a.jsonl' }}
      />,
    )
    fireEvent.click(screen.getByText('定位首包超时根因'))

    fireEvent.click(screen.getByTestId('todo-detail-open-session'))
    expect(onNavigateSession).toHaveBeenCalledWith('/home/u/.omp/agent/sessions/-repo/a.jsonl')
    expect(screen.queryByTestId('todo-detail-session-missing')).toBeNull()
    unmount()

    // An unindexed session gets an explanation instead of a dead button.
    render(
      <TodoPanel
        {...baseProps}
        todos={[projected]}
        counts={{ total: 1, open: 0, inProgress: 0, done: 0, blocked: 1, dropped: 0 }}
        onNavigateSession={onNavigateSession}
        sessionPathById={{}}
      />,
    )
    fireEvent.click(screen.getByText('定位首包超时根因'))
    expect(screen.queryByTestId('todo-detail-open-session')).toBeNull()
    expect(screen.getByTestId('todo-detail-session-missing').textContent).toContain('未被索引')
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

  it('clears change relation only after confirmation', async () => {
    const onUpdate = vi.fn().mockResolvedValue(makeTodo({ id: 'cc1', title: 'With change', change: null }))
    const todo = makeTodo({
      id: 'cc1',
      title: 'With change',
      change: { workspace: 'ws1', name: 'my-change' },
    })
    render(<TodoPanel {...baseProps} todos={[todo]} onUpdate={onUpdate} />)
    fireEvent.click(screen.getByText('With change'))
    fireEvent.click(screen.getByTestId('todo-clear-change'))
    expect(onUpdate).not.toHaveBeenCalled()
    fireEvent.click(screen.getByTestId('todo-destructive-confirm-submit'))
    await waitFor(() => expect(onUpdate).toHaveBeenCalledWith('cc1', { change: null }))
  })

  it('removes wiki ref only after confirmation', async () => {
    const onUpdate = vi.fn().mockResolvedValue(makeTodo({ id: 'wr1', title: 'With wiki ref', wikiRefs: [] }))
    const todo = makeTodo({
      id: 'wr1',
      title: 'With wiki ref',
      wikiRefs: [{ componentId: 'comp-a', workspace: 'ws1', titleSnapshot: 'Doc A' }],
    })
    render(<TodoPanel {...baseProps} todos={[todo]} onUpdate={onUpdate} />)
    fireEvent.click(screen.getByText('With wiki ref'))
    fireEvent.click(screen.getByTestId('todo-remove-wikiref-comp-a'))
    expect(onUpdate).not.toHaveBeenCalled()
    fireEvent.click(screen.getByTestId('todo-destructive-confirm-submit'))
    await waitFor(() => expect(onUpdate).toHaveBeenCalledWith('wr1', { wikiRefs: [] }))
  })

  it('searches documents, prioritizes current workspace, and supports keyboard selection', () => {
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
          { id: 'other-workspace', type: 'plan', title: 'Other workspace', path: '/other.md', workspace: 'ws2', updatedAt: '2026-07-31T00:00:00Z' },
          { id: 'linked', type: 'tasks', title: 'Already linked', path: '/linked.md', workspace: 'ws1', updatedAt: '2026-07-30T00:00:00Z' },
          { id: 'recent', type: 'proposal', title: 'Duplicate recent', path: '/duplicate.md', workspace: 'ws1', updatedAt: '2026-07-30T00:00:00Z' },
        ]}
      />,
    )

    fireEvent.click(screen.getByText('Attach docs'))
    const combobox = screen.getByTestId('todo-detail-wiki-combobox')
    fireEvent.focus(combobox)
    const listbox = screen.getByRole('listbox')
    expect(listbox.textContent).toContain('当前工作区 · ws1')
    expect(listbox.textContent).toContain('其他工作区')
    const options = within(listbox).getAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual([
      expect.stringContaining('Recent doc'),
      expect.stringContaining('Old doc'),
      expect.stringContaining('Other workspace'),
    ])
    fireEvent.keyDown(combobox, { key: 'ArrowDown' })
    expect(within(listbox).getAllByRole('option')[1].getAttribute('aria-selected')).toBe('true')
    fireEvent.keyDown(combobox, { key: 'Enter' })
    expect(onUpdate).toHaveBeenCalledWith('docs1', {
      wikiRefs: [
        linkedRef,
        { componentId: 'old', workspace: 'ws1', titleSnapshot: 'Old doc' },
      ],
    })
    fireEvent.focus(combobox)
    fireEvent.change(combobox, { target: { value: 'other' } })
    expect(within(screen.getByRole('listbox')).getAllByRole('option')).toHaveLength(1)
    fireEvent.keyDown(combobox, { key: 'Escape' })
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('limits document combobox results to twenty', () => {
    const todo = makeTodo({ id: 'docs-limit', title: 'Many docs' })
    const wikiComponents = Array.from({ length: 25 }, (_, index) => ({
      id: `doc-${index}`,
      type: 'spec' as const,
      title: `Document ${index}`,
      path: `/doc-${index}.md`,
      workspace: 'ws1',
      updatedAt: new Date(Date.UTC(2026, 6, index + 1)).toISOString(),
    }))
    render(<TodoPanel {...baseProps} todos={[todo]} wikiComponents={wikiComponents} />)
    fireEvent.click(screen.getByText('Many docs'))
    fireEvent.focus(screen.getByTestId('todo-detail-wiki-combobox'))
    expect(within(screen.getByRole('listbox')).getAllByRole('option')).toHaveLength(20)
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
    expect(screen.getByTestId('todo-detail-wiki-combobox')).toBeTruthy()
  })

  it('appends a wiki ref on select', () => {
    const onUpdate = vi.fn().mockResolvedValue(makeTodo({ id: 'wp2', title: 'Wiki adder' }))
    const todo = makeTodo({ id: 'wp2', title: 'Wiki adder' })
    const comps = [{ id: 'comp-b', workspace: 'ws1', title: 'Doc B', type: 'spec' as const, path: '/b/doc.md', updatedAt: '2026-01-01T00:00:00Z' }]
    render(<TodoPanel {...baseProps} todos={[todo]} wikiComponents={comps} onUpdate={onUpdate} />)
    fireEvent.click(screen.getByText('Wiki adder'))
    const combobox = screen.getByTestId('todo-detail-wiki-combobox')
    fireEvent.focus(combobox)
    fireEvent.click(screen.getByRole('option', { name: /Doc B/ }))
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

describe('TodoPanel row keyboard semantics', () => {
  it('keeps one row tabbable and moves focus with roving keyboard controls', () => {
    const first = makeTodo({ id: 'keyboard-1', title: 'Keyboard first' })
    const second = makeTodo({ id: 'keyboard-2', title: 'Keyboard second' })
    const third = makeTodo({ id: 'keyboard-3', title: 'Keyboard third' })
    render(<TodoPanel {...baseProps} todos={[first, second, third]} />)
    const firstRow = screen.getByRole('button', { name: '查看待办：Keyboard first' })
    const secondRow = screen.getByRole('button', { name: '查看待办：Keyboard second' })
    const thirdRow = screen.getByRole('button', { name: '查看待办：Keyboard third' })

    expect(firstRow.getAttribute('tabindex')).toBe('0')
    expect(secondRow.getAttribute('tabindex')).toBe('-1')
    expect(thirdRow.getAttribute('tabindex')).toBe('-1')

    firstRow.focus()
    fireEvent.keyDown(firstRow, { key: 'ArrowDown' })
    expect(secondRow.getAttribute('tabindex')).toBe('0')
    expect(document.activeElement).toBe(secondRow)
    expect(screen.getByTestId('todo-detail')).toBeTruthy()

    fireEvent.keyDown(secondRow, { key: 'End' })
    expect(document.activeElement).toBe(thirdRow)
    fireEvent.keyDown(thirdRow, { key: 'Home' })
    expect(document.activeElement).toBe(firstRow)
    fireEvent.keyDown(firstRow, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(firstRow)

    fireEvent.keyDown(firstRow, { key: 'Enter' })
    expect(screen.queryByTestId('todo-detail')).toBeNull()
    fireEvent.keyDown(firstRow, { key: ' ' })
    expect(screen.getByTestId('todo-detail')).toBeTruthy()
  })
})

describe('TodoPanel destructive confirmation', () => {
  it('cancels deletion without a request and confirms deletion exactly once', async () => {
    const todo = makeTodo({ id: 'delete-1', title: 'Delete me' })
    const onDelete = vi.fn().mockResolvedValue(undefined)
    render(<TodoPanel {...baseProps} todos={[todo]} onDelete={onDelete} />)
    fireEvent.click(screen.getByText('Delete me'))
    fireEvent.click(screen.getByTestId('todo-delete-btn'))
    expect(onDelete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('取消'))
    expect(onDelete).not.toHaveBeenCalled()

    fireEvent.click(screen.getByTestId('todo-delete-btn'))
    const confirm = screen.getByTestId('todo-destructive-confirm-submit')
    fireEvent.click(confirm)
    fireEvent.click(confirm)
    await waitFor(() => expect(onDelete).toHaveBeenCalledTimes(1))
  })
  it('keeps confirmation open when the selected todo is refetched', () => {
    const todo = makeTodo({ id: 'refetch-1', title: 'Refetched todo' })
    const { rerender } = render(<TodoPanel {...baseProps} todos={[todo]} />)
    fireEvent.click(screen.getByText('Refetched todo'))
    fireEvent.click(screen.getByTestId('todo-delete-btn'))
    expect(screen.getByTestId('todo-destructive-confirm-submit')).toBeTruthy()

    rerender(
      <TodoPanel
        {...baseProps}
        todos={[{ ...todo, title: 'Refetched todo updated', updatedAt: '2026-07-30T01:00:00Z' }]}
      />,
    )

    expect(screen.getByTestId('todo-destructive-confirm-submit')).toBeTruthy()
  })

})

describe('TodoPanel list windowing', () => {
  it('preserves the full list height and renders the last of 520 items after scrolling', () => {
    const todos = Array.from({ length: 520 }, (_, index) =>
      makeTodo({ id: `window-${index}`, title: `Window item ${index}` }),
    )
    render(<TodoPanel {...baseProps} todos={todos} />)
    expect(screen.getAllByTestId(/^todo-row-/).length).toBeLessThanOrEqual(60)
    expect(screen.queryByText('Window item 519')).toBeNull()
    expect(screen.getByTestId('todo-list-bottom-spacer').style.height).toBe(`${460 * 56}px`)

    const scroller = screen.getByTestId('todo-list-scroll')
    fireEvent.scroll(scroller, { target: { scrollTop: 100_000 } })
    expect(screen.getByText('Window item 519')).toBeTruthy()
    expect(screen.getByTestId('todo-list-top-spacer').style.height).toBe(`${460 * 56}px`)
    expect(screen.queryByTestId('todo-list-bottom-spacer')).toBeNull()
    expect(screen.getAllByTestId(/^todo-row-/).length).toBeLessThanOrEqual(60)

    const lastRow = screen.getByRole('button', { name: '查看待办：Window item 519' })
    lastRow.focus()
    fireEvent.keyDown(lastRow, { key: 'Home' })
    const firstRow = screen.getByRole('button', { name: '查看待办：Window item 0' })
    expect(document.activeElement).toBe(firstRow)
    expect(screen.getByTestId('todo-list-bottom-spacer').style.height).toBe(`${460 * 56}px`)
    expect(screen.queryByTestId('todo-list-top-spacer')).toBeNull()

    fireEvent.keyDown(firstRow, { key: 'End' })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看待办：Window item 519' }))
    expect(screen.getByTestId('todo-list-top-spacer').style.height).toBe(`${460 * 56}px`)
    expect(screen.queryByTestId('todo-list-bottom-spacer')).toBeNull()
  })
})
