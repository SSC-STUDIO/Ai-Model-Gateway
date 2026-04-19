import { memo, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import { useFlashValue } from '../../hooks'
import { LineChart, DonutChart } from '../Charts'
import type {
  DataResponse,
  DonutEntry,
  PricingCost,
  PricingCurrencySummary,
  PricingModelSummary,
  PricingSummary,
  TimeSeriesResponse,
} from '../../types'
import { bucketsToDataPoints, bucketsToRateDataPoints } from '../../utils/timeseries'
import { formatUsd, formatInteger } from '../../utils/formatting'

interface TelemetryTabProps {
  telemetry: DataResponse | null
  timeseries: TimeSeriesResponse | null
  hours: string
  onHoursChange: (hours: string) => void
}

const TELEMETRY_WINDOW_OPTIONS = [
  { value: '24', label: '24h' },
  { value: '168', label: '7d' },
  { value: '720', label: '30d' },
  { value: 'all', label: 'All' },
]

type StatusTone = 'success' | 'warning' | 'error' | 'neutral'

function normalizeCurrency(currency: string | null | undefined): string {
  return currency && currency.trim() ? currency.trim().toUpperCase() : 'USD'
}

function formatMoney(value: number | null | undefined, currency: string | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'

  const normalized = normalizeCurrency(currency)
  if (normalized === 'USD') return formatUsd(value)

  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: normalized,
      maximumFractionDigits: value >= 1000 ? 1 : 2,
    }).format(value)
  } catch {
    return `${value.toFixed(2)} ${normalized}`
  }
}

function costCurrency(cost: PricingCost | null | undefined, pricingCurrency?: string | null): string {
  return normalizeCurrency(cost?.currency ?? pricingCurrency)
}

function costTotal(cost: PricingCost | null | undefined): number {
  if (typeof cost?.total === 'number' && Number.isFinite(cost.total)) return cost.total
  if (typeof cost?.total_usd === 'number' && Number.isFinite(cost.total_usd)) return cost.total_usd

  const prompt = typeof cost?.prompt === 'number' && Number.isFinite(cost.prompt)
    ? cost.prompt
    : cost?.prompt_usd
  const completion = typeof cost?.completion === 'number' && Number.isFinite(cost.completion)
    ? cost.completion
    : cost?.completion_usd

  return (prompt ?? 0) + (completion ?? 0)
}

function primaryPricingTotals(summary: PricingSummary | undefined): PricingCurrencySummary[] {
  if (!summary) return []
  if (Array.isArray(summary.totals_by_currency) && summary.totals_by_currency.length > 0) {
    return summary.totals_by_currency
  }

  const total = typeof summary.total === 'number' && Number.isFinite(summary.total)
    ? summary.total
    : summary.total_usd ?? 0
  const prompt = typeof summary.prompt === 'number' && Number.isFinite(summary.prompt)
    ? summary.prompt
    : summary.prompt_usd ?? 0
  const completion = typeof summary.completion === 'number' && Number.isFinite(summary.completion)
    ? summary.completion
    : summary.completion_usd ?? 0
  const cacheSavings = typeof summary.cache_savings === 'number' && Number.isFinite(summary.cache_savings)
    ? summary.cache_savings
    : summary.cache_savings_usd ?? 0

  if (total <= 0 && prompt <= 0 && completion <= 0 && cacheSavings <= 0) return []

  return [{
    currency: normalizeCurrency(summary.currency),
    prompt,
    completion,
    total,
    cache_savings: cacheSavings,
    priced_models: summary.priced_models,
  }]
}

function formatLatency(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? `${value.toFixed(1)}ms` : '-'
}

function formatTimestamp(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
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

function joinSecondary(parts: Array<string | null | undefined>): string | undefined {
  const filtered = parts.filter((part): part is string => Boolean(part && part.trim().length > 0))
  return filtered.length > 0 ? filtered.join(' · ') : undefined
}

interface CellStackProps {
  primary: string
  secondary?: string
  title?: string
  mono?: boolean
}

function CellStack({ primary, secondary, title, mono = false }: CellStackProps) {
  return (
    <div class={`table-cell-stack${mono ? ' mono' : ''}`} title={title}>
      <span class="table-cell-primary">{primary}</span>
      {secondary && <span class="table-cell-secondary">{secondary}</span>}
    </div>
  )
}

function pricingToDonut(models: PricingModelSummary[], currency: string): DonutEntry[] {
  const colors = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
  if (!Array.isArray(models)) return []
  return models
    .filter((m) => costCurrency(m?.cost, m?.pricing?.currency) === currency && costTotal(m?.cost) > 0)
    .slice(0, 12)
    .map((m, i) => ({
      label: m?.display_model ?? 'unknown',
      value: costTotal(m?.cost),
      color: colors[i % colors.length],
    }))
}

function modelsToDonut(
  models: DataResponse['models'],
  upstreams: DataResponse['upstreams']
): DonutEntry[] {
  const colors = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
  const entries: DonutEntry[] = []

  if (models) {
    models.forEach((item) => {
      entries.push({
        label: `Model:${item.value}`,
        value: item.requests,
        color: colors[entries.length % colors.length],
      })
    })
  }

  if (upstreams) {
    upstreams.forEach((item) => {
      entries.push({
        label: `Upstream:${item.value}`,
        value: item.requests,
        color: colors[entries.length % colors.length],
      })
    })
  }

  return entries.filter((e) => e.value > 0)
}

const TelemetryTabComponent = ({ telemetry, timeseries, hours, onHoursChange }: TelemetryTabProps) => {
  const { t } = useI18n()

  const bucketMinutes = timeseries?.bucket_minutes ?? 1
  const periodMs = bucketMinutes * 60000

  const requestsData = useMemo(() => {
    if (!timeseries?.points) return []
    return bucketsToDataPoints(timeseries.points, (bucket) => bucket.Requests, periodMs)
  }, [timeseries?.points, periodMs])

  const latencyData = useMemo(() => {
    if (!timeseries?.points) return []
    return bucketsToDataPoints(timeseries.points, (bucket) => bucket.AvgLatencyMs, periodMs)
  }, [timeseries?.points, periodMs])

  const successRateData = useMemo(() => {
    if (!timeseries?.points) return []
    return bucketsToRateDataPoints(timeseries.points, (bucket) => bucket.Successes, (bucket) => bucket.Requests, periodMs)
  }, [timeseries?.points, periodMs])

  const distributionData = useMemo(() => {
    return modelsToDonut(telemetry?.models, telemetry?.upstreams)
  }, [telemetry?.models, telemetry?.upstreams])

  const errors = useMemo(() => telemetry?.errors?.slice(0, 20) ?? [], [telemetry?.errors])
  const requests = useMemo(() => telemetry?.requests?.slice(0, 30) ?? [], [telemetry?.requests])
  const pricingModels = useMemo(
    () => telemetry?.pricing_economics?.models?.slice(0, 20) ?? [],
    [telemetry?.pricing_economics?.models]
  )
  const pricingTotals = useMemo(
    () => primaryPricingTotals(telemetry?.pricing_economics?.summary),
    [telemetry?.pricing_economics?.summary]
  )
  const pricingCharts = useMemo(() => {
    if (!telemetry?.pricing_economics?.models || pricingTotals.length === 0) return []
    return pricingTotals
      .map((total) => ({
        currency: total.currency,
        data: pricingToDonut(telemetry.pricing_economics?.models ?? [], total.currency),
      }))
      .filter((group) => group.data.length > 0)
  }, [pricingTotals, telemetry?.pricing_economics?.models])

  const hasData = useMemo(() => {
    if (!telemetry) return false
    return (errors.length > 0 || requests.length > 0 ||
      (telemetry.models && telemetry.models.length > 0) ||
      (telemetry.upstreams && telemetry.upstreams.length > 0) ||
      pricingModels.length > 0 ||
      pricingTotals.length > 0)
  }, [telemetry, errors, pricingModels.length, pricingTotals.length, requests])

  if (!telemetry || !timeseries) {
    return (
      <section class="panel">
        <h2>{t('telemetry.title')}</h2>
        <div class="skeleton-grid" style={{ marginTop: '20px' }}>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
        </div>
        <div class="skeleton-grid-2" style={{ marginTop: '20px' }}>
          <div class="skeleton chart-skeleton" />
          <div class="skeleton chart-skeleton" />
          <div class="skeleton chart-skeleton" />
          <div class="skeleton chart-skeleton" />
        </div>
      </section>
    )
  }

  if (!hasData) {
    return (
      <section class="panel">
        <h2>{t('telemetry.title')}</h2>
        <div class="empty-state-box">
          <div class="empty-state-icon">📊</div>
          <p class="empty-state-title">{t('empty.noTelemetry')}</p>
          <p class="empty-state-hint">{t('empty.noTelemetry')}</p>
        </div>
      </section>
    )
  }

  return (
    <section class="panel">
      <div class="timeseries-header">
        <h2>{t('telemetry.title')}</h2>
        <div class="timeseries-controls">
          <div class="timeseries-selector">
            <span>{t('timeseries.timeRange')}:</span>
            {TELEMETRY_WINDOW_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                class={`ts-btn${hours === opt.value ? ' active' : ''}`}
                onClick={() => onHoursChange(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {telemetry.summary && (
        <div class="panel-subsection telemetry-summary-strip">
          <SummaryMetrics summary={telemetry.summary} t={t} />
        </div>
      )}

      <div class="charts-grid panel-stagger">
        <LineChart
          data={requestsData}
          title={t('telemetry.requestsPerMinute')}
          color="#3b82f6"
          unit=" req"
        />
        <LineChart
          data={latencyData}
          title={t('telemetry.latency')}
          color="#22c55e"
          unit=" ms"
        />
        <LineChart
          data={successRateData}
          title={t('telemetry.successRate')}
          color="#f59e0b"
          unit="%"
        />
        <DonutChart
          data={distributionData}
          title={t('telemetry.providerDistribution')}
          singleRowLegend={distributionData.length > 4}
        />
      </div>

      {errors.length > 0 && (
        <div class="panel-subsection">
          <h3>{t('telemetry.recentErrors')}</h3>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('telemetry.errorTime')}</th>
                  <th>{t('telemetry.errorUpstream')}</th>
                  <th>{t('telemetry.errorModel')}</th>
                  <th>{t('telemetry.errorStatus')}</th>
                  <th>{t('telemetry.errorMessage')}</th>
                  <th>{t('telemetry.errorCount')}</th>
                </tr>
              </thead>
              <tbody>
                {errors.map((err, idx) => {
                  const errorStatus = err.StatusCode ?? err.status
                  const message = (err.Message ?? err.message ?? '').trim()
                  return (
                    <tr key={idx} class="data-row">
                      <td>
                        <CellStack primary={formatTimestamp(err.Timestamp ?? err.time)} />
                      </td>
                      <td>{err.Upstream ?? err.upstream ?? '-'}</td>
                      <td>{err.Model ?? err.model ?? '-'}</td>
                      <td>
                        <span class={`status-badge ${statusTone(errorStatus)}`}>{statusText(errorStatus)}</span>
                      </td>
                      <td class="error-message">
                        <CellStack
                          primary={message ? `${message.slice(0, 100)}${message.length > 100 ? '…' : ''}` : '-'}
                          title={message || undefined}
                        />
                      </td>
                      <td>
                        <span class="status-badge neutral">{formatInteger(err.count ?? err.Attempts ?? 1)}</span>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {requests.length > 0 && (
        <div class="panel-subsection">
          <h3>{t('telemetry.recentRequests')}</h3>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('telemetry.requestTime')}</th>
                  <th>{t('telemetry.requestMethod')}</th>
                  <th>{t('telemetry.requestPath')}</th>
                  <th>{t('telemetry.requestStatus')}</th>
                  <th>{t('telemetry.requestUpstream')}</th>
                  <th>{t('telemetry.requestModel')}</th>
                  <th>{t('telemetry.requestAttempts')}</th>
                  <th>{t('telemetry.requestLatency')}</th>
                  <th>{t('telemetry.requestTokens')}</th>
                </tr>
              </thead>
              <tbody>
                {requests.map((req, idx) => {
                  const status = req.StatusCode ?? req.status ?? 0
                  const inputTk = req.InputTokens ?? req.input_tokens
                  const outputTk = req.OutputTokens ?? req.output_tokens
                  const cachedPromptTokens = req.CachedPromptTokens ?? 0
                  const isCached = (req.CachedPromptTokens ?? 0) > 0 || req.cached
                  const method = req.Path ? 'POST' : req.method ?? 'POST'
                  const path = req.Path ?? req.path ?? '-'
                  const attemptCount = req.Attempts ?? req.attempts
                  const tokenSummary = inputTk != null || outputTk != null
                    ? `${formatInteger(inputTk)} / ${formatInteger(outputTk)}`
                    : '-'
                  const tokenMeta = joinSecondary([
                    isCached ? 'cached' : undefined,
                    cachedPromptTokens > 0 ? formatInteger(cachedPromptTokens) : undefined,
                  ])
                  return (
                    <tr key={idx} class="data-row">
                      <td>
                        <CellStack primary={formatTimestamp(req.Timestamp ?? req.time)} />
                      </td>
                      <td>
                        <span class="status-badge neutral">{method}</span>
                      </td>
                      <td class="path-cell">
                        <CellStack
                          primary={path}
                          secondary={isCached ? 'cached' : undefined}
                          title={path}
                        />
                      </td>
                      <td>
                        <span class={`status-badge ${statusTone(status)}`}>{statusText(status)}</span>
                      </td>
                      <td>{req.Upstream ?? req.upstream ?? '-'}</td>
                      <td>{req.Model ?? req.model ?? '-'}</td>
                      <td>
                        <span class={`status-badge ${(attemptCount ?? 1) > 1 ? 'warning' : 'neutral'}`}>
                          {formatInteger(attemptCount ?? 1)}
                        </span>
                      </td>
                      <td>
                        <CellStack
                          primary={formatLatency(req.LatencyMs ?? req.latency_ms)}
                          secondary={status >= 400 ? statusText(status) : undefined}
                          mono
                        />
                      </td>
                      <td>
                        <CellStack primary={tokenSummary} secondary={tokenMeta} mono />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {telemetry.pricing_economics && (
        <div class="panel-subsection">
          <h3>{t('telemetry.costTracking')}</h3>
          <div class="metrics-grid">
            {pricingTotals.map((total) => (
              <article key={total.currency} class="metric-card">
                <div class="metric-label">{t('telemetry.totalCost')} ({total.currency})</div>
                <div class="metric-value">{formatMoney(total.total, total.currency)}</div>
              </article>
            ))}
            {pricingTotals.length === 1 && (
              <>
                <article class="metric-card">
                  <div class="metric-label">{t('telemetry.promptCost')}</div>
                  <div class="metric-value">{formatMoney(pricingTotals[0].prompt, pricingTotals[0].currency)}</div>
                </article>
                <article class="metric-card">
                  <div class="metric-label">{t('telemetry.completionCost')}</div>
                  <div class="metric-value">{formatMoney(pricingTotals[0].completion, pricingTotals[0].currency)}</div>
                </article>
                <article class="metric-card">
                  <div class="metric-label">{t('telemetry.cacheSavings')}</div>
                  <div class="metric-value">{formatMoney(pricingTotals[0].cache_savings, pricingTotals[0].currency)}</div>
                </article>
              </>
            )}
            <article class="metric-card">
              <div class="metric-label">{t('telemetry.cachedTokens')}</div>
              <div class="metric-value">
                {(telemetry.pricing_economics.summary.cached_prompt_tokens ?? 0).toLocaleString()}
              </div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('telemetry.pricedModels')}</div>
              <div class="metric-value">
                {telemetry.pricing_economics.summary.priced_models} /{' '}
                {telemetry.pricing_economics.summary.unpriced_models}
              </div>
            </article>
          </div>

          {pricingCharts.length > 0 && (
            <div class="charts-grid" style={{ marginTop: '16px' }}>
              {pricingCharts.map((chart) => (
                <DonutChart
                  key={chart.currency}
                  data={chart.data}
                  title={`${t('telemetry.costByModel')} (${chart.currency})`}
                  singleRowLegend={chart.data.length > 4}
                />
              ))}
            </div>
          )}

          {pricingModels.length > 0 && (
            <div class="table-wrap" style={{ marginTop: '16px' }}>
              <table>
                <thead>
                  <tr>
                    <th>{t('telemetry.costModel')}</th>
                    <th>{t('telemetry.costPromptTokens')}</th>
                    <th>{t('telemetry.costCompletionTokens')}</th>
                    <th>{t('telemetry.costTotal')}</th>
                  </tr>
                </thead>
                <tbody>
                  {pricingModels.map((m, idx) => (
                    <tr key={idx} class="data-row">
                      <td>
                        <CellStack
                          primary={m?.display_model ?? 'unknown'}
                          secondary={joinSecondary([m?.effective_model, m?.upstream ?? m?.provider, m?.pricing_model])}
                          title={m?.display_model ?? 'unknown'}
                        />
                      </td>
                      <td>
                        <CellStack
                          primary={formatInteger(m?.usage?.prompt_tokens ?? 0)}
                          secondary={m?.usage?.cached_prompt_tokens ? `cached ${formatInteger(m.usage.cached_prompt_tokens)}` : undefined}
                          mono
                        />
                      </td>
                      <td>{formatInteger(m?.usage?.completion_tokens ?? 0)}</td>
                      <td class="cost-value">{formatMoney(costTotal(m?.cost), costCurrency(m?.cost, m?.pricing?.currency))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

function SummaryMetrics({ summary, t }: { summary: NonNullable<DataResponse['summary']>; t: (k: string) => string }) {
  const requestsFlash = useFlashValue(summary.requests)
  const successesFlash = useFlashValue(summary.successes)
  const failuresFlash = useFlashValue(summary.failures)
  const latencyFlash = useFlashValue(summary.avg_latency_ms)

  return (
    <div class="metrics-grid panel-stagger">
      <article class="metric-card">
        <div class="metric-label">{t('overview.requests')}</div>
        <div class={`metric-value ${requestsFlash ? 'flash' : ''}`}>{formatInteger(summary.requests)}</div>
      </article>
      <article class="metric-card">
        <div class="metric-label">{t('overview.successes')}</div>
        <div class={`metric-value ${successesFlash ? 'flash' : ''}`}>{formatInteger(summary.successes)}</div>
      </article>
      <article class="metric-card">
        <div class="metric-label">{t('overview.failures')}</div>
        <div class={`metric-value ${failuresFlash ? 'flash' : ''}`}>{formatInteger(summary.failures)}</div>
      </article>
      <article class="metric-card">
        <div class="metric-label">{t('overview.avgLatency')}</div>
        <div class={`metric-value ${latencyFlash ? 'flash' : ''}`}>{formatLatency(summary.avg_latency_ms)}</div>
      </article>
    </div>
  )
}

export const TelemetryTab = memo(TelemetryTabComponent)
