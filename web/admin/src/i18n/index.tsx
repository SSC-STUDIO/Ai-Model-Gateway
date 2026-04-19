import { createContext } from 'preact'
import { useContext, useState, useEffect, useMemo } from 'preact/hooks'
import en from './locales/en.json'
import zh from './locales/zh.json'
import ja from './locales/ja.json'
import ko from './locales/ko.json'
import es from './locales/es.json'
import fr from './locales/fr.json'
import de from './locales/de.json'

export type LocaleKey = 'en' | 'zh' | 'ja' | 'ko' | 'es' | 'fr' | 'de'

const locales: Record<LocaleKey, Record<string, unknown>> = {
  en: en as Record<string, unknown>,
  zh: zh as Record<string, unknown>,
  ja: ja as Record<string, unknown>,
  ko: ko as Record<string, unknown>,
  es: es as Record<string, unknown>,
  fr: fr as Record<string, unknown>,
  de: de as Record<string, unknown>,
}

const STORAGE_KEY = 'admin-locale'

function getBrowserLocale(): LocaleKey {
  const lang = navigator.language || (navigator as unknown as { browserLanguage?: string }).browserLanguage || 'en'
  if (lang.startsWith('zh')) return 'zh'
  if (lang.startsWith('ja')) return 'ja'
  if (lang.startsWith('ko')) return 'ko'
  if (lang.startsWith('es')) return 'es'
  if (lang.startsWith('fr')) return 'fr'
  if (lang.startsWith('de')) return 'de'
  return 'en'
}

function getStoredLocale(): LocaleKey | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'en' || raw === 'zh' || raw === 'ja' || raw === 'ko' || raw === 'es' || raw === 'fr' || raw === 'de') return raw
  } catch {}
  return null
}

function setStoredLocale(locale: LocaleKey) {
  try {
    localStorage.setItem(STORAGE_KEY, locale)
  } catch {}
}

export function getInitialLocale(): LocaleKey {
  return getStoredLocale() || getBrowserLocale()
}

type I18nContextValue = {
  locale: LocaleKey
  t: (key: string) => string
  setLocale: (locale: LocaleKey) => void
}

const I18nContext = createContext<I18nContextValue | undefined>(undefined)

function getString(obj: unknown, path: string): string {
  const parts = path.split('.')
  let current: unknown = obj
  for (const part of parts) {
    if (current && typeof current === 'object') {
      current = (current as Record<string, unknown>)[part]
    } else {
      return path
    }
  }
  return typeof current === 'string' ? current : path
}

export function I18nProvider({ children }: { children: preact.ComponentChildren }) {
  const [locale, setLocaleState] = useState<LocaleKey>(() => getInitialLocale())

  const setLocale = (next: LocaleKey) => {
    setLocaleState(next)
    setStoredLocale(next)
  }

  useEffect(() => {
    const langMap: Record<LocaleKey, string> = {
      en: 'en',
      zh: 'zh-CN',
      ja: 'ja-JP',
      ko: 'ko-KR',
      es: 'es-ES',
      fr: 'fr-FR',
      de: 'de-DE',
    }
    document.documentElement.lang = langMap[locale]
  }, [locale])

  const t = useMemo(() => {
    return (key: string) => {
      const localized = getString(locales[locale], key)
      if (localized !== key) {
        return localized
      }
      return getString(locales.en, key)
    }
  }, [locale])

  const value = useMemo(() => ({ locale, t, setLocale }), [locale, t])

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext)
  if (!ctx) {
    throw new Error('useI18n must be used within I18nProvider')
  }
  return ctx
}
