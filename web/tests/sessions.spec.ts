import { test, expect } from '@playwright/test'

// Runs against the deployed binary with its embedded frontend, like
// integration.spec.ts: the session layer needs the real API, and this is the
// artifact that actually ships.
test.use({ baseURL: 'http://localhost:8989' })

// The panel holds an open SSE stream (/api/wiki/events), so the network never
// goes idle - wait for the document, then for the elements themselves.
const OPEN = { waitUntil: 'domcontentloaded' } as const

test('the Agent 会话 rail item opens the sessions panel and a session detail', async ({ page }) => {
  await page.goto('/', OPEN)

  await page.getByRole('button', { name: 'Agent 会话' }).click()
  await expect(page.getByTestId('sessions-panel')).toBeVisible()

  const summary = page.getByTestId('sessions-summary')
  await expect(summary).toContainText('会话')
  const rows = page.locator('[data-testid="sessions-panel"] li button')
  await expect(rows.first()).toBeVisible()
  const total = await rows.count()
  expect(total).toBeGreaterThan(0)

  await page.screenshot({ path: 'test-results/sessions-panel.png', fullPage: false })

  // Free-text filter narrows the list without touching the server.
  const firstTitle = (await rows.first().textContent()) ?? ''
  await page.getByLabel('搜索会话').fill('这个字符串不该匹配任何会话')
  await expect(page.getByText('没有匹配的会话。')).toBeVisible()
  await page.getByLabel('搜索会话').fill('')
  await expect(rows.first()).toBeVisible()

  // A row opens the session summary card, never the Markdown viewer.
  await rows.first().click()
  await expect(page.getByTestId('session-detail')).toBeVisible()
  await expect(page.getByTestId('markdown-viewer')).toHaveCount(0)
  await page.screenshot({ path: 'test-results/session-detail.png', fullPage: false })

  expect(firstTitle.length).toBeGreaterThan(0)
})

// The task record is the only surviving trace of what a session set out to do,
// and it is served by the single-session endpoint - so it has to survive the
// click, not just the API.
test('a session that used the tracker shows its task record', async ({ page }) => {
  await page.goto('/', OPEN)
  await page.getByRole('button', { name: 'Agent 会话' }).click()

  const rows = page.locator('[data-testid="sessions-panel"] li button')
  await expect(rows.first()).toBeVisible()
  const total = await rows.count()

  for (let index = 0; index < total; index++) {
    await rows.nth(index).click()
    await expect(page.getByTestId('session-detail')).toBeVisible()
    const record = page.getByTestId('session-todos')
    if (await record.count()) {
      await expect(record).toContainText('会话待办')
      await expect(record).toContainText(/当前 \d+ 项/)
      await page.screenshot({ path: 'test-results/session-todos.png' })
      return
    }
    await page.getByRole('button', { name: '关闭' }).click()
    await expect(rows.first()).toBeVisible()
  }
  test.skip(true, 'no indexed session used the tracker on this machine')
})

test('Ctrl+9 selects the sessions view', async ({ page }) => {
  await page.goto('/', OPEN)
  // domcontentloaded fires before React attaches its key listener.
  const railItem = page.getByRole('button', { name: 'Agent 会话' })
  await expect(railItem).toBeVisible()
  await page.keyboard.press('Control+9')
  await expect(page.getByRole('button', { name: 'Agent 会话' })).toHaveAttribute('aria-current', 'page')
  await expect(page.getByTestId('sessions-panel')).toBeVisible()
})
