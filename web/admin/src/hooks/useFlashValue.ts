import { useEffect, useRef, useState } from 'preact/hooks'

export function useFlashValue<T>(value: T, delay = 500): boolean {
  const [flashing, setFlashing] = useState(false)
  const prevRef = useRef<T>(value)

  useEffect(() => {
    if (value !== prevRef.current) {
      prevRef.current = value
      setFlashing(true)
      const timer = setTimeout(() => setFlashing(false), delay)
      return () => clearTimeout(timer)
    }
  }, [value, delay])

  return flashing
}
