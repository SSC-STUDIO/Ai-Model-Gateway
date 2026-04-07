import { memo, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import { LineChart, DonutChart } from '../Charts'
import type { DataResponse, TimeSeriesResponse, TimeSeriesPoint } from '../../types'

interface TelemetryTabProps {
  telemetry: DataResponse | null
  timeseries: TimeSeriesResponse | null
}

type TimeSeriesBucket = {
  Bucket: string
  Requests: number
  Successes: number
  Failures: number
  AvgLatencyMs: number
  InputTokens: number
  OutputTokens: number
}

function timeSeriesToDataPoints(
  points: TimeSeriesBucket[],
  field: 'Requests' | 'AvgLatencyMs',
  periodMs: number
): TimeSeriesPoint[] {
  return points.map((p, i) => ({
    timestamp: Date.now() - (points.length - 1 - i) * periodMs,
    value: p[field],
  }))
}

function timeSeriesToSuccessRate(points: TimeSeriesBucket[], periodMs: number): TimeSeriesPoint[] {
  return points.map((p, i) => ({
    timestamp: Date.now() - (points.length - 1 - i) * periodMs,
    value: p.Requests > 0 ? (p.Successes / p.Requests) * 100 : 0,
  }))
}

function formatUsd(value: number): string {
  if (value < 0.01 && value > 0) return '<$0.01'
  if (value >= 1000) return `$${(value / 1000).toFixed(2)}K`
  return `$${value.toFixed(2)}`
}

interface DonutEntry {
  label: string
  value: number
  color: string
}

function pricingToDonut(models: NonNullable<DataResponse['pricing_economics']>['models']): DonutEntry[] {
  const colors = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
  return models
    .filter((m) => m.cost.total_usd > 0)
    .slice(0, 8)
    .map((m, i) => ({
      label: m.display_model,
      value: m.cost.total_usd,
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

const TelemetryTabComponent = ({ telemetry, timeseries }: TelemetryTabProps) => {
  const { t } = useI18n()

  const bucketMinutes = timeseries?.bucket_minutes ?? 1
  const periodMs = bucketMinutes * 60000

  const requestsData = useMemo(() => {
    if (!timeseries?.points) return []
    return timeSeriesToDataPoints(timeseries.points, 'Requests', periodMs)
  }, [timeseries?.points, periodMs])

  const latencyData = useMemo(() => {
    if (!timeseries?.points) return []
    return timeSeriesToDataPoints(timeseries.points, 'AvgLatencyMs', periodMs)
  }, [timeseries?.points, periodMs])

  const successRateData = useMemo(() => {
    if (!timeseries?.points) return []
    return timeSeriesToSuccessRate(timeseries.points, periodMs)
  }, [timeseries?.points, periodMs])

  const distributionData = useMemo(() => {
    return modelsToDonut(telemetry?.models, telemetry?.upstreams)
  }, [telemetry?.models, telemetry?.upstreams])

  const pricingData = useMemo(() => {
    if (!telemetry?.pricing_economics?.models) return []
    return pricingToDonut(telemetry.pricing_economics.models)
  }, [telemetry?.pricing_economics?.models])

  const errors = useMemo(() => telemetry?.errors?.slice(0, 20) ?? [], [telemetry?.errors])
  const requests = useMemo(() => telemetry?.requests?.slice(0, 30) ?? [], [telemetry?.requests])
  const pricingModels = useMemo(
    () => telemetry?.pricing_economics?.models?.slice(0, 15) ?? [],
    [telemetry?.pricing_economics?.models]
  )

  if (!telemetry || !timeseries) {
    return (
      <section class="panel">
        <h2>{t('telemetry.title')}</h2>
        <p class="muted">{t('telemetry.loading')}</p>
      </section>
    )
  }

  return (
    <section class="panel">
      <h2>{t('telemetry.title')}</h2>
      <div class="charts-grid">
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
        <DonutChart data={distributionData} title={t('telemetry.providerDistribution')} />
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
                {errors.map((err, idx) => (
                  <tr key={idx}>
                    <td>{err.time}</td>
                    <td>{err.upstream}</td>
                    <td>{err.model}</td>
                    <td class={err.status >= 500 ? 'status-error' : 'status-warn'}>{err.status}</td>
                    <td class="error-message">{err.message.slice(0, 100)}</td>
                    <td>{err.count}</td>
                  </tr>
                ))}
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
                {requests.map((req, idx) => (
                  <tr key={idx}>
                    <td>{req.time}</td>
                    <td>{req.method}</td>
                    <td class="path-cell">{req.path}</td>
                    <td class={req.status >= 400 ? 'status-error' : 'status-ok'}>{req.status}</td>
                    <td>{req.upstream}</td>
                    <td>{req.model}</td>
                    <td>{req.attempts}</td>
                    <td>{req.latency_ms}ms</td>
                    <td>
                      {req.cached && <span class="cached-badge">cached</span>}
                      {req.input_tokens && <span>{req.input_tokens}</span>}
                      {req.input_tokens && req.output_tokens && <span> / {req.output_tokens}</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {telemetry.pricing_economics && (
        <div class="panel-subsection">
          <h3>{t('telemetry.costTracking')}</h3>
          <div class="metrics-grid">
            <article class="metric-card">
              <div class="metric-label">{t('telemetry.totalCost')}</div>
              <div class="metric-value">{formatUsd(telemetry.pricing_economics.summary.total_usd)}</div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('telemetry.promptCost')}</div>
              <div class="metric-value">{formatUsd(telemetry.pricing_economics.summary.prompt_usd)}</div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('telemetry.completionCost')}</div>
              <div class="metric-value">{formatUsd(telemetry.pricing_economics.summary.completion_usd)}</div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('telemetry.cacheSavings')}</div>
              <div class="metric-value">{formatUsd(telemetry.pricing_economics.summary.cache_savings_usd)}</div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('telemetry.cachedTokens')}</div>
              <div class="metric-value">
                {telemetry.pricing_economics.summary.cached_prompt_tokens.toLocaleString()}
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

          {pricingData.length > 0 && (
            <div class="charts-grid charts-grid-single" style={{ marginTop: '16px' }}>
              <DonutChart data={pricingData} title={t('telemetry.costByModel')} />
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
                    <tr key={idx}>
                      <td>{m.display_model}</td>
                      <td>{m.usage.prompt_tokens.toLocaleString()}</td>
                      <td>{m.usage.completion_tokens.toLocaleString()}</td>
                      <td class="cost-value">{formatUsd(m.cost.total_usd)}</td>
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

export const TelemetryTab = memo(TelemetryTabComponent)
