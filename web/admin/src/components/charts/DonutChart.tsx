import { memo, useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import { ChartFrame } from '../ChartFrame'
import {
  clamp,
  clampActiveIndex,
  collapseLabeledValues,
  describeDonutArc,
  formatPointLabel,
  sanitizeLabeledValues,
  truncateLabel,
} from '../../utils/charting'
import { EmptyChart } from './EmptyChart'

interface DonutChartProps {
  data: { label: string; value: number; color: string }[]
  title: string
  singleRowLegend?: boolean
}

const DONUT_VIEWBOX_SIZE = 240
const MAX_DONUT_SEGMENTS = 8
const OTHER_DONUT_COLOR = '#94a3b8'

const DonutChartComponent = ({ data, title, singleRowLegend = false }: DonutChartProps) => {
  const { t } = useI18n()
  const [activeIndex, setActiveIndex] = useState<number | null>(null)

  const segments = useMemo(
    () => {
      const sorted = sanitizeLabeledValues(data)
        .filter((segment) => segment.value >= 0)
        .sort((left, right) => right.value - left.value || left.label.localeCompare(right.label))
      return collapseLabeledValues(sorted, MAX_DONUT_SEGMENTS, t('charts.other'), OTHER_DONUT_COLOR)
    },
    [data, t]
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