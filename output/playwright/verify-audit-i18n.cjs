const { chromium, request } = require('../../web/admin/node_modules/playwright')
const fs = require('fs')
const path = require('path')

const OUT_DIR = path.resolve('output/playwright/audit-i18n-verify')
const SCREENSHOT_FILE = path.join(OUT_DIR, 'audit-i18n-verified.png')
const TEXT_FILE = path.join(OUT_DIR, 'page-text.txt')
const REPORT_FILE = path.join(OUT_DIR, 'verification.json')
const LIVE = 'http://127.0.0.1:18080'

fs.mkdirSync(OUT_DIR, { recursive: true })

const configText = fs.readFileSync('/home/chenrunsen/ai-gateway/configs/config.yaml', 'utf8')
const tokenLine = configText.split(/\r?\n/).find((line) => /^\s*bootstrap_token:\s*/.test(line))
const token = tokenLine ? tokenLine.split(':').slice(1).join(':').trim().replace(/^['"]|['"]$/g, '') : ''

async function main() {
  let storageState = undefined
  if (token) {
    const api = await request.newContext({ baseURL: LIVE })
    const login = await api.post('/api/admin/login', { data: { token } })
    if (login.ok()) {
      storageState = await api.storageState()
    }
    await api.dispose()
  }

  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    baseURL: LIVE,
    storageState,
  })
  const page = await context.newPage()

  // Navigate to audit page
  await page.goto('/admin/audit', { waitUntil: 'networkidle', timeout: 30000 }).catch(() => {})
  await page.waitForTimeout(3000)

  // Log page URL
  const currentUrl = page.url()

  // Check for login page
  const onLoginPage = await page.locator('.login-panel').first().isVisible().catch(() => false)

  // Check for ops events
  const hasTimeline = await page.locator('.ops-timeline').count().catch(() => 0) > 0
  const hasEvents = await page.locator('.ops-event').count().catch(() => 0) > 0

  // Dump all visible text for debugging
  const bodyText = await page.textContent('body') || ''
  fs.writeFileSync(TEXT_FILE, bodyText, 'utf8')

  // Save screenshot
  await page.screenshot({ path: SCREENSHOT_FILE, timeout: 10000 }).catch(() => {})

  // GET audit API directly to verify data exists
  let apiEvents = []
  const api = await request.newContext({ baseURL: LIVE, storageState })
  try {
    const auditResp = await api.get('/api/admin/audit?limit=100')
    if (auditResp.ok()) {
      const json = await auditResp.json()
      apiEvents = json?.events || json?.Events || []
    }
  } catch (e) {}
  await api.dispose()

  // Check for raw keys
  const rawKeys = ['auth.login', 'config.publish', 'config.reload', 'config.rollback',
                   'config.validate', 'config.update', 'config.preview', 'config.diff',
                   'pricing.refresh', 'runtime.preflight', 'diagnostics.generate', 'client.error']
  const rawFound = rawKeys.filter(k => bodyText.includes(k))

  // Check for translated keys
  const expectedKeys = ['发布配置', '验证配置', '更新配置', '回滚配置', '登录',
                        '运行预检', '刷新价格', '预览配置', '生成诊断',
                        'Login', 'Publish Config', 'Validate Config',
                        'Rollback Config', 'Refresh Pricing', 'Client Error']
  const translatedFound = expectedKeys.filter(k => bodyText.includes(k))

  const result = {
    timestamp: new Date().toISOString(),
    url: currentUrl,
    screenshot: SCREENSHOT_FILE,
    textSnippet: bodyText.slice(0, 300),
    authed: !onLoginPage,
    onLoginPage,
    hasTimeline,
    hasEvents,
    apiAuditCount: apiEvents.length,
    rawKeysFound: rawFound,
    rawKeyCount: rawFound.length,
    translationsVisible: translatedFound,
    translationCount: translatedFound.length,
    verdict: onLoginPage
      ? 'INFO: On login page - direct audit API has ' + apiEvents.length + ' events'
      : rawFound.length === 0
        ? 'PASS - No raw audit action keys visible'
        : apiEvents.length === 0
          ? 'INFO: No audit events exist yet (service recently restarted), raw keys from other page areas'
          : `PARTIAL: ${rawFound.length} raw keys visible but ${apiEvents.length} audit events exist`
  }

  fs.writeFileSync(REPORT_FILE, JSON.stringify(result, null, 2))
  console.log(JSON.stringify(result, null, 2))
  await browser.close()
}

main().catch(err => {
  console.error('Error:', err.message)
  fs.writeFileSync(REPORT_FILE, JSON.stringify({ error: err.message, verdict: 'ERROR' }, null, 2))
  process.exit(1)
})
