import { memo, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import { useFlashValue } from '../../hooks'
import { TimeSeriesChart, DonutChart } from '../Charts'
import { Icon } from '../Icon'
import { ServiceStatePanel } from '../ServiceStatePanel'
import type { DataResponse, DonutEntry, TimeSeriesResponse } from '../../types'
import { bucketsToDataPoints, bucketsToRateDataPoints } from '../../utils/timeseries'
import { formatInteger } from '../../utils/formatting'

interface TelemetryTabProps {
  telemetry: DataResponse | null
  timeseries: TimeSeriesResponse | null
  hours: string
  onHoursChange: (hours: string) => void
  bucketMinutes?: number
  onBucketChange?: (bucket: string) => void
  telemetryStatus?: string
  telemetryError?: string
  telemetryLastCheckedAt?: string
  onRetry?: () => void
}

const TELEMETRY_WINDOW_OPTIONS = [
  { value: '24', label: '24h' },
  { value: '168', label: '7d' },
  { value: '720', label: '30d' },
  { value: 'all', label: 'All' },
]

const BUCKET_OPTIONS = [
  { value: '1', label: '1m' },
  { value: '5', label: '5m' },
  { value: '15', label: '15m' },
  { value: '60', label: '1h' },
  { value: '1440', label: '24h' },
]

function formatLatency(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? `${value.toFixed(1)}ms` : '-'
}

function modelsToDonut(models: DataResponse['models']): DonutEntry[] {
  const colors = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
  if (!models) return []
  return models
    .filter((item) => item.requests > 0)
    .map((item, index) => ({
      label: item.value,
      value: item.requests,
      color: colors[index % colors.length],
    }))
}

function upstreamsToDonut(upstreams: DataResponse['upstreams']): DonutEntry[] {
  const colors = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
  if (!upstreams) return []
  return upstreams
    .filter((item) => item.requests > 0)
    .map((item, index) => ({
      label: item.value,
      value: item.requests,
      color: colors[index % colors.length],
    }))
}

function formatStatusTime(value: string | undefined): string {
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

const TelemetryTabComponent = ({
  telemetry,
  timeseries,
  hours,
  onHoursChange,
  bucketMinutes = 1,
  onBucketChange,
  telemetryStatus,
  telemetryError,
  telemetryLastCheckedAt,
  onRetry,
}: TelemetryTabProps) => {
  const { t } = useI18n()
  const telemetryUnavailable = telemetryStatus && telemetryStatus !== 'connected'

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

  const modelDistributionData = useMemo(() => {
    return modelsToDonut(telemetry?.models)
  }, [telemetry?.models])

  const upstreamDistributionData = useMemo(() => {
    return upstreamsToDonut(telemetry?.upstreams)
  }, [telemetry?.upstreams])

  const hasData = useMemo(() => {
    if (!telemetry) return false
    const hasTimeseries = timeseries?.points && timeseries.points.length > 0
    return (hasTimeseries ||
      (telemetry.models && telemetry.models.length > 0) ||
      (telemetry.upstreams && telemetry.upstreams.length > 0))
  }, [telemetry, timeseries])

  const currentBucket = useMemo(() => {
    const bm = String(bucketMinutes)
    return BUCKET_OPTIONS.some((o) => o.value === bm) ? bm : '1'
  }, [bucketMinutes])

  if (telemetryUnavailable) {
    return (
      <section class="panel">
        <h2>{t('telemetry.title')}</h2>
        <ServiceStatePanel
          icon="telemetry"
          title={t('services.telemetryUnavailableTitle')}
          message={t('services.telemetryUnavailableMessage')}
          hint={t('services.telemetryUnavailableHint')}
          detail={telemetryError}
          actionLabel={t('common.retry')}
          onAction={onRetry}
          items={[
            { label: t('header.telemetry'), value: telemetryStatus, tone: telemetryStatus === 'error' ? 'error' : 'warning' },
            ...(telemetryLastCheckedAt ? [{ label: t('services.lastChecked'), value: formatStatusTime(telemetryLastCheckedAt) }] : []),
          ]}
        />
      </section>
    )
  }

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
        <div class="timeseries-charts" style={{ marginTop: '20px' }}>
          <div class="skeleton chart-skeleton" />
          <div class="skeleton chart-skeleton" />
          <div class="skeleton chart-skeleton" />
        </div>
        <div class="charts-grid" style={{ marginTop: '20px' }}>
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
          <div class="empty-state-icon"><Icon name="chart" size={30} /></div>
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
            <span>{t('timeseries.bucketSize')}:</span>
            {BUCKET_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                class={`ts-btn${currentBucket === opt.value ? ' active' : ''}`}
                onClick={() => onBucketChange?.(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>
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

      <div class="timeseries-charts panel-stagger">
        <TimeSeriesChart
          data={requestsData}
          title={t('telemetry.requests')}
          color="#3b82f6"
          unit=" req"
        />
        <TimeSeriesChart
          data={latencyData}
          title={t('telemetry.latency')}
          color="#f59e0b"
          unit="ms"
        />
        <TimeSeriesChart
          data={successRateData}
          title={t('telemetry.successRate')}
          color="#22c55e"
          unit="%"
        />
      </div>

      <div class="charts-grid panel-stagger">
        {modelDistributionData.length > 0 && (
          <DonutChart
            data={modelDistributionData}
            title={t('telemetry.providerDistribution')}
            singleRowLegend={modelDistributionData.length > 4}
          />
        )}
        {upstreamDistributionData.length > 0 && (
          <DonutChart
            data={upstreamDistributionData}
            title={t('telemetry.upstreamDistribution')}
            singleRowLegend={upstreamDistributionData.length > 4}
          />
        )}
      </div>
    </section>
  )
}

function SummaryMetrics({ summary, t }: { summary: NonNullable<DataResponse['summary']>; t: (k: string) => string }) {
  const requestsFlash = useFlashValue(summary.requests)
  const successesFlash = useFlashValue(summary.successes)
  const failuresFlash = useFlashValue(summary.failures)
  const latencyFlash = useFlashValue(summary.avg_latency_ms)
  const totalTokens = summary.total_tokens ?? ((summary.input_tokens ?? 0) + (summary.output_tokens ?? 0))
  const tokensFlash = useFlashValue(totalTokens)

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
      <article class="metric-card">
        <div class="metric-label">{t('telemetry.totalTokens')}</div>
        <div class={`metric-value ${tokensFlash ? 'flash' : ''}`}>{formatInteger(totalTokens)}</div>
      </article>
    </div>
  )
}

export const TelemetryTab = memo(TelemetryTabComponent)
