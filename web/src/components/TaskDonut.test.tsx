import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { TaskDonut } from './TaskDonut'

describe('TaskDonut', () => {
  it('renders the percentage and fraction text', () => {
    render(<TaskDonut completed={19} total={31} />)
    expect(screen.getByTestId('donut-percent').textContent).toBe('61%')
    expect(screen.getByTestId('donut-fraction').textContent).toBe('19/31 任务完成')
  })

  it('handles zero total without dividing by zero', () => {
    render(<TaskDonut completed={0} total={0} />)
    expect(screen.getByTestId('donut-percent').textContent).toBe('0%')
  })

  it('uses success color at 100%', () => {
    render(<TaskDonut completed={5} total={5} />)
    const ring = screen.getByTestId('donut-ring')
    expect(ring.style.background).toContain('var(--color-success)')
  })
})
