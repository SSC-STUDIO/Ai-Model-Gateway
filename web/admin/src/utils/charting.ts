import type { DataPoint } from './dataSampling'

export type TooltipState = {
  xPct: number
  yPct: number
  value: string
  meta?: string
  label?: string
}

export const MAX_POINT_LABELS = 12
export const MAX_HISTORY_POINT_LABELS = 14

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

export function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

export function clampActiveIndex(index: number | null, length: number): number | null {
  if (index === null || length <= 0 || !Number.isInteger(index)) return null
  return clamp(index, 0, length - 1)
}

export function getNearestIndexFromClientX(
  clientX: number,
  rectLeft: number,
  rectWidth: number,
  count: number
): number | null {
  if (count <= 0 || rectWidth <= 0 || !Number.isFinite(clientX)) return null
  const ratio = clamp((clientX - rectLeft) / rectWidth, 0, 1)
  return count === 1 ? 0 : Math.round(ratio * (count - 1))
}

export function sanitizeDataPoints<T extends DataPoint>(points: T[]): T[] {
  return points
    .filter((point) => isFiniteNumber(point.timestamp) && isFiniteNumber(point.value))
    .sort((left, right) => left.timestamp - right.timestamp)
}

export function sanitizeLabeledValues<T extends { label: string; value: number }>(items: T[]): T[] {
  return items.filter((item) => item.label.trim().length > 0 && isFiniteNumber(item.value))
}

export function formatPointLabel(value: number): string {
  const abs = Math.abs(value)
  if (abs >= 1000) {
    return new Intl.NumberFormat('en-US', {
      notation: 'compact',
      maximumFractionDigits: abs >= 100000 ? 0 : 1,
    }).format(value)
  }
  if (abs >= 100) return value.toFixed(0)
  if (abs >= 10) return value.toFixed(1)
  if (Number.isInteger(value)) return value.toFixed(0)
  return value.toFixed(2)
}

export function formatTooltipValue(value: number, unit: string): string {
  const suffix = unit.trim()
  if (suffix === '%') return `${value.toFixed(2)}%`
  if (suffix === 'ms') return `${value.toFixed(1)} ms`
  return `${formatPointLabel(value)}${unit}`
}

export function truncateLabel(label: string, maxLength: number): string {
  return label.length > maxLength ? `${label.slice(0, maxLength - 1)}…` : label
}

export function buildChartAssetId(prefix: string, ...parts: string[]): string {
  const tokens = parts
    .map((part) => part.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, ''))
    .filter(Boolean)
  return [prefix, ...tokens].join('-')
}

export function pickLabelIndices(length: number, maxLabels: number): Set<number> {
  const picked = new Set<number>()
  if (length <= 0) return picked
  const step = Math.max(1, Math.ceil(length / Math.max(1, maxLabels)))
  for (let i = 0; i < length; i += step) picked.add(i)
  picked.add(0)
  picked.add(length - 1)
  return picked
}

export function getLineDomain(values: number[]): { min: number; max: number } {
  if (values.length === 0) return { min: 0, max: 1 }
  const min = Math.min(...values)
  const max = Math.max(...values)
  if (min === max) {
    const padding = Math.max(Math.abs(max) * 0.15, 1)
    return { min: min - padding, max: max + padding }
  }
  const padding = (max - min) * 0.08
  return { min: min - padding, max: max + padding }
}

export function getBarDomain(values: number[]): { min: number; max: number } {
  if (values.length === 0) return { min: 0, max: 1 }
  const min = Math.min(0, ...values)
  const max = Math.max(0, ...values)
  if (min === max) return { min: min - 1, max: max + 1 }
  const padding = (max - min) * 0.06
  return { min: min - padding, max: max + padding }
}

export function buildLinePath(
  data: DataPoint[],
  xScale: (index: number) => number,
  yScale: (value: number) => number
): string {
  if (data.length === 0) return ''

  const points = data.map((point, index) => ({
    x: xScale(index),
    y: yScale(point.value),
  }))

  if (points.length === 1) {
    return `M ${points[0].x} ${points[0].y}`
  }

  const slopes = new Array<number>(points.length - 1)
  for (let i = 0; i < points.length - 1; i++) {
    const dx = points[i + 1].x - points[i].x || 1
    slopes[i] = (points[i + 1].y - points[i].y) / dx
  }

  const tangents = new Array<number>(points.length)
  tangents[0] = slopes[0]
  tangents[points.length - 1] = slopes[slopes.length - 1]
  for (let i = 1; i < points.length - 1; i++) {
    tangents[i] = (slopes[i - 1] + slopes[i]) / 2
  }

  for (let i = 0; i < slopes.length; i++) {
    if (slopes[i] === 0) {
      tangents[i] = 0
      tangents[i + 1] = 0
      continue
    }

    const alpha = tangents[i] / slopes[i]
    const beta = tangents[i + 1] / slopes[i]
    const norm = alpha * alpha + beta * beta

    if (norm > 9) {
      const scale = 3 / Math.sqrt(norm)
      tangents[i] = scale * alpha * slopes[i]
      tangents[i + 1] = scale * beta * slopes[i]
    }
  }

  let path = `M ${points[0].x} ${points[0].y}`

  for (let i = 0; i < points.length - 1; i++) {
    const current = points[i]
    const next = points[i + 1]
    const controlOffsetX = (next.x - current.x) / 3
    const cp1x = current.x + controlOffsetX
    const cp1y = current.y + tangents[i] * controlOffsetX
    const cp2x = next.x - controlOffsetX
    const cp2y = next.y - tangents[i + 1] * controlOffsetX
    path += ` C ${cp1x} ${cp1y}, ${cp2x} ${cp2y}, ${next.x} ${next.y}`
  }

  return path
}

export function buildAreaPath(
  data: DataPoint[],
  xScale: (index: number) => number,
  yScale: (value: number) => number,
  baselineY: number
): string {
  if (data.length === 0) return ''

  const linePath = buildLinePath(data, xScale, yScale)
  const startX = xScale(0)
  const endX = xScale(data.length - 1)
  return `${linePath} L ${endX} ${baselineY} L ${startX} ${baselineY} Z`
}

export function formatTimestamp(timestamp: number, spanMs: number, locale = 'en'): string {
  const date = new Date(timestamp)
  if (spanMs >= 24 * 60 * 60 * 1000) {
    return date.toLocaleString(locale, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }
  return date.toLocaleTimeString(locale, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export function buildTooltipState(
  x: number,
  y: number,
  width: number,
  height: number,
  value: string,
  meta?: string,
  label?: string
): TooltipState {
  return {
    xPct: clamp((x / width) * 100, 16, 84),
    yPct: clamp((y / height) * 100, 18, 78),
    value,
    meta,
    label,
  }
}

export function polarToCartesian(cx: number, cy: number, radius: number, angle: number) {
  return {
    x: cx + Math.cos(angle) * radius,
    y: cy + Math.sin(angle) * radius,
  }
}

export function describeDonutArc(
  cx: number,
  cy: number,
  innerRadius: number,
  outerRadius: number,
  startAngle: number,
  endAngle: number
): string {
  const outerStart = polarToCartesian(cx, cy, outerRadius, startAngle)
  const outerEnd = polarToCartesian(cx, cy, outerRadius, endAngle)
  const innerEnd = polarToCartesian(cx, cy, innerRadius, endAngle)
  const innerStart = polarToCartesian(cx, cy, innerRadius, startAngle)
  const largeArcFlag = endAngle - startAngle > Math.PI ? 1 : 0
  return [
    `M ${outerStart.x} ${outerStart.y}`,
    `A ${outerRadius} ${outerRadius} 0 ${largeArcFlag} 1 ${outerEnd.x} ${outerEnd.y}`,
    `L ${innerEnd.x} ${innerEnd.y}`,
    `A ${innerRadius} ${innerRadius} 0 ${largeArcFlag} 0 ${innerStart.x} ${innerStart.y}`,
    'Z',
  ].join(' ')
}
