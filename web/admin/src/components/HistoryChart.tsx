import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'preact/compat'
import { useI18n } from '../i18n'
import { lttbSampling, type DataPoint } from '../utils/dataSampling'
import { useChartInteraction } from '../hooks/useChartInteraction'
import { useChartTooltip } from '../hooks/useChartTooltip'
import { ChartFrame } from './ChartFrame'
import {
	buildAreaPath,
	buildLinePath,
	buildTooltipState,
	clamp,
	formatPointLabel,
	formatTimestamp,
  formatTooltipValue,
  getLineDomain,
  MAX_HISTORY_POINT_LABELS,
  pickLabelIndices,
  sanitizeDataPoints,
} from '../utils/charting'

interface HistoryChartProps {
  data: DataPoint[]
  title: string
  color?: string
  unit?: string
  bucketDays?: number
}

const ONE_DAY_MS = 24 * 60 * 60 * 1000
const DEFAULT_BUCKET_DAYS = 7
const DEFAULT_VIEW_WINDOW = 12
const HISTORY_VIEWBOX_WIDTH = 800
const HISTORY_VIEWBOX_HEIGHT = 320
const HISTORY_PADDING = { top: 40, right: 30, bottom: 52, left: 60 }
const DRAG_START_THRESHOLD_PX = 6

type DragState = {
  pointerId: number | null
  startX: number
  startView: number
  dragging: boolean
}

function aggregateByDays(data: DataPoint[], days: number): DataPoint[] {
  if (data.length === 0) return []
  if (days <= 1) return data

  const bucketMs = days * ONE_DAY_MS
  const buckets = new Map<number, { sum: number; count: number }>()

  data.forEach((point) => {
    const bucketTime = Math.floor(point.timestamp / bucketMs) * bucketMs
    const existing = buckets.get(bucketTime)
    if (existing) {
      existing.sum += point.value
      existing.count += 1
      return
    }
    buckets.set(bucketTime, { sum: point.value, count: 1 })
  })

  return Array.from(buckets.entries())
    .map(([timestamp, bucket]) => ({
      timestamp,
      value: bucket.sum / bucket.count,
    }))
    .sort((a, b) => a.timestamp - b.timestamp)
}

const HistoryChartComponent = ({
  data,
  title,
  color = '#3b82f6',
  unit = '',
  bucketDays = DEFAULT_BUCKET_DAYS,
}: HistoryChartProps) => {
  const { t, locale } = useI18n()
  const [viewStart, setViewStart] = useState(0)
  const [viewWindow, setViewWindow] = useState(DEFAULT_VIEW_WINDOW)
  const [isDragging, setIsDragging] = useState(false)
  const dragRef = useRef<DragState>({ pointerId: null, startX: 0, startView: 0, dragging: false })

  const aggregatedData = useMemo(
    () => sanitizeDataPoints(aggregateByDays(data, bucketDays)),
    [data, bucketDays]
  )

  useEffect(() => {
    if (aggregatedData.length === 0) {
      return
    }

    const minWindow = Math.min(4, aggregatedData.length)
    if (viewWindow < minWindow || viewWindow > aggregatedData.length) {
      setViewWindow(clamp(viewWindow, minWindow, aggregatedData.length))
      return
    }

    const maxStart = Math.max(0, aggregatedData.length - viewWindow)
    if (viewStart > maxStart) {
      setViewStart(maxStart)
    }
  }, [aggregatedData.length, viewStart, viewWindow])

  useEffect(() => {
    if (aggregatedData.length > 0 && viewStart === 0 && viewWindow === DEFAULT_VIEW_WINDOW) {
      setViewStart(Math.max(0, aggregatedData.length - Math.min(DEFAULT_VIEW_WINDOW, aggregatedData.length)))
    }
  }, [aggregatedData.length])

  const visibleData = useMemo(() => {
    if (aggregatedData.length === 0) return []
    const start = Math.max(0, Math.min(viewStart, aggregatedData.length - 1))
    const end = Math.min(start + viewWindow, aggregatedData.length)
    return aggregatedData.slice(start, end)
  }, [aggregatedData, viewStart, viewWindow])

  const sampledData = useMemo(() => lttbSampling(visibleData, 100), [visibleData])
  const labelIndices = useMemo(() => pickLabelIndices(sampledData.length, MAX_HISTORY_POINT_LABELS), [sampledData.length])
  const xAxisIndices = useMemo(() => pickLabelIndices(sampledData.length, 6), [sampledData.length])
  const spanMs = sampledData.length > 1
    ? sampledData[sampledData.length - 1].timestamp - sampledData[0].timestamp
    : 0
  const domain = useMemo(() => getLineDomain(sampledData.map((point) => point.value)), [sampledData])

  const chartWidth = HISTORY_VIEWBOX_WIDTH - HISTORY_PADDING.left - HISTORY_PADDING.right
  const chartHeight = HISTORY_VIEWBOX_HEIGHT - HISTORY_PADDING.top - HISTORY_PADDING.bottom
  const xScale = useCallback((index: number) => {
    if (sampledData.length <= 1) return HISTORY_PADDING.left + chartWidth / 2
    return HISTORY_PADDING.left + (index / (sampledData.length - 1)) * chartWidth
  }, [chartWidth, sampledData.length])

  const yScale = useCallback((value: number) => {
    const range = domain.max - domain.min || 1
    return HISTORY_PADDING.top + chartHeight - ((value - domain.min) / range) * chartHeight
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
          HISTORY_VIEWBOX_WIDTH,
          HISTORY_VIEWBOX_HEIGHT,
          formatTooltipValue(point.value, unit),
          formatTimestamp(point.timestamp, spanMs, locale)
        )
        show(ts.xPct, ts.yPct, ts.value, ts.meta)
      }
    },
    onClear: hide,
  })

  const activePoint = interaction.activeIndex !== null ? sampledData[interaction.activeIndex] : null

  const canScrollLeft = viewStart > 0
  const canScrollRight = viewStart + viewWindow < aggregatedData.length

  const handleScroll = useCallback((direction: 'left' | 'right') => {
    const step = Math.max(1, Math.floor(viewWindow / 2))
    if (direction === 'left' && canScrollLeft) {
      setViewStart((prev) => Math.max(0, prev - step))
    } else if (direction === 'right' && canScrollRight) {
      setViewStart((prev) => Math.min(Math.max(0, aggregatedData.length - viewWindow), prev + step))
    }
  }, [aggregatedData.length, canScrollLeft, canScrollRight, viewWindow])

  const handleResetView = useCallback(() => {
    if (aggregatedData.length === 0) return
    const nextWindow = Math.min(DEFAULT_VIEW_WINDOW, aggregatedData.length)
    setViewWindow(nextWindow)
    setViewStart(Math.max(0, aggregatedData.length - nextWindow))
    interaction.setActiveIndex(null)
    hide()
  }, [aggregatedData.length, hide, interaction])

  const resetDrag = useCallback((event?: PointerEvent) => {
    const current = dragRef.current
    if (event && current.pointerId === event.pointerId) {
      ;(event.currentTarget as HTMLDivElement).releasePointerCapture?.(event.pointerId)
    }
    dragRef.current = { pointerId: null, startX: 0, startView: 0, dragging: false }
    setIsDragging(false)
  }, [])

  const { handleMouseMove, handleMouseLeave, setActiveIndex } = interaction

  const handlePointerDown = useCallback((event: PointerEvent) => {
    if (event.pointerType === 'mouse' && event.button !== 0) return
    const element = event.currentTarget as HTMLDivElement
    element.setPointerCapture?.(event.pointerId)
    dragRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startView: viewStart,
      dragging: false,
    }
    const rect = element.getBoundingClientRect()
    handleMouseMove(event, rect)
  }, [handleMouseMove, viewStart])

  const handlePointerMove = useCallback((event: PointerEvent) => {
    const element = event.currentTarget as HTMLDivElement
    const current = dragRef.current

    if (current.pointerId === event.pointerId) {
      const deltaX = event.clientX - current.startX
      if (!current.dragging && Math.abs(deltaX) >= DRAG_START_THRESHOLD_PX) {
        current.dragging = true
        setIsDragging(true)
        setActiveIndex(null)
        hide()
      }

      if (current.dragging) {
        const rect = element.getBoundingClientRect()
        if (rect.width <= 0) return
        const pointsPerPixel = viewWindow / rect.width
        const deltaPoints = Math.round(-deltaX * pointsPerPixel)
        const nextStart = clamp(current.startView + deltaPoints, 0, Math.max(0, aggregatedData.length - viewWindow))
        setViewStart(nextStart)
        return
      }
    }

    if (!isDragging && sampledData.length > 0) {
      const rect = element.getBoundingClientRect()
      handleMouseMove(event, rect)
    }
  }, [aggregatedData.length, handleMouseMove, hide, isDragging, sampledData.length, setActiveIndex, viewWindow])

  const handleWheel = useCallback((event: WheelEvent) => {
    event.preventDefault()
    if (aggregatedData.length === 0) return
    const minWindow = Math.min(4, aggregatedData.length)
    const delta = event.deltaY > 0 ? 1 : -1
    const nextWindow = clamp(viewWindow + delta * 2, minWindow, aggregatedData.length)
    const center = viewStart + viewWindow / 2
    const nextStart = clamp(Math.round(center - nextWindow / 2), 0, Math.max(0, aggregatedData.length - nextWindow))
    setViewWindow(nextWindow)
    setViewStart(nextStart)
  }, [aggregatedData.length, viewStart, viewWindow])

  const handleKeyboard = useCallback((event: KeyboardEvent) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      handleScroll('left')
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      handleScroll('right')
    } else if (event.key === '+' || event.key === '=') {
      event.preventDefault()
      if (aggregatedData.length === 0) return
      const minWindow = Math.min(4, aggregatedData.length)
      const nextWindow = clamp(viewWindow - 2, minWindow, aggregatedData.length)
      const center = viewStart + viewWindow / 2
      setViewWindow(nextWindow)
      setViewStart(clamp(Math.round(center - nextWindow / 2), 0, Math.max(0, aggregatedData.length - nextWindow)))
    } else if (event.key === '-' || event.key === '_') {
      event.preventDefault()
      if (aggregatedData.length === 0) return
      const minWindow = Math.min(4, aggregatedData.length)
      const nextWindow = clamp(viewWindow + 2, minWindow, aggregatedData.length)
      const center = viewStart + viewWindow / 2
      setViewWindow(nextWindow)
      setViewStart(clamp(Math.round(center - nextWindow / 2), 0, Math.max(0, aggregatedData.length - nextWindow)))
    } else if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      handleResetView()
    }
  }, [aggregatedData.length, handleResetView, handleScroll, viewStart, viewWindow])

  if (aggregatedData.length === 0) {
    return (
      <div class="chart-container history-chart">
        <div class="chart-header">
          <h3>{title}</h3>
        </div>
        <div class="chart-body chart-empty">
          <div class="chart-empty-title">{t('timeseries.emptyHistoryTitle')}</div>
          <div class="chart-empty-hint">{t('timeseries.emptyHistoryHint')}</div>
        </div>
      </div>
    )
  }

  return (
    <div class="chart-container history-chart">
      <div class="chart-header">
        <h3>{title}</h3>
        <div class="history-controls">
          <button
            type="button"
            class="history-nav-btn"
            onClick={() => handleScroll('left')}
            disabled={!canScrollLeft}
            title={t('timeseries.earlier')}
          >
            {t('timeseries.earlier')}
          </button>
          <span class="history-range">
            {new Date(visibleData[0]?.timestamp || 0).toLocaleDateString(locale)} ~{' '}
            {new Date(visibleData[visibleData.length - 1]?.timestamp || 0).toLocaleDateString(locale)}
            ({t('timeseries.weeksTotal').replace('{count}', String(aggregatedData.length))})
          </span>
          <button
            type="button"
            class="history-nav-btn"
            onClick={handleResetView}
            title={t('timeseries.resetView')}
          >
            {t('timeseries.latest')}
          </button>
          <button
            type="button"
            class="history-nav-btn"
            onClick={() => handleScroll('right')}
            disabled={!canScrollRight}
            title={t('timeseries.later')}
          >
            {t('timeseries.later')}
          </button>
        </div>
      </div>

      <div
        class={`chart-body interactive ${isDragging ? 'dragging' : ''}`}
        tabIndex={0}
        onFocus={interaction.handleFocus}
        onBlur={interaction.handleBlur}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={(event) => resetDrag(event)}
        onPointerCancel={(event) => {
          resetDrag(event)
          interaction.setActiveIndex(null)
          hide()
        }}
        onPointerLeave={(event) => {
          if (!dragRef.current.dragging && event.pointerType === 'mouse') {
            handleMouseLeave()
          }
        }}
        onWheel={handleWheel}
        onKeyDown={handleKeyboard}
        onDblClick={handleResetView}
        style={{ cursor: isDragging ? 'grabbing' : 'grab' }}
      >
        <ChartFrame
          width={HISTORY_VIEWBOX_WIDTH}
          height={HISTORY_VIEWBOX_HEIGHT}
          className="history-svg"
          ariaLabel={title ? `${title} chart` : 'History chart'}
        >
          <g class="grid-lines">
            {Array.from({ length: 6 }).map((_, index) => {
              const y = HISTORY_PADDING.top + (chartHeight / 5) * index
              const value = domain.max - ((domain.max - domain.min) / 5) * index
              return (
                <g key={index}>
                  <line
                    x1={HISTORY_PADDING.left}
                    y1={y}
                    x2={HISTORY_VIEWBOX_WIDTH - HISTORY_PADDING.right}
                    y2={y}
                    stroke="var(--border-color)"
                    stroke-opacity="0.3"
                    stroke-dasharray="4 4"
                  />
                  <text
                    x={HISTORY_PADDING.left - 10}
                    y={y}
                    text-anchor="end"
                    dominant-baseline="middle"
                    fill="var(--text-muted)"
                    font-size="11"
                  >
                    {formatPointLabel(value)}
                    {unit}
                  </text>
                </g>
              )
            })}
          </g>

          {sampledData.length > 1 && (
            <path
              d={buildAreaPath(sampledData, xScale, yScale, HISTORY_PADDING.top + chartHeight)}
              fill={color}
              fill-opacity="0.14"
            />
          )}
          {sampledData.length > 1 && (
            <path
              d={buildLinePath(sampledData, xScale, yScale)}
              fill="none"
              stroke={color}
              stroke-width="2.4"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          )}

          {activePoint && interaction.activeIndex !== null && (
            <line
              class="chart-focus-guide"
              x1={xScale(interaction.activeIndex)}
              y1={HISTORY_PADDING.top}
              x2={xScale(interaction.activeIndex)}
              y2={HISTORY_PADDING.top + chartHeight}
            />
          )}

          <g class="data-points">
            {sampledData.map((point, index) => {
              const x = xScale(index)
              const y = yScale(point.value)
              const highlighted = index === interaction.activeIndex
              return (
                <circle
                  key={`${point.timestamp}-${index}`}
                  cx={x}
                  cy={y}
                  r={highlighted ? 5.8 : labelIndices.has(index) ? 3.8 : 2.8}
                  fill={color}
                  opacity={highlighted || labelIndices.has(index) ? 1 : 0.46}
                  stroke="var(--bg-primary)"
                  stroke-width={highlighted ? 2.8 : 1.6}
                />
              )
            })}
          </g>

          <g class="data-labels">
            {sampledData.map((point, index) => {
              if (!labelIndices.has(index)) return null
              const x = xScale(index)
              const y = yScale(point.value)
              const placeBelow = y <= HISTORY_PADDING.top + 14
              return (
                <text
                  key={`history-label-${point.timestamp}-${index}`}
                  x={x}
                  y={placeBelow ? y + 12 : y - 10}
                  text-anchor="middle"
                  dominant-baseline={placeBelow ? 'hanging' : 'auto'}
                  fill="var(--text-primary)"
                  stroke="var(--bg-primary)"
                  stroke-width={index === interaction.activeIndex ? 3.8 : 3}
                  paint-order="stroke"
                  font-size="11"
                  font-weight={index === interaction.activeIndex ? '700' : '600'}
                >
                  {`${formatPointLabel(point.value)}${unit}`}
                </text>
              )
            })}
          </g>

          <g class="x-axis-labels">
            {sampledData.map((point, index) => {
              if (!xAxisIndices.has(index)) return null
              return (
                <text
                  key={`history-axis-${point.timestamp}-${index}`}
                  x={xScale(index)}
                  y={HISTORY_VIEWBOX_HEIGHT - 14}
                  text-anchor="middle"
                  fill="var(--text-muted)"
                  font-size="11"
                >
                  {new Date(point.timestamp).toLocaleDateString(locale, {
                    month: 'short',
                    day: 'numeric',
                  })}
                </text>
              )
            })}
          </g>

          {activePoint && interaction.activeIndex !== null && (
            <>
              <circle
                class="chart-focus-dot"
                cx={xScale(interaction.activeIndex)}
                cy={yScale(activePoint.value)}
                r="8"
                fill={color}
                opacity="0.16"
              />
              <circle
                class="chart-focus-dot-inner"
                cx={xScale(interaction.activeIndex)}
                cy={yScale(activePoint.value)}
                r="4.8"
                fill={color}
                stroke="var(--bg-primary)"
                stroke-width="2"
              />
            </>
          )}

          <rect
            class="chart-event-layer"
            x={HISTORY_PADDING.left}
            y={HISTORY_PADDING.top}
            width={chartWidth}
            height={chartHeight}
            fill="transparent"
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

      <div class="history-hint">
        {t('timeseries.dragHint')}
      </div>
    </div>
  )
}

export const HistoryChart = memo(HistoryChartComponent)
