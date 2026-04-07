import { useRef, useMemo, useEffect } from 'preact/hooks'

export function useDeepCompareMemo<T>(factory: () => T, deps: unknown[]): T {
  const ref = useRef<{ deps: unknown[]; value: T; initialized: boolean }>({
    deps: [],
    value: undefined as unknown as T,
    initialized: false,
  })

  const hasChanged = useMemo(() => {
    if (!ref.current.initialized) return true
    if (deps.length !== ref.current.deps.length) return true
    return deps.some((dep, i) => !deepEqual(dep, ref.current.deps[i]))
  }, [deps])

  if (hasChanged) {
    ref.current.deps = deps
    ref.current.value = factory()
    ref.current.initialized = true
  }

  return ref.current.value
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== typeof b) return false
  if (typeof a !== 'object' || a === null || b === null) return false

  const aObj = a as Record<string, unknown>
  const bObj = b as Record<string, unknown>
  const aKeys = Object.keys(aObj)
  const bKeys = Object.keys(bObj)

  if (aKeys.length !== bKeys.length) return false

  for (const key of aKeys) {
    if (!bKeys.includes(key)) return false
    if (!deepEqual(aObj[key], bObj[key])) return false
  }

  return true
}

export function usePrevious<T>(value: T): T | undefined {
  const ref = useRef<T | undefined>(undefined)

  useEffect(() => {
    ref.current = value
  }, [value])

  return ref.current
}

export function useStableCallback<T extends (...args: unknown[]) => unknown>(
  callback: T
): T {
  const ref = useRef(callback)

  useEffect(() => {
    ref.current = callback
  }, [callback])

  return useMemo(
    () =>
      ((...args: unknown[]) => ref.current(...args)) as T,
    []
  )
}
