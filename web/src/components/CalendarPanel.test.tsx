import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CalendarPanel } from './CalendarPanel'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('CalendarPanel', () => {
  it('shows the aggregate error state when any month request fails', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({ ok: true, json: async () => ({ year: 2026, month: 1, days: {} }) } as Response)
      .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
      .mockResolvedValueOnce({ ok: true, json: async () => ({ year: 2026, month: 3, days: {} }) } as Response)

    render(<CalendarPanel onOpen={() => {}} />)

    expect((await screen.findByText('加载日历失败')).getAttribute('class')).toContain('text-[var(--color-danger)]')
    expect(screen.queryByText('2026年 1月')).toBeNull()
  })

  it('keeps a successfully loaded empty quarter distinct from an error', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = new URL(String(input), 'http://localhost')
      const year = Number(url.searchParams.get('year'))
      const month = Number(url.searchParams.get('month'))
      return { ok: true, json: async () => ({ year, month, days: {} }) } as Response
    })

    render(<CalendarPanel onOpen={() => {}} />)

    await waitFor(() => expect(screen.getAllByText(/年 \d+月/)).toHaveLength(3))
    expect(screen.queryByText('加载日历失败')).toBeNull()
    expect(screen.getByText('点击日期查看当天产物')).toBeTruthy()
  })
})
