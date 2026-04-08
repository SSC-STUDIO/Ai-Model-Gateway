import { render } from 'preact'
import { App } from './app'
import { I18nProvider } from './i18n'
import { applyTheme, getInitialTheme } from './theme/ThemeToggle'
import './theme/index.css'

applyTheme(getInitialTheme())

const root = document.getElementById('app')!
// Clear any existing content before rendering to prevent duplicate DOM nodes
root.innerHTML = ''

render(
  <I18nProvider>
    <App />
  </I18nProvider>,
  root
)
