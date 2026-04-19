import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/preact'
import { useCachedFetch, primeCache, invalidateCache } from './useCachedFetch'

describe('useCachedFetch', () => {
  beforeEach(() => {
    invalidateCache()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('starts loading then resolves data', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      text: vi.fn().mockResolvedValue(''),
      json: vi.fn().mockResolvedValue({ value: 42 }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() =>
      useCachedFetch('/api/test', { enabled: true })
    )

    expect(result.current.loading).toBe(true)
    expect(result.current.data).toBeNull()

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toEqual({ value: 42 })
    expect(result.current.error).toBeNull()
  })

  it('handles fetch error', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      text: vi.fn().mockResolvedValue('boom'),
    })
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() =>
      useCachedFetch('/api/error', { enabled: true })
    )

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBeInstanceOf(Error)
    expect(result.current.error?.message).toContain('boom')
  })

  it('does not fetch when disabled', () => {
    const mockFetch = vi.fn()
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() =>
      useCachedFetch('/api/test', { enabled: false })
    )

    expect(result.current.loading).toBe(false)
    expect(mockFetch).not.toHaveBeenCalled()
  })

  it('refetch invalidates and re-fetches', async () => {
    let callCount = 0
    const mockFetch = vi.fn().mockImplementation(() => {
      callCount++
      return Promise.resolve({
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({ count: callCount }),
      })
    })
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() =>
      useCachedFetch('/api/counter', { enabled: true })
    )

    await waitFor(() => expect(result.current.data).toEqual({ count: 1 }))

    act(() => {
      result.current.refetch()
    })

    await waitFor(() => expect(result.current.data).toEqual({ count: 2 }))
  })

  it('deduplicates concurrent requests', async () => {
    let resolveFn: (() => void) | null = null
    const promise = new Promise<void>((r) => { resolveFn = r })

    const mockFetch = vi.fn().mockImplementation(async () => {
      await promise
      return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({ dedup: true }),
      }
    })
    vi.stubGlobal('fetch', mockFetch)

    // Two hooks with same URL
    const { result: r1 } = renderHook(() =>
      useCachedFetch('/api/dedup', { enabled: true })
    )
    const { result: r2 } = renderHook(() =>
      useCachedFetch('/api/dedup', { enabled: true })
    )

    expect(mockFetch).toHaveBeenCalledTimes(1)

    act(() => { resolveFn && resolveFn() })

    await waitFor(() => expect(r1.current.data).toEqual({ dedup: true }))
    expect(r2.current.data).toEqual({ dedup: true })
  })
})

describe('primeCache', () => {
  beforeEach(() => {
    invalidateCache()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches and caches data', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      text: vi.fn().mockResolvedValue(''),
      json: vi.fn().mockResolvedValue({ primed: true }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const result = await primeCache('/api/prime')
    expect(result).toEqual({ primed: true })
    expect(mockFetch).toHaveBeenCalledTimes(1)

    // Second call should return cached without fetch
    const result2 = await primeCache('/api/prime')
    expect(result2).toEqual({ primed: true })
    expect(mockFetch).toHaveBeenCalledTimes(1)
  })

  it('force refetches when requested', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      text: vi.fn().mockResolvedValue(''),
      json: vi.fn().mockResolvedValue({ version: 1 }),
    })
    vi.stubGlobal('fetch', mockFetch)

    await primeCache('/api/version')
    await primeCache('/api/version', { force: true })
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })
})

describe('invalidateCache', () => {
  beforeEach(() => {
    invalidateCache()
  })

  it('invalidates by exact string', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      text: vi.fn().mockResolvedValue(''),
      json: vi.fn().mockResolvedValue({}),
    })
    vi.stubGlobal('fetch', mockFetch)

    await primeCache('/api/exact')
    invalidateCache('/api/exact')
    await primeCache('/api/exact')
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })

  it('invalidates by regex pattern', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      text: vi.fn().mockResolvedValue(''),
      json: vi.fn().mockResolvedValue({}),
    })
    vi.stubGlobal('fetch', mockFetch)

    await primeCache('/api/foo/1')
    await primeCache('/api/bar/2')
    invalidateCache(/\/api\/foo/)
    await primeCache('/api/foo/1')
    await primeCache('/api/bar/2')
    expect(mockFetch).toHaveBeenCalledTimes(3) // foo re-fetched, bar cached
  })
})
