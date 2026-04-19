import { memo, useMemo, useState, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import { useUrlState, usePageVisibility, useCachedFetch } from '../../hooks'
import { LineChart, HistoryChart } from '../Charts'
import type { TimeSeriesBucket, TimeSeriesResponse } from '../../types'
import { bucketsToDataPoints, bucketsToRateDataPoints } from '../../utils/timeseries'
import { FULL_WINDOW_HOURS, normalizeTimeSeriesResponse } from '../../utils/controlApi'

const BUCKET_OPTIONS = [
  { value: '1', label: '1m' },
  { value: '5', label: '5m' },
  { value: '15', label: '15m' },
  { value: '60', label: '1h' },
]

const HOURS_OPTIONS = [
  { value: '1', label: '1h' },
  { value: '6', label: '6h' },
  { value: '24', label: '24h' },
  { value: '168', label: '7d' },
]

type ViewMode = 'standard' | 'history'
const HISTORY_BUCKET_MINUTES = 7 * 24 * 60

function normalizeTimeSeriesPoints(points: TimeSeriesBucket[]): TimeSeriesBucket[] {
  return [...points].sort((left, right) => {
    const leftTs = Date.parse(left.Bucket)
    const rightTs = Date.parse(right.Bucket)
    return leftTs - rightTs
  })
}

const TimeSeriesTabComponent = () => {
  const { t } = useI18n()
  const [bucket, setBucket] = useUrlState<string>('bucket', '5')
  const [hours, setHours] = useUrlState<string>('tsHours', '168')
  const [viewMode, setViewMode] = useState<ViewMode>('standard')
  const isPageVisible = usePageVisibility()

  const standardURL = useMemo(
    () => `/api/admin/timeseries?bucket=${bucket}&hours=${hours}`,
    [bucket, hours]
  )

  const { data: standardRaw, loading } = useCachedFetch<unknown>(standardURL, {
    ttl: 30000,
    enabled: viewMode === 'standard' && isPageVisible,
  })

  const { data: historyRaw, loading: historyLoading } = useCachedFetch<unknown>(
    `/api/admin/timeseries?bucket=${HISTORY_BUCKET_MINUTES}&hours=${FULL_WINDOW_HOURS}`,
    {
      ttl: 60000,
      enabled: viewMode === 'history' && isPageVisible,
    }
  )

  const localData = useMemo<TimeSeriesResponse | null>(() => {
    if (!standardRaw) return null
    const next = normalizeTimeSeriesResponse(standardRaw)
    return next ? {
      ...next,
      points: normalizeTimeSeriesPoints(next.points ?? []),
    } : null
  }, [standardRaw])

  const historyData = useMemo<TimeSeriesResponse | null>(() => {
    if (!historyRaw) return null
    const next = normalizeTimeSeriesResponse(historyRaw)
    return next ? {
      ...next,
      points: normalizeTimeSeriesPoints(next.points ?? []),
    } : null
  }, [historyRaw])

  const points = localData?.points ?? []

  const requestsData = useMemo(
    () => bucketsToDataPoints(points, (bucket) => bucket.Requests),
    [points]
  )

  const latencyData = useMemo(
    () => bucketsToDataPoints(points, (bucket) => bucket.AvgLatencyMs),
    [points]
  )

  const successRateData = useMemo(
    () => bucketsToRateDataPoints(points, (bucket) => bucket.Successes, (bucket) => bucket.Requests),
    [points]
  )

  const historyPoints = historyData?.points ?? []
  const historyRequestsData = useMemo(
    () => bucketsToDataPoints(historyPoints, (bucket) => bucket.Requests),
    [historyPoints]
  )
  const historyLatencyData = useMemo(
    () => bucketsToDataPoints(historyPoints, (bucket) => bucket.AvgLatencyMs),
    [historyPoints]
  )
  const historySuccessRateData = useMemo(
    () => bucketsToRateDataPoints(historyPoints, (bucket) => bucket.Successes, (bucket) => bucket.Requests),
    [historyPoints]
  )

  const handleBucketChange = useCallback((val: string) => {
    setBucket(val)
  }, [])

  const handleHoursChange = useCallback((val: string) => {
    setHours(val)
  }, [])

  const handleViewModeChange = useCallback((mode: ViewMode) => {
    setViewMode(mode)
  }, [])

  return (
    <section class="panel">
      <div class="timeseries-header">
        <h2>{t('timeseries.title')}</h2>

        {/* View mode switcher */}
        <div class="view-mode-tabs">
          <button
            type="button"
            class={`view-mode-btn ${viewMode === 'standard' ? 'active' : ''}`}
            onClick={() => handleViewModeChange('standard')}
          >
            {t('timeseries.standardView')}
          </button>
          <button
            type="button"
            class={`view-mode-btn ${viewMode === 'history' ? 'active' : ''}`}
            onClick={() => handleViewModeChange('history')}
          >
            {t('timeseries.historyView')}
          </button>
        </div>

        {viewMode === 'standard' && (
          <div class="timeseries-controls">
            <div class="timeseries-selector">
              <span>{t('timeseries.bucketSize')}:</span>
              {BUCKET_OPTIONS.map((opt) => (
                <button
                  key={opt.value}
                  type="button"
                  class={`ts-btn${bucket === opt.value ? ' active' : ''}`}
                  onClick={() => handleBucketChange(opt.value)}
                >
                  {opt.label}
                </button>
              ))}
            </div>
            <div class="timeseries-selector">
              <span>{t('timeseries.timeRange')}:</span>
              {HOURS_OPTIONS.map((opt) => (
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
            {loading && <span class="muted">...</span>}
          </div>
        )}

        {viewMode === 'history' && (
          <div class="timeseries-controls">
            <span class="history-info">{t('timeseries.historyInfo')}</span>
            {historyLoading && <span class="muted">{t('timeseries.historyLoading')}</span>}
          </div>
        )}
      </div>

      {viewMode === 'standard' && (
        <>
          {loading && points.length === 0 ? (
            <div class="skeleton-grid-2" style={{ marginTop: '20px' }}>
              <div class="skeleton chart-skeleton" />
              <div class="skeleton chart-skeleton" />
              <div class="skeleton chart-skeleton" />
            </div>
          ) : points.length === 0 ? (
            <div class="empty-state-box" style={{ marginTop: '20px' }}>
              <div class="empty-state-icon">📈</div>
              <p class="empty-state-title">{t('empty.noTimeseries')}</p>
            </div>
          ) : (
            <div class="timeseries-charts panel-stagger">
              <LineChart
                data={requestsData}
                title={t('timeseries.requests')}
                color="#3b82f6"
              />
              <LineChart
                data={latencyData}
                title={t('timeseries.avgLatency')}
                color="#f59e0b"
                unit="ms"
              />
              <LineChart
                data={successRateData}
                title={t('timeseries.successRate')}
                color="#22c55e"
                unit="%"
              />
            </div>
          )}
        </>
      )}

      {viewMode === 'history' && (
        <>
          {historyLoading && historyPoints.length === 0 ? (
            <div class="skeleton-grid-2" style={{ marginTop: '20px' }}>
              <div class="skeleton chart-skeleton" />
              <div class="skeleton chart-skeleton" />
              <div class="skeleton chart-skeleton" />
            </div>
          ) : historyPoints.length === 0 ? (
            <div class="empty-state-box" style={{ marginTop: '20px' }}>
              <div class="empty-state-icon">🗓️</div>
              <p class="empty-state-title">{t('timeseries.historyEmpty')}</p>
            </div>
          ) : (
            <div class="history-charts panel-stagger">
              <HistoryChart
                data={historyRequestsData}
                title={t('timeseries.historyRequests')}
                color="#3b82f6"
                bucketDays={7}
              />
              <HistoryChart
                data={historyLatencyData}
                title={t('timeseries.historyLatency')}
                color="#f59e0b"
                unit="ms"
                bucketDays={7}
              />
              <HistoryChart
                data={historySuccessRateData}
                title={t('timeseries.historySuccessRate')}
                color="#22c55e"
                unit="%"
                bucketDays={7}
              />
            </div>
          )}
        </>
      )}
    </section>
  )
}

export const TimeSeriesTab = memo(TimeSeriesTabComponent)
