import { useState, useCallback, useEffect, useRef } from 'preact/hooks'

type UrlValue = string | number | string[]

interface Serializer<T> {
  stringify: (value: T) => string
  parse: (value: string) => T
}

const stringSerializer: Serializer<string> = {
  stringify: (v) => v,
  parse: (v) => v,
}

const numberSerializer: Serializer<number> = {
  stringify: (v) => String(v),
  parse: (v) => {
    const n = Number(v)
    return Number.isFinite(n) ? n : 0
  },
}

const stringArraySerializer: Serializer<string[]> = {
  stringify: (v) => v.join(','),
  parse: (v) => v.split(',').map((s) => s.trim()).filter(Boolean),
}

function getSerializer<T extends UrlValue>(defaultValue: T): Serializer<T> {
  if (Array.isArray(defaultValue)) return stringArraySerializer as unknown as Serializer<T>
  if (typeof defaultValue === 'number') return numberSerializer as unknown as Serializer<T>
  return stringSerializer as unknown as Serializer<T>
}

function readParam<T>(key: string, defaultValue: T, serializer: Serializer<T>): T {
  try {
    const params = new URLSearchParams(window.location.search)
    const raw = params.get(key)
    if (raw !== null) {
      return serializer.parse(raw)
    }
  } catch {
    // ignore
  }
  return defaultValue
}

function writeParam(key: string, value: string, defaultValue: UrlValue) {
  const url = new URL(window.location.href)
  const isDefault = value === getSerializer(defaultValue).stringify(defaultValue as never)

  if (isDefault || value === '' || value === '0') {
    url.searchParams.delete(key)
  } else {
    url.searchParams.set(key, value)
  }

  window.history.replaceState(window.history.state, '', url.toString())
}

/**
 * Sync a piece of Preact state with a URL query parameter.
 *
 * - Uses `history.replaceState` (no back-button pollution).
 * - Supports `string`, `number`, and `string[]` values.
 * - Default values are automatically cleaned from the URL.
 * - Listens to `popstate` so back/forward navigation syncs state.
 *
 * @param key          Query parameter name.
 * @param defaultValue Value used when the param is absent from the URL.
 * @returns A `[state, setState]` tuple matching `useState` ergonomics.
 */
export function useUrlState<T extends UrlValue>(
  key: string,
  defaultValue: T
): [T, (value: T | ((prev: T) => T)) => void] {
  const serializer = useRef(getSerializer(defaultValue)).current
  const [state, setState] = useState<T>(() => readParam(key, defaultValue, serializer))

  const setUrlState = useCallback(
    (value: T | ((prev: T) => T)) => {
      setState((prev) => {
        const next = typeof value === 'function' ? (value as (prev: T) => T)(prev) : value
        const serialized = serializer.stringify(next)
        writeParam(key, serialized, defaultValue)
        return next
      })
    },
    [key, defaultValue, serializer]
  )

  // Sync from URL on popstate / hashchange
  useEffect(() => {
    const handler = () => {
      const next = readParam(key, defaultValue, serializer)
      setState((prev) => {
        const prevSerialized = serializer.stringify(prev)
        const nextSerialized = serializer.stringify(next)
        return prevSerialized === nextSerialized ? prev : next
      })
    }
    window.addEventListener('popstate', handler)
    return () => window.removeEventListener('popstate', handler)
  }, [key, defaultValue, serializer])

  return [state, setUrlState]
}
