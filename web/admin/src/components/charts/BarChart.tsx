import { memo, useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import { useChartTooltip } from '../../hooks/useChartTooltip'
import { ChartFrame } from '../ChartFrame'
import {
  buildChartAssetId,
  buildTooltipState,
  clamp,
  clampActiveIndex,
  formatPointLabel,
  formatTooltipValue,
  getBarDomain,
  sanitizeLabeledValues,
  truncateLabel,
} from '../../utils/charting'
import { EmptyChart } from './EmptyChart'

type BarDatum = { label: string; value: number; color?: string }

interface BarChartProps {
  data: BarDatum[]
  title: string
  unit?: string
  horizontal?: boolean
}

const BAR_PALETTE = [
  '#2b4f7c', '#2f7b5b', '#a5622a', '#c24a3d',
  '#8661c5', '#0f8b8d', '#b3477a', '#64748b',
]

const BAR_VIEWBOX_WIDTH = 460
const BAR_VIEWBOX_HEIGHT = 240
const BAR_PADDING = { top: 32, right: 24, bottom: 52, left: 56 }
const BAR_H_PADDING = { top: 32, right: 28, bottom: 32, left: 128 }

const BarChartComponent = ({ data, title, unit = '', horizontal = false }: BarChartProps) => {
  const { t } = useI18n()
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const chartAssetKey = useMemo(() => Math.random().toString(36).slice(2, 10), [])

  const chartData = useMemo(() => sanitizeLabeledValues(data), [data])
  const colors = useMemo(
    () => chartData.map((item, index) => item.color || BAR_PALETTE[index % BAR_PALETTE.length]),
    [chartData]
  )
  const domain = useMemo(() => getBarDomain(chartData.map((item) => item.value)), [chartData])
  const chartWidth = BAR_VIEWBOX_WIDTH
  const chartHeight = horizontal
    ? Math.max(BAR_VIEWBOX_HEIGHT, 52 + chartData.length * 36)
    : BAR_VIEWBOX_HEIGHT
  const padding = horizontal ? BAR_H_PADDING : BAR_PADDING
  const drawableWidth = chartWidth - padding.left - padding.right
  const drawableHeight = chartHeight - padding.top - padding.bottom
  const range = domain.max - domain.min || 1
  const zeroRatio = clamp((0 - domain.min) / range, 0, 1)
  const zeroX = padding.left + zeroRatio * drawableWidth
  const zeroY = padding.top + drawableHeight - zeroRatio * drawableHeight

  const maxItem = useMemo(() => {
    if (chartData.length === 0) return null
    return chartData.reduce((best, item) =>
      item.value > best.value ? item : best
    )
  }, [chartData])

  useEffect(() => {
    setActiveIndex((prev) => clampActiveIndex(prev, chartData.length))
  }, [chartData.length])

  const { tooltip, show, hide } = useChartTooltip()

  useEffect(() => {
    if (activeIndex === null || !chartData[activeIndex]) {
      hide()
      return
    }
    const item = chartData[activeIndex]
    const valueText = formatTooltipValue(item.value, unit)
    if (horizontal) {
      const barSlot = drawableHeight / Math.max(1, chartData.length)
      const barHeight = barSlot * 0.72
      const y = padding.top + activeIndex * barSlot + barHeight / 2
      const valueWidth = (Math.abs(item.value) / range) * drawableWidth
      const x = item.value >= 0 ? zeroX + valueWidth : zeroX - valueWidth
      const ts = buildTooltipState(
        clamp(x, padding.left, chartWidth - padding.right),
        y,
        chartWidth,
        chartHeight,
        valueText,
        truncateLabel(item.label, 22),
        item.label
      )
      show(ts.xPct, ts.yPct, ts.value, ts.meta)
    } else {
      const barSlot = drawableWidth / Math.max(1, chartData.length)
      const barWidth = barSlot * 0.7
      const x = padding.left + activeIndex * barSlot + barWidth / 2
      const valueHeight = (Math.abs(item.value) / range) * drawableHeight
      const y = item.value >= 0 ? zeroY - valueHeight : zeroY + valueHeight
      const ts = buildTooltipState(
        x,
        clamp(y, padding.top, chartHeight - padding.bottom),
        chartWidth,
        chartHeight,
        valueText,
        truncateLabel(item.label, 22),
        item.label
      )
      show(ts.xPct, ts.yPct, ts.value, ts.meta)
    }
  }, [
    activeIndex, chartData, chartHeight, chartWidth,
    drawableHeight, drawableWidth, hide, horizontal,
    padding.bottom, padding.left, padding.right, padding.top,
    range, show, unit, zeroX, zeroY,
  ])

  const handleKeyboard = useCallback((event: KeyboardEvent) => {
    if (chartData.length === 0) return
    if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
      event.preventDefault()
      setActiveIndex((prev) => clamp((prev ?? chartData.length - 1) - 1, 0, chartData.length - 1))
    } else if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
      event.preventDefault()
      setActiveIndex((prev) => clamp((prev ?? -1) + 1, 0, chartData.length - 1))
    } else if (event.key === 'Home') {
      event.preventDefault()
      setActiveIndex(0)
    } else if (event.key === 'End') {
      event.preventDefault()
      setActiveIndex(chartData.length - 1)
    }
  }, [chartData.length])

  if (chartData.length === 0) {
    return <EmptyChart title={title} message={t('charts.barEmpty')} hint={t('charts.barEmptyHint')} />
  }
  return (
    <div class="chart-container">
      <div class="chart-header">
        <h3>{title}</h3>
        <div class="chart-summary">
          <span class="chart-summary-label">{t('charts.barMax')}</span>
          <strong class="chart-summary-value">
            {maxItem ? `${formatPointLabel(maxItem.value)}${unit}` : `0${unit}`}
          </strong>
        </div>
      </div>

      <div
        class="chart-body interactive"
        tabIndex={0}
        onFocus={() => setActiveIndex((prev) => prev ?? 0)}
        onBlur={() => setActiveIndex(null)}
        onKeyDown={handleKeyboard}
        onPointerLeave={(event) => {
          if (event.pointerType === 'mouse') setActiveIndex(null)
        }}
      >
        <ChartFrame width={chartWidth} height={chartHeight} ariaLabel={title ? `${title} chart` : 'Bar chart'}>
          <defs>
            {chartData.map((_, index) => {
              const c = colors[index]
              const gradId = buildChartAssetId('bar-grad', `${title}-${index}`, c, chartAssetKey)
              return (
                <linearGradient
                  key={`grad-${index}`}
                  id={gradId}
                  x1="0%"
                  y1="0%"
                  x2={horizontal ? '100%' : '0%'}
                  y2={horizontal ? '0%' : '100%'}
                >
                  <stop offset="0%" stop-color={c} stop-opacity="0.95" />
                  <stop offset="40%" stop-color={c} stop-opacity="0.85" />
                  <stop offset="100%" stop-color={c} stop-opacity="0.55" />
                </linearGradient>
              )
            })}
          </defs>

          <g class="grid-lines">
            {Array.from({ length: 5 }).map((_, index) => {
              if (horizontal) {
                const x = padding.left + (drawableWidth / 4) * index
                return (
                  <line
                    key={index}
                    x1={x}
                    y1={padding.top}
                    x2={x}
                    y2={chartHeight - padding.bottom}
                    stroke="var(--border-color)"
                    stroke-opacity="0.25"
                    stroke-dasharray="4 4"
                  />
                )
              }
              const y = padding.top + (drawableHeight / 4) * index
              return (
                <line
                  key={index}
                  x1={padding.left}
                  y1={y}
                  x2={chartWidth - padding.right}
                  y2={y}
                  stroke="var(--border-color)"
                  stroke-opacity="0.25"
                  stroke-dasharray="4 4"
                />
              )
            })}
          </g>

          {horizontal ? (
            <line class="chart-zero-line" x1={zeroX} y1={padding.top} x2={zeroX} y2={chartHeight - padding.bottom} />
          ) : (
            <line class="chart-zero-line" x1={padding.left} y1={zeroY} x2={chartWidth - padding.right} y2={zeroY} />
          )}

          {chartData.map((item, index) => {
            const active = index === activeIndex
            const gradId = buildChartAssetId('bar-grad', `${title}-${index}`, colors[index], chartAssetKey)
            if (horizontal) {
              const barSlot = drawableHeight / Math.max(1, chartData.length)
              const barHeight = Math.min(barSlot * 0.68, 48)
              const y = padding.top + index * barSlot + (barSlot - barHeight) / 2
              const barWidth = (Math.abs(item.value) / range) * drawableWidth
              const x = item.value >= 0 ? zeroX : zeroX - barWidth
              return (
                <g key={`${item.label}-${index}`}>
                  <text
                    x={padding.left - 10}
                    y={y + barHeight / 2}
                    text-anchor="end"
                    dominant-baseline="middle"
                    fill="var(--text-secondary)"
                    font-size="11"
                    font-weight={active ? '700' : '500'}
                    opacity={activeIndex === null || active ? 1 : 0.65}
                    title={item.label}
                  >
                    {truncateLabel(item.label, 16)}
                  </text>
                  <rect
                    class="bar-rect"
                    x={x}
                    y={y}
                    width={Math.max(barWidth, 2)}
                    height={barHeight}
                    rx="6"
                    fill={`url(#${gradId})`}
                    opacity={activeIndex === null || active ? 1 : 0.45}
                    stroke={active ? colors[index] : 'transparent'}
                    stroke-width={active ? 2 : 0}
                    onPointerEnter={() => setActiveIndex(index)}
                    onPointerDown={() => setActiveIndex(index)}
                  />
                  <text
                    class="bar-value-label"
                    x={item.value >= 0 ? x + Math.max(barWidth, 2) + 8 : x - 8}
                    y={y + barHeight / 2}
                    text-anchor={item.value >= 0 ? 'start' : 'end'}
                    dominant-baseline="middle"
                    opacity={activeIndex === null || active ? 1 : 0.55}
                  >
                    {formatPointLabel(item.value)}{unit}
                  </text>
                </g>
              )
            }

            const barSlot = drawableWidth / Math.max(1, chartData.length)
            const barWidth = Math.min(barSlot * 0.65, 80)
            const x = padding.left + index * barSlot + (barSlot - barWidth) / 2
            const valueHeight = (Math.abs(item.value) / range) * drawableHeight
            const y = item.value >= 0 ? zeroY - valueHeight : zeroY
            return (
              <g key={`${item.label}-${index}`}>
                <rect
                  class="bar-rect"
                  x={x}
                  y={y}
                  width={barWidth}
                  height={Math.max(valueHeight, 2)}
                  rx="6"
                  fill={`url(#${gradId})`}
                  opacity={activeIndex === null || active ? 1 : 0.45}
                  stroke={active ? colors[index] : 'transparent'}
                  stroke-width={active ? 2 : 0}
                  onPointerEnter={() => setActiveIndex(index)}
                  onPointerDown={() => setActiveIndex(index)}
                />
                <text
                  class="bar-value-label"
                  x={x + barWidth / 2}
                  y={item.value >= 0 ? y - 10 : y + valueHeight + 14}
                  text-anchor="middle"
                  opacity={activeIndex === null || active ? 1 : 0.55}
                >
                  {formatPointLabel(item.value)}{unit}
                </text>
                <text
                  x={x + barWidth / 2}
                  y={chartHeight - 14}
                  text-anchor="middle"
                  fill="var(--text-secondary)"
                  font-size="10"
                  font-weight={active ? '700' : '500'}
                  opacity={activeIndex === null || active ? 1 : 0.65}
                  title={item.label}
                >
                  {truncateLabel(item.label, 10)}
                </text>
              </g>
            )
          })}
        </ChartFrame>

        {tooltip.visible && (
          <div
            class="chart-tooltip"
            style={{
              left: `${tooltip.x}%`,
              top: `${tooltip.y}%`,
            }}
          >
            {tooltip.meta && <div class="tooltip-label">{tooltip.meta}</div>}
            <div class="tooltip-value">{tooltip.content}</div>
          </div>
        )}
      </div>
    </div>
  )
}

export const BarChart = memo(BarChartComponent)