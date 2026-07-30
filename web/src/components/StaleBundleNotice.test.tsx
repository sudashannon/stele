import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { StaleBundleNotice } from './StaleBundleNotice'

function dispatchChunkError(message: string) {
  fireEvent(window, new ErrorEvent('error', { error: new TypeError(message), message }))
}

describe('StaleBundleNotice', () => {
  it('stays silent until a chunk fails to load', () => {
    render(<StaleBundleNotice />)
    expect(screen.queryByTestId('stale-bundle-notice')).toBeNull()
  })

  it('surfaces a reload prompt when a lazy chunk 404s after a deploy', () => {
    const onReload = vi.fn()
    render(<StaleBundleNotice onReload={onReload} />)

    dispatchChunkError('Failed to fetch dynamically imported module: http://host/assets/WikiGraph-abc.js')

    const notice = screen.getByTestId('stale-bundle-notice')
    expect(notice.getAttribute('role')).toBe('alert')
    expect(notice.textContent).toContain('刷新')
    fireEvent.click(screen.getByTestId('stale-bundle-reload'))
    expect(onReload).toHaveBeenCalledTimes(1)
  })

  it('also reacts to the rejected-promise form of the same failure', () => {
    render(<StaleBundleNotice />)

    const event = new Event('unhandledrejection') as Event & { reason?: unknown }
    event.reason = new TypeError('error loading dynamically imported module: /assets/mermaid.core-x.js')
    fireEvent(window, event)

    expect(screen.getByTestId('stale-bundle-notice')).toBeTruthy()
  })

  it('ignores unrelated runtime errors', () => {
    render(<StaleBundleNotice />)
    dispatchChunkError('Cannot read properties of undefined (reading foo)')
    expect(screen.queryByTestId('stale-bundle-notice')).toBeNull()
  })
})
