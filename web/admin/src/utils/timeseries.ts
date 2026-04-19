import type { TimeSeriesBucket } from '../types'
import type { DataPoint } from './dataSampling'
import { isFiniteNumber, sanitizeDataPoints } from './charting'

const DEFAULT_FALLBACK_PERIOD_MS = 60_000

function parseBucketTimestamp(bucket: string, fallbackTimestamp: number): number {
  const parsed = Date.parse(bucket)
  return Number.isFinite(parsed) ? parsed : fallbackTimestamp
}

function getFallbackPeriodMs(periodMs?: number): number {
  return isFiniteNumber(periodMs) && periodMs > 0 ? periodMs : DEFAULT_FALLBACK_PERIOD_MS
}

export function bucketsToDataPoints(
  points: TimeSeriesBucket[],
  accessor: (bucket: TimeSeriesBucket) => number,
  periodMs?: number
): DataPoint[] {
  const safePeriodMs = getFallbackPeriodMs(periodMs)
  const anchor = Date.now()

  return sanitizeDataPoints(
    points.map((bucket, index) => {
      const fallbackTimestamp = anchor - (points.length - 1 - index) * safePeriodMs
      const value = accessor(bucket)
      return {
        timestamp: parseBucketTimestamp(bucket.Bucket, fallbackTimestamp),
        value: isFiniteNumber(value) ? value : 0,
      }
    })
  )
}

export function bucketsToRateDataPoints(
  points: TimeSeriesBucket[],
  numerator: (bucket: TimeSeriesBucket) => number,
  denominator: (bucket: TimeSeriesBucket) => number,
  periodMs?: number
): DataPoint[] {
  return bucketsToDataPoints(
    points,
    (bucket) => {
      const total = denominator(bucket)
      if (!isFiniteNumber(total) || total <= 0) return 0
      const value = numerator(bucket)
      return isFiniteNumber(value) ? (value / total) * 100 : 0
    },
    periodMs
  )
}
