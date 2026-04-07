import { useEffect, useState } from 'preact/hooks'

const STORAGE_KEY = 'admin-theme'
type Theme = 'light' | 'dark'

function getStoredTheme(): Theme | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'light' || raw === 'dark') return raw
  } catch {}
  return null
}

function setStoredTheme(theme: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {}
}

export function getInitialTheme(): Theme {
  const stored = getStoredTheme()
  if (stored) return stored
  if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    return 'dark'
  }
  return 'light'
}

export function applyTheme(theme: Theme) {
  document.documentElement.setAttribute('data-theme', theme)
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => getInitialTheme())

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  const toggle = () => {
    const next = theme === 'light' ? 'dark' : 'light'
    setTheme(next)
    setStoredTheme(next)
  }

  return (
    <button
      type="button"
      class="icon-btn"
      onClick={toggle}
      aria-label={theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'}
      title={theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'}
    >
      {theme === 'light' ? '\u2600\uFE0F' : '\uD83C\udf19'}
    </button>
  )
}
