import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  formatAbsoluteTime,
  formatInteger,
  formatRelativeTime,
  formatStatusTime,
  formatUsd,
  getChecked,
  getInputValue,
  getSelectValue,
  pretty,
  versionIdOf,
} from './formatting'

describe('formatting utilities', () => {
  describe('pretty', () => {
    it('serializes values as two-space JSON', () => {
      expect(pretty({ ok: true, count: 2 })).toBe('{\n  "ok": true,\n  "count": 2\n}')
    })
  })

  describe('formatUsd', () => {
    it('formats small, normal, and large values', () => {
      expect(formatUsd(0.0001)).toBe('<$0.01')
      expect(formatUsd(0)).toBe('$0.00')
      expect(formatUsd(12.345)).toBe('$12.35')
      expect(formatUsd(1234.5)).toBe('$1.23K')
    })
  })

  describe('versionIdOf', () => {
    it('reads supported version id shapes', () => {
      expect(versionIdOf({ version_id: 'snake' })).toBe('snake')
      expect(versionIdOf({ versionId: 'camel' })).toBe('camel')
      expect(versionIdOf({ id: 'plain' })).toBe('plain')
      expect(versionIdOf({ id: 12 })).toBe('')
      expect(versionIdOf(null)).toBe('')
    })
  })

  describe('event value helpers', () => {
    it('reads current target values from form events', () => {
      const input = document.createElement('input')
      input.value = 'gateway'
      expect(getInputValue({ currentTarget: input } as unknown as Event)).toBe('gateway')

      const select = document.createElement('select')
      const option = document.createElement('option')
      option.value = 'telemetry'
      select.appendChild(option)
      select.value = 'telemetry'
      expect(getSelectValue({ currentTarget: select } as unknown as Event)).toBe('telemetry')

      input.checked = true
      expect(getChecked({ currentTarget: input } as unknown as Event)).toBe(true)
    })
  })

  describe('integer and time formatters', () => {
    beforeEach(() => {
      vi.useFakeTimers()
      vi.setSystemTime(new Date('2026-05-19T08:00:00Z'))
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    it('formats integers and missing values', () => {
      expect(formatInteger(1200)).toBe('1,200')
      expect(formatInteger(Number.NaN)).toBe('-')
      expect(formatInteger(undefined)).toBe('-')
    })

    it('formats relative times and preserves invalid input', () => {
      expect(formatRelativeTime(null)).toBe('-')
      expect(formatRelativeTime('bad-date')).toBe('bad-date')
      expect(formatRelativeTime('2026-05-19T07:59:55Z')).toBe('just now')
      expect(formatRelativeTime('2026-05-19T07:58:00Z')).toBe('2m ago')
      expect(formatRelativeTime('2026-05-18T08:00:00Z')).toBe('1d ago')
    })

    it('formats absolute and status times with invalid fallbacks', () => {
      expect(formatAbsoluteTime(undefined)).toBe('-')
      expect(formatAbsoluteTime('bad-date')).toBe('bad-date')
      expect(formatAbsoluteTime('2026-05-19T08:00:00Z')).not.toBe('2026-05-19T08:00:00Z')

      expect(formatStatusTime(null)).toBe('-')
      expect(formatStatusTime('bad-date')).toBe('bad-date')
      expect(formatStatusTime('2026-05-19T08:00:00Z')).not.toBe('2026-05-19T08:00:00Z')
    })
  })
})
