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
      useControlData('telemetry', '168', '15', '24', true)
    )

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/timeseries?hours=168&bucket=15'))
      ).toBe(true)
    })
  })

  it('fetches telemetry without logs for the monitoring page', async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/admin/status') || url.includes('/api/admin/overview')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({}),
        }
      }
      if (url.includes('/api/admin/telemetry?hours=168')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ summary: {} }),
        }
      }
      if (url.includes('/api/admin/timeseries?hours=168&bucket=5')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ points: [], bucket_minutes: 5 }),
        }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() =>
      useControlData('monitoring', '168', '5', '24', true)
    )

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/telemetry?hours=168'))
      ).toBe(true)
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/timeseries?hours=168&bucket=5'))
      ).toBe(true)
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/telemetry?hours=24&limit=500&offset=0'))
      ).toBe(false)
    })
  })

  it('fetches logs only for the top-level logs page', async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/admin/status') || url.includes('/api/admin/overview')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({}),
        }
      }
      if (url.includes('/api/admin/telemetry?hours=24&limit=500&offset=0')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ events: [], total: 0 }),
        }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() =>
      useControlData('logs', '168', '5', '24', true)
    )

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/telemetry?hours=24&limit=500&offset=0'))
      ).toBe(true)
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/timeseries?hours=168&bucket=5'))
      ).toBe(false)
    })
  })

  it('fetches upstream and model benchmark groups for the benchmark page', async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/admin/status') || url.includes('/api/admin/overview')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({}),
        }
      }
      if (url.includes('/api/admin/benchmark?hours=168&group=upstream')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ group: 'upstream', benchmarks: [] }),
        }
      }
      if (url.includes('/api/admin/benchmark?hours=168')) {
        return {
          ok: true,
          text: vi.fn().mockResolvedValue(''),
          json: vi.fn().mockResolvedValue({ group: 'model', benchmarks: [] }),
        }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() =>
      useControlData('benchmark', '168', '5', '24', true)
    )

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).includes('/api/admin/benchmark?hours=168&group=upstream'))
      ).toBe(true)
      expect(
        fetchMock.mock.calls.some(([input]) => String(input).endsWith('/api/admin/benchmark?hours=168'))
      ).toBe(true)
    })
  })
})
