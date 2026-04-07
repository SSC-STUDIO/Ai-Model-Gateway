import { useEffect, useRef, useState, useCallback } from 'preact/hooks'

interface CacheEntry<T> {
  data: T
  timestamp: number
  key: string
}

const globalCache = new Map<string, CacheEntry<unknown>>()
const pendingRequests = new Map<string, Promise<unknown>>()

interface UseCachedFetchOptions {
  ttl?: number
  enabled?: boolean
  deduplicate?: boolean
}

export function useCachedFetch<T>(
  url: string | null,
  options: UseCachedFetchOptions = {}
) {
  const { ttl = 60000, enabled = true, deduplicate = true } = options
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const lastFetchedUrl = useRef<string | null>(null)

  const fetchData = useCallback(async () => {
    if (!url || !enabled) return

    const cacheKey = url
    const now = Date.now()

    const cached = globalCache.get(cacheKey) as CacheEntry<T> | undefined
    if (cached && now - cached.timestamp < ttl) {
      setData(cached.data)
      setError(null)
      return
    }

    if (deduplicate && pendingRequests.has(cacheKey)) {
      setLoading(true)
      try {
        const result = await pendingRequests.get(cacheKey) as T
        setData(result)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setLoading(false)
      }
      return
    }

    setLoading(true)
    setError(null)

    const fetchPromise = fetch(url, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    }).then(async (resp) => {
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(text || `${resp.status} ${resp.statusText}`)
      }
      return resp.json() as T
    })

    if (deduplicate) {
      pendingRequests.set(cacheKey, fetchPromise)
    }

    try {
      const result = await fetchPromise
      globalCache.set(cacheKey, { data: result, timestamp: now, key: cacheKey })
      setData(result)
      lastFetchedUrl.current = url
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setLoading(false)
      if (deduplicate) {
        pendingRequests.delete(cacheKey)
      }
    }
  }, [url, ttl, enabled, deduplicate])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const refetch = useCallback(() => {
    if (url) {
      globalCache.delete(url)
    }
    return fetchData()
  }, [fetchData, url])

  const invalidate = useCallback(() => {
    if (url) {
      globalCache.delete(url)
    }
  }, [url])

  return { data, loading, error, refetch, invalidate }
}

export function invalidateCache(urlPattern?: RegExp | string) {
  if (!urlPattern) {
    globalCache.clear()
    return
  }

  if (typeof urlPattern === 'string') {
    globalCache.delete(urlPattern)
  } else {
    for (const key of globalCache.keys()) {
      if (urlPattern.test(key)) {
        globalCache.delete(key)
      }
    }
  }
}
