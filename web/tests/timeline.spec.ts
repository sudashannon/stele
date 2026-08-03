import { test, expect } from '@playwright/test'

test.use({ baseURL: 'http://localhost:8989' })

// The chart was blank after mid-July because it drew changes only. Against real
// data the default scope must reach today, and each scope must isolate a layer.
test('the timeline covers the recent window across scopes', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 700 })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: '时间线' }).click()
  await expect(page.getByTestId('wiki-timeline')).toBeVisible()
  // Months of history scroll horizontally, so the view has to land on today.
  const scrolled = await page.getByTestId('wiki-timeline').evaluate(async (element) => {
    for (let i = 0; i < 40 && element.scrollLeft === 0; i++) await new Promise((r) => setTimeout(r, 50))
    return { scrollLeft: element.scrollLeft, scrollWidth: element.scrollWidth, clientWidth: element.clientWidth }
  })
  expect(scrolled.scrollLeft).toBeGreaterThan(0)
  expect(scrolled.scrollLeft + scrolled.clientWidth).toBeGreaterThan(scrolled.scrollWidth * 0.5)

  const bars = page.getByTestId('wiki-timeline-bar')
  const all = await bars.count()
  const documents = await page.locator('[data-testid="wiki-timeline-bar"][data-kind="document"]').count()
  const sessions = await page.locator('[data-testid="wiki-timeline-bar"][data-kind="session"]').count()
  expect(documents).toBeGreaterThan(0)
  expect(sessions).toBeGreaterThan(0)
  expect(all).toBeGreaterThan(documents)
  await page.screenshot({ path: 'test-results/timeline-all.png' })

  const scope = (label: string) => page.getByRole('group', { name: '时间线口径' }).getByRole('button', { name: new RegExp(`^${label}`) })
  await scope('变更').click()
  await expect(page.locator('[data-testid="wiki-timeline-bar"][data-kind="document"]')).toHaveCount(0)
  await scope('会话').click()
  await expect(page.locator('[data-testid="wiki-timeline-bar"][data-kind="session"]').first()).toBeVisible()
  await page.screenshot({ path: 'test-results/timeline-sessions.png' })
})

// A purely-CSS invariant, so it is checked against computed style rather than in
// jsdom: the community strip is a horizontal scroller, and on a platform with
// space-taking scrollbars (Windows) a 26px strip left 9px of client height for
// 22px chips - the scrollbar painted across the legend.
test('the community legend never scrolls horizontally', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 700 })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: '时间线' }).click()
  await expect(page.getByTestId('wiki-timeline')).toBeVisible()

  const strip = page.getByTestId('community-filter-strip')
  await expect(strip).toBeVisible()
  // A legend with a horizontal scrollbar hid most of itself (4411px of scroll in
  // a 964px strip) and, where scrollbars take space, painted over its own chips.
  const collapsed = await strip.evaluate((element) => ({
    scrollWidth: element.scrollWidth,
    clientWidth: element.clientWidth,
    chips: element.querySelectorAll('[data-testid="community-chip"]').length,
  }))
  expect(collapsed.scrollWidth).toBeLessThanOrEqual(collapsed.clientWidth + 1)
  expect(collapsed.chips).toBeLessThanOrEqual(8)

  // The rest is one click away, and expanding still must not introduce a scroller.
  await page.getByTestId('community-expand').click()
  const expanded = await strip.evaluate((element) => ({
    scrollWidth: element.scrollWidth,
    clientWidth: element.clientWidth,
    chips: element.querySelectorAll('[data-testid="community-chip"]').length,
  }))
  expect(expanded.chips).toBeGreaterThan(collapsed.chips)
  expect(expanded.scrollWidth).toBeLessThanOrEqual(expanded.clientWidth + 1)
})
