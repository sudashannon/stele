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
  it('lists changes and calls onSelect when clicked', () => {
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
    fireEvent.click(screen.getByText('bar'))
    expect(onSelect).toHaveBeenCalledWith('bar', undefined)
  })

  it('renders a phase badge and workflow tag on each change card', () => {
    render(
      <ChangeExplorer
        changes={[makeChange({ name: 'foo', phase: 'build', workflow: 'full' })]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    expect(screen.getByText('foo')).toBeTruthy()
    expect(screen.getAllByText('build', { selector: 'span' })[0]).toBeTruthy()
    expect(screen.getAllByText('full', { selector: 'span' })[0]).toBeTruthy()
  })

  it('groups archived changes under a collapsible "已归档" section', () => {
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

    // The active change is visible in the flat list above the summary.
    expect(screen.getByText('active-1')).toBeTruthy()

    // The collapsible summary reports the archived count.
    expect(screen.getByText('已归档 (2)')).toBeTruthy()

    // Archived change cards live inside the <details>; in jsdom a collapsed
    // <details> still has the children in the DOM, but a real browser would
    // not render them — so we just check the structural grouping here.
    const details = container.querySelector('details')
    expect(details).toBeTruthy()
    expect(details?.contains(screen.getByText('archived-1'))).toBe(true)
    expect(details?.contains(screen.getByText('archived-2'))).toBe(true)
  })

  it('auto-expands the archived section when an archived change is selected', () => {
    const { container } = render(
      <ChangeExplorer
        changes={[makeChange({ name: 'archived-1', archived: true })]}
        selected="archived-1"
        onSelect={vi.fn()}
      />,
    )
    const details = container.querySelector('details')
    expect(details?.hasAttribute('open')).toBe(true)
  })

  it('leaves the archived section collapsed when nothing is selected', () => {
    const { container } = render(
      <ChangeExplorer
        changes={[makeChange({ name: 'archived-1', archived: true })]}
        selected={null}
        onSelect={vi.fn()}
      />,
    )
    const details = container.querySelector('details')
    expect(details?.hasAttribute('open')).toBe(false)
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
  it('labels standalone Superpowers items without reclassifying them as OpenSpec', () => {
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
    expect(screen.getByText('Superpowers')).toBeTruthy()
    expect(screen.queryByText('OpenSpec')).toBeNull()
  })
})
