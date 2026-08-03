import { test, expect } from '@playwright/test'

test.use({ baseURL: 'http://localhost:8989' })

// The calendar colours each artifact badge from the type palette. knowledge,
// report and artifact all fell through to the same grey fallback, so a day
// mixing them told the reader nothing.
test('calendar badges give each document type its own colour', async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 1000 })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: '日历' }).click()

  // Walk days until one shows at least two different artifact types.
  // Only days that carry artifacts are worth opening: the dot marks them.
  const marked = page.locator('[data-testid^="calendar-dot-"]')
  await expect(marked.first()).toBeVisible()
  const days = page.locator('[data-testid^="calendar-day-"]').filter({ has: page.locator('[data-testid^="calendar-dot-"]') })
  const total = await days.count()
  for (let i = 0; i < total; i++) {
    await days.nth(i).click()
    const badges = page.locator('span[style*="background-color"]')
    if (await badges.count() < 2) continue
    const seen = await badges.evaluateAll((els) =>
      els.map((el) => ({ type: el.textContent?.trim(), color: (el as HTMLElement).style.backgroundColor })))
    const types = [...new Set(seen.map((b) => b.type))]
    if (types.length < 2) continue
    // Distinct types must not share a colour.
    const byType = new Map(seen.map((b) => [b.type, b.color]))
    expect(new Set(byType.values()).size).toBe(byType.size)
    await page.screenshot({ path: 'test-results/calendar-badges.png' })
    return
  }
  test.skip(true, 'no day with two artifact types on this machine')
})
