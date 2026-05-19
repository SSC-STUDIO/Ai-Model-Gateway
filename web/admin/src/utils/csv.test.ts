import { afterEach, describe, expect, it, vi } from 'vitest'
import { csvValue, downloadCsv } from './csv'

describe('csv utilities', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  describe('csvValue', () => {
    it('quotes values only when required', () => {
      expect(csvValue('gateway')).toBe('gateway')
      expect(csvValue(42)).toBe('42')
      expect(csvValue(null)).toBe('')
      expect(csvValue(undefined)).toBe('')
      expect(csvValue('hello,world')).toBe('"hello,world"')
      expect(csvValue('hello "gateway"')).toBe('"hello ""gateway"""')
      expect(csvValue('line 1\nline 2')).toBe('"line 1\nline 2"')
    })
  })

  describe('downloadCsv', () => {
    it('creates a utf-8 csv blob, clicks a temporary link, and revokes the object url', async () => {
      const url = 'blob:admin-export'
      const createObjectURL = vi.fn().mockReturnValue(url)
      const revokeObjectURL = vi.fn()
      vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
      const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

      downloadCsv('requests.csv', [
        ['model', 'requests'],
        ['gpt-5', 12],
        ['quoted "name"', null],
      ])

      expect(createObjectURL).toHaveBeenCalledTimes(1)
      const blob = createObjectURL.mock.calls[0][0] as Blob
      expect(blob.type).toBe('text/csv;charset=utf-8')
      expect(await blob.text()).toBe('model,requests\r\ngpt-5,12\r\n"quoted ""name""",')
      expect(clickSpy).toHaveBeenCalledTimes(1)
      expect(document.querySelector('a[download="requests.csv"]')).toBeNull()
      expect(revokeObjectURL).toHaveBeenCalledWith(url)
    })
  })
})
