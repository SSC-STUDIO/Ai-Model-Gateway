import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'preact/compat'
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
  clamp,
  formatPointLabel,
  formatTimestamp,
  formatTooltipValue,
  getLineDomain,
  MAX_POINT_LABELS,
  MAX_HISTORY_POINT_LABELS,
  pickLabelIndices,
  sanitizeDataPoints,
} from '../utils/charting'

interface TimeSeriesChartProps {
  data: DataPoint[]
  title: string
  color?: string
  unit?: string
}

const WINDOW_MODE_THRESHOLD = 50

// Compact mode constants (LineChart style)
const COMPACT_VIEWBOX_WIDTH = 400
const COMPACT_VIEWBOX_HEIGHT = 220
const COMPACT_PADDING = { top: 32, right: 20, bottom: 30, left: 50 }
const COMPACT_MAX_POINTS = 150

// Window mode constants (HistoryChart style)
const WINDOW_VIEWBOX_WIDTH = 800
const WINDOW_VIEWBOX_HEIGHT = 320
const WINDOW_PADDING = { top: 40, right: 30, bottom: 52, left: 60 }
const DEFAULT_VIEW_WINDOW = 12
const DRAG_START_THRESHOLD_PX = 6
const HOUR_MS = 60 * 60 * 1000
const DAY_MS = 24 * HOUR_MS

type DragState = {
  pointerId: number | null
  startX: number
  startView: number
  dragging: boolean
}

const TimeSeriesChartComponent = ({
  data,
  title,
  color = '#3b82f6',
  unit = '',
}: TimeSeriesChartProps) => {
  const { t, locale } = useI18n()
  const normalizedData = useMemo(() => sanitizeDataPoints(data), [data])

  // Window mode state (only used when data exceeds threshold)
  const [viewStart, setViewStart] = useState(0)
  const [viewWindow, setViewWindow] = useState(DEFAULT_VIEW_WINDOW)
  const [isDragging, setIsDragging] = useState(false)
  const dragRef = useRef<DragState>({ pointerId: null, startX: 0, startView: 0, dragging: false })

  // Determine mode based on data count
  const enableWindowMode = normalizedData.length > WINDOW_MODE_THRESHOLD

  // Asset IDs
  const chartAssetKey = useMemo(() => Math.random().toString(36).slice(2, 10), [])
  const fillGradientId = useMemo(
    () => buildChartAssetId('ts-fill', title, color, chartAssetKey),
    [chartAssetKey, color, title]
  )
  const glowId = useMemo(
    () => buildChartAssetId('ts-glow', title, color, chartAssetKey),
    [chartAssetKey, color, title]
  )
  const focusGradientId = useMemo(
    () => buildChartAssetId('ts-focus', title, color, chartAssetKey),
    [chartAssetKey, color, title]
  )

  // Viewbox and padding based on mode
  const viewBoxWidth = enableWindowMode ? WINDOW_VIEWBOX_WIDTH : COMPACT_VIEWBOX_WIDTH
  const viewBoxHeight = enableWindowMode ? WINDOW_VIEWBOX_HEIGHT : COMPACT_VIEWBOX_HEIGHT
  const padding = enableWindowMode ? WINDOW_PADDING : COMPACT_PADDING

  // Window mode: manage view window
  useEffect(() => {
    if (!enableWindowMode) return
    if (normalizedData.length === 0) return

    const minWindow = Math.min(4, normalizedData.length)
    if (viewWindow < minWindow || viewWindow > normalizedData.length) {
      setViewWindow(clamp(viewWindow, minWindow, normalizedData.length))
      return
    }
    const maxStart = Math.max(0, normalizedData.length - viewWindow)
    if (viewStart > maxStart) {
      setViewStart(maxStart)
    }
  }, [enableWindowMode, normalizedData.length, viewStart, viewWindow])

  useEffect(() => {
    if (!enableWindowMode) return
    if (normalizedData.length > 0 && viewStart === 0 && viewWindow === DEFAULT_VIEW_WINDOW) {
      setViewStart(Math.max(0, normalizedData.length - Math.min(DEFAULT_VIEW_WINDOW, normalizedData.length)))
    }
  }, [enableWindowMode, normalizedData.length, viewStart, viewWindow])

  // Compute visible data
  const visibleData = useMemo(() => {
    if (enableWindowMode) {
      if (normalizedData.length === 0) return []
      const start = Math.max(0, Math.min(viewStart, normalizedData.length - 1))
      const end = Math.min(start + viewWindow, normalizedData.length)
      return normalizedData.slice(start, end)
    }
    return normalizedData
  }, [enableWindowMode, normalizedData, viewStart, viewWindow])

  // Sampling
  const maxSamplePoints = enableWindowMode ? 100 : COMPACT_MAX_POINTS
  const sampledData = useMemo(() => lttbSampling(visibleData, maxSamplePoints), [visibleData, maxSamplePoints])
  const labelIndices = useMemo(
    () => pickLabelIndices(sampledData.length, enableWindowMode ? MAX_HISTORY_POINT_LABELS : Math.min(6, MAX_POINT_LABELS)),
    [sampledData.length, enableWindowMode]
  )
  const xAxisIndices = useMemo(() => pickLabelIndices(sampledData.length, enableWindowMode ? 6 : 4), [sampledData.length, enableWindowMode])

  // Domain and scales
  const domain = useMemo(() => getLineDomain(sampledData.map((point) => point.value)), [sampledData])
  const spanMs = sampledData.length > 1
    ? sampledData[sampledData.length - 1].timestamp - sampledData[0].timestamp
    : 0

  const formatAxisLabel = useCallback((timestamp: number): string => {
    const date = new Date(timestamp)
    if (enableWindowMode || spanMs >= 3 * DAY_MS) {
      return date.toLocaleDateString(locale, { month: 'short', day: 'numeric' })
    }
    if (spanMs >= DAY_MS) {
      return date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
    }
    return date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
  }, [enableWindowMode, locale, spanMs])

  const chartWidth = viewBoxWidth - padding.left - padding.right
  const chartHeight = viewBoxHeight - padding.top - padding.bottom

  const xScale = useCallback((index: number) => {
    if (sampledData.length <= 1) return padding.left + chartWidth / 2
    return padding.left + (index / (sampledData.length - 1)) * chartWidth
  }, [chartWidth, sampledData.length, padding.left])

  const yScale = useCallback((value: number) => {
    const range = domain.max - domain.min || 1
    return padding.top + chartHeight - ((value - domain.min) / range) * chartHeight
  }, [chartHeight, domain.max, domain.min, padding.top])

  // Tooltip
  const { tooltip, show, hide } = useChartTooltip()

  const interaction = useChartInteraction({
    dataLength: sampledData.length,
    onPointChange: (index) => {
      const point = sampledData[index]
      if (point) {
        const ts = buildTooltipState(
          xScale(index),
          yScale(point.value),
          viewBoxWidth,
          viewBoxHeight,
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

  // Window mode: navigation
  const canScrollLeft = enableWindowMode && viewStart > 0
  const canScrollRight = enableWindowMode && viewStart + viewWindow < normalizedData.length

  const handleScroll = useCallback((direction: 'left' | 'right') => {
    if (!enableWindowMode) return
    const step = Math.max(1, Math.floor(viewWindow / 2))
    if (direction === 'left' && canScrollLeft) {
      setViewStart((prev) => Math.max(0, prev - step))
    } else if (direction === 'right' && canScrollRight) {
      setViewStart((prev) => Math.min(Math.max(0, normalizedData.length - viewWindow), prev + step))
    }
  }, [enableWindowMode, normalizedData.length, canScrollLeft, canScrollRight, viewWindow])

  const handleResetView = useCallback(() => {
    if (!enableWindowMode) return
    if (normalizedData.length === 0) return
    const nextWindow = Math.min(DEFAULT_VIEW_WINDOW, normalizedData.length)
    setViewWindow(nextWindow)
    setViewStart(Math.max(0, normalizedData.length - nextWindow))
    interaction.setActiveIndex(null)
    hide()
  }, [enableWindowMode, normalizedData.length, hide, interaction])

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

    if (current.pointerId === event.pointerId && enableWindowMode) {
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
        const nextStart = clamp(current.startView + deltaPoints, 0, Math.max(0, normalizedData.length - viewWindow))
        setViewStart(nextStart)
        return
      }
    }

    if (!isDragging && sampledData.length > 0) {
      const rect = element.getBoundingClientRect()
      handleMouseMove(event, rect)
    }
  }, [enableWindowMode, normalizedData.length, handleMouseMove, hide, isDragging, sampledData.length, setActiveIndex, viewWindow])

  const handleWheel = useCallback((event: WheelEvent) => {
    if (!enableWindowMode) return
    event.preventDefault()
    if (normalizedData.length === 0) return
    const minWindow = Math.min(4, normalizedData.length)
    const delta = event.deltaY > 0 ? 1 : -1
    const nextWindow = clamp(viewWindow + delta * 2, minWindow, normalizedData.length)
    const center = viewStart + viewWindow / 2
    const nextStart = clamp(Math.round(center - nextWindow / 2), 0, Math.max(0, normalizedData.length - nextWindow))
    setViewWindow(nextWindow)
    setViewStart(nextStart)
  }, [enableWindowMode, normalizedData.length, viewStart, viewWindow])

  const handleKeyboard = useCallback((event: KeyboardEvent) => {
    if (!enableWindowMode) {
      // Compact mode: only arrow keys for point navigation (handled by useChartInteraction)
      return
    }
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      handleScroll('left')
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      handleScroll('right')
    } else if (event.key === '+' || event.key === '=') {
      event.preventDefault()
      if (normalizedData.length === 0) return
      const minWindow = Math.min(4, normalizedData.length)
      const nextWindow = clamp(viewWindow - 2, minWindow, normalizedData.length)
      const center = viewStart + viewWindow / 2
      setViewWindow(nextWindow)
      setViewStart(clamp(Math.round(center - nextWindow / 2), 0, Math.max(0, normalizedData.length - nextWindow)))
    } else if (event.key === '-' || event.key === '_') {
      event.preventDefault()
      if (normalizedData.length === 0) return
      const minWindow = Math.min(4, normalizedData.length)
      const nextWindow = clamp(viewWindow + 2, minWindow, normalizedData.length)
      const center = viewStart + viewWindow / 2
      setViewWindow(nextWindow)
      setViewStart(clamp(Math.round(center - nextWindow / 2), 0, Math.max(0, normalizedData.length - nextWindow)))
    } else if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      handleResetView()
    }
  }, [enableWindowMode, normalizedData.length, handleResetView, handleScroll, viewStart, viewWindow])

  if (normalizedData.length === 0) {
    return (
      <div class={`chart-container timeseries-chart${enableWindowMode ? ' window-mode' : ' compact-mode'}`}>
        <div class="chart-header">
          <h3>{title}</h3>
        </div>
        <div class="chart-body chart-empty">
          <div class="empty-state-icon" style={{ width: '56px', height: '56px' }}><Icon name="chart" size={26} /></div>
          <div class="chart-empty-title">{t('charts.lineEmpty')}</div>
          <div class="chart-empty-hint">{t('charts.lineEmptyHint')}</div>
        </div>
      </div>
    )
  }

  return (
    <div class={`chart-container timeseries-chart${enableWindowMode ? ' window-mode' : ' compact-mode'}`}>
      <div class="chart-header">
        {enableWindowMode ? (
          <div class="history-header-main">
            <h3>{title}</h3>
            <div class="chart-summary history-summary">
              <span class="chart-summary-label">{activePoint ? t('charts.current') : t('charts.latest')}</span>
              <strong class="chart-summary-value">{formatPointLabel(metricPoint.value)}{unit}</strong>
            </div>
          </div>
        ) : (
          <>
            <h3>{title}</h3>
            <div class="chart-summary">
              <span class="chart-summary-label">{activePoint ? t('charts.current') : t('charts.latest')}</span>
              <strong class="chart-summary-value">{formatPointLabel(metricPoint.value)}{unit}</strong>
            </div>
          </>
        )}

        {enableWindowMode && (
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
              ({t('timeseries.dataPoints').replace('{count}', String(normalizedData.length))})
            </span>
            <button
              type="button"
              class="history-nav-btn history-nav-btn-primary"
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
        )}
      </div>

      <div
        class={`chart-body interactive${isDragging ? ' dragging' : ''}`}
        tabIndex={0}
        onFocus={interaction.handleFocus}
        onBlur={interaction.handleBlur}
        onKeyDown={handleKeyboard}
        onPointerDown={enableWindowMode ? handlePointerDown : undefined}
        onPointerMove={enableWindowMode ? handlePointerMove : undefined}
        onPointerUp={enableWindowMode ? (event) => resetDrag(event) : undefined}
        onPointerCancel={enableWindowMode ? (event) => {
          resetDrag(event)
          interaction.setActiveIndex(null)
          hide()
        } : undefined}
        onPointerLeave={(event) => {
          if (enableWindowMode) {
            if (!dragRef.current.dragging && event.pointerType === 'mouse') {
              handleMouseLeave()
            }
          } else {
            if (event.pointerType === 'mouse') handleMouseLeave()
          }
        }}
        onWheel={enableWindowMode ? handleWheel : undefined}
        onDblClick={enableWindowMode ? handleResetView : undefined}
        style={{ cursor: enableWindowMode ? (isDragging ? 'grabbing' : 'grab') : undefined }}
      >
        <ChartFrame
          width={viewBoxWidth}
          height={viewBoxHeight}
          className={enableWindowMode ? 'history-svg' : undefined}
          ariaLabel={title ? `${title} chart` : 'Timeseries chart'}
        >
          <defs>
            <linearGradient id={fillGradientId} x1="0%" y1="0%" x2="0%" y2="100%">
              <stop offset="0%" stop-color={color} stop-opacity={enableWindowMode ? '0.30' : '0.34'} />
              <stop offset="48%" stop-color={color} stop-opacity={enableWindowMode ? '0.16' : '0.18'} />
              <stop offset="100%" stop-color={color} stop-opacity={enableWindowMode ? '0.02' : '0.02'} />
            </linearGradient>
            <filter id={glowId} x="-20%" y="-20%" width="140%" height="140%">
              <feGaussianBlur stdDeviation={enableWindowMode ? '6' : '5.5'} result="blur" />
              <feComposite in="SourceGraphic" in2="blur" operator="over" />
            </filter>
            <radialGradient id={focusGradientId} cx="50%" cy="50%" r="50%">
              <stop offset="0%" stop-color={color} stop-opacity={enableWindowMode ? '0.30' : '0.32'} />
              <stop offset="55%" stop-color={color} stop-opacity={enableWindowMode ? '0.11' : '0.12'} />
              <stop offset="100%" stop-color={color} stop-opacity="0" />
            </radialGradient>
          </defs>

          <rect
            class="chart-plot-backdrop"
            x={padding.left}
            y={padding.top}
            width={chartWidth}
            height={chartHeight}
            rx={enableWindowMode ? '24' : '20'}
          />

          <g class="grid-lines">
            {Array.from({ length: enableWindowMode ? 6 : 5 }).map((_, index) => {
              const y = padding.top + (chartHeight / (enableWindowMode ? 5 : 4)) * index
              const value = domain.max - ((domain.max - domain.min) / (enableWindowMode ? 5 : 4)) * index
              return (
                <g key={index}>
                  <line
                    class={`chart-grid-line${index === (enableWindowMode ? 5 : 4) ? ' is-baseline' : ''}`}
                    x1={padding.left}
                    y1={y}
                    x2={viewBoxWidth - padding.right}
                    y2={y}
                  />
                  <text
                    class="chart-grid-label"
                    x={padding.left - 10}
                    y={y}
                    text-anchor="end"
                    dominant-baseline="middle"
                  >
                    {formatPointLabel(value)}
                    {unit}
                  </text>
                </g>
              )
            })}
          </g>

          {activePoint && interaction.activeIndex !== null && (
            <rect
              class="chart-focus-band"
              x={xScale(interaction.activeIndex) - (enableWindowMode ? 10 : 9)}
              y={padding.top + 4}
              width={enableWindowMode ? '20' : '18'}
              height={chartHeight - 8}
              rx={enableWindowMode ? '10' : '9'}
              fill={color}
              opacity="0.08"
            />
          )}

          {sampledData.length > 1 && (
            <path
              class="chart-series-area"
              d={buildAreaPath(sampledData, xScale, yScale, padding.top + chartHeight)}
              fill={`url(#${fillGradientId})`}
            />
          )}
          {sampledData.length > 1 && (
            <path
              class="chart-series-line-glow"
              d={buildLinePath(sampledData, xScale, yScale)}
              fill="none"
              stroke={color}
              stroke-width={enableWindowMode ? '9' : '8'}
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
              stroke-width={enableWindowMode ? '3.3' : '3.2'}
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          )}
          {sampledData.length === 1 && (
            <>
              <circle
                cx={xScale(0)}
                cy={yScale(sampledData[0].value)}
                r={enableWindowMode ? '16' : '14'}
                fill={color}
                opacity={enableWindowMode ? '0.12' : '0.14'}
              />
              <circle
                cx={xScale(0)}
                cy={yScale(sampledData[0].value)}
                r={enableWindowMode ? '9' : '8'}
                fill={color}
                opacity={enableWindowMode ? '0.30' : '0.35'}
              />
              <circle
                cx={xScale(0)}
                cy={yScale(sampledData[0].value)}
                r={enableWindowMode ? '5' : '4.5'}
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
              y1={padding.top}
              x2={xScale(interaction.activeIndex)}
              y2={padding.top + chartHeight}
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
                      r={highlighted ? (enableWindowMode ? 9 : 8.5) : (enableWindowMode ? 6 : 5.4)}
                      fill={color}
                      opacity={highlighted ? (enableWindowMode ? '0.17' : '0.16') : '0.10'}
                    />
                  )}
                  <circle
                    class={`chart-series-point${highlighted ? ' is-active' : marked ? ' is-key' : ''}`}
                    cx={x}
                    cy={y}
                    r={highlighted ? (enableWindowMode ? 5.1 : 4.8) : marked ? (enableWindowMode ? 3.6 : 3.3) : (enableWindowMode ? 2.5 : 2.2)}
                    fill={color}
                    opacity={highlighted || marked ? 1 : (enableWindowMode ? 0.52 : 0.5)}
                    stroke="var(--bg-primary)"
                    stroke-width={highlighted ? (enableWindowMode ? 2.2 : 2.1) : 1.4}
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
              const placeBelow = y <= padding.top + 14
              return (
                <text
                  class={`chart-point-label${index === interaction.activeIndex ? ' is-active' : ''}`}
                  key={`label-${point.timestamp}-${index}`}
                  x={x}
                  y={placeBelow ? y + (enableWindowMode ? 12 : 10) : y - 10}
                  text-anchor="middle"
                  dominant-baseline={placeBelow ? 'hanging' : 'auto'}
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
                  class={`chart-axis-label${enableWindowMode ? ' history-axis-label' : ''}`}
                  key={`axis-${point.timestamp}-${index}`}
                  x={xScale(index)}
                  y={viewBoxHeight - 14}
                  text-anchor="middle"
                >
                  {formatAxisLabel(point.timestamp)}
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
                r={enableWindowMode ? '18' : '16'}
                fill={`url(#${focusGradientId})`}
              />
              <circle
                class="chart-focus-dot"
                cx={xScale(interaction.activeIndex)}
                cy={yScale(activePoint.value)}
                r={enableWindowMode ? '10' : '9'}
                fill={color}
                opacity="0.22"
              />
              <circle
                class="chart-focus-dot-inner"
                cx={xScale(interaction.activeIndex)}
                cy={yScale(activePoint.value)}
                r={enableWindowMode ? '5' : '4.8'}
                fill={color}
                stroke="var(--bg-primary)"
                stroke-width="2.2"
              />
            </>
          )}

          <rect
            class="chart-event-layer"
            x={padding.left}
            y={padding.top}
            width={chartWidth}
            height={chartHeight}
            fill="transparent"
            onPointerMove={!enableWindowMode ? (event) => interaction.handleMouseMove(event, event.currentTarget.getBoundingClientRect()) : undefined}
            onPointerDown={!enableWindowMode ? (event) => interaction.handleMouseMove(event, event.currentTarget.getBoundingClientRect()) : undefined}
            onPointerLeave={!enableWindowMode ? (event) => {
              if (event.pointerType === 'mouse') interaction.handleMouseLeave()
            } : undefined}
            onPointerCancel={!enableWindowMode ? () => interaction.handleMouseLeave() : undefined}
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

      {enableWindowMode && (
        <div class="history-hint">
          {t('timeseries.dragHint')}
        </div>
      )}
    </div>
  )
}

export const TimeSeriesChart = memo(TimeSeriesChartComponent)
