import { test, expect } from '@playwright/test'

test.use({ baseURL: 'http://localhost:8989' })

// A projected todo's one-line blocker is rarely enough context. The jump back to
// the session that produced it has to work end to end: the id in the projection
// is a session uuid, and only the session index knows its transcript path.
test('a projected todo opens its source session from the detail panel', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 900 })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: '待办' }).click()

  const projected = page.locator('[data-testid^="todo-omp-origin-"]').first()
  if (await projected.count() === 0) {
    test.skip(true, 'no session-projected todo on this machine')
  }
  // Open the row that owns the first projected origin chip.
  await projected.locator('xpath=ancestor::*[starts-with(@data-testid,"todo-row-")]').first().click()

  const open = page.getByTestId('todo-detail-open-session')
  await expect(open).toBeVisible()
  await open.click()

  await expect(page.getByTestId('session-detail')).toBeVisible()
  await expect(page.getByTestId('session-detail')).toContainText('时间范围')
  await page.screenshot({ path: 'test-results/todo-to-session.png' })
})
