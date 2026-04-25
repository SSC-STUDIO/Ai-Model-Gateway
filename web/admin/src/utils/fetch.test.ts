import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { fetchJSON } from './fetch'

describe('fetchJSON', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('resolves JSON on success', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: vi.fn().mockResolvedValue('{"result":42}'),
    })
    vi.stubGlobal('fetch', mockFetch)

    const data = await fetchJSON<unknown>('/api/test')
    expect(data).toEqual({ result: 42 })
    expect(mockFetch).toHaveBeenCalledTimes(1)

    const requestInit = mockFetch.mock.calls[0][1]
    expect(requestInit.headers.get('Accept')).toBe('application/json')
  })

  it('sets Content-Type header when body is present', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: vi.fn().mockResolvedValue('{}'),
    })
    vi.stubGlobal('fetch', mockFetch)

    await fetchJSON('/api/test', { method: 'POST', body: '{}' })

    const requestInit = mockFetch.mock.calls[0][1]
    expect(requestInit.headers.get('Content-Type')).toBe('application/json')
  })

  it('throws on HTTP error with response text', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      text: vi.fn().mockResolvedValue('server error'),
    })
    vi.stubGlobal('fetch', mockFetch)

    await expect(fetchJSON('/api/error')).rejects.toThrow('server error')
  })

  it('throws on HTTP error with JSON error message', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
      text: vi.fn().mockResolvedValue('{"error":"invalid input"}'),
    })
    vi.stubGlobal('fetch', mockFetch)

    await expect(fetchJSON('/api/error')).rejects.toThrow('invalid input')
  })

  it('calls onUnauthorized on 401', async () => {
    const onUnauthorized = vi.fn()
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      statusText: 'Unauthorized',
      text: vi.fn().mockResolvedValue('unauthorized'),
    })
    vi.stubGlobal('fetch', mockFetch)

    await expect(fetchJSON('/api/protected', { onUnauthorized })).rejects.toThrow()
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('falls back to status code when no error message', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      statusText: 'Service Unavailable',
      text: vi.fn().mockResolvedValue(''),
    })
    vi.stubGlobal('fetch', mockFetch)

    await expect(fetchJSON('/api/down')).rejects.toThrow('503 Service Unavailable')
  })

  it('parses plain text response when JSON is invalid', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
      text: vi.fn().mockResolvedValue('not json'),
    })
    vi.stubGlobal('fetch', mockFetch)

    await expect(fetchJSON('/api/error')).rejects.toThrow('not json')
  })
})
