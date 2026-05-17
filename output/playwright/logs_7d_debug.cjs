const { chromium, request } = require('../../web/admin/node_modules/playwright')
const fs = require('fs')

function readToken() {
  const configText = fs.readFileSync('/home/chenrunsen/ai-gateway/configs/config.yaml', 'utf8')
  const tokenLine = configText.split(/\r?\n/).find((line) => /^\s*bootstrap_token:\s*/.test(line))
  return tokenLine ? tokenLine.split(':').slice(1).join(':').trim().replace(/^['"]|['"]$/g, '') : ''
}

async function main() {
  const baseURL = 'http://127.0.0.1:5173'
  const api = await request.newContext({ baseURL })
  await api.post('/api/admin/login', { data: { token: readToken() } })
  const storageState = await api.storageState()
  await api.dispose()
  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ baseURL, storageState, viewport: { width: 1440, height: 1000 } })
  const page = await context.newPage()
  page.on('request', (req) => {
    if (req.url().includes('/api/admin/telemetry')) console.log('REQ', req.url())
  })
  page.on('response', (res) => {
    if (res.url().includes('/api/admin/telemetry')) console.log('RES', res.status(), res.url())
  })
  await page.goto('/admin/logs', { waitUntil: 'domcontentloaded' })
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(700)
  console.log('before', await page.evaluate(() => document.querySelector('.logs-paginator')?.innerText || ''))
  const responsePromise = page.waitForResponse((res) => res.url().includes('/api/admin/telemetry?hours=168') && res.status() === 200, { timeout: 60000 })
  await page.getByRole('button', { name: '7d' }).first().click()
  await responsePromise
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1500)
  console.log('after', await page.evaluate(() => ({
    text: document.querySelector('.logs-paginator')?.innerText || '',
    loadMore: document.querySelector('.logs-load-more')?.innerText || '',
    buttons: Array.from(document.querySelectorAll('button')).map((b) => ({ text: b.innerText, cls: b.className, disabled: b.disabled })).filter((x) => /more|更多|No more|没有/.test(x.text)),
    rows: document.querySelectorAll('tr.logs-row').length,
  })))
  await browser.close()
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
