import { describe, expect, it } from 'vitest'
import en from './locales/en.json'
import zh from './locales/zh.json'
import ja from './locales/ja.json'
import ko from './locales/ko.json'
import es from './locales/es.json'
import fr from './locales/fr.json'
import de from './locales/de.json'

const locales = { en, zh, ja, ko, es, fr, de }

function collectPlaceholderPaths(value: unknown, path = ''): string[] {
  if (typeof value === 'string') {
    return /\?{3,}/.test(value) ? [path] : []
  }
  if (!value || typeof value !== 'object') return []
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) => (
    collectPlaceholderPaths(child, path ? `${path}.${key}` : key)
  ))
}

describe('locale dictionaries', () => {
  it('does not ship unresolved question-mark placeholders', () => {
    const unresolved = Object.entries(locales).flatMap(([locale, dictionary]) => (
      collectPlaceholderPaths(dictionary).map((key) => `${locale}:${key}`)
    ))

    expect(unresolved).toEqual([])
  })
})
