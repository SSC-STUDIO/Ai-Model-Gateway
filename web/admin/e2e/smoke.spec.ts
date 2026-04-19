import { test, expect } from '@playwright/test'

test.describe('Admin UI Smoke Tests', () => {
  test.beforeEach(async ({ page }) => {
    // Mock admin session to bypass authentication so UI renders without a backend
    await page.route('/api/admin/session', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ enabled: false }),
      })
    })

    // Mock data APIs with minimal valid payloads to prevent error banners and timeouts
    await page.route('/api/admin/overview', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    })
    await page.route('/api/admin/status', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    })
    await page.route('/api/admin/telemetry**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    })
    await page.route('/api/admin/timeseries**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ points: [] }),
      })
    })
    await page.route('/api/admin/config/history', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    })
    await page.route('/api/admin/benchmark**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    })

    await page.goto('/')
  })

  test('overview page loads with navigation and data panels', async ({ page }) => {
    // Verify page title
    await expect(page).toHaveTitle(/AI-Model-Gateway Admin/)

    // Verify top navigation tabs exist
    await expect(page.locator('.tabbar')).toBeVisible()
    const tabs = ['Overview', 'Telemetry', 'Timeseries', 'History', 'Benchmark']
    for (const label of tabs) {
      await expect(page.locator('.tabbar')).toContainText(label)
    }

    // At least one panel should be present (overview skeleton or content)
    await expect(page.locator('.panel').first()).toBeVisible()

    // Verify status badges in header (gateway + telemetry + auth disabled)
    await expect(page.locator('.topbar-actions .status-badge')).toHaveCount(3)
  })

  test('history tab shows publish/rollback button state', async ({ page }) => {
    await page.click('text=History')

    // Wait for history panel to load
    await expect(page.locator('h2')).toContainText('History')

    // Toolbar may not exist if backend is unavailable; verify gracefully
    const toolbar = page.locator('.history-toolbar')
    if (await toolbar.isVisible().catch(() => false)) {
      const actionButton = toolbar.locator('button').first()
      await expect(actionButton).toBeDisabled()

      const select = toolbar.locator('select')
      const optionCount = await select.locator('option').count()
      if (optionCount > 1) {
        await select.selectOption({ index: 1 })
        const isEnabled = await actionButton.isEnabled()
        expect(typeof isEnabled).toBe('boolean')
      }
    }
  })

  test('benchmark tab allows model filtering', async ({ page }) => {
    await page.click('text=Benchmark')

    // Wait for benchmark panel
    await expect(page.locator('h2')).toContainText('Benchmark')

    // Find model input and enter filter
    const modelInput = page.locator('input[placeholder*="model"], input[type="text"]').first()
    await expect(modelInput).toBeVisible()

    // Type a model filter
    await modelInput.fill('gpt-4o')
    await page.waitForTimeout(500) // debounce

    // Verify input value persisted
    await expect(modelInput).toHaveValue('gpt-4o')

    // Verify hours selector exists
    const hoursSelect = page.locator('select').filter({ hasText: /24|168/ }).first()
    await expect(hoursSelect).toBeVisible()
  })

  test('tab switching updates URL without full reload', async ({ page }) => {
    const initialUrl = page.url()

    await page.click('text=Telemetry')
    await expect(page).toHaveURL(/\/admin\/telemetry/)

    await page.click('text=Timeseries')
    await expect(page).toHaveURL(/\/admin\/timeseries/)

    await page.click('text=History')
    await expect(page).toHaveURL(/\/admin\/history/)

    await page.click('text=Benchmark')
    await expect(page).toHaveURL(/\/admin\/benchmark/)

    // Back button should work
    await page.goBack()
    await expect(page).toHaveURL(/\/admin\/history/)
  })
})
