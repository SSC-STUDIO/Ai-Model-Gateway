const { chromium, request } = require('../../web/admin/node_modules/playwright')
const fs = require('fs')

function readToken() {
  const configText = fs.readFileSync('/home/chenrunsen/ai-gateway/configs/config.yaml', 'utf8')
  const tokenLine = configText.split(/\r?\n/).find((line) => /^\s*bootstrap_token:\s*/.test(line))
  return tokenLine ? tokenLine.split(':').slice(1).join(':').trim().replace(/^['"]|['"]$/g, '') : ''
}

async function login(baseURL) {
  const token = readToken()
  const api = await request.newContext({ baseURL })
  if (token) {
    await api.post('/api/admin/login', { data: { token } })
  }
  const storageState = await api.storageState()
  await api.dispose()
  return storageState
}

async function main() {
  const baseURL = process.env.AIGW_ADMIN_BASE_URL || 'http://127.0.0.1:5173'
  const storageState = await login(baseURL)
  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({
    baseURL,
    storageState,
    viewport: { width: 1440, height: 1000 },
  })
  const page = await context.newPage()
  const events = []
  page.on('console', (msg) => events.push({ type: 'console', level: msg.type(), text: msg.text() }))
  page.on('request', (req) => {
    if (req.url().includes('/api/admin/telemetry')) events.push({ type: 'request', url: req.url() })
  })
  page.on('response', (res) => {
    if (res.url().includes('/api/admin/telemetry')) events.push({ type: 'response', status: res.status(), url: res.url() })
  })

  await page.goto('/admin/logs', { waitUntil: 'domcontentloaded' })
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1000)

  const before = await page.evaluate(() => {
    const rows = Array.from(document.querySelectorAll('tr.logs-row'))
    return {
      rowCount: rows.length,
      detailRows: document.querySelectorAll('tr.logs-detail-row').length,
      status504: rows.findIndex((row) => row.textContent?.includes('504')),
      buttons: Array.from(document.querySelectorAll('button'))
        .map((button, index) => ({
          index,
          text: button.innerText,
          aria: button.getAttribute('aria-label'),
          className: button.className,
          disabled: button.disabled,
        }))
        .filter((item) => /更多|more|‹|›|detail|详情|expand|展开|Errors|错误|Requests|请求|All|全部/.test(`${item.text} ${item.aria} ${item.className}`)),
      firstRows: rows.slice(0, 8).map((row, index) => ({
        index,
        text: (row.textContent || '').replace(/\s+/g, ' ').slice(0, 360),
        className: row.className,
        role: row.getAttribute('role'),
        aria: row.getAttribute('aria-label'),
      })),
    }
  })
  console.log('BEFORE', JSON.stringify(before, null, 2))

  const errorsButton = page.getByRole('button', { name: /Errors|错误/ }).first()
  if (await errorsButton.isVisible().catch(() => false)) {
    await errorsButton.click()
  }
  await page.waitForTimeout(500)

  const search = page.locator('.logs-search input').first()
  if (await search.isVisible().catch(() => false)) {
    await search.fill('504')
  }
  await page.waitForTimeout(500)

  const afterFilter = await page.evaluate(() => {
    const rows = Array.from(document.querySelectorAll('tr.logs-row'))
    return {
      rowCount: rows.length,
      detailRows: document.querySelectorAll('tr.logs-detail-row').length,
      rows: rows.slice(0, 5).map((row, index) => ({
        index,
        text: (row.textContent || '').replace(/\s+/g, ' ').slice(0, 700),
        className: row.className,
      })),
      buttons: Array.from(document.querySelectorAll('button'))
        .map((button, index) => ({
          index,
          text: button.innerText,
          aria: button.getAttribute('aria-label'),
          className: button.className,
          disabled: button.disabled,
        }))
        .filter((item) => /更多|more|‹|›|detail|详情|expand|展开/.test(`${item.text} ${item.aria} ${item.className}`)),
    }
  })
  console.log('AFTER_FILTER', JSON.stringify(afterFilter, null, 2))

  const firstRow = page.locator('tr.logs-row').first()
  if (await firstRow.isVisible().catch(() => false)) {
    await firstRow.click()
  }
  await page.waitForTimeout(500)

  const afterClick = await page.evaluate(() => ({
    detailRows: document.querySelectorAll('tr.logs-detail-row').length,
    detailText: (document.querySelector('tr.logs-detail-row')?.textContent || '').replace(/\s+/g, ' ').slice(0, 1500),
    firstRowClass: document.querySelector('tr.logs-row')?.className || '',
  }))
  console.log('AFTER_CLICK', JSON.stringify(afterClick, null, 2))
  console.log('EVENTS', JSON.stringify(events, null, 2))

  await page.screenshot({ path: 'output/playwright/logs-interaction-debug.png', fullPage: true })
  await browser.close()
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
