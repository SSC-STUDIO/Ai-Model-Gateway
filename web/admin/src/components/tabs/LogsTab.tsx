import { memo, useCallback, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { DataResponse, ErrorEntry, RequestEntry } from '../../types'
import { formatInteger } from '../../utils/formatting'
import { Icon } from '../Icon'
import { ServiceStatePanel } from '../ServiceStatePanel'

interface LogsTabProps {
  telemetry: DataResponse | null
  hours: string
  onHoursChange: (hours: string) => void
  telemetryStatus?: string
  telemetryError?: string
  telemetryLastCheckedAt?: string
  onRetry?: () => void
}

type LogType = 'all' | 'requests' | 'errors'

const LOGS_WINDOW_OPTIONS = [
  { value: '1', label: '1h' },
  { value: '6', label: '6h' },
  { value: '24', label: '24h' },
  { value: '168', label: '7d' },
  { value: '720', label: '30d' },
  { value: 'all', label: 'All' },
]

const DEFAULT_PAGE_SIZE = 50

type StatusTone = 'success' | 'warning' | 'error' | 'neutral'

function formatRelativeTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = new Date(value)
  const ts = parsed.getTime()
  if (Number.isNaN(ts)) return value

  const now = Date.now()
  const diffMs = now - ts
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)

  if (diffSec < 10) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHour < 24) return `${diffHour}h ago`
  if (diffDay < 7) return `${diffDay}d ago`
  return parsed.toLocaleDateString()
}

function formatAbsoluteTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}

function formatLatency(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? `${value.toFixed(1)}ms` : '-'
}

function statusTone(status: number | null | undefined): StatusTone {
  if (typeof status !== 'number' || !Number.isFinite(status) || status <= 0) return 'neutral'
  if (status >= 500) return 'error'
  if (status >= 400) return 'warning'
  return 'success'
}

function statusText(status: number | null | undefined): string {
  return typeof status === 'number' && Number.isFinite(status) && status > 0 ? String(status) : '-'
}

interface UnifiedLogEntry {
  id: number
  timestamp: string
  path: string
  model: string
  upstream: string
  statusCode: number
  latencyMs: number
  inputTokens: number | undefined
  outputTokens: number | undefined
  cachedPromptTokens: number | undefined
  attempts: number | undefined
  errorMessage: string | undefined
  isError: boolean
}

function unifyEntries(requests: RequestEntry[] | undefined, errors: ErrorEntry[] | undefined): UnifiedLogEntry[] {
  const result: UnifiedLogEntry[] = []
  let id = 0

  if (requests) {
    for (const req of requests) {
      result.push({
        id: id++,
        timestamp: req.Timestamp ?? req.time ?? '',
        path: req.Path ?? req.path ?? '-',
        model: req.Model ?? req.model ?? '-',
        upstream: req.Upstream ?? req.upstream ?? '-',
        statusCode: req.StatusCode ?? req.status ?? 0,
        latencyMs: req.LatencyMs ?? req.latency_ms ?? 0,
        inputTokens: req.InputTokens ?? req.input_tokens,
        outputTokens: req.OutputTokens ?? req.output_tokens,
        cachedPromptTokens: req.CachedPromptTokens,
        attempts: req.Attempts ?? req.attempts,
        errorMessage: undefined,
        isError: false,
      })
    }
  }

  if (errors) {
    for (const err of errors) {
      const existing = result.find(
        (r) =>
          r.timestamp === (err.Timestamp ?? err.time) &&
          r.model === (err.Model ?? err.model) &&
          r.upstream === (err.Upstream ?? err.upstream) &&
          !r.isError
      )
      if (existing) {
        existing.errorMessage = err.Message ?? err.message
        existing.isError = true
      } else {
        result.push({
          id: id++,
          timestamp: err.Timestamp ?? err.time ?? '',
          path: '-',
          model: err.Model ?? err.model ?? '-',
          upstream: err.Upstream ?? err.upstream ?? '-',
          statusCode: err.StatusCode ?? err.status ?? 0,
          latencyMs: 0,
          inputTokens: undefined,
          outputTokens: undefined,
          cachedPromptTokens: undefined,
          attempts: err.Attempts ?? err.count,
          errorMessage: err.Message ?? err.message,
          isError: true,
        })
      }
    }
  }

  return result.sort((a, b) => {
    const ta = Date.parse(a.timestamp) || 0
    const tb = Date.parse(b.timestamp) || 0
    return tb - ta
  })
}

function joinSecondary(parts: Array<string | null | undefined>): string | undefined {
  const filtered = parts.filter((part): part is string => Boolean(part && part.trim().length > 0))
  return filtered.length > 0 ? filtered.join(' · ') : undefined
}

const LogsTabComponent = ({
  telemetry,
  hours,
  onHoursChange,
  telemetryStatus,
  telemetryError,
  telemetryLastCheckedAt,
  onRetry,
}: LogsTabProps) => {
  const { t } = useI18n()
  const [logType, setLogType] = useState<LogType>('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [expandedRow, setExpandedRow] = useState<number | null>(null)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)

  const allEntries = useMemo(() => {
    return unifyEntries(telemetry?.requests, telemetry?.errors)
  }, [telemetry?.requests, telemetry?.errors])

  const filteredEntries = useMemo(() => {
    let entries = allEntries
    if (logType === 'errors') {
      entries = entries.filter((e) => e.isError || e.statusCode >= 400)
    } else if (logType === 'requests') {
      entries = entries.filter((e) => !e.isError && e.statusCode < 400)
    }

    if (!searchQuery.trim()) return entries

    const q = searchQuery.toLowerCase()
    return entries.filter(
      (e) =>
        e.model.toLowerCase().includes(q) ||
        e.upstream.toLowerCase().includes(q) ||
        e.path.toLowerCase().includes(q) ||
        String(e.statusCode).includes(q) ||
        (e.errorMessage && e.errorMessage.toLowerCase().includes(q))
    )
  }, [allEntries, logType, searchQuery])

  const visibleEntries = useMemo(() => {
    return filteredEntries.slice(0, pageSize)
  }, [filteredEntries, pageSize])

  const hasMore = visibleEntries.length < filteredEntries.length

  const handleRowClick = useCallback(
    (id: number) => {
      setExpandedRow((prev) => (prev === id ? null : id))
    },
    []
  )

  const handleClearSearch = useCallback(() => {
    setSearchQuery('')
  }, [])

  const handleLoadMore = useCallback(() => {
    setPageSize((prev) => prev + DEFAULT_PAGE_SIZE)
  }, [])

  const handleHoursChange = useCallback((val: string) => {
    setPageSize(DEFAULT_PAGE_SIZE)
    onHoursChange(val)
  }, [onHoursChange])

  const hasData = allEntries.length > 0

  if (telemetryStatus && telemetryStatus !== 'connected') {
    return (
      <section class="panel">
        <h2>{t('logs.title')}</h2>
        <ServiceStatePanel
          icon="logs"
          title={t('services.telemetryUnavailableTitle')}
          message={t('services.telemetryUnavailableMessage')}
          hint={t('services.telemetryUnavailableHint')}
          detail={telemetryError}
          actionLabel={t('common.retry')}
          onAction={onRetry}
          items={[
            { label: t('header.telemetry'), value: telemetryStatus, tone: telemetryStatus === 'error' ? 'error' : 'warning' },
            ...(telemetryLastCheckedAt ? [{ label: t('services.lastChecked'), value: formatAbsoluteTime(telemetryLastCheckedAt) }] : []),
          ]}
        />
      </section>
    )
  }

  if (!telemetry) {
    return (
      <section class="panel">
        <h2>{t('logs.title')}</h2>
        <div class="skeleton-grid" style={{ marginTop: '20px' }}>
          <div class="skeleton skeleton-card">
            <div style={{ padding: '18px' }}>
              <div class="skeleton skeleton-label" />
              <div class="skeleton skeleton-metric" />
            </div>
          </div>
        </div>
        <div class="table-wrap" style={{ marginTop: '20px' }}>
          <div class="skeleton chart-skeleton" style={{ height: '200px' }} />
        </div>
      </section>
    )
  }

  return (
    <section class="panel">
      <div class="timeseries-header">
        <h2>{t('logs.title')}</h2>
        <div class="timeseries-controls">
          <div class="timeseries-selector">
            <span>{t('timeseries.timeRange')}:</span>
            {LOGS_WINDOW_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                class={`ts-btn${hours === opt.value ? ' active' : ''}`}
                onClick={() => handleHoursChange(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div class="logs-toolbar">
        <div class="logs-type-selector">
          <button
            type="button"
            class={`logs-type-btn${logType === 'all' ? ' active' : ''}`}
            onClick={() => setLogType('all')}
          >
            {t('logs.typeAll')}
          </button>
          <button
            type="button"
            class={`logs-type-btn${logType === 'requests' ? ' active' : ''}`}
            onClick={() => setLogType('requests')}
          >
            {t('logs.typeRequests')}
          </button>
          <button
            type="button"
            class={`logs-type-btn${logType === 'errors' ? ' active' : ''}`}
            onClick={() => setLogType('errors')}
          >
            {t('logs.typeErrors')}
          </button>
        </div>
        <div class="logs-search">
          <input
            type="text"
            value={searchQuery}
            placeholder={t('logs.searchPlaceholder')}
            onInput={(e) => setSearchQuery((e.currentTarget as HTMLInputElement).value)}
          />
          {searchQuery && (
            <button
              type="button"
              class="logs-search-clear"
              onClick={handleClearSearch}
              aria-label="Clear search"
            >
              ×
            </button>
          )}
        </div>
      </div>

      <div class="logs-stats">
        <span class="logs-stat">
          {t('logs.showing')}: <strong>{formatInteger(visibleEntries.length)}</strong> /{' '}
          {formatInteger(filteredEntries.length)} {t('logs.entries')}
        </span>
      </div>

      {!hasData ? (
        <div class="empty-state-box">
          <div class="empty-state-icon"><Icon name="logs" size={30} /></div>
          <p class="empty-state-title">{t('logs.noData')}</p>
          <p class="empty-state-hint">{t('logs.noDataHint')}</p>
        </div>
      ) : filteredEntries.length === 0 ? (
        <div class="empty-state-box">
          <div class="empty-state-icon"><Icon name="search" size={30} /></div>
          <p class="empty-state-title">{t('logs.noMatch')}</p>
          <p class="empty-state-hint">{t('logs.noMatchHint')}</p>
        </div>
      ) : (
        <>
          <div class="table-wrap logs-table-wrap">
            <table class="logs-table">
              <thead>
                <tr>
                  <th>{t('logs.colTime')}</th>
                  <th>{t('logs.colPath')}</th>
                  <th>{t('logs.colModel')}</th>
                  <th>{t('logs.colUpstream')}</th>
                  <th>{t('logs.colStatus')}</th>
                  <th>{t('logs.colLatency')}</th>
                  <th>{t('logs.colTokens')}</th>
                  <th>{t('logs.colError')}</th>
                </tr>
              </thead>
              <tbody>
                {visibleEntries.map((entry) => {
                  const isExpanded = expandedRow === entry.id
                  const tokenSummary =
                    entry.inputTokens != null || entry.outputTokens != null
                      ? `${formatInteger(entry.inputTokens)} / ${formatInteger(entry.outputTokens)}`
                      : '-'
                  const tokenMeta = joinSecondary([
                    entry.cachedPromptTokens && entry.cachedPromptTokens > 0 ? `cached ${formatInteger(entry.cachedPromptTokens)}` : undefined,
                  ])
                  const attemptCount = entry.attempts ?? 1
                  const hasError = entry.isError || entry.statusCode >= 400
                  const tone = statusTone(entry.statusCode)

                  return (
                    <>
                      <tr
                        key={entry.id}
                        class={`data-row logs-row${hasError ? ' error-row' : ''}${isExpanded ? ' expanded' : ''}`}
                        onClick={() => handleRowClick(entry.id)}
                      >
                        <td class="logs-time" title={formatAbsoluteTime(entry.timestamp)}>
                          {formatRelativeTime(entry.timestamp)}
                        </td>
                        <td class="logs-path" title={entry.path}>
                          <span class="logs-path-text">{entry.path}</span>
                          {entry.cachedPromptTokens && entry.cachedPromptTokens > 0 && (
                            <span class="status-badge neutral cached-badge">cached</span>
                          )}
                        </td>
                        <td>{entry.model}</td>
                        <td>{entry.upstream}</td>
                        <td>
                          <span class={`status-dot ${tone}`} />
                          <span class={`status-badge ${tone}`}>
                            {statusText(entry.statusCode)}
                          </span>
                        </td>
                        <td class="mono">
                          {formatLatency(entry.latencyMs)}
                          {attemptCount > 1 && (
                            <span class="logs-attempts">×{attemptCount}</span>
                          )}
                        </td>
                        <td class="mono">
                          <span>{tokenSummary}</span>
                          {tokenMeta && <span class="logs-token-meta">{tokenMeta}</span>}
                        </td>
                        <td class="logs-error-cell">
                          {entry.errorMessage ? (
                            <span class="logs-error-text" title={entry.errorMessage}>
                              {entry.errorMessage.length > 40
                                ? `${entry.errorMessage.slice(0, 40)}…`
                                : entry.errorMessage}
                            </span>
                          ) : (
                            '-'
                          )}
                        </td>
                      </tr>
                      {isExpanded && (
                        <tr class="logs-detail-row">
                          <td colSpan={8}>
                            <div class="logs-detail">
                              <div class="logs-detail-grid">
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailTime')}</span>
                                  <span class="logs-detail-value">{entry.timestamp}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailPath')}</span>
                                  <span class="logs-detail-value">{entry.path}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailModel')}</span>
                                  <span class="logs-detail-value">{entry.model}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailUpstream')}</span>
                                  <span class="logs-detail-value">{entry.upstream}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailStatus')}</span>
                                  <span class="logs-detail-value">{entry.statusCode}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailLatency')}</span>
                                  <span class="logs-detail-value">{formatLatency(entry.latencyMs)}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailInputTokens')}</span>
                                  <span class="logs-detail-value">{formatInteger(entry.inputTokens)}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailOutputTokens')}</span>
                                  <span class="logs-detail-value">{formatInteger(entry.outputTokens)}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailCachedTokens')}</span>
                                  <span class="logs-detail-value">{formatInteger(entry.cachedPromptTokens)}</span>
                                </div>
                                <div class="logs-detail-item">
                                  <span class="logs-detail-label">{t('logs.detailAttempts')}</span>
                                  <span class="logs-detail-value">{attemptCount}</span>
                                </div>
                                {entry.errorMessage && (
                                  <div class="logs-detail-item logs-detail-full">
                                    <span class="logs-detail-label">{t('logs.detailError')}</span>
                                    <span class="logs-detail-value logs-detail-error">{entry.errorMessage}</span>
                                  </div>
                                )}
                              </div>
                            </div>
                          </td>
                        </tr>
                      )}
                    </>
                  )
                })}
              </tbody>
            </table>
          </div>
          {hasMore && (
            <div class="logs-load-more">
              <button type="button" class="logs-load-more-btn" onClick={handleLoadMore}>
                {t('logs.loadMore')}
              </button>
            </div>
          )}
        </>
      )}
    </section>
  )
}

export const LogsTab = memo(LogsTabComponent)
