const { chromium, request } = require('../../web/admin/node_modules/playwright')
const fs = require('fs')
const path = require('path')

const outDir = path.resolve('output/playwright/ui-review-20260508')
fs.mkdirSync(outDir, { recursive: true })

const configText = fs.readFileSync('/home/chenrunsen/ai-gateway/configs/config.yaml', 'utf8')
const tokenLine = configText.split(/\r?\n/).find((line) => /^\s*bootstrap_token:\s*/.test(line))
const token = tokenLine ? tokenLine.split(':').slice(1).join(':').trim().replace(/^['"]|['"]$/g, '') : ''

const viewports = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
]

const routes = [
  { name: 'overview', url: '/admin/' },
  { name: 'monitoring', url: '/admin/monitoring', subClicks: ['Pricing', '价格', 'Telemetry', '遥测'] },
  { name: 'telemetry-route', url: '/admin/telemetry' },
  { name: 'pricing-route', url: '/admin/pricing' },
  { name: 'benchmark', url: '/admin/benchmark', subClicks: ['Model capability', '模型能力', 'Model upstream', '模型上游'] },
  { name: 'ops', url: '/admin/ops', subClicks: ['Probe', '探测', 'Audit', '审计', 'Diagnostics', '诊断', 'Runtime', '运行'] },
  { name: 'audit-route', url: '/admin/audit' },
  { name: 'probe-route', url: '/admin/probe' },
  { name: 'diagnostics-route', url: '/admin/diagnostics' },
  { name: 'config', url: '/admin/config', subClicks: ['JSON', '图形化', 'Visual', '历史', 'History', '当前', 'Current'] },
  { name: 'logs', url: '/admin/logs', subClicks: ['Requests', '请求', 'Errors', '错误', 'All', '全部'] },
]

function sanitizeFileName(value) {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9\u4e00-\u9fa5]+/g, '-')
    .replace(/^-|-$/g, '')
    .slice(0, 80) || 'view'
}

function redact(value) {
  return String(value).replace(/(sk-|tp-|ec)[A-Za-z0-9_:-]+/g, '[REDACTED]')
}

async function maskSensitive(page) {
  await page.addStyleTag({
    content: `
      textarea, pre, code, .config-json-editor, .yaml-preview, .monaco-editor, .cm-editor {
        color: transparent !important;
        text-shadow: 0 0 0 #64748b !important;
      }
      textarea::selection {
        color: transparent !important;
        background: #cbd5e1 !important;
      }
      [data-sensitive], .secret, .token, .api-key {
        color: transparent !important;
        text-shadow: 0 0 0 #64748b !important;
      }
    `,
  }).catch(() => {})

  await page.evaluate(() => {
    for (const el of Array.from(document.querySelectorAll('textarea'))) {
      if (el.value && el.value.length > 160) {
        el.value = '[redacted for UI review screenshot]\n'.repeat(8)
      }
    }
  }).catch(() => {})
}

async function collectIssues(page, label) {
  return page.evaluate((label) => {
    const issues = []
    const vw = document.documentElement.clientWidth
    const bodyText = document.body.innerText || ''
    const missing = Array.from(new Set((bodyText.match(/\b[a-z]+(?:\.[a-zA-Z0-9_-]+){1,}\b/g) || [])
      .filter((x) => /^(tabs|overview|telemetry|benchmark|config|ops|logs|pricing|auth|common|services|empty|history)\./.test(x))))
    if (missing.length) issues.push({ type: 'missing-i18n', label, values: missing.slice(0, 20) })

    const doc = document.documentElement
    if (doc.scrollWidth > vw + 2) {
      issues.push({ type: 'page-horizontal-overflow', label, scrollWidth: doc.scrollWidth, viewport: vw })
    }

    const selectors = [
      '.topbar',
      '.tabbar',
      '.workspace-nav',
      '.panel',
      '.table-wrap',
      'table',
      '.config-card',
      '.ops-workspace-switch',
      '.benchmark-verification',
      '.chart-container',
    ]
    for (const selector of selectors) {
      for (const el of Array.from(document.querySelectorAll(selector)).slice(0, 80)) {
        const rect = el.getBoundingClientRect()
        if (!rect.width || !rect.height) continue
        const style = getComputedStyle(el)
        if (rect.right > vw + 2 && style.position !== 'fixed') {
          issues.push({
            type: 'element-overflow-right',
            label,
            selector,
            text: (el.textContent || '').trim().slice(0, 80),
            right: Math.round(rect.right),
            viewport: vw,
          })
          break
        }
        if (rect.left < -2 && style.position !== 'fixed') {
          issues.push({ type: 'element-overflow-left', label, selector, left: Math.round(rect.left) })
          break
        }
      }
    }

    const controls = Array.from(document.querySelectorAll('button, [role=button], a, input, select, textarea'))
    for (const el of controls) {
      const rect = el.getBoundingClientRect()
      if (rect.width === 0 || rect.height === 0) continue
      const text = (el.textContent || el.getAttribute('aria-label') || el.getAttribute('title') || '').trim()
      if (rect.width < 24 || rect.height < 24) {
        issues.push({
          type: 'tiny-control',
          label,
          tag: el.tagName,
          text: text.slice(0, 60),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
        })
      }
    }

    const errors = Array.from(document.querySelectorAll('.error, [role=alert]'))
      .map((el) => (el.textContent || '').trim())
      .filter(Boolean)
    if (errors.length) issues.push({ type: 'visible-error', label, values: errors.slice(0, 10) })

    const blankPanels = Array.from(document.querySelectorAll('.panel, .config-card, .workspace-band')).filter((el) => {
      const rect = el.getBoundingClientRect()
      const text = (el.textContent || '').trim()
      return rect.height > 80 && text.length === 0
    })
    if (blankPanels.length) issues.push({ type: 'blank-panels', label, count: blankPanels.length })

    return issues
  }, label)
}

async function clickSubView(page, label) {
  const escaped = label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const locator = page.getByRole('button', { name: new RegExp(escaped, 'i') }).first()
  if (!(await locator.isVisible().catch(() => false))) return false
  await locator.click().catch(() => {})
  await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {})
  await page.waitForTimeout(700)
  return true
}

async function main() {
  const browser = await chromium.launch({ headless: true })
  const report = { startedAt: new Date().toISOString(), console: [], pageErrors: [], results: [] }
  let storageState = undefined
  if (token) {
    const api = await request.newContext({ baseURL: 'http://127.0.0.1:5173' })
    const login = await api.post('/api/admin/login', { data: { token } })
    if (login.ok()) {
      storageState = await api.storageState()
    } else {
      report.console.push({ viewport: 'setup', type: 'warning', text: `login failed: ${login.status()}` })
    }
    await api.dispose()
  }

  for (const viewport of viewports) {
    const context = await browser.newContext({
      viewport: { width: viewport.width, height: viewport.height },
      baseURL: 'http://127.0.0.1:5173',
      storageState,
    })
    const page = await context.newPage()

    page.on('console', (msg) => {
      if (['error', 'warning'].includes(msg.type())) {
        report.console.push({ viewport: viewport.name, type: msg.type(), text: redact(msg.text()).slice(0, 600) })
      }
    })
    page.on('pageerror', (err) => {
      report.pageErrors.push({ viewport: viewport.name, text: redact(err.message || err).slice(0, 600) })
    })

    await page.goto('/admin/login', { waitUntil: 'domcontentloaded' })
    await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
    await page.waitForTimeout(700)
    if (await page.locator('.login-panel').first().isVisible().catch(() => false)) {
      await maskSensitive(page)
      await page.screenshot({ path: path.join(outDir, `${viewport.name}-login.png`), fullPage: true })
      report.results.push({ viewport: viewport.name, route: 'login', issues: await collectIssues(page, `${viewport.name}:login`) })
    }

    for (const route of routes) {
      await page.goto(route.url, { waitUntil: 'domcontentloaded' })
      await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
      await page.waitForTimeout(1200)
      await maskSensitive(page)
      const baseLabel = `${viewport.name}:${route.name}`
      await page.screenshot({ path: path.join(outDir, `${viewport.name}-${route.name}.png`), fullPage: true })
      report.results.push({ viewport: viewport.name, route: route.name, url: route.url, issues: await collectIssues(page, baseLabel) })

      for (const subClick of route.subClicks || []) {
        if (!(await clickSubView(page, subClick))) continue
        await maskSensitive(page)
        const sub = sanitizeFileName(subClick)
        const label = `${baseLabel}:${subClick}`
        await page.screenshot({ path: path.join(outDir, `${viewport.name}-${route.name}-${sub}.png`), fullPage: true })
        report.results.push({ viewport: viewport.name, route: `${route.name}:${subClick}`, issues: await collectIssues(page, label) })
      }
    }

    await context.close()
  }

  await browser.close()
  fs.writeFileSync(path.join(outDir, 'report.json'), JSON.stringify(report, null, 2))

  const allIssues = report.results.flatMap((result) => result.issues)
  const issueCounts = allIssues.reduce((acc, issue) => {
    acc[issue.type] = (acc[issue.type] || 0) + 1
    return acc
  }, {})

  console.log(JSON.stringify({
    outDir,
    resultCount: report.results.length,
    issueCounts,
    consoleCount: report.console.length,
    pageErrorCount: report.pageErrors.length,
  }, null, 2))
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
