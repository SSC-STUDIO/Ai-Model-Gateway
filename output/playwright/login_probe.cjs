const { request } = require('../../web/admin/node_modules/playwright')
const fs = require('fs')

const configText = fs.readFileSync('/home/chenrunsen/ai-gateway/configs/config.yaml', 'utf8')
const tokenLine = configText.split(/\r?\n/).find((line) => /^\s*bootstrap_token:\s*/.test(line))
const token = tokenLine ? tokenLine.split(':').slice(1).join(':').trim().replace(/^['"]|['"]$/g, '') : ''

async function main() {
  const api = await request.newContext({ baseURL: 'http://127.0.0.1:5173' })
  const sessionBefore = await api.get('/api/admin/session')
  const login = await api.post('/api/admin/login', { data: { token } })
  const sessionAfter = await api.get('/api/admin/session')
  console.log(JSON.stringify({
    sessionBeforeStatus: sessionBefore.status(),
    sessionBefore: await sessionBefore.json().catch(() => null),
    loginStatus: login.status(),
    loginBody: await login.json().catch(() => null),
    setCookie: Boolean(login.headers()['set-cookie']),
    sessionAfterStatus: sessionAfter.status(),
    sessionAfter: await sessionAfter.json().catch(() => null),
  }, null, 2))
  await api.dispose()
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
