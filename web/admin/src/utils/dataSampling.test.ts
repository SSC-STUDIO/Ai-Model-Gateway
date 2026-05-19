import { describe, expect, it } from 'vitest'
import { lttbSampling, minMaxSampling, sampleDataPoints, type DataPoint } from './dataSampling'

function makePoints(length: number): DataPoint[] {
  return Array.from({ length }, (_, index) => ({
    timestamp: index,
    value: index % 2 === 0 ? index : -index,
  }))
}

describe('data sampling utilities', () => {
  it('returns the original array when no sampling is needed', () => {
    const points = makePoints(3)
    expect(sampleDataPoints(points, 5)).toBe(points)
    expect(lttbSampling(points, 5)).toBe(points)
    expect(minMaxSampling(points, 5)).toBe(points)
  })

  it('averages buckets for simple downsampling', () => {
    const result = sampleDataPoints(makePoints(6), 3)

    expect(result).toEqual([
      { timestamp: 1, value: -0.5 },
      { timestamp: 3, value: -0.5 },
      { timestamp: 5, value: -0.5 },
    ])
  })

  it('preserves endpoints with lttb sampling', () => {
    const points = makePoints(20)
    const result = lttbSampling(points, 6)

    expect(result).toHaveLength(6)
    expect(result[0]).toBe(points[0])
    expect(result[result.length - 1]).toBe(points[points.length - 1])
  })

  it('includes min and max values per bucket in timestamp order', () => {
    const result = minMaxSampling([
      { timestamp: 1, value: 5 },
      { timestamp: 2, value: -2 },
      { timestamp: 3, value: 9 },
      { timestamp: 4, value: 1 },
      { timestamp: 5, value: -7 },
      { timestamp: 6, value: 3 },
    ], 4)

    expect(result).toEqual([
      { timestamp: 2, value: -2 },
      { timestamp: 3, value: 9 },
      { timestamp: 5, value: -7 },
      { timestamp: 6, value: 3 },
    ])
  })
})
