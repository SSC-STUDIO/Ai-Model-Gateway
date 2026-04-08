import { memo, useMemo, useState, useEffect, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import { LineChart } from '../Charts'
import type { TimeSeriesResponse, TimeSeriesBucket } from '../../types'
import type { DataPoint } from '../../utils/dataSampling'

interface TimeSeriesTabProps {
  timeseries: TimeSeriesResponse | null
}

function bucketToDataPoints(
  points: TimeSeriesBucket[],
  accessor: (b: TimeSeriesBucket) => number
): DataPoint[] {
  return points.map((b) => ({
    timestamp: new Date(b.Bucket).getTime(),
    value: accessor(b),
  }))
}

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

const TimeSeriesTabComponent = ({ timeseries: initialTimeseries }: TimeSeriesTabProps) => {
  const { t } = useI18n()
  const [bucket, setBucket] = useState('5')
  const [hours, setHours] = useState('1')
  const [localData, setLocalData] = useState<TimeSeriesResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const fetchTimeseries = useCallback(async (b: string, h: string) => {
    setLoading(true)
    try {
      const resp = await fetch(`/api/admin/timeseries?bucket=${b}&hours=${h}`, {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      })
      if (resp.ok) {
        setLocalData(await resp.json() as TimeSeriesResponse)
      }
    } catch {
      // keep existing data on error
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchTimeseries(bucket, hours)
  }, [bucket, hours, fetchTimeseries])

  const data = localData ?? initialTimeseries
  const points = data?.points ?? []

  const requestsData = useMemo(
    () => bucketToDataPoints(points, (b) => b.Requests),
    [points]
  )

  const latencyData = useMemo(
    () => bucketToDataPoints(points, (b) => b.AvgLatencyMs),
    [points]
  )

  const successRateData = useMemo(
    () =>
      bucketToDataPoints(points, (b) =>
        b.Requests > 0 ? (b.Successes / b.Requests) * 100 : 0
      ),
    [points]
  )

  const handleBucketChange = useCallback((val: string) => {
    setBucket(val)
  }, [])

  const handleHoursChange = useCallback((val: string) => {
    setHours(val)
  }, [])

  return (
    <section class="panel">
      <div class="timeseries-header">
        <h2>{t('timeseries.title')}</h2>
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
      </div>

      {loading && points.length === 0 ? (
        <p class="muted"><span class="loading-dots"></span> {t('timeseries.noData')}</p>
      ) : points.length === 0 ? (
        <p class="empty-state">{t('empty.noTimeseries')}</p>
      ) : (
        <div class="timeseries-charts">
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
    </section>
  )
}

export const TimeSeriesTab = memo(TimeSeriesTabComponent)
