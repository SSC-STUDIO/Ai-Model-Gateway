import { useEffect, useRef, useState, useCallback } from 'preact/hooks'
import { fetchJSON } from '../utils/fetch'

interface CacheEntry<T> {
  data: T
  timestamp: number
  key: string
}

interface PendingRequest<T> {
  generation: number
  promise: Promise<T>
}

const globalCache = new Map<string, CacheEntry<unknown>>()
const pendingRequests = new Map<string, PendingRequest<unknown>>()
const cacheGenerations = new Map<string, number>()
const MAX_CACHE_ENTRIES = 100

function trimCache() {
  if (globalCache.size <= MAX_CACHE_ENTRIES) return
  const entries = Array.from(globalCache.entries())
  entries.sort((a, b) => a[1].timestamp - b[1].timestamp)
  const toDelete = entries.slice(0, entries.length - MAX_CACHE_ENTRIES)
  for (const [key] of toDelete) {
    globalCache.delete(key)
    cacheGenerations.delete(key)
  }
}

interface UseCachedFetchOptions {
  ttl?: number
  enabled?: boolean
  deduplicate?: boolean
  staleWhileRevalidate?: boolean
  onUnauthorized?: () => void
}

interface PrimeCacheOptions {
  ttl?: number
  deduplicate?: boolean
  force?: boolean
}

function getCachedEntry<T>(cacheKey: string): CacheEntry<T> | undefined {
  return globalCache.get(cacheKey) as CacheEntry<T> | undefined
}

function isFresh(entry: CacheEntry<unknown> | undefined, ttl: number): boolean {
  return Boolean(entry && Date.now() - entry.timestamp < ttl)
}

function getGeneration(cacheKey: string): number {
  return cacheGenerations.get(cacheKey) ?? 0
}

function bumpGeneration(cacheKey: string): number {
  const nextGeneration = getGeneration(cacheKey) + 1
  cacheGenerations.set(cacheKey, nextGeneration)
  return nextGeneration
}

function invalidateKey(cacheKey: string) {
  bumpGeneration(cacheKey)
  globalCache.delete(cacheKey)
  pendingRequests.delete(cacheKey)
}

async function fetchAndCache<T>(
  url: string,
  deduplicate: boolean,
  onUnauthorized?: () => void
): Promise<T> {
  const generation = getGeneration(url)
  const pending = pendingRequests.get(url) as PendingRequest<T> | undefined

  if (deduplicate && pending && pending.generation === generation) {
    return pending.promise
  }

  const fetchPromise = fetchJSON<T>(url, {
    onUnauthorized,
  }).then((result) => {
    if (getGeneration(url) === generation) {
      globalCache.set(url, { data: result, timestamp: Date.now(), key: url })
      trimCache()
    }
    return result
  })

  if (deduplicate) {
    pendingRequests.set(url, { generation, promise: fetchPromise })
  }

  try {
    return await fetchPromise
  } finally {
    if (deduplicate) {
      const activePending = pendingRequests.get(url)
      if (activePending?.promise === fetchPromise) {
        pendingRequests.delete(url)
      }
    }
  }
}

/**
 * React hook that fetches JSON data through a global deduplicated cache.
 *
 * Features:
 * - Deduplicates concurrent requests for the same URL.
 * - Serves stale data immediately while revalidating in the background.
 * - Auto-invalidates on 401 responses via optional `onUnauthorized`.
 * - LRU cache capped at 100 entries to prevent unbounded memory growth.
 *
 * @param url     API endpoint (or `null` to skip fetching).
 * @param options `ttl` (ms), `enabled`, `deduplicate`, `staleWhileRevalidate`, `onUnauthorized`.
 * @returns `{ data, loading, error, refetch, invalidate }`
 */
export function useCachedFetch<T>(
  url: string | null,
  options: UseCachedFetchOptions = {}
) {
  const {
    ttl = 60000,
    enabled = true,
    deduplicate = true,
    staleWhileRevalidate = true,
    onUnauthorized,
  } = options
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const requestSeq = useRef(0)

  const fetchData = useCallback(async (force = false) => {
    if (!url || (!enabled && !force)) return

    const cacheKey = url
    const cached = getCachedEntry<T>(cacheKey)

    if (cached) {
      setData(cached.data)
      setError(null)
      if (!force && isFresh(cached, ttl)) {
        setLoading(false)
        return cached.data
      }
    }

    const requestId = ++requestSeq.current
    setLoading(!cached || force || !staleWhileRevalidate)
    setError(null)

    try {
      const result = await fetchAndCache<T>(cacheKey, deduplicate, onUnauthorized)
      if (requestId === requestSeq.current) {
        setData(result)
        setError(null)
      }
      return result
    } catch (err) {
      if (requestId === requestSeq.current) {
        setError(err instanceof Error ? err : new Error(String(err)))
      }
    } finally {
      if (requestId === requestSeq.current) {
        setLoading(false)
      }
    }
  }, [url, ttl, enabled, deduplicate, staleWhileRevalidate, onUnauthorized])

  useEffect(() => {
    if (!url || !enabled) return
    const cached = getCachedEntry<T>(url)
    setData(cached?.data ?? null)
    void fetchData()
  }, [fetchData, enabled, url])

  const refetch = useCallback(() => {
    if (url) {
      invalidateKey(url)
    }
    return fetchData(true)
  }, [fetchData, url])

  const invalidate = useCallback(() => {
    if (url) {
      invalidateKey(url)
    }
  }, [url])

  return { data, loading, error, refetch, invalidate }
}

export async function primeCache<T>(
  url: string,
  options: PrimeCacheOptions = {}
): Promise<T | null> {
  const { ttl = 60000, deduplicate = true, force = false } = options
  if (force) {
    invalidateKey(url)
  }
  const cached = getCachedEntry<T>(url)
  if (!force && isFresh(cached, ttl)) {
    return cached?.data ?? null
  }
  return fetchAndCache<T>(url, deduplicate)
}

export function invalidateCache(urlPattern?: RegExp | string) {
  const knownKeys = new Set([
    ...globalCache.keys(),
    ...pendingRequests.keys(),
    ...cacheGenerations.keys(),
  ])

  if (!urlPattern) {
    for (const key of knownKeys) {
      invalidateKey(key)
    }
    return
  }

  if (typeof urlPattern === 'string') {
    invalidateKey(urlPattern)
  } else {
    for (const key of knownKeys) {
      if (urlPattern.test(key)) {
        invalidateKey(key)
      }
    }
  }
}
