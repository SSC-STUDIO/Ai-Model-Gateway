import { defineConfig, devices } from '@playwright/test'

const previewPort = Number(process.env.ADMIN_PREVIEW_PORT || 4174)
const previewURL = process.env.ADMIN_BASE_URL || `http://127.0.0.1:${previewPort}`

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'list',
  use: {
    baseURL: previewURL,
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: `npx vite preview --host 127.0.0.1 --port ${previewPort} --outDir dist`,
    url: previewURL,
    reuseExistingServer: false,
    timeout: 120 * 1000,
  },
})
