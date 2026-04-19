import { useState, useCallback } from 'preact/hooks'

type Serializer<T> = {
  stringify: (value: T) => string
  parse: (value: string) => T
}

const defaultSerializer: Serializer<unknown> = {
  stringify: JSON.stringify,
  parse: JSON.parse,
}

export function usePersistentState<T>(
  key: string,
  initialValue: T,
  serializer: Serializer<T> = defaultSerializer as Serializer<T>
): [T, (value: T | ((prev: T) => T)) => void] {
  const [state, setState] = useState<T>(() => {
    try {
      const item = localStorage.getItem(key)
      if (item) {
        return serializer.parse(item)
      }
    } catch {
      // ignore parse errors
    }
    return initialValue
  })

  const setPersistentState = useCallback(
    (value: T | ((prev: T) => T)) => {
      setState((prev) => {
        const next = typeof value === 'function' ? (value as (prev: T) => T)(prev) : value
        try {
          localStorage.setItem(key, serializer.stringify(next))
        } catch {
          // ignore storage errors
        }
        return next
      })
    },
    [key, serializer]
  )

  return [state, setPersistentState]
}
