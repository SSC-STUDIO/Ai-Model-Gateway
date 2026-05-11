import { memo, useCallback, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import { lttbSampling, type DataPoint } from '../../utils/dataSampling'
import { useChartInteraction } from '../../hooks/useChartInteraction'
import { useChartTooltip } from '../../hooks/useChartTooltip'
import { ChartFrame } from '../ChartFrame'
import {
  buildAreaPath,
  buildChartAssetId,
  buildLinePath,
  buildTooltipState,
  formatPointLabel,
  formatTimestamp,
  formatTooltipValue,
  getLineDomain,
  MAX_POINT_LABELS,
  pickLabelIndices,
  sanitizeDataPoints,
} from '../../utils/charting'
import { EmptyChart } from './EmptyChart'

type ChartProps = {
  data: DataPoint[]
  title: string
  color?: string
  unit?: string
}

const MAX_POINTS = 150
const LINE_VIEWBOX_WIDTH = 400
const LINE_VIEWBOX_HEIGHT = 220
const LINE_PADDING = { top: 32, right: 20, bottom: 30, left: 50 }

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
              <circle cx={xScale(0)} cy={yScale(sampledData[0].value)} r="14" fill={color} opacity="0.14" />
              <circle cx={xScale(0)} cy={yScale(sampledData[0].value)} r="8" fill={color} opacity="0.35" />
              <circle cx={xScale(0)} cy={yScale(sampledData[0].value)} r="4.5" fill={color} stroke="var(--bg-primary)" stroke-width="2.4" />
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