import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { ChangeExplorer } from './ChangeExplorer'
import type { ChangeSummary } from '../api/types'

function makeChange(overrides: Partial<ChangeSummary> & { name: string }): ChangeSummary {
  return {
    workflow: 'full', phase: 'build', archived: false,
    tasksCompleted: 1, tasksTotal: 2, verifyResult: 'pending', createdAt: '2026-07-01',
    artifacts: {}, visualized: false, designReviewed: false, verifyReviewed: false,
    verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
    ...overrides,
  }
}

describe('ChangeExplorer', () => {
  it('lists changes in a table and calls onSelect when a row is clicked', () => {
    const onSelect = vi.fn()
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'foo' }), makeChange({ name: 'bar' })]}
        selected={null}
        onSelect={onSelect}
      />,
    )
    expect(screen.getByText('foo')).toBeTruthy()
    expect(screen.getByText('bar')).toBeTruthy()
    // Rows are <tr role="button"> — click the row containing 'bar'
    fireEvent.click(screen.getByRole('button', { name: /打开变更 bar/ }))
    expect(onSelect).toHaveBeenCalledWith('bar', undefined)
  })

  it('shows phase as a coloured dot and neutral-ink label in the table, and hoists constant-valued workflow/source-type chips to the summary line', () => {
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'foo', phase: 'build', workflow: 'full' })]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    // Change name is in the table.
    expect(screen.getByText('foo')).toBeTruthy()

    // The phase label 'build' appears in the table row with neutral ink
    // (color-text-primary). Scope to the table row so the phase-filter
    // dropdown option with the same text is not confused with the label.
    const table = screen.getByRole('table')
    expect(table.textContent).toContain('build')

    // The constant workflow 'full' is hoisted to the summary line,
    // not repeated on every row. It appears in the summary text.
    expect(screen.getByText(/full 工作流/)).toBeTruthy()
  })

  it('groups archived changes under a clickable "已归档 (N)" table divider row', () => {
    const onSelect = vi.fn()
    const { container } = render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'active-1' }),
          makeChange({ name: 'archived-1', archived: true }),
          makeChange({ name: 'archived-2', archived: true }),
        ]}
        selected={null}
        onSelect={onSelect}
      />,
    )

    // The active change is visible in the table.
    expect(screen.getByText('active-1')).toBeTruthy()

    // The archived divider shows the count.
    expect(screen.getByText('已归档 (2)')).toBeTruthy()

    // The archived divider is a clickable table row (role="button").
    const divider = screen.getByRole('button', { name: /已归档/ })
    expect(divider).toBeTruthy()
    // By default the archived rows are collapsed.
    expect(divider.getAttribute('aria-expanded')).toBe('false')

    // Click to expand.
    fireEvent.click(divider)
    expect(screen.getByText('archived-1')).toBeTruthy()
    expect(screen.getByText('archived-2')).toBeTruthy()
  })

  it('auto-expands the archived section when an archived change is selected', () => {
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'archived-1', archived: true })]}
        selected="archived-1"
        onSelect={vi.fn()}
      />,
    )
    const divider = screen.getByRole('button', { name: /已归档/ })
    expect(divider.getAttribute('aria-expanded')).toBe('true')
  })

  it('leaves the archived section collapsed when nothing is selected', () => {
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'archived-1', archived: true })]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    const divider = screen.getByRole('button', { name: /已归档/ })
    expect(divider.getAttribute('aria-expanded')).toBe('false')
  })

  it('narrows the list to a case-insensitive substring match on name via the search input', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'Add-Wiki-Feature' }),
          makeChange({ name: 'fix-bug' }),
          makeChange({ name: 'add-chat' }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    const input = screen.getByPlaceholderText('搜索变更名称…')
    fireEvent.change(input, { target: { value: 'add' } })
    expect(screen.getByText('Add-Wiki-Feature')).toBeTruthy()
    expect(screen.getByText('add-chat')).toBeTruthy()
    expect(screen.queryByText('fix-bug')).toBeNull()
  })

  it('filters by workflow using the workflow select', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'full-1', workflow: 'full' }),
          makeChange({ name: 'hotfix-1', workflow: 'hotfix' }),
          makeChange({ name: 'tweak-1', workflow: 'tweak' }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    const select = screen.getByLabelText('工作流')
    fireEvent.change(select, { target: { value: 'hotfix' } })
    expect(screen.getByText('hotfix-1')).toBeTruthy()
    expect(screen.queryByText('full-1')).toBeNull()
    expect(screen.queryByText('tweak-1')).toBeNull()
  })

  it('filters by phase using the phase select', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'open-1', phase: 'open' }),
          makeChange({ name: 'design-1', phase: 'design' }),
          makeChange({ name: 'build-1', phase: 'build' }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    const select = screen.getByLabelText('阶段')
    fireEvent.change(select, { target: { value: 'design' } })
    expect(screen.getByText('design-1')).toBeTruthy()
    expect(screen.queryByText('open-1')).toBeNull()
    expect(screen.queryByText('build-1')).toBeNull()
  })

  it('filters by status using the status select, spanning both active and archived groups', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'active-1', archived: false }),
          makeChange({ name: 'archived-1', archived: true }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    const select = screen.getByLabelText('状态')
    fireEvent.change(select, { target: { value: 'archived' } })
    expect(screen.queryByText('active-1')).toBeNull()
    expect(screen.getByText('archived-1')).toBeTruthy()
  })

  it('applies search + workflow + phase filters as an intersection', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'add-wiki', workflow: 'full', phase: 'build' }),
          makeChange({ name: 'add-hotfix', workflow: 'hotfix', phase: 'build' }),
          makeChange({ name: 'add-other', workflow: 'full', phase: 'verify' }),
          makeChange({ name: 'skip-this', workflow: 'full', phase: 'build' }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByPlaceholderText('搜索变更名称…'), { target: { value: 'add' } })
    fireEvent.change(screen.getByLabelText('工作流'), { target: { value: 'full' } })
    fireEvent.change(screen.getByLabelText('阶段'), { target: { value: 'build' } })

    expect(screen.getByText('add-wiki')).toBeTruthy()
    expect(screen.queryByText('add-hotfix')).toBeNull()
    expect(screen.queryByText('add-other')).toBeNull()
    expect(screen.queryByText('skip-this')).toBeNull()
  })

  it('shows a helpful empty-state with a working "清除筛选" button when filters produce an empty result', () => {
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'foo' }), makeChange({ name: 'bar' })]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByPlaceholderText('搜索变更名称…'), { target: { value: 'nonexistent' } })
    expect(screen.getByText('无匹配的变更')).toBeTruthy()

    fireEvent.click(screen.getByText('清除筛选'))
    expect(screen.getByText('foo')).toBeTruthy()
    expect(screen.getByText('bar')).toBeTruthy()
    expect((screen.getByPlaceholderText('搜索变更名称…') as HTMLInputElement).value).toBe('')
  })

  it('clearing the search input restores the full list', () => {
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'foo' }), makeChange({ name: 'bar' })]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    const input = screen.getByPlaceholderText('搜索变更名称…')
    fireEvent.change(input, { target: { value: 'foo' } })
    expect(screen.queryByText('bar')).toBeNull()
    fireEvent.change(input, { target: { value: '' } })
    expect(screen.getByText('foo')).toBeTruthy()
    expect(screen.getByText('bar')).toBeTruthy()
  })

  it('offers and filters source-specific Trellis workflow and phases', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'open-change', workflow: 'full', phase: 'build' }),
          makeChange({
            name: 'trellis-task',
            sourceType: 'trellis',
            workflow: 'trellis',
            phase: 'in_progress',
          }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText('工作流'), { target: { value: 'trellis' } })
    fireEvent.change(screen.getByLabelText('阶段'), { target: { value: 'in_progress' } })
    expect(screen.getByText('trellis-task')).toBeTruthy()
    expect(screen.queryByText('open-change')).toBeNull()
  })

  it('labels standalone Superpowers items in the summary line', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({
            name: 'cache-redesign',
            sourceType: 'superpowers',
            workflow: 'superpowers',
            phase: 'design',
          }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    // The source type is hoisted to the summary line.
    expect(screen.getByText(/Superpowers/)).toBeTruthy()
    // The change name still renders in the table.
    expect(screen.getByText('cache-redesign')).toBeTruthy()
  })

  it('opens a change from the keyboard and preserves the workspace argument', () => {
    const onSelect = vi.fn()
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'shared', workspace: 'alpha' })]}
        selected={null}
        onSelect={onSelect}
      />,
    )

    fireEvent.keyDown(screen.getByRole('button', { name: /打开变更 shared，工作区 alpha/ }), { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith('shared', 'alpha')
  })

  it('shows workspace in the dedicated table column when change names collide across workspaces', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'shared', workspace: 'alpha' }),
          makeChange({ name: 'shared', workspace: 'beta' }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )

    // Workspace is a dedicated column now — both aliases appear in table cells.
    expect(screen.getByText('alpha')).toBeTruthy()
    expect(screen.getByText('beta')).toBeTruthy()
  })

  it('handles empty workspace gracefully in the table column', () => {
    render(
      <ChangeExplorer
        changes={[
          makeChange({ name: 'duplicate', workspace: '   ' }),
          makeChange({ name: 'duplicate', workspace: 'ideas' }),
        ]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )

    // Both rows exist, identified by their workspace values.
    const rows = screen.getAllByRole('button', { name: /打开变更 duplicate/ })
    // First row has empty/whitespace workspace, second has 'ideas'.
    expect(rows.length).toBe(2)
    expect(rows[1].textContent).toContain('ideas')
  })

  it('opens a context menu on right-click with the card actions', async () => {
    const onSelect = vi.fn()
    render(
      <ChangeExplorer changes={[makeChange({ name: 'foo' })]} selected={null} onSelect={onSelect} />,
    )

    expect(screen.queryByRole('menu')).toBeNull()
    fireEvent.contextMenu(screen.getByRole('button', { name: /打开变更 foo/ }), {
      clientX: 120,
      clientY: 240,
    })

    const menu = await screen.findByRole('menu')
    expect(menu.getAttribute('aria-orientation')).toBe('vertical')
    const labels = Array.from(menu.querySelectorAll('[role=menuitem]')).map((item) => item.textContent)
    expect(labels).toEqual(['打开', '复制名称'])

    fireEvent.click(screen.getByRole('menuitem', { name: '打开' }))
    expect(onSelect).toHaveBeenCalledWith('foo', undefined)
  })

})
