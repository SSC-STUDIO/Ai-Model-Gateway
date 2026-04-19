import { useState, useCallback, useRef, useEffect } from 'preact/hooks'

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface ToastItem {
  id: string
  message: string
  type: ToastType
}

export function useToast() {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const timersRef = useRef<Record<string, number>>({})

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
    if (timersRef.current[id]) {
      clearTimeout(timersRef.current[id])
      delete timersRef.current[id]
    }
  }, [])

  const addToast = useCallback((message: string, type: ToastType = 'info', duration = 4000) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
    setToasts((prev) => [...prev, { id, message, type }])
    timersRef.current[id] = window.setTimeout(() => {
      removeToast(id)
    }, duration)
    return id
  }, [removeToast])

  useEffect(() => {
    return () => {
      Object.values(timersRef.current).forEach((timer) => clearTimeout(timer))
      timersRef.current = {}
    }
  }, [])

  return { toasts, addToast, removeToast }
}
