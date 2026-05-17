const { chromium, request } = require('../../web/admin/node_modules/playwright')
const fs = require('fs')
const path = require('path')

function readToken() {
  const configText = fs.readFileSync('/home/chenrunsen/ai-gateway/configs/config.yaml', 'utf8')
  const tokenLine = configText.split(/\r?\n/).find((line) => /^\s*bootstrap_token:\s*/.test(line))
  return tokenLine ? tokenLine.split(':').slice(1).join(':').trim().replace(/^['"]|['"]$/g, '') : ''
}

async function login(baseURL) {
  const api = await request.newContext({ baseURL })
  const token = readToken()
  if (token) await api.post('/api/admin/login', { data: { token } })
  const storageState = await api.storageState()
  await api.dispose()
  return storageState
}

async function main() {
  const baseURL = process.env.AIGW_ADMIN_BASE_URL || 'http://127.0.0.1:18080'
  const storageState = await login(baseURL)
  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({
    baseURL,
    storageState,
    viewport: { width: 1440, height: 1000 },
    locale: 'zh-CN',
  })
  const page = await context.newPage()
  await page.addInitScript(() => localStorage.setItem('admin-locale', 'zh'))
  const consoleMessages = []
  page.on('console', (msg) => {
    if (['error', 'warning'].includes(msg.type())) {
      consoleMessages.push(msg.type() + ': ' + msg.text())
    }
  })
  page.on('pageerror', (err) => {
    throw err
  })

  await page.goto('/admin/benchmark', { waitUntil: 'domcontentloaded' })
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
  await page.getByRole('tab', { name: /模型能力|Capability/ }).first().click()
  await page.waitForTimeout(1200)

  const bodyText = await page.locator('body').innerText()
  const missingMessages = []
  for (const forbidden of [
    'benchmark.verification.quickRun',
    'benchmark.verification.quickRunHint',
    'benchmark.verification.polling',
    'benchmark.verification.readOnlyNotice',
  ]) {
    if (bodyText.includes(forbidden)) {
      throw new Error('raw i18n key visible: ' + forbidden)
    }
  }
  for (const expected of [
    '测试全部上游模型',
    '自动用当前套件测试每个已启用的上游 Provider/模型路由',
    '全部已启用上游模型',
  ]) {
    if (!bodyText.includes(expected)) {
      missingMessages.push(expected)
    }
  }
  if (missingMessages.length > 0) {
    const outDir = path.resolve('output/playwright')
    fs.mkdirSync(outDir, { recursive: true })
    fs.writeFileSync(path.join(outDir, 'benchmark-capability-body.txt'), bodyText)
    throw new Error('missing expected Chinese text: ' + missingMessages.join(', '))
  }

  const checked = await page.locator('.verification-field-check input').first().isChecked()
  if (!checked) throw new Error('all enabled upstream models checkbox is not checked by default')
  const providerDisabled = await page.locator("input[placeholder='provider-id']").first().isDisabled()
  const modelDisabled = await page.locator("input[placeholder='gpt-4o']").first().isDisabled()
  if (!providerDisabled || !modelDisabled) {
    throw new Error('provider/model fields should be disabled while all upstream models is selected')
  }
  const quickButton = page.getByRole('button', { name: /测试全部上游模型/ }).first()
  if (!(await quickButton.isVisible())) throw new Error('quick run button is not visible')

  let chartCount = await page.locator('.chart-container').count()
  if (chartCount < 1) {
    await page.goto('/admin/monitoring', { waitUntil: 'domcontentloaded' })
    await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
    await page.waitForTimeout(1200)
    chartCount = await page.locator('.chart-container').count()
  }
  if (chartCount < 1) throw new Error('no chart containers rendered on benchmark or monitoring pages')
  const chartBackground = await page.locator('.chart-container').first().evaluate((el) => getComputedStyle(el).backgroundImage)
  if (/radial-gradient/.test(chartBackground)) {
    throw new Error('chart container still uses radial background: ' + chartBackground)
  }

  const outDir = path.resolve('output/playwright')
  fs.mkdirSync(outDir, { recursive: true })
  await page.screenshot({ path: path.join(outDir, 'benchmark-capability-quickrun-charts.png'), fullPage: true })
  await browser.close()
  console.log(JSON.stringify({ ok: true, chartCount, chartBackground, consoleMessages: consoleMessages.slice(0, 5) }, null, 2))
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
