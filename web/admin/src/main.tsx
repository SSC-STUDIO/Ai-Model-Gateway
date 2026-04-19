import { render } from 'preact'
import { App } from './app'
import { I18nProvider } from './i18n'
import { applyTheme, getInitialTheme } from './theme/ThemeToggle'
import './theme/index.css'

applyTheme(getInitialTheme())

const root = document.getElementById('app')
if (!root) {
  console.error('[AI Gateway] Root element #app not found')
  throw new Error('Root element #app not found')
}

// Prevent duplicate app initialization - multiple guards
const APP_INIT_KEY = '__ai_gateway_admin_init_v2__'

// Check if already initialized
if ((window as any)[APP_INIT_KEY]) {
  console.log('[AI Gateway] App already initialized, skipping duplicate render')
  // Try to clean up any duplicate DOM elements
  const existingMains = root.querySelectorAll('main.app-shell')
  if (existingMains.length > 1) {
    console.log(`[AI Gateway] Found ${existingMains.length} main elements, cleaning up...`)
    // Keep only the first one
    for (let i = 1; i < existingMains.length; i++) {
      existingMains[i].remove()
    }
  }
} else {
  // Mark as initialized immediately
  ;(window as any)[APP_INIT_KEY] = true

  // Clear any existing content before rendering
  while (root.firstChild) {
    root.removeChild(root.firstChild)
  }

  // Render the app
  render(
    <I18nProvider>
      <App />
    </I18nProvider>,
    root
  )

  console.log('[AI Gateway] App rendered successfully')
}
