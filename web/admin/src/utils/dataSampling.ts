export interface DataPoint {
  timestamp: number
  value: number
}

const MAX_POINTS = 200

export function sampleDataPoints(points: DataPoint[], maxPoints: number = MAX_POINTS): DataPoint[] {
  if (points.length <= maxPoints) return points

  const sampled: DataPoint[] = []
  const bucketSize = points.length / maxPoints

  for (let i = 0; i < maxPoints; i++) {
    const startIdx = Math.floor(i * bucketSize)
    const endIdx = Math.floor((i + 1) * bucketSize)
    const bucket = points.slice(startIdx, endIdx)

    if (bucket.length === 0) continue

    const avgValue = bucket.reduce((sum, p) => sum + p.value, 0) / bucket.length
    const midTimestamp = bucket[Math.floor(bucket.length / 2)].timestamp

    sampled.push({
      timestamp: midTimestamp,
      value: avgValue,
    })
  }

  return sampled
}

export function lttbSampling(points: DataPoint[], threshold: number = MAX_POINTS): DataPoint[] {
  if (points.length <= threshold) return points

  const sampled: DataPoint[] = []
  let a = 0
  let maxAreaPoint: DataPoint = points[0]
  let maxArea: number
  let area: number
  let nextA = 0

  sampled.push(points[a])

  const every = (points.length - 2) / (threshold - 2)

  for (let i = 0; i < threshold - 2; i++) {
    let avgX = 0
    let avgY = 0
    let avgRangeStart = Math.floor((i + 1) * every) + 1
    let avgRangeEnd = Math.floor((i + 2) * every) + 1
    avgRangeEnd = avgRangeEnd < points.length ? avgRangeEnd : points.length
    const avgRangeLength = avgRangeEnd - avgRangeStart

    for (; avgRangeStart < avgRangeEnd; avgRangeStart++) {
      avgX += points[avgRangeStart].timestamp / avgRangeLength
      avgY += points[avgRangeStart].value / avgRangeLength
    }

    let rangeOffs = Math.floor((i + 0) * every) + 1
    const rangeTo = Math.floor((i + 1) * every) + 1
    const pointAX = points[a].timestamp
    const pointAY = points[a].value

    maxArea = -1

    for (; rangeOffs < rangeTo; rangeOffs++) {
      area =
        Math.abs(
          (pointAX - avgX) * (points[rangeOffs].value - pointAY) -
            (pointAX - points[rangeOffs].timestamp) * (avgY - pointAY)
        ) * 0.5

      if (area > maxArea) {
        maxArea = area
        maxAreaPoint = points[rangeOffs]
        nextA = rangeOffs
      }
    }

    sampled.push(maxAreaPoint)
    a = nextA
  }

  sampled.push(points[points.length - 1])
  return sampled
}

export function minMaxSampling(points: DataPoint[], maxPoints: number = MAX_POINTS): DataPoint[] {
  if (points.length <= maxPoints) return points

  const sampled: DataPoint[] = []
  const bucketSize = Math.ceil(points.length / (maxPoints / 2))

  for (let i = 0; i < points.length; i += bucketSize) {
    const bucket = points.slice(i, Math.min(i + bucketSize, points.length))
    if (bucket.length === 0) continue

    let minPoint = bucket[0]
    let maxPoint = bucket[0]

    for (const point of bucket) {
      if (point.value < minPoint.value) minPoint = point
      if (point.value > maxPoint.value) maxPoint = point
    }

    if (minPoint.timestamp < maxPoint.timestamp) {
      sampled.push(minPoint)
      sampled.push(maxPoint)
    } else {
      sampled.push(maxPoint)
      sampled.push(minPoint)
    }
  }

  return sampled.slice(0, maxPoints)
}
