import { useState, useEffect } from 'preact/hooks'

export function usePageVisibility(): boolean {
  const [isVisible, setIsVisible] = useState<boolean>(() => {
    if (typeof document === 'undefined') return true
    return !document.hidden
  })

  useEffect(() => {
    const handleVisibilityChange = () => {
      setIsVisible(!document.hidden)
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [])

  return isVisible
}

export function useRefreshWhenVisible(
  isVisible: boolean,
  refreshFn: () => void,
  intervalMs: number
): void {
  useEffect(() => {
    if (!isVisible || intervalMs <= 0) return

    const interval = setInterval(() => {
      refreshFn()
    }, intervalMs)

    return () => clearInterval(interval)
  }, [isVisible, intervalMs, refreshFn])
}
