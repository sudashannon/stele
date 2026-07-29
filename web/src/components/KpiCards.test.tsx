import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { KpiCards, classifyChanges } from './KpiCards'
import type { ChangeSummary } from '../api/types'

function makeChange(overrides: Partial<ChangeSummary>): ChangeSummary {
  return {
    name: 'x', workflow: 'full', phase: 'build', archived: false,
    tasksCompleted: 0, tasksTotal: 0, verifyResult: 'pending', createdAt: '',
    artifacts: {}, visualized: false, designReviewed: false, verifyReviewed: false,
    verifiedAt: '', buildMode: '', reviewMode: '', tddMode: '', autoTransition: false,
    ...overrides,
  }
}

const today = new Date('2026-07-09')
const sampleChanges = [
  makeChange({ name: 'a', archived: false, phase: 'build', createdAt: '2026-07-01', tasksCompleted: 5, tasksTotal: 10 }),
  makeChange({ name: 'b', archived: true }),
  makeChange({ name: 'c', archived: false, phase: 'verify', verifyResult: 'fail' }),
  makeChange({ name: 'd', archived: false, phase: 'build', createdAt: '2026-05-01', tasksCompleted: 0, tasksTotal: 3 }),
]

describe('classifyChanges', () => {
  it('classifies changes into active, archived, stuck, verifyFailed, and incomplete buckets', () => {
    const changes = [
      ...sampleChanges,
      // active, complete tasks, design phase (not build) -> must NOT count as stuck despite being old
      makeChange({ name: 'e', archived: false, phase: 'design', createdAt: '2026-01-01', tasksCompleted: 2, tasksTotal: 2 }),
    ]

    const result = classifyChanges(changes, 14, today)

    expect(result.active.map((c) => c.name)).toEqual(['a', 'c', 'd', 'e'])
    expect(result.archived.map((c) => c.name)).toEqual(['b'])
    expect(result.stuck.map((c) => c.name)).toEqual(['d'])
    expect(result.verifyFailed.map((c) => c.name)).toEqual(['c'])
    expect(result.incomplete.map((c) => c.name)).toEqual(['a', 'd'])
  })

  it('treats an old Trellis in-progress task as stuck', () => {
    const result = classifyChanges(
      [
        makeChange({
          name: 'trellis-task',
          sourceType: 'trellis',
          phase: 'in_progress',
          createdAt: '2026-06-01',
        }),
      ],
      14,
      today,
    )
    expect(result.stuck.map((c) => c.name)).toEqual(['trellis-task'])
  })
})

describe('KpiCards', () => {
  it('counts active, archived, verify-failed, incomplete tasks, and stuck changes', () => {
    render(
      <KpiCards
        changes={sampleChanges}
        stuckThresholdDays={14}
        now={today}
        activeFilter={null}
        onFilterSelect={() => {}}
      />,
    )

    expect(screen.getByTestId('kpi-active').textContent).toContain('3')
    expect(screen.getByTestId('kpi-archived').textContent).toContain('1')
    expect(screen.getByTestId('kpi-verify-failed').textContent).toContain('1')
    expect(screen.getByTestId('kpi-stuck').textContent).toContain('1')
    expect(screen.getByTestId('kpi-incomplete-tasks').textContent).toContain('8')
  })

  it('clicking a card that is not the active filter calls onFilterSelect with that card key', () => {
    const onFilterSelect = vi.fn()
    render(
      <KpiCards
        changes={sampleChanges}
        stuckThresholdDays={14}
        now={today}
        activeFilter={null}
        onFilterSelect={onFilterSelect}
      />,
    )

    fireEvent.click(screen.getByTestId('kpi-archived'))

    expect(onFilterSelect).toHaveBeenCalledWith('archived')
  })

  it('clicking the currently-active card toggles it off by calling onFilterSelect(null)', () => {
    const onFilterSelect = vi.fn()
    render(
      <KpiCards
        changes={sampleChanges}
        stuckThresholdDays={14}
        now={today}
        activeFilter="archived"
        onFilterSelect={onFilterSelect}
      />,
    )

    fireEvent.click(screen.getByTestId('kpi-archived'))

    expect(onFilterSelect).toHaveBeenCalledWith(null)
  })

  it('marks only the active-filter card with the selected-state indicator', () => {
    render(
      <KpiCards
        changes={sampleChanges}
        stuckThresholdDays={14}
        now={today}
        activeFilter="archived"
        onFilterSelect={() => {}}
      />,
    )

    expect(screen.getByTestId('kpi-archived').getAttribute('data-filter-active')).toBe('true')
    expect(screen.getByTestId('kpi-active').getAttribute('data-filter-active')).toBe('false')
  })

  it('supports keyboard activation (Enter) on a card, same as a click', () => {
    const onFilterSelect = vi.fn()
    render(
      <KpiCards
        changes={sampleChanges}
        stuckThresholdDays={14}
        now={today}
        activeFilter={null}
        onFilterSelect={onFilterSelect}
      />,
    )

    fireEvent.keyDown(screen.getByTestId('kpi-stuck'), { key: 'Enter' })

    expect(onFilterSelect).toHaveBeenCalledWith('stuck')
  })
})
