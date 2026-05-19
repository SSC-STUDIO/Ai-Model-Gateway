import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { bucketsToDataPoints, bucketsToRateDataPoints } from './timeseries'
import type { TimeSeriesBucket } from '../types'

function bucket(overrides: Partial<TimeSeriesBucket>): TimeSeriesBucket {
  return {
    Bucket: '2026-05-19T08:00:00Z',
    Requests: 0,
    Successes: 0,
    Failures: 0,
    AvgLatencyMs: 0,
    InputTokens: 0,
    OutputTokens: 0,
    ...overrides,
  }
}

describe('timeseries utilities', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-05-19T08:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('parses valid buckets, falls back for invalid buckets, and sorts by timestamp', () => {
    const points = bucketsToDataPoints(
      [
        bucket({ Bucket: 'bad-bucket', Requests: 12 }),
        bucket({ Bucket: '2026-05-19T07:58:00Z', Requests: 5 }),
      ],
      (item) => item.Requests,
      60_000
    )

    expect(points).toEqual([
      { timestamp: Date.parse('2026-05-19T07:58:00Z'), value: 5 },
      { timestamp: Date.parse('2026-05-19T07:59:00Z'), value: 12 },
    ])
  })

  it('normalizes invalid values to zero', () => {
    const points = bucketsToDataPoints(
      [bucket({ Requests: Number.NaN })],
      (item) => item.Requests
    )

    expect(points).toEqual([{ timestamp: Date.parse('2026-05-19T08:00:00Z'), value: 0 }])
  })

  it('converts numerator and denominator accessors to percentages', () => {
    const points = bucketsToRateDataPoints(
      [
        bucket({ Bucket: '2026-05-19T07:59:00Z', Successes: 8, Requests: 10 }),
        bucket({ Bucket: '2026-05-19T08:00:00Z', Successes: 1, Requests: 0 }),
      ],
      (item) => item.Successes,
      (item) => item.Requests
    )

    expect(points).toEqual([
      { timestamp: Date.parse('2026-05-19T07:59:00Z'), value: 80 },
      { timestamp: Date.parse('2026-05-19T08:00:00Z'), value: 0 },
    ])
  })
})
