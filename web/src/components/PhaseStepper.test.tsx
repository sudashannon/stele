import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { PhaseStepper } from './PhaseStepper'

describe('PhaseStepper', () => {
  it('marks phases before current as done, current as current, rest as pending', () => {
    render(<PhaseStepper currentPhase="build" />)
    expect(screen.getByTestId('step-open').dataset.state).toBe('done')
    expect(screen.getByTestId('step-design').dataset.state).toBe('done')
    expect(screen.getByTestId('step-build').dataset.state).toBe('current')
    expect(screen.getByTestId('step-verify').dataset.state).toBe('pending')
    expect(screen.getByTestId('step-archive').dataset.state).toBe('pending')
  })

  it('provides a visible text state in addition to phase color', () => {
    render(<PhaseStepper currentPhase="build" />)
    expect(screen.getAllByText('已完成').length).toBe(2)
    expect(screen.getByText('当前')).toBeTruthy()
    expect(screen.getAllByText('待开始').length).toBe(2)
  })

  it('renders Chinese labels', () => {
    render(<PhaseStepper currentPhase="open" />)
    expect(screen.getByText('启动')).toBeTruthy()
    expect(screen.getByText('设计')).toBeTruthy()
    expect(screen.getByText('构建')).toBeTruthy()
    expect(screen.getByText('验证')).toBeTruthy()
    expect(screen.getByText('归档')).toBeTruthy()
  })

  it('marks all phases as unknown (not pending) when currentPhase is empty, e.g. a change with no .comet.yaml', () => {
    render(<PhaseStepper currentPhase="" />)
    expect(screen.getByTestId('step-open').dataset.state).toBe('unknown')
    expect(screen.getByTestId('step-design').dataset.state).toBe('unknown')
    expect(screen.getByTestId('step-build').dataset.state).toBe('unknown')
    expect(screen.getByTestId('step-verify').dataset.state).toBe('unknown')
    expect(screen.getByTestId('step-archive').dataset.state).toBe('unknown')
    expect(screen.getByTestId('phase-unknown-notice')).toBeTruthy()
    expect(screen.getByText('阶段信息缺失')).toBeTruthy()
  })

  it('marks all phases as unknown for an arbitrary unrecognized phase string', () => {
    render(<PhaseStepper currentPhase="not-a-real-phase" />)
    expect(screen.getByTestId('step-open').dataset.state).toBe('unknown')
    expect(screen.getByTestId('step-archive').dataset.state).toBe('unknown')
    expect(screen.getByTestId('phase-unknown-notice')).toBeTruthy()
  })

  it('renders a source-provided Trellis lifecycle', () => {
    render(
      <PhaseStepper
        currentPhase="in_progress"
        lifecycle={[
          { key: 'planning', label: '规划' },
          { key: 'in_progress', label: '执行' },
          { key: 'completed', label: '完成' },
        ]}
      />,
    )
    expect(screen.getByTestId('step-planning').dataset.state).toBe('done')
    expect(screen.getByTestId('step-in_progress').dataset.state).toBe('current')
    expect(screen.getByTestId('step-completed').dataset.state).toBe('pending')
    expect(screen.queryByTestId('step-build')).toBeNull()
  })
})
