import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { ReviewBadges } from './ReviewBadges'

describe('ReviewBadges', () => {
  it('shows pass tone when true, neutral tone when false', () => {
    render(<ReviewBadges visualized={true} designReviewed={false} verifyReviewed={true} />)
    expect(screen.getByTestId('badge-visualized').dataset.tone).toBe('ok')
    expect(screen.getByTestId('badge-design-reviewed').dataset.tone).toBe('neutral')
    expect(screen.getByTestId('badge-verify-reviewed').dataset.tone).toBe('ok')
    expect(screen.getByTestId('badge-design-reviewed').textContent).toContain('设计未审')
    expect(screen.getByTestId('badge-verify-reviewed').textContent).toContain('验证已审')
  })
})
