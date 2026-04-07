import { useEffect, useRef, useState, useCallback } from 'preact/hooks'
import { useI18n } from '../i18n'

export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

export interface LogEntry {
  timestamp: string
  level: LogLevel
  message: string
  source?: string
}

interface LogViewerProps {
  maxEntries?: number
}

const LOG_LEVELS: LogLevel[] = ['debug', 'info', 'warn', 'error']

const LEVEL_COLORS: Record<LogLevel, string> = {
  debug: '#6b7280',
  info: '#3b82f6',
  warn: '#f59e0b',
  error: '#ef4444',
}

export function LogViewer({ maxEntries = 1000 }: LogViewerProps) {
  const { t } = useI18n()
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  const [minLevel, setMinLevel] = useState<LogLevel>('debug')
  const [searchQuery, setSearchQuery] = useState('')
  const [isPaused, setIsPaused] = useState(false)
  const [pendingLogs, setPendingLogs] = useState<LogEntry[]>([])

  const logContainerRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  const logsRef = useRef<LogEntry[]>([])

  // Keep ref in sync with state
  useEffect(() => {
    logsRef.current = logs
  }, [logs])

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (autoScroll && logContainerRef.current && !isPaused) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
    }
  }, [logs, autoScroll, isPaused])

  // Connect to SSE endpoint
  useEffect(() => {
    const connect = () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
      }

      const params = new URLSearchParams()
      params.set('level', minLevel)
      if (searchQuery) {
        params.set('search', searchQuery)
      }
      params.set('tail', '100')

      const url = `/api/admin/v2/logs/stream?${params.toString()}`
      const es = new EventSource(url)
      eventSourceRef.current = es

      es.onopen = () => {
        setIsConnected(true)
      }

      es.onmessage = (event) => {
        try {
          const entry: LogEntry = JSON.parse(event.data)
          if (isPaused) {
            setPendingLogs((prev) => [...prev, entry])
          } else {
            setLogs((prev) => {
              const newLogs = [...prev, entry]
              if (newLogs.length > maxEntries) {
                return newLogs.slice(-maxEntries)
              }
              return newLogs
            })
          }
        } catch {
          // Ignore parse errors
        }
      }

      es.onerror = () => {
        setIsConnected(false)
        es.close()
        // Auto-reconnect after 3 seconds
        setTimeout(connect, 3000)
      }
    }

    connect()

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
      }
    }
  }, [minLevel, searchQuery, maxEntries, isPaused])

  // Handle scroll to detect if user scrolled up
  const handleScroll = useCallback(() => {
    if (!logContainerRef.current) return

    const { scrollTop, scrollHeight, clientHeight } = logContainerRef.current
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 50

    if (autoScroll !== isAtBottom) {
      setAutoScroll(isAtBottom)
    }
  }, [autoScroll])

  // Toggle pause/resume
  const togglePause = useCallback(() => {
    setIsPaused((prev) => {
      const newPaused = !prev
      if (!newPaused && pendingLogs.length > 0) {
        // Flush pending logs
        setLogs((current) => {
          const combined = [...current, ...pendingLogs]
          if (combined.length > maxEntries) {
            return combined.slice(-maxEntries)
          }
          return combined
        })
        setPendingLogs([])
      }
      return newPaused
    })
  }, [pendingLogs, maxEntries])

  // Clear logs
  const clearLogs = useCallback(() => {
    setLogs([])
    setPendingLogs([])
  }, [])

  // Export logs
  const exportLogs = useCallback(async () => {
    const params = new URLSearchParams()
    params.set('level', minLevel)
    if (searchQuery) {
      params.set('search', searchQuery)
    }

    try {
      const response = await fetch(`/api/admin/v2/logs/export?${params.toString()}`, {
        credentials: 'same-origin',
      })

      if (!response.ok) {
        throw new Error('Export failed')
      }

      const blob = await response.blob()
      const url = window.URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `gateway-logs-${new Date().toISOString().slice(0, 10)}.txt`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      window.URL.revokeObjectURL(url)
    } catch (err) {
      console.error('Failed to export logs:', err)
      alert(t('logs.exportError'))
    }
  }, [minLevel, searchQuery, t])

  // Format timestamp
  const formatTime = (timestamp: string): string => {
    try {
      const date = new Date(timestamp)
      return date.toLocaleTimeString()
    } catch {
      return timestamp
    }
  }

  // Get filtered logs for display
  const displayLogs = logs

  return (
    <div class="log-viewer">
      {/* Toolbar */}
      <div class="log-toolbar">
        <div class="log-toolbar-left">
          {/* Level filter */}
          <label class="log-filter-label">
            {t('logs.levelFilter')}
            <select
              value={minLevel}
              onChange={(e) => setMinLevel((e.currentTarget as HTMLSelectElement).value as LogLevel)}
              class="log-level-select"
            >
              {LOG_LEVELS.map((level) => (
                <option key={level} value={level}>
                  {t(`logs.level.${level}`)}
                </option>
              ))}
            </select>
          </label>

          {/* Search */}
          <label class="log-filter-label">
            {t('logs.search')}
            <input
              type="text"
              value={searchQuery}
              onInput={(e) => setSearchQuery((e.currentTarget as HTMLInputElement).value)}
              placeholder={t('logs.searchPlaceholder')}
              class="log-search-input"
            />
          </label>
        </div>

        <div class="log-toolbar-right">
          {/* Connection status */}
          <span class={`log-status ${isConnected ? 'connected' : 'disconnected'}`}>
            {isConnected ? t('logs.connected') : t('logs.disconnected')}
          </span>

          {/* Pending count */}
          {isPaused && pendingLogs.length > 0 && (
            <span class="log-pending-badge">
              {pendingLogs.length} {t('logs.pending')}
            </span>
          )}

          {/* Pause/Resume button */}
          <button
            type="button"
            onClick={togglePause}
            class={`log-control-btn ${isPaused ? 'active' : ''}`}
            title={isPaused ? t('logs.resume') : t('logs.pause')}
          >
            {isPaused ? '▶' : '⏸'}
          </button>

          {/* Auto-scroll indicator */}
          <button
            type="button"
            onClick={() => setAutoScroll(!autoScroll)}
            class={`log-control-btn ${autoScroll ? 'active' : ''}`}
            title={autoScroll ? t('logs.autoScrollOn') : t('logs.autoScrollOff')}
          >
            {autoScroll ? '⬇' : '⇣'}
          </button>

          {/* Clear button */}
          <button
            type="button"
            onClick={clearLogs}
            class="log-control-btn"
            title={t('logs.clear')}
          >
            🗑
          </button>

          {/* Export button */}
          <button type="button" onClick={exportLogs} class="log-export-btn">
            {t('logs.export')}
          </button>
        </div>
      </div>

      {/* Log entries */}
      <div
        ref={logContainerRef}
        class="log-container"
        onScroll={handleScroll}
      >
        {displayLogs.length === 0 ? (
          <div class="log-empty">{t('logs.empty')}</div>
        ) : (
          displayLogs.map((log, index) => (
            <div
              key={`${log.timestamp}-${index}`}
              class={`log-entry log-entry-${log.level}`}
            >
              <span class="log-timestamp">{formatTime(log.timestamp)}</span>
              <span
                class="log-level-badge"
                style={{ backgroundColor: LEVEL_COLORS[log.level] }}
              >
                {log.level.toUpperCase()}
              </span>
              {log.source && <span class="log-source">[{log.source}]</span>}
              <span class="log-message">{log.message}</span>
            </div>
          ))
        )}
      </div>

      {/* Stats footer */}
      <div class="log-footer">
        <span>
          {t('logs.showing')} {displayLogs.length} {t('logs.entries')}
          {isPaused && pendingLogs.length > 0 && ` (${pendingLogs.length} ${t('logs.pending')})`}
        </span>
        {!autoScroll && (
          <button
            type="button"
            onClick={() => {
              setAutoScroll(true)
              if (logContainerRef.current) {
                logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
              }
            }}
            class="log-scroll-to-bottom"
          >
            {t('logs.scrollToBottom')}
          </button>
        )}
      </div>
    </div>
  )
}
