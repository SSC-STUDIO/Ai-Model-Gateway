import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/preact'
import { useControlData } from './useControlData'
import { invalidateCache } from './useCachedFetch'

describe('useControlData', () => {
  beforeEach(() => {
    invalidateCache()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('threads the selected telemetry bucket into the timeseries fetch', async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/admin/status')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({}),
        }
      }
      if (url.includes('/api/admin/overview')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ windows: {} }),
        }
      }
      if (url.includes('/api/admin/telemetry')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ events: [], total: 0 }),
        }
      }
      if (url.includes('/api/admin/timeseries')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ points: [], bucket_minutes: 15 }),
        }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() =>
      useControlData('telemetry', '168', '15', 168, [], '24', true)
    )

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/timeseries?hours=168&bucket=15'))
      ).toBe(true)
    })
  })
})
