import { Fragment, memo, useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { DataResponse, ErrorEntry, RequestEntry } from '../../types'
import { formatAbsoluteTime, formatInteger, formatRelativeTime } from '../../utils/formatting'
import { copyText } from '../../utils/clipboard'
import { downloadCsv } from '../../utils/csv'
import { generatePageNumbers } from '../../utils/pagination'
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

const PAGE_SIZES = [10, 25, 50, 100] as const

const LOGS_WINDOW_OPTIONS = [
  { value: '6', label: '6h' },
  { value: '24', label: '24h' },
  { value: '168', label: '7d' },
  { value: '720', label: '30d' },
  { value: 'all', label: 'All' },
]

type StatusTone = 'success' | 'warning' | 'error' | 'neutral'

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

function statusLabel(status: number | null | undefined): string {
  const text = statusText(status)
  return text === '-' ? text : `HTTP ${text}`
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
  const index = new Map<string, UnifiedLogEntry>()
  let id = 0

  const makeKey = (timestamp: string, model: string, upstream: string) =>
    `${timestamp}\x00${model}\x00${upstream}`

  if (requests) {
    for (const req of requests) {
      const timestamp = req.Timestamp ?? req.time ?? ''
      const model = req.Model ?? req.model ?? '-'
      const upstream = req.Upstream ?? req.upstream ?? '-'
      const entry: UnifiedLogEntry = {
        id: id++,
        timestamp,
        path: req.Path ?? req.path ?? '-',
        model,
        upstream,
        statusCode: req.StatusCode ?? req.status ?? 0,
        latencyMs: req.LatencyMs ?? req.latency_ms ?? 0,
        inputTokens: req.InputTokens ?? req.input_tokens,
        outputTokens: req.OutputTokens ?? req.output_tokens,
        cachedPromptTokens: req.CachedPromptTokens,
        attempts: req.Attempts ?? req.attempts,
        errorMessage: undefined,
        isError: false,
      }
      result.push(entry)
      index.set(makeKey(timestamp, model, upstream), entry)
    }
  }

  if (errors) {
    for (const err of errors) {
      const timestamp = err.Timestamp ?? err.time ?? ''
      const model = err.Model ?? err.model ?? '-'
      const upstream = err.Upstream ?? err.upstream ?? '-'
      const existing = index.get(makeKey(timestamp, model, upstream))
      if (existing) {
        const errStatus = err.StatusCode ?? err.status ?? 0
        const errMsg = err.Message ?? err.message ?? ''
        if (!existing.isError) {
          // Merge error into matching request entry
          existing.errorMessage = errMsg
          existing.isError = true
          // Update status code if error provides a non-zero one
          if (errStatus > 0) existing.statusCode = errStatus
        } else if (errMsg) {
          // Append subsequent error messages for duplicate keys
          existing.errorMessage = existing.errorMessage
            ? `${existing.errorMessage}; ${errMsg}`
            : errMsg
        }
      } else {
        const errStatus = err.StatusCode ?? err.status
        result.push({
          id: id++,
          timestamp,
          path: '-',
          model,
          upstream,
          statusCode: errStatus && errStatus > 0 ? errStatus : 500,
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

function buildLogCsvRows(entries: UnifiedLogEntry[]) {
  return [
    [
      'time',
      'path',
      'model',
      'upstream',
      'status',
      'latency_ms',
      'input_tokens',
      'output_tokens',
      'cached_prompt_tokens',
      'attempts',
      'error',
    ],
    ...entries.map((entry) => [
      entry.timestamp,
      entry.path,
      entry.model,
      entry.upstream,
      statusLabel(entry.statusCode),
      Number.isFinite(entry.latencyMs) ? entry.latencyMs.toFixed(1) : '',
      entry.inputTokens,
      entry.outputTokens,
      entry.cachedPromptTokens,
      entry.attempts ?? 1,
      entry.errorMessage,
    ]),
  ]
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
  const [copiedRow, setCopiedRow] = useState<number | null>(null)
  const [copyErrorRow, setCopyErrorRow] = useState<number | null>(null)

  // UI page navigation
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState<number>(50)

  // Reset page-local state when telemetry prop changes (hours change / new data)
  useEffect(() => {
    setCurrentPage(1)
    setExpandedRow(null)
    setCopiedRow(null)
    setCopyErrorRow(null)
  }, [telemetry?.requests, telemetry?.errors, telemetry?.total])

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

  // Page-based navigation
  const totalPages = useMemo(() => Math.max(1, Math.ceil(filteredEntries.length / pageSize)), [filteredEntries, pageSize])
  const safePage = Math.max(1, Math.min(currentPage, totalPages))
  const startIndex = (safePage - 1) * pageSize
  const endIndex = Math.min(startIndex + pageSize, filteredEntries.length)

  const visibleEntries = useMemo(() => {
    return filteredEntries.slice(startIndex, endIndex)
  }, [filteredEntries, startIndex, endIndex])

  const buildLogDetailText = useCallback((entry: UnifiedLogEntry) => {
    return [
      `${t('logs.detailTime')}: ${entry.timestamp || '-'}`,
      `${t('logs.detailPath')}: ${entry.path || '-'}`,
      `${t('logs.detailModel')}: ${entry.model || '-'}`,
      `${t('logs.detailUpstream')}: ${entry.upstream || '-'}`,
      `${t('logs.detailStatus')}: ${statusLabel(entry.statusCode)}`,
      `${t('logs.detailLatency')}: ${formatLatency(entry.latencyMs)}`,
      `${t('logs.detailInputTokens')}: ${formatInteger(entry.inputTokens)}`,
      `${t('logs.detailOutputTokens')}: ${formatInteger(entry.outputTokens)}`,
      `${t('logs.detailCachedTokens')}: ${formatInteger(entry.cachedPromptTokens)}`,
      `${t('logs.detailAttempts')}: ${entry.attempts ?? 1}`,
      ...(entry.errorMessage ? [`${t('logs.detailError')}: ${entry.errorMessage}`] : []),
    ].join('\n')
  }, [t])

  const handleRowClick = useCallback(
    (id: number) => {
      setExpandedRow((prev) => (prev === id ? null : id))
    },
    []
  )

  const handleClearSearch = useCallback(() => {
    setSearchQuery('')
  }, [])

  const handleExportCsv = useCallback(() => {
    if (filteredEntries.length === 0) return
    const scope = logType === 'all' ? 'all' : logType
    const windowLabel = hours === 'all' ? 'all' : `${hours}h`
    const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')
    downloadCsv(`ai-gateway-logs-${scope}-${windowLabel}-${stamp}.csv`, buildLogCsvRows(filteredEntries))
  }, [filteredEntries, hours, logType])

  const handleCopyDetails = useCallback(async (entry: UnifiedLogEntry) => {
    try {
      await copyText(buildLogDetailText(entry))
      setCopiedRow(entry.id)
      setCopyErrorRow(null)
      window.setTimeout(() => {
        setCopiedRow((current) => (current === entry.id ? null : current))
      }, 2000)
    } catch (err) {
      console.error('Failed to copy log details:', err)
      setCopyErrorRow(entry.id)
      setCopiedRow(null)
      window.setTimeout(() => {
        setCopyErrorRow((current) => (current === entry.id ? null : current))
      }, 3000)
    }
  }, [buildLogDetailText])

  const handlePageChange = useCallback((page: number) => {
    const targetPage = Math.max(1, Math.min(page, totalPages))
    setCurrentPage(targetPage)
    setExpandedRow(null)
  }, [totalPages])

  const handlePageSizeChange = useCallback((size: number) => {
    setPageSize(size)
    setCurrentPage(1)
  }, [])

  const handleHoursChange = useCallback((val: string) => {
    setCurrentPage(1)
    setExpandedRow(null)
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
              aria-label={t('logs.clearSearch')}
            >
              ×
            </button>
          )}
        </div>
        <div class="logs-toolbar-actions">
          <button
            type="button"
            class="secondary logs-export-btn"
            onClick={handleExportCsv}
            disabled={filteredEntries.length === 0}
            title={filteredEntries.length === 0 ? t('logs.exportDisabled') : t('logs.exportCsv')}
          >
            <Icon name="download" />
            {t('logs.exportCsv')}
          </button>
        </div>
      </div>

      <div class="logs-stats">
        <span class="logs-stat">
          {totalPages > 1 ? (
            <>{t('logs.showing')}: <strong>{formatInteger(startIndex + 1)}</strong>-<strong>{formatInteger(endIndex)}</strong> / {formatInteger(filteredEntries.length)} {t('logs.entries')}</>
          ) : (
            <>{t('logs.showing')}: <strong>{formatInteger(filteredEntries.length)}</strong> {t('logs.entries')}</>
          )}
        </span>
        <span class="logs-paginator-size">
          <span>{t('logs.perPage') || 'Per page'}:</span>
          <select value={pageSize} onChange={(e) => handlePageSizeChange(Number((e.currentTarget as HTMLSelectElement).value))}>
            {PAGE_SIZES.map((size) => (
              <option key={size} value={size}>{size}</option>
            ))}
          </select>
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
                  <th class="logs-action-col">{t('logs.colDetails')}</th>
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
                  const hasError = entry.isError || entry.statusCode >= 500
                  const hasWarning = !hasError && entry.statusCode >= 400
                  const tone = statusTone(entry.statusCode)

                  return (
                    <Fragment key={entry.id}>
                      <tr class={`data-row logs-row${hasError ? ' error-row' : ''}${hasWarning ? ' warning-row' : ''}${isExpanded ? ' expanded' : ''}`}>
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
                          <span class={`status-dot ${tone}`} title={statusLabel(entry.statusCode)} aria-hidden="true" />
                          <span class={`status-badge ${tone} ${tone === 'error' ? 'status-badge-strong' : ''}`}>
                            {statusLabel(entry.statusCode)}
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
                            <button
                              type="button"
                              class="logs-error-preview"
                              title={entry.errorMessage}
                              onClick={() => handleRowClick(entry.id)}
                              aria-label={isExpanded ? t('logs.collapseDetails') : t('logs.expandDetails')}
                              aria-expanded={isExpanded}
                            >
                              {entry.errorMessage.length > 40
                                ? `${entry.errorMessage.slice(0, 40)}…`
                                : entry.errorMessage}
                            </button>
                          ) : (
                            '-'
                          )}
                        </td>
                        <td class="logs-action-cell">
                          <button
                            type="button"
                            class={`icon-btn logs-detail-toggle${isExpanded ? ' active' : ''}`}
                            onClick={() => handleRowClick(entry.id)}
                            aria-label={isExpanded ? t('logs.collapseDetails') : t('logs.expandDetails')}
                            aria-expanded={isExpanded}
                            title={isExpanded ? t('logs.collapseDetails') : t('logs.expandDetails')}
                          >
                            <span aria-hidden="true">{isExpanded ? '⌃' : '⌄'}</span>
                          </button>
                        </td>
                      </tr>
                      {isExpanded && (
                        <tr class="logs-detail-row">
                          <td colSpan={9}>
                            <div class="logs-detail">
                              <div class="logs-detail-header">
                                <span class="logs-detail-title">{t('logs.detailTitle')}</span>
                                <div class="logs-detail-actions">
                                  {copyErrorRow === entry.id && (
                                    <span class="logs-copy-feedback error">{t('logs.copyFailed')}</span>
                                  )}
                                  <button
                                    type="button"
                                    class={`secondary logs-copy-btn${copiedRow === entry.id ? ' copied' : ''}`}
                                    onClick={() => { void handleCopyDetails(entry) }}
                                  >
                                    <Icon name={copiedRow === entry.id ? 'check' : 'copy'} size={16} />
                                    {copiedRow === entry.id ? t('logs.copiedDetails') : t('logs.copyDetails')}
                                  </button>
                                </div>
                              </div>
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
                                  <span class="logs-detail-value">{statusLabel(entry.statusCode)}</span>
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
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </div>
          <div class="logs-paginator">
            <div class="logs-paginator-info">
              <span>{t('logs.showing')}: <strong>{formatInteger(startIndex + 1)}</strong>-<strong>{formatInteger(endIndex)}</strong>, {t('logs.total')} <strong>{formatInteger(filteredEntries.length)}</strong></span>
              {totalPages > 1 && <span class="logs-paginator-sep">{t('logs.totalPages')}: <strong>{formatInteger(totalPages)}</strong></span>}
            </div>
            {totalPages > 1 && (
            <div class="logs-paginator-nav">
              <button type="button" class="logs-page-btn" onClick={() => handlePageChange(safePage - 1)} disabled={safePage <= 1} aria-label={t('logs.prevPage') || 'Previous'}>
                ‹
              </button>
              {generatePageNumbers(safePage, totalPages).map((page, idx) =>
                page === '...' ? (
                  <span key={`ellipsis-${idx}`} class="logs-page-ellipsis">…</span>
                ) : (
                  <button
                    key={page}
                    type="button"
                    class={`logs-page-btn${safePage === page ? ' active' : ''}`}
                    onClick={() => handlePageChange(page as number)}
                  >
                    {page}
                  </button>
                )
              )}
              <button type="button" class="logs-page-btn" onClick={() => handlePageChange(safePage + 1)} disabled={safePage >= totalPages} aria-label={t('logs.nextPage') || 'Next'}>
                ›
              </button>
            </div>
            )}
            <div class="logs-paginator-size">
              <span>{t('logs.perPage') || 'Per page'}:</span>
              <select value={pageSize} onChange={(e) => handlePageSizeChange(Number((e.currentTarget as HTMLSelectElement).value))}>
                {PAGE_SIZES.map((size) => (
                  <option key={size} value={size}>{size}</option>
                ))}
              </select>
            </div>
          </div>
        </>
      )}
    </section>
  )
}

export const LogsTab = memo(LogsTabComponent)
