// Playwright UI Audit Script for AI-Model-Gateway Admin (CJS)
// Usage: node output/playwright/ui_audit.cjs

const { chromium } = require('playwright')
const fs = require('fs')
const path = require('path')

const ADMIN_BASE = process.env.AIGW_ADMIN_BASE_URL || 'http://127.0.0.1:18080'
const ADMIN_TOKEN = process.env.AIGW_ADMIN_TOKEN || 'ec6a94485ddd476b96cdc3d5a9a9fe14'
const SCREENSHOT_DIR = 'output/playwright/screenshots'
const REPORT_FILE = 'output/playwright/ui-audit-report.json'

const VIEWS = [
  { path: '/admin', label: 'overview' },
  { path: '/admin/monitoring', label: 'monitoring' },
  { path: '/admin/telemetry', label: 'telemetry' },
  { path: '/admin/pricing', label: 'pricing' },
  { path: '/admin/benchmark', label: 'benchmark' },
  { path: '/admin/ops', label: 'ops' },
  { path: '/admin/audit', label: 'audit' },
  { path: '/admin/probe', label: 'probe' },
  { path: '/admin/diagnostics', label: 'diagnostics' },
  { path: '/admin/config', label: 'config' },
  { path: '/admin/logs', label: 'logs' },
]

async function sleep(ms) {
  return new Promise(r => setTimeout(r, ms))
}

async function run() {
  fs.mkdirSync(SCREENSHOT_DIR, { recursive: true })

  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    locale: 'zh-CN',
  })
  const page = await context.newPage()
  const errors = []
  let consoleErrors = 0
  let pageErrors = 0

  page.on('console', msg => {
    if (msg.type() === 'error') {
      consoleErrors++
      errors.push({ type: 'console.error', text: msg.text(), url: page.url() })
    }
  })
  page.on('pageerror', err => {
    pageErrors++
    errors.push({ type: 'page.error', text: err.message, url: page.url() })
  })

  // Login
  console.log(`[1] Logging in to ${ADMIN_BASE}/admin/login ...`)
  await page.goto(`${ADMIN_BASE}/admin/login`, { waitUntil: 'networkidle', timeout: 30000 })
  // Try fill password or token field
  const pwField = page.locator('input[type="password"]')
  if (await pwField.isVisible()) {
    await pwField.fill(ADMIN_TOKEN)
  } else {
    await page.fill('input[placeholder*="token" i]', ADMIN_TOKEN)
  }
  await page.click('button[type="submit"]')
  await page.waitForURL('**/admin', { timeout: 15000 }).catch(() => {})
  await sleep(2000)
  console.log('  Current URL:', page.url())

  const results = []

  for (const view of VIEWS) {
    const url = `${ADMIN_BASE}${view.path}`
    console.log(`\n[${VIEWS.indexOf(view) + 2}/${VIEWS.length + 1}] ${view.label}`)

    try {
      await page.goto(url, { waitUntil: 'networkidle', timeout: 30000 })
      await sleep(2000)
    } catch (err) {
      console.log(`  WARN: timeout, continuing...`)
    }

    const screenshotPath = path.join(SCREENSHOT_DIR, `${view.label}.png`)
    try {
      await page.screenshot({ path: screenshotPath, timeout: 30000 })
    } catch (err) {
      console.log(`  ERROR: screenshot: ${err.message}`)
      errors.push({ type: 'screenshot.error', text: err.message, url })
    }

    let visibleErrors = 0
    try {
      visibleErrors = await page.locator('.error, [class*="error"], .status-badge.error, .toast-error').count()
    } catch (_) {}

    console.log(`  Console errors: ${consoleErrors} | Page errors: ${pageErrors} | Visible errors: ${visibleErrors}`)
    results.push({ view: view.label, url, consoleErrors, pageErrors, visibleErrors })
  }

  console.log('\n=== Audit Summary ===')
  console.log(`Views: ${VIEWS.length}`)
  console.log(`Console errors: ${consoleErrors}`)
  console.log(`Page errors: ${pageErrors}`)
  console.log(`Screenshots: ${results.length}`)

  const report = { timestamp: new Date().toISOString(), summary: { views: VIEWS.length, consoleErrors, pageErrors }, errors, results }
  fs.writeFileSync(REPORT_FILE, JSON.stringify(report, null, 2))
  console.log(`Report: ${REPORT_FILE}`)

  await browser.close()
}

run().catch(err => { console.error('Fatal:', err); process.exit(1) })
