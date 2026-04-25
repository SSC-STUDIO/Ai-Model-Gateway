import { useEffect, useState } from 'preact/hooks'
import { useI18n } from '../i18n'
import { Icon } from '../components/Icon'

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
  const { t } = useI18n()
  const [theme, setTheme] = useState<Theme>(() => getInitialTheme())

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = (e: MediaQueryListEvent) => {
      if (!getStoredTheme()) {
        setTheme(e.matches ? 'dark' : 'light')
      }
    }
    media.addEventListener('change', handler)
    return () => media.removeEventListener('change', handler)
  }, [])

  const toggle = () => {
    const next = theme === 'light' ? 'dark' : 'light'
    setTheme(next)
    setStoredTheme(next)
  }

  return (
    <button
      type="button"
      class="icon-btn theme-toggle-btn"
      onClick={toggle}
      aria-pressed={theme === 'dark'}
      aria-label={t('theme.toggleLabel')}
      title={t('theme.toggleLabel')}
    >
      <Icon name={theme === 'light' ? 'sun' : 'moon'} size={17} />
    </button>
  )
}
