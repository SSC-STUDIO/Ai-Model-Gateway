const { chromium, request } = require('../../web/admin/node_modules/playwright')
const fs = require('fs')

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
  const baseURL = process.env.AIGW_ADMIN_BASE_URL || 'http://127.0.0.1:5173'
  const storageState = await login(baseURL)
  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ baseURL, storageState, viewport: { width: 1440, height: 1000 } })
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: baseURL })
  const page = await context.newPage()
  const telemetryRequests = []
  page.on('request', (req) => {
    if (req.url().includes('/api/admin/telemetry')) telemetryRequests.push(req.url())
  })
  page.on('pageerror', (err) => {
    throw err
  })

  await page.goto('/admin/logs', { waitUntil: 'domcontentloaded' })
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(700)

  const sevenDayResponse = page.waitForResponse((res) => res.url().includes('/api/admin/telemetry?hours=168') && res.status() === 200, { timeout: 60000 })
  const sevenDays = page.getByRole('button', { name: '7d' }).first()
  await sevenDays.click()
  await sevenDayResponse
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(700)

  const loadMore = page.getByRole('button', { name: /Load more|加载更多/ }).first()
  if (!(await loadMore.isVisible().catch(() => false))) {
    throw new Error('load more button is not visible for 7d logs')
  }
  const loadMoreResponse = page.waitForResponse((res) => res.url().includes('/api/admin/telemetry?hours=168') && res.url().includes('offset=500') && res.status() === 200, { timeout: 60000 })
  await loadMore.click()
  await loadMoreResponse
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(700)
  if (!telemetryRequests.some((url) => url.includes('offset=500'))) {
    throw new Error(`load more did not request offset=500: ${telemetryRequests.join('\n')}`)
  }

  const search = page.locator('.logs-search input').first()
  await search.fill('504')
  await page.waitForTimeout(500)

  const exportButton = page.getByRole('button', { name: /Export CSV|导出 CSV/ }).first()
  if (!(await exportButton.isVisible().catch(() => false))) {
    throw new Error('CSV export button is not visible')
  }
  const download = await Promise.all([
    page.waitForEvent('download', { timeout: 15000 }),
    exportButton.click(),
  ]).then(([value]) => value)
  const suggestedFilename = download.suggestedFilename()
  if (!/^ai-gateway-logs-.*\.csv$/.test(suggestedFilename)) {
    throw new Error(`unexpected CSV filename: ${suggestedFilename}`)
  }
  const csvPath = await download.path()
  const csvText = csvPath ? fs.readFileSync(csvPath, 'utf8') : ''
  if (!csvText.includes('time,path,model,upstream,status,latency_ms') || !csvText.includes('HTTP 504')) {
    throw new Error(`CSV export missing expected content: ${csvText.slice(0, 240)}`)
  }

  const detailButton = page.locator('.logs-detail-toggle').first()
  if (!(await detailButton.isVisible().catch(() => false))) {
    throw new Error('detail icon button is not visible for 504 log row')
  }
  await detailButton.click()
  await page.waitForTimeout(500)
  const detailText = await page.locator('tr.logs-detail-row').first().innerText()
  if (!detailText.includes('HTTP 504') || !/context canceled|Post/.test(detailText)) {
    throw new Error(`504 detail did not render expected content: ${detailText}`)
  }
  const statusText = await page.locator('tr.logs-row').first().innerText()
  if (!statusText.includes('HTTP 504')) {
    throw new Error(`row status did not include HTTP 504: ${statusText}`)
  }

  const copyButton = page.getByRole('button', { name: /Copy details|复制详情/ }).first()
  if (!(await copyButton.isVisible().catch(() => false))) {
    throw new Error('copy details button is not visible in expanded log detail')
  }
  await copyButton.click()
  await page.waitForTimeout(300)
  const copiedVisible = await page.getByRole('button', { name: /Copied|已复制/ }).first().isVisible().catch(() => false)
  if (!copiedVisible) {
    throw new Error('copy details action did not show copied state')
  }

  await page.screenshot({ path: 'output/playwright/logs-interaction-fixed.png', fullPage: true })
  await browser.close()
  console.log(JSON.stringify({ ok: true, telemetryRequests: telemetryRequests.filter((url) => url.includes('offset=')) }, null, 2))
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
