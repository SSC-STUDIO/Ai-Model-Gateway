import { memo, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import { useFlashValue } from '../../hooks'
import type { AnyRecord, ProviderHealthView } from '../../types'
import { ServiceStatePanel } from '../ServiceStatePanel'

interface OverviewTabProps {
  overview: AnyRecord | null
  telemetryStatus?: string
  telemetryError?: string
  telemetryLastCheckedAt?: string
  onRetry?: () => void
}

const WINDOW_KEYS = ['last_1m', 'last_5m', 'last_1h', 'last_24h'] as const

type BadgeTone = 'success' | 'warning' | 'error' | 'neutral'

function metricValue(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}

function numericValue(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function formatCount(value: unknown): string {
  const num = numericValue(value)
  return num === null ? '-' : num.toLocaleString()
}

function formatLatency(value: unknown): string {
  const num = numericValue(value)
  return num === null ? '-' : `${num.toFixed(1)}ms`
}

function formatTimestamp(value: string | undefined): string {
  if (!value) return '-'
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return value
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(parsed)
}

function isProviderHealthView(value: unknown): value is ProviderHealthView {
  return Boolean(value)
    && typeof value === 'object'
    && typeof (value as ProviderHealthView).name === 'string'
    && typeof (value as ProviderHealthView).healthy === 'boolean'
}

function getWindowBadgeTone(requests: number | null, failures: number | null): BadgeTone {
  if (requests === null || requests <= 0) return 'neutral'
  if ((failures ?? 0) > 0) return 'warning'
  return 'success'
}

function getProviderTone(entry: ProviderHealthView): BadgeTone {
  switch (entry.status) {
    case 'cooldown':
      return 'warning'
    case 'unhealthy':
      return 'error'
    default:
      return 'success'
  }
}

function getProviderStatusLabel(entry: ProviderHealthView, t: (key: string) => string): string {
  switch (entry.status) {
    case 'cooldown':
      return t('overview.statusCooldown')
    case 'unhealthy':
      return t('overview.statusUnhealthy')
    default:
      return t('overview.statusHealthy')
  }
}

const OverviewTabComponent = ({
  overview,
  telemetryStatus,
  telemetryError,
  telemetryLastCheckedAt,
  onRetry,
}: OverviewTabProps) => {
  const { t } = useI18n()
  const telemetryUnavailable = telemetryStatus && telemetryStatus !== 'connected'

  const windowCards = useMemo(() => {
    return WINDOW_KEYS.map((key) => {
      const windowData = overview?.[key] as AnyRecord | undefined
      const requests = numericValue(windowData?.requests)
      const successes = numericValue(windowData?.successes)
      const failures = numericValue(windowData?.failures)
      const avgLatencyMs = numericValue(windowData?.avg_latency_ms)
      const successRate =
        requests && requests > 0 && successes !== null
          ? Math.max(0, Math.min(100, (successes / requests) * 100))
          : null

      return {
        key,
        title: t(`overview.${key}`),
        requests,
        successes,
        failures,
        avgLatencyMs,
        successRate,
        tone: getWindowBadgeTone(requests, failures),
      }
    })
  }, [overview, t])

  const runtimeEntries = useMemo(() => {
    const runtime = overview?.runtime as AnyRecord | undefined
    return runtime ? Object.entries(runtime) : []
  }, [overview])

  const availableModels = useMemo(() => {
    const models = overview?.available_models
    return Array.isArray(models) ? models : []
  }, [overview])

  const providerHealth = useMemo(() => {
    const items = overview?.provider_health
    return Array.isArray(items) ? items.filter((item): item is ProviderHealthView => isProviderHealthView(item)) : []
  }, [overview])

  const providerHealthSummary = useMemo(() => {
    const total = providerHealth.length
    const healthy = providerHealth.filter((item) => item.healthy).length
    const cooldown = providerHealth.filter((item) => item.status === 'cooldown').length
    const blocked = providerHealth.filter((item) => !item.healthy).length
    return { total, healthy, cooldown, blocked }
  }, [providerHealth])

  const hasWindowMetrics = useMemo(() => {
    if (!overview) return false
    return WINDOW_KEYS.some((key) => {
      const windowData = overview[key] as AnyRecord | undefined
      return (
        numericValue(windowData?.requests) !== null ||
        numericValue(windowData?.successes) !== null ||
        numericValue(windowData?.failures) !== null ||
        numericValue(windowData?.avg_latency_ms) !== null
      )
    })
  }, [overview])

  const totalRequests = useMemo(() => {
    if (!overview || !hasWindowMetrics) return null
    let total = 0
    for (const key of WINDOW_KEYS) {
      const w = overview[key] as AnyRecord | undefined
      if (w && typeof w.requests === 'number') total += w.requests
    }
    return total
  }, [overview, hasWindowMetrics])

  const hasRecentRequests = (totalRequests ?? 0) > 0

  if (telemetryUnavailable) {
    return (
      <section class="panel">
        <h2>{t('overview.title')}</h2>
        <ServiceStatePanel
          icon="overview"
          title={t('services.telemetryUnavailableTitle')}
          message={t('services.telemetryUnavailableMessage')}
          hint={t('services.telemetryUnavailableHint')}
          detail={telemetryError}
          actionLabel={t('common.retry')}
          onAction={onRetry}
          items={[
            { label: t('header.telemetry'), value: telemetryStatus, tone: telemetryStatus === 'error' ? 'error' : 'warning' },
            ...(telemetryLastCheckedAt ? [{ label: t('services.lastChecked'), value: formatTimestamp(telemetryLastCheckedAt) }] : []),
          ]}
        />
      </section>
    )
  }

  if (!overview || !hasWindowMetrics) {
    return (
      <section class="panel">
        <h2>{t('overview.title')}</h2>
        <div class="skeleton-grid" style={{ marginTop: '20px' }}>
          <div class="skeleton skeleton-card">
            <div style={{ padding: '18px' }}>
              <div class="skeleton skeleton-label" />
              <div class="skeleton skeleton-metric" />
            </div>
          </div>
          <div class="skeleton skeleton-card">
            <div style={{ padding: '18px' }}>
              <div class="skeleton skeleton-label" />
              <div class="skeleton skeleton-metric" />
            </div>
          </div>
          <div class="skeleton skeleton-card">
            <div style={{ padding: '18px' }}>
              <div class="skeleton skeleton-label" />
              <div class="skeleton skeleton-metric" />
            </div>
          </div>
          <div class="skeleton skeleton-card">
            <div style={{ padding: '18px' }}>
              <div class="skeleton skeleton-label" />
              <div class="skeleton skeleton-metric" />
            </div>
          </div>
        </div>
      </section>
    )
  }

  return (
    <section class="panel">
      <h2>{t('overview.title')}</h2>
      <div class="panel-subsection">
        <h3>{t('overview.windows')}</h3>
        {!hasRecentRequests && totalRequests === 0 && (
          <p class="muted overview-subtle-banner">{t('empty.noRequests')}</p>
        )}
        <div class="overview-window-grid panel-stagger">
          {windowCards.map((card) => (
            <WindowCard key={card.key} card={card} t={t} />
          ))}
        </div>
      </div>

      <div class="panel-subsection">
        <h3>{t('overview.runtime')}</h3>
        <div class="runtime-grid panel-stagger">
          {runtimeEntries.map(([key, value]) => (
            <RuntimeCard key={key} rKey={key} value={value} />
          ))}
        </div>
      </div>

      <div class="panel-subsection">
        <h3>{t('overview.availableModels')}</h3>
        <div class="config-card overview-models-card">
          {availableModels.length > 0 ? (
            <div class="model-chip-grid">
              {availableModels.map((model) => (
                <span key={String(model)} class="model-chip" title={String(model)}>
                  {String(model)}
                </span>
              ))}
            </div>
          ) : (
            <p class="muted">{t('overview.noModels')}</p>
          )}
        </div>
      </div>

      <div class="panel-subsection">
        <h3>{t('overview.providerHealth')}</h3>
        {providerHealth.length > 0 ? (
          <>
            <div class="runtime-grid panel-stagger">
              <article class="runtime-card">
                <div class="metric-label">{t('overview.providersTotal')}</div>
                <div class="runtime-value">{formatCount(providerHealthSummary.total)}</div>
              </article>
              <article class="runtime-card">
                <div class="metric-label">{t('overview.providersHealthy')}</div>
                <div class="runtime-value">{formatCount(providerHealthSummary.healthy)}</div>
              </article>
              <article class="runtime-card">
                <div class="metric-label">{t('overview.providersBlocked')}</div>
                <div class="runtime-value">{formatCount(providerHealthSummary.blocked)}</div>
              </article>
              <article class="runtime-card">
                <div class="metric-label">{t('overview.providersCooldown')}</div>
                <div class="runtime-value">{formatCount(providerHealthSummary.cooldown)}</div>
              </article>
            </div>

            <div class="table-wrap" style={{ marginTop: '16px' }}>
              <table>
                <thead>
                  <tr>
                    <th>{t('overview.providerName')}</th>
                    <th>{t('overview.providerStatus')}</th>
                    <th>{t('overview.providerFailures')}</th>
                    <th>{t('overview.providerLatency')}</th>
                    <th>{t('overview.providerLastCheck')}</th>
                    <th>{t('overview.providerCooldownUntil')}</th>
                  </tr>
                </thead>
                <tbody>
                  {providerHealth.map((entry) => (
                    <tr key={entry.name}>
                      <td>
                        <div class="table-cell-stack mono" title={entry.name}>
                          <span class="table-cell-primary">{entry.name}</span>
                          {entry.last_success && (
                            <span class="table-cell-secondary">
                              {t('overview.providerLastSuccess')}: {formatTimestamp(entry.last_success)}
                            </span>
                          )}
                        </div>
                      </td>
                      <td>
                        <span class={`status-badge ${getProviderTone(entry)}`}>
                          {getProviderStatusLabel(entry, t)}
                        </span>
                      </td>
                      <td>{formatCount(entry.consecutive_failures)}</td>
                      <td>{formatLatency(entry.latency_ms)}</td>
                      <td>{formatTimestamp(entry.last_check)}</td>
                      <td>{entry.cooldown_until ? formatTimestamp(entry.cooldown_until) : '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <p class="muted">{t('overview.providerNoData')}</p>
        )}
      </div>
    </section>
  )
}

interface WindowCardData {
  key: string
  title: string
  requests: number | null
  successes: number | null
  failures: number | null
  avgLatencyMs: number | null
  successRate: number | null
  tone: BadgeTone
}

function WindowCard({ card, t }: { card: WindowCardData; t: (k: string) => string }) {
  const requestsFlash = useFlashValue(card.requests)
  const successesFlash = useFlashValue(card.successes)
  const failuresFlash = useFlashValue(card.failures)
  const latencyFlash = useFlashValue(card.avgLatencyMs)

  return (
    <article class={`overview-window-card tone-${card.tone}`}>
      <div class="overview-window-header">
        <h3>{card.title}</h3>
        <span class={`status-badge ${card.tone}`}>
          {card.successRate === null ? '-' : `${card.successRate.toFixed(0)}%`}
        </span>
      </div>
      <div class={`overview-window-value ${requestsFlash ? 'flash' : ''}`}>{formatCount(card.requests)}</div>
      <div class="overview-window-meta">{t('overview.requests')}</div>
      <div class="overview-window-stats">
        <div class="overview-window-stat">
          <span>{t('overview.successes')}</span>
          <strong class={successesFlash ? 'flash' : ''}>{formatCount(card.successes)}</strong>
        </div>
        <div class="overview-window-stat">
          <span>{t('overview.failures')}</span>
          <strong class={failuresFlash ? 'flash' : ''}>{formatCount(card.failures)}</strong>
        </div>
        <div class="overview-window-stat">
          <span>{t('overview.avgLatency')}</span>
          <strong class={latencyFlash ? 'flash' : ''}>{formatLatency(card.avgLatencyMs)}</strong>
        </div>
      </div>
    </article>
  )
}

function RuntimeCard({ rKey, value }: { rKey: string; value: unknown }) {
  const vStr = metricValue(value)
  const flash = useFlashValue(vStr)

  return (
    <article class="runtime-card">
      <div class="metric-label">{rKey}</div>
      {typeof value === 'boolean' ? (
        <span class={`status-badge ${value ? 'success' : 'neutral'}`}>{String(value)}</span>
      ) : (
        <div class={`runtime-value ${flash ? 'flash' : ''}`} title={vStr}>
          {vStr}
        </div>
      )}
    </article>
  )
}

export const OverviewTab = memo(OverviewTabComponent)
