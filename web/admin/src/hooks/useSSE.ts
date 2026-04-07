import { useEffect, useRef, useState, useCallback } from 'preact/hooks'

interface UseSSEResult {
  connected: boolean
  reconnecting: boolean
}

export function useSSE(
  url: string,
  onEvent: (type: string, data: string) => void
): UseSSEResult {
  const [connected, setConnected] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const onEventRef = useRef(onEvent)
  const retriesRef = useRef(0)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Keep callback ref fresh without re-triggering effect
  useEffect(() => {
    onEventRef.current = onEvent
  }, [onEvent])

  const connect = useCallback(() => {
    const es = new EventSource(url, { withCredentials: true })

    es.onopen = () => {
      setConnected(true)
      setReconnecting(false)
      retriesRef.current = 0
    }

    es.addEventListener('metrics_update', (e) => {
      onEventRef.current('metrics_update', (e as MessageEvent).data)
    })

    es.addEventListener('config_changed', (e) => {
      onEventRef.current('config_changed', (e as MessageEvent).data)
    })

    es.onerror = () => {
      es.close()
      setConnected(false)
      setReconnecting(true)

      const delay = Math.min(1000 * Math.pow(2, retriesRef.current), 30000)
      retriesRef.current++

      timerRef.current = setTimeout(() => {
        connect()
      }, delay)
    }

    return es
  }, [url])

  useEffect(() => {
    if (!url) {
      setConnected(false)
      setReconnecting(false)
      return
    }

    const es = connect()

    return () => {
      es.close()
      setConnected(false)
      setReconnecting(false)
      if (timerRef.current) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }
  }, [connect, url])

  return { connected, reconnecting }
}
