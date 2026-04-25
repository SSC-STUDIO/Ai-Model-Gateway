import { memo, useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from '../i18n'
import { lttbSampling, type DataPoint } from '../utils/dataSampling'
import { useChartInteraction } from '../hooks/useChartInteraction'
import { useChartTooltip } from '../hooks/useChartTooltip'
import { ChartFrame } from './ChartFrame'
import { Icon } from './Icon'
import {
  buildAreaPath,
  buildChartAssetId,
  buildLinePath,
  buildTooltipState,
  clampActiveIndex,
  clamp,
  describeDonutArc,
  formatPointLabel,
  formatTimestamp,
  formatTooltipValue,
  getBarDomain,
  getLineDomain,
  MAX_POINT_LABELS,
  pickLabelIndices,
  sanitizeDataPoints,
  sanitizeLabeledValues,
  truncateLabel,
} from '../utils/charting'

type ChartProps = {
  data: DataPoint[]
  title: string
  color?: string
  unit?: string
}

type BarDatum = { label: string; value: number; color?: string }

const MAX_POINTS = 150
const LINE_VIEWBOX_WIDTH = 400
const LINE_VIEWBOX_HEIGHT = 220
const LINE_PADDING = { top: 32, right: 20, bottom: 30, left: 50 }
const DONUT_VIEWBOX_SIZE = 240

function EmptyChart({
  title,
  message = 'No data available',
  hint = 'This chart will update automatically once data arrives.',
}: {
  title: string
  message?: string
  hint?: string
}) {
  const { t } = useI18n()
  return (
    <div class="chart-container" role="img" aria-label={t('charts.emptyAriaLabel')}>
      <div class="chart-header">
        <h3>{title}</h3>
      </div>
      <div class="chart-body chart-empty">
        <div class="empty-state-icon" style={{ width: '56px', height: '56px' }}><Icon name="chart" size={26} /></div>
        <div class="chart-empty-title">{message}</div>
        <div class="chart-empty-hint">{hint}</div>
      </div>
    </div>
  )
}

const LineChartComponent = ({ data, title, color = '#3b82f6', unit = '' }: ChartProps) => {
  const { t, locale } = useI18n()
  const normalizedData = useMemo(() => sanitizeDataPoints(data), [data])
  const sampledData = useMemo(() => lttbSampling(normalizedData, MAX_POINTS), [normalizedData])
  const labelIndices = useMemo(() => pickLabelIndices(sampledData.length, MAX_POINT_LABELS), [sampledData.length])
  const chartAssetKey = useMemo(() => Math.random().toString(36).slice(2, 10), [])
  const fillGradientId = useMemo(
    () => buildChartAssetId('line-fill', title, color, chartAssetKey),
    [chartAssetKey, color, title]
  )
  const glowId = useMemo(
    () => buildChartAssetId('line-glow', title, color, chartAssetKey),
    [chartAssetKey, color, title]
  )
  const focusGradientId = useMemo(
    () => buildChartAssetId('line-focus', title, color, chartAssetKey),
    [chartAssetKey, color, title]
  )
  const chartWidth = LINE_VIEWBOX_WIDTH - LINE_PADDING.left - LINE_PADDING.right
  const chartHeight = LINE_VIEWBOX_HEIGHT - LINE_PADDING.top - LINE_PADDING.bottom
  const domain = useMemo(() => getLineDomain(sampledData.map((point) => point.value)), [sampledData])
  const spanMs = sampledData.length > 1
    ? sampledData[sampledData.length - 1].timestamp - sampledData[0].timestamp
    : 0

  const xScale = useCallback((index: number) => {
    if (sampledData.length <= 1) return LINE_PADDING.left + chartWidth / 2
    return LINE_PADDING.left + (index / (sampledData.length - 1)) * chartWidth
  }, [chartWidth, sampledData.length])

  const yScale = useCallback((value: number) => {
    const range = domain.max - domain.min || 1
    return LINE_PADDING.top + chartHeight - ((value - domain.min) / range) * chartHeight
  }, [chartHeight, domain.max, domain.min])

  const { tooltip, show, hide } = useChartTooltip()

  const interaction = useChartInteraction({
    dataLength: sampledData.length,
    onPointChange: (index) => {
      const point = sampledData[index]
      if (point) {
        const ts = buildTooltipState(
          xScale(index),
          yScale(point.value),
          LINE_VIEWBOX_WIDTH,
          LINE_VIEWBOX_HEIGHT,
          formatTooltipValue(point.value, unit),
          formatTimestamp(point.timestamp, spanMs, locale)
        )
        show(ts.xPct, ts.yPct, ts.value, ts.meta)
      }
    },
    onClear: hide,
  })

  const activePoint = interaction.activeIndex !== null ? sampledData[interaction.activeIndex] : null
  const latestPoint = sampledData[sampledData.length - 1]!
  const metricPoint = activePoint ?? latestPoint

  if (sampledData.length === 0) {
    return <EmptyChart title={title} message={t('charts.lineEmpty')} hint={t('charts.lineEmptyHint')} />
  }

  return (
    <div class="chart-container line-chart">
      <div class="chart-header">
        <h3>{title}</h3>
        <div class="chart-summary">
          <span class="chart-summary-label">{activePoint ? t('charts.current') : t('charts.latest')}</span>
          <strong class="chart-summary-value">{formatPointLabel(metricPoint.value)}{unit}</strong>
        </div>
      </div>
      <div
        class="chart-body interactive"
        tabIndex={0}
        onFocus={interaction.handleFocus}
        onBlur={interaction.handleBlur}
        onKeyDown={interaction.handleKeyDown}
      >
        <ChartFrame width={LINE_VIEWBOX_WIDTH} height={LINE_VIEWBOX_HEIGHT} ariaLabel={title ? `${title} chart` : 'Data chart'}>
          <defs>
            <linearGradient id={fillGradientId} x1="0%" y1="0%" x2="0%" y2="100%">
              <stop offset="0%" stop-color={color} stop-opacity="0.34" />
              <stop offset="45%" stop-color={color} stop-opacity="0.18" />
              <stop offset="100%" stop-color={color} stop-opacity="0.02" />
            </linearGradient>
            <filter id={glowId} x="-20%" y="-20%" width="140%" height="140%">
              <feGaussianBlur stdDeviation="5.5" result="blur" />
              <feComposite in="SourceGraphic" in2="blur" operator="over" />
            </filter>
            <radialGradient id={focusGradientId} cx="50%" cy="50%" r="50%">
              <stop offset="0%" stop-color={color} stop-opacity="0.32" />
              <stop offset="55%" stop-color={color} stop-opacity="0.12" />
              <stop offset="100%" stop-color={color} stop-opacity="0" />
            </radialGradient>
          </defs>

          <rect
            class="chart-plot-backdrop"
            x={LINE_PADDING.left}
            y={LINE_PADDING.top}
            width={chartWidth}
            height={chartHeight}
            rx="20"
          />

          <g class="grid-lines">
            {Array.from({ length: 5 }).map((_, index) => {
              const y = LINE_PADDING.top + (chartHeight / 4) * index
              return (
                <line
                  key={index}
                  class={`chart-grid-line${index === 4 ? ' is-baseline' : ''}`}
                  x1={LINE_PADDING.left}
                  y1={y}
                  x2={LINE_VIEWBOX_WIDTH - LINE_PADDING.right}
                  y2={y}
                />
              )
            })}
          </g>

          {activePoint && interaction.activeIndex !== null && (
            <rect
              class="chart-focus-band"
              x={xScale(interaction.activeIndex) - 9}
              y={LINE_PADDING.top + 4}
              width="18"
              height={chartHeight - 8}
              rx="9"
              fill={color}
              opacity="0.08"
            />
          )}

          {sampledData.length > 1 && (
            <path
              class="chart-series-area"
              d={buildAreaPath(sampledData, xScale, yScale, LINE_PADDING.top + chartHeight)}
              fill={`url(#${fillGradientId})`}
            />
          )}
          {sampledData.length > 1 && (
            <path
              class="chart-series-line-glow"
              d={buildLinePath(sampledData, xScale, yScale)}
              fill="none"
              stroke={color}
              stroke-width="8"
              stroke-linecap="round"
              stroke-linejoin="round"
              filter={`url(#${glowId})`}
              opacity="0.18"
            />
          )}
          {sampledData.length > 1 && (
            <path
              class="chart-series-line"
              d={buildLinePath(sampledData, xScale, yScale)}
              fill="none"
              stroke={color}
              stroke-width="3.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          )}
          {sampledData.length === 1 && (
            <>
              <circle
                cx={xScale(0)}
                cy={yScale(sampledData[0].value)}
                r="14"
                fill={color}
                opacity="0.14"
              />
              <circle
                cx={xScale(0)}
                cy={yScale(sampledData[0].value)}
                r="8"
                fill={color}
                opacity="0.35"
              />
              <circle
                cx={xScale(0)}
                cy={yScale(sampledData[0].value)}
                r="4.5"
                fill={color}
                stroke="var(--bg-primary)"
                stroke-width="2.4"
              />
            </>
          )}

          {activePoint && interaction.activeIndex !== null && (
            <line
              class="chart-focus-guide"
              x1={xScale(interaction.activeIndex)}
              y1={LINE_PADDING.top}
              x2={xScale(interaction.activeIndex)}
              y2={LINE_PADDING.top + chartHeight}
            />
          )}

          <g class="data-points">
            {sampledData.map((point, index) => {
              const x = xScale(index)
              const y = yScale(point.value)
              const highlighted = index === interaction.activeIndex
              const marked = labelIndices.has(index)
              return (
                <g key={`${point.timestamp}-${index}`}>
                  {(highlighted || marked) && (
                    <circle
                      class="chart-series-point-shell"
                      cx={x}
                      cy={y}
                      r={highlighted ? 8.5 : 5.4}
                      fill={color}
                      opacity={highlighted ? '0.16' : '0.10'}
                    />
                  )}
                  <circle
                    class={`chart-series-point${highlighted ? ' is-active' : marked ? ' is-key' : ''}`}
                    cx={x}
                    cy={y}
                    r={highlighted ? 4.8 : marked ? 3.3 : 2.2}
                    fill={color}
                    opacity={highlighted || marked ? 1 : 0.5}
                    stroke="var(--bg-primary)"
                    stroke-width={highlighted ? 2.1 : 1.3}
                  />
                </g>
              )
            })}
          </g>

          <g class="data-labels">
            {sampledData.map((point, index) => {
              if (!labelIndices.has(index)) return null
              const x = xScale(index)
              const y = yScale(point.value)
              const placeBelow = y <= LINE_PADDING.top + 14
              return (
                <text
                  class={`chart-point-label${index === interaction.activeIndex ? ' is-active' : ''}`}
                  key={`label-${point.timestamp}-${index}`}
                  x={x}
                  y={placeBelow ? y + 10 : y - 10}
                  text-anchor="middle"
                  dominant-baseline={placeBelow ? 'hanging' : 'auto'}
                >
                  {`${formatPointLabel(point.value)}${unit}`}
                </text>
              )
            })}
          </g>

          {activePoint && interaction.activeIndex !== null && (
            <>
              <circle
                class="chart-focus-halo"
                cx={xScale(interaction.activeIndex)}
                cy={yScale(activePoint.value)}
                r="16"
                fill={`url(#${focusGradientId})`}
              />
              <circle
                class="chart-focus-dot"
                cx={xScale(interaction.activeIndex)}
                cy={yScale(activePoint.value)}
                r="9"
                fill={color}
                opacity="0.22"
              />
              <circle
                class="chart-focus-dot-inner"
                cx={xScale(interaction.activeIndex)}
                cy={yScale(activePoint.value)}
                r="4.8"
                fill={color}
                stroke="var(--bg-primary)"
                stroke-width="2.2"
              />
            </>
          )}

          <rect
            class="chart-event-layer"
            x={LINE_PADDING.left}
            y={LINE_PADDING.top}
            width={chartWidth}
            height={chartHeight}
            fill="transparent"
            onPointerMove={(event) => interaction.handleMouseMove(event, event.currentTarget.getBoundingClientRect())}
            onPointerDown={(event) => interaction.handleMouseMove(event, event.currentTarget.getBoundingClientRect())}
            onPointerLeave={(event) => {
              if (event.pointerType === 'mouse') interaction.handleMouseLeave()
            }}
            onPointerCancel={() => interaction.handleMouseLeave()}
          />
        </ChartFrame>

        {tooltip.visible && (
          <div
            class="chart-tooltip"
            style={{
              left: `${tooltip.x}%`,
              top: `${tooltip.y}%`,
            }}
          >
            <div class="tooltip-value">{tooltip.content}</div>
            {tooltip.meta && <div class="tooltip-time">{tooltip.meta}</div>}
          </div>
        )}
      </div>
    </div>
  )
}

export const LineChart = memo(LineChartComponent)

interface DonutChartProps {
  data: { label: string; value: number; color: string }[]
  title: string
  singleRowLegend?: boolean
}

const DonutChartComponent = ({ data, title, singleRowLegend = false }: DonutChartProps) => {
  const { t } = useI18n()
  const [activeIndex, setActiveIndex] = useState<number | null>(null)

  const segments = useMemo(
    () => sanitizeLabeledValues(data).filter((segment) => segment.value >= 0),
    [data]
  )
  const total = useMemo(() => segments.reduce((sum, segment) => sum + segment.value, 0), [segments])
  const allZero = segments.length > 0 && total === 0

  const arcs = useMemo(() => {
    let currentAngle = -Math.PI / 2
    return segments.map((segment) => {
      const angle = allZero
        ? (Math.PI * 2) / segments.length
        : total > 0
          ? (segment.value / total) * Math.PI * 2
          : 0
      const startAngle = currentAngle
      const endAngle = currentAngle + angle
      currentAngle = endAngle
      return {
        ...segment,
        pct: allZero ? 100 / segments.length : total > 0 ? (segment.value / total) * 100 : 0,
        startAngle,
        endAngle,
        midAngle: startAngle + angle / 2,
      }
    })
  }, [segments, total, allZero])

  const activeSegment = activeIndex !== null ? arcs[activeIndex] : null

  useEffect(() => {
    setActiveIndex((prev) => clampActiveIndex(prev, arcs.length))
  }, [arcs.length])

  const handleKeyboard = useCallback((event: KeyboardEvent) => {
    if (arcs.length === 0) return
    if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
      event.preventDefault()
      setActiveIndex((prev) => clamp((prev ?? arcs.length) - 1, 0, arcs.length - 1))
    } else if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
      event.preventDefault()
      setActiveIndex((prev) => clamp((prev ?? -1) + 1, 0, arcs.length - 1))
    } else if (event.key === 'Home') {
      event.preventDefault()
      setActiveIndex(0)
    } else if (event.key === 'End') {
      event.preventDefault()
      setActiveIndex(arcs.length - 1)
    }
  }, [arcs.length])

  if (arcs.length === 0) {
    return <EmptyChart title={title} message={t('charts.donutEmpty')} hint={t('charts.donutEmptyHint')} />
  }

  return (
    <div class="chart-container donut">
      <div class="chart-header">
        <h3>{title}</h3>
        <div class="chart-summary">
          <span class="chart-summary-label">{t('charts.donutTotal')}</span>
          <strong class="chart-summary-value">{formatPointLabel(total)}</strong>
        </div>
      </div>

      <div
        class="chart-body interactive donut-body"
        tabIndex={arcs.length === 0 ? -1 : 0}
        aria-disabled={arcs.length === 0 ? 'true' : undefined}
        onFocus={() => setActiveIndex((prev) => prev ?? 0)}
        onBlur={() => setActiveIndex(null)}
        onKeyDown={handleKeyboard}
      >
        <ChartFrame
          width={DONUT_VIEWBOX_SIZE}
          height={DONUT_VIEWBOX_SIZE}
          className="donut-svg"
          ariaLabel={title ? `${title} chart` : 'Distribution chart'}
          onMouseLeave={() => setActiveIndex(null)}
        >
          {arcs.map((segment, index) => {
            const active = index === activeIndex
            const offset = active ? 6 : 0
            const dx = Math.cos(segment.midAngle) * offset
            const dy = Math.sin(segment.midAngle) * offset
            return (
              <path
                key={`${segment.label}-${index}`}
                d={describeDonutArc(120, 120, 52, active ? 88 : 82, segment.startAngle, segment.endAngle)}
                fill={segment.color}
                stroke="var(--bg-primary)"
                stroke-width={active ? 3 : 2}
                opacity={activeIndex === null || active ? 1 : 0.52}
                transform={`translate(${dx} ${dy})`}
                onPointerEnter={() => setActiveIndex(index)}
                onPointerDown={() => setActiveIndex(index)}
              />
            )
          })}
          <circle cx="120" cy="120" r="46" fill="var(--bg-primary)" opacity="0.92" />
          <text x="120" y="104" text-anchor="middle" class="donut-center-label">
            {activeSegment ? truncateLabel(activeSegment.label, 16) : t('charts.donutOverview')}
          </text>
          <text x="120" y="128" text-anchor="middle" class="donut-center-value">
            {formatPointLabel((activeSegment ?? { value: total }).value)}
          </text>
          <text x="120" y="148" text-anchor="middle" class="donut-center-meta">
            {activeSegment ? `${activeSegment.pct.toFixed(1)}%` : `${arcs.length} ${t('charts.donutItems')}`}
          </text>
        </ChartFrame>
      </div>

      <div
        class={`donut-legend${singleRowLegend ? ' single-row' : ''}`}
        title={singleRowLegend ? t('charts.scrollHint') : undefined}
      >
        {arcs.map((segment, index) => {
          const active = index === activeIndex
          return (
            <div
              key={segment.label}
              class={`donut-legend-item${active ? ' active' : ''}`}
              role="button"
              tabIndex={0}
              title={`${segment.label}: ${formatPointLabel(segment.value)} (${segment.pct.toFixed(1)}%)`}
              onPointerEnter={() => setActiveIndex(index)}
              onPointerDown={() => setActiveIndex(index)}
              onFocus={() => setActiveIndex(index)}
              onBlur={() => setActiveIndex(null)}
              onPointerLeave={(event) => {
                if (event.pointerType === 'mouse') setActiveIndex(null)
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  setActiveIndex(index)
                }
              }}
            >
              <span class="donut-legend-swatch" style={{ backgroundColor: segment.color }} />
              <span class="donut-legend-label">{segment.label}</span>
              <span class="donut-legend-value">{formatPointLabel(segment.value)}</span>
              <span class="donut-legend-pct">({segment.pct.toFixed(1)}%)</span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export const DonutChart = memo(DonutChartComponent)

interface BarChartProps {
  data: BarDatum[]
  title: string
  unit?: string
  horizontal?: boolean
}

// Unified chart color palette — matches LineChart accent system
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
    activeIndex,
    chartData,
    chartHeight,
    chartWidth,
    drawableHeight,
    drawableWidth,
    hide,
    horizontal,
    padding.bottom,
    padding.left,
    padding.right,
    padding.top,
    range,
    show,
    unit,
    zeroX,
    zeroY,
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
                  <stop offset="0%" stop-color={c} stop-opacity="0.92" />
                  <stop offset="100%" stop-color={c} stop-opacity="0.62" />
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

export { HistoryChart } from './HistoryChart'
export { TimeSeriesChart } from './TimeSeriesChart'
