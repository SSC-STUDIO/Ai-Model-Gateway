import { memo, useEffect, useRef, useState, useMemo } from 'preact/compat'
import { lttbSampling, type DataPoint } from '../utils/dataSampling'

type ChartProps = {
  data: DataPoint[]
  title: string
  color?: string
  unit?: string
}

const MAX_POINTS = 150

const LineChartComponent = ({ data, title, color = '#3b82f6', unit = '' }: ChartProps) => {
  const svgRef = useRef<SVGSVGElement>(null)
  const [tooltip, setTooltip] = useState<{ x: number; y: number; value: number; time: string } | null>(null)
  const prevDataRef = useRef<DataPoint[]>([])
  const renderedRef = useRef(false)

  const sampledData = useMemo(() => {
    return lttbSampling(data, MAX_POINTS)
  }, [data])

  const gradientId = useMemo(() => `gradient-${title.replace(/\s+/g, '-')}-${Math.random().toString(36).slice(2, 8)}`, [title])

  const shouldRedraw = useMemo(() => {
    if (!renderedRef.current) return true
    if (sampledData.length !== prevDataRef.current.length) return true
    if (sampledData.length === 0) return false

    const lastIdx = sampledData.length - 1
    const prevLastIdx = prevDataRef.current.length - 1

    if (sampledData[lastIdx].timestamp !== prevDataRef.current[prevLastIdx]?.timestamp) return true
    if (sampledData[lastIdx].value !== prevDataRef.current[prevLastIdx]?.value) return true

    return false
  }, [sampledData])

  useEffect(() => {
    if (!svgRef.current || sampledData.length === 0) return
    if (!shouldRedraw) return

    const svg = svgRef.current
    const width = svg.clientWidth || 400
    const height = svg.clientHeight || 200
    const padding = { top: 20, right: 20, bottom: 30, left: 50 }
    const chartWidth = Math.max(0, width - padding.left - padding.right)
    const chartHeight = Math.max(0, height - padding.top - padding.bottom)

    if (chartWidth <= 0 || chartHeight <= 0) return

    while (svg.firstChild) {
      svg.removeChild(svg.firstChild)
    }

    const values = sampledData.map(d => d.value)
    const minVal = Math.min(...values)
    const maxVal = Math.max(...values)
    const range = maxVal - minVal || 1

    const xScale = (i: number) => padding.left + (i / (sampledData.length - 1 || 1)) * chartWidth
    const yScale = (v: number) => padding.top + chartHeight - ((v - minVal) / range) * chartHeight

    const fragment = document.createDocumentFragment()

    const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs')
    const gradient = document.createElementNS('http://www.w3.org/2000/svg', 'linearGradient')
    gradient.setAttribute('id', gradientId)
    gradient.setAttribute('x1', '0%')
    gradient.setAttribute('y1', '0%')
    gradient.setAttribute('x2', '0%')
    gradient.setAttribute('y2', '100%')

    const stop1 = document.createElementNS('http://www.w3.org/2000/svg', 'stop')
    stop1.setAttribute('offset', '0%')
    stop1.setAttribute('stop-color', color)
    stop1.setAttribute('stop-opacity', '0.3')

    const stop2 = document.createElementNS('http://www.w3.org/2000/svg', 'stop')
    stop2.setAttribute('offset', '100%')
    stop2.setAttribute('stop-color', color)
    stop2.setAttribute('stop-opacity', '0.05')

    gradient.appendChild(stop1)
    gradient.appendChild(stop2)
    defs.appendChild(gradient)
    fragment.appendChild(defs)

    const gridGroup = document.createElementNS('http://www.w3.org/2000/svg', 'g')
    gridGroup.setAttribute('class', 'grid-lines')

    for (let i = 0; i <= 4; i++) {
      const y = padding.top + (chartHeight / 4) * i
      const line = document.createElementNS('http://www.w3.org/2000/svg', 'line')
      line.setAttribute('x1', String(padding.left))
      line.setAttribute('y1', String(y))
      line.setAttribute('x2', String(width - padding.right))
      line.setAttribute('y2', String(y))
      line.setAttribute('stroke', 'var(--border-color)')
      line.setAttribute('stroke-opacity', '0.3')
      line.setAttribute('stroke-dasharray', '4,4')
      gridGroup.appendChild(line)
    }
    fragment.appendChild(gridGroup)

    const areaPath = document.createElementNS('http://www.w3.org/2000/svg', 'path')
    let areaD = `M ${xScale(0)} ${yScale(sampledData[0].value)}`
    for (let i = 1; i < sampledData.length; i++) {
      areaD += ` L ${xScale(i)} ${yScale(sampledData[i].value)}`
    }
    areaD += ` L ${xScale(sampledData.length - 1)} ${padding.top + chartHeight}`
    areaD += ` L ${xScale(0)} ${padding.top + chartHeight} Z`
    areaPath.setAttribute('d', areaD)
    areaPath.setAttribute('fill', `url(#${gradientId})`)
    fragment.appendChild(areaPath)

    const linePath = document.createElementNS('http://www.w3.org/2000/svg', 'path')
    let lineD = `M ${xScale(0)} ${yScale(sampledData[0].value)}`
    for (let i = 1; i < sampledData.length; i++) {
      lineD += ` L ${xScale(i)} ${yScale(sampledData[i].value)}`
    }
    linePath.setAttribute('d', lineD)
    linePath.setAttribute('fill', 'none')
    linePath.setAttribute('stroke', color)
    linePath.setAttribute('stroke-width', '2')
    linePath.setAttribute('stroke-linecap', 'round')
    linePath.setAttribute('stroke-linejoin', 'round')
    fragment.appendChild(linePath)

    const pointsGroup = document.createElementNS('http://www.w3.org/2000/svg', 'g')
    pointsGroup.setAttribute('class', 'data-points')

    const step = Math.ceil(sampledData.length / 50)

    sampledData.forEach((point, i) => {
      if (i % step !== 0 && i !== sampledData.length - 1) return

      const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
      circle.setAttribute('cx', String(xScale(i)))
      circle.setAttribute('cy', String(yScale(point.value)))
      circle.setAttribute('r', '3')
      circle.setAttribute('fill', color)
      circle.setAttribute('stroke', 'var(--bg-primary)')
      circle.setAttribute('stroke-width', '2')
      circle.setAttribute('style', 'cursor: pointer')
      circle.setAttribute('data-index', i.toString())

      circle.addEventListener('mouseenter', () => {
        setTooltip({
          x: xScale(i),
          y: yScale(point.value),
          value: point.value,
          time: new Date(point.timestamp).toLocaleTimeString(),
        })
      })

      circle.addEventListener('mouseleave', () => {
        setTooltip(null)
      })

      pointsGroup.appendChild(circle)
    })

    fragment.appendChild(pointsGroup)
    svg.appendChild(fragment)

    prevDataRef.current = sampledData
    renderedRef.current = true
  }, [sampledData, color, gradientId, shouldRedraw])

  return (
    <div class="chart-container">
      <div class="chart-header">
        <h3>{title}</h3>
      </div>
      <div class="chart-body">
        <svg ref={svgRef} class="chart-svg" />
        {tooltip && (
          <div
            class="chart-tooltip"
            style={{
              left: tooltip.x + 'px',
              top: tooltip.y + 'px',
            }}
          >
            <div class="tooltip-value">
              {tooltip.value.toFixed(2)}
              {unit}
            </div>
            <div class="tooltip-time">{tooltip.time}</div>
          </div>
        )}
      </div>
    </div>
  )
}

export const LineChart = memo(LineChartComponent, (prev, next) => {
  if (prev.title !== next.title) return false
  if (prev.color !== next.color) return false
  if (prev.unit !== next.unit) return false
  if (prev.data.length !== next.data.length) return false
  if (prev.data.length === 0) return true

  const lastIdx = prev.data.length - 1
  return (
    prev.data[lastIdx].timestamp === next.data[lastIdx].timestamp &&
    prev.data[lastIdx].value === next.data[lastIdx].value
  )
})

interface DonutChartProps {
  data: { label: string; value: number; color: string }[]
  title: string
}

const DonutChartComponent = ({ data, title }: DonutChartProps) => {
  const svgRef = useRef<SVGSVGElement>(null)
  const prevDataRef = useRef<DonutChartProps['data']>([])
  const renderedRef = useRef(false)

  const total = useMemo(() => data.reduce((sum, d) => sum + d.value, 0), [data])

  const shouldRedraw = useMemo(() => {
    if (!renderedRef.current) return true
    if (data.length !== prevDataRef.current.length) return true
    return data.some((d, i) =>
      d.label !== prevDataRef.current[i]?.label ||
      d.value !== prevDataRef.current[i]?.value ||
      d.color !== prevDataRef.current[i]?.color
    )
  }, [data])

  useEffect(() => {
    if (!svgRef.current || data.length === 0) return
    if (!shouldRedraw) return

    const svg = svgRef.current
    const size = Math.min(svg.clientWidth || 200, svg.clientHeight || 200)
    const centerX = size / 2
    const centerY = size / 2
    const radius = size / 2 - 20
    const innerRadius = radius * 0.6

    while (svg.firstChild) {
      svg.removeChild(svg.firstChild)
    }

    const fragment = document.createDocumentFragment()
    let currentAngle = -Math.PI / 2

    data.forEach((segment) => {
      if (segment.value <= 0) return

      const angle = (segment.value / total) * Math.PI * 2
      const path = document.createElementNS('http://www.w3.org/2000/svg', 'path')

      const x1 = centerX + Math.cos(currentAngle) * radius
      const y1 = centerY + Math.sin(currentAngle) * radius
      const x2 = centerX + Math.cos(currentAngle + angle) * radius
      const y2 = centerY + Math.sin(currentAngle + angle) * radius
      const x3 = centerX + Math.cos(currentAngle + angle) * innerRadius
      const y3 = centerY + Math.sin(currentAngle + angle) * innerRadius
      const x4 = centerX + Math.cos(currentAngle) * innerRadius
      const y4 = centerY + Math.sin(currentAngle) * innerRadius

      const largeArc = angle > Math.PI ? 1 : 0

      const d = [
        `M ${x1} ${y1}`,
        `A ${radius} ${radius} 0 ${largeArc} 1 ${x2} ${y2}`,
        `L ${x3} ${y3}`,
        `A ${innerRadius} ${innerRadius} 0 ${largeArc} 0 ${x4} ${y4}`,
        'Z',
      ].join(' ')

      path.setAttribute('d', d)
      path.setAttribute('fill', segment.color)
      path.setAttribute('stroke', 'var(--bg-primary)')
      path.setAttribute('stroke-width', '2')

      fragment.appendChild(path)
      currentAngle += angle
    })

    svg.appendChild(fragment)

    prevDataRef.current = data
    renderedRef.current = true
  }, [data, total, shouldRedraw])

  return (
    <div class="chart-container donut">
      <div class="chart-header">
        <h3>{title}</h3>
      </div>
      <div class="chart-body">
        <svg ref={svgRef} class="chart-svg donut-svg" />
      </div>
    </div>
  )
}

export const DonutChart = memo(DonutChartComponent, (prev, next) => {
  if (prev.title !== next.title) return false
  if (prev.data.length !== next.data.length) return false
  return prev.data.every((d, i) =>
    d.label === next.data[i]?.label &&
    d.value === next.data[i]?.value &&
    d.color === next.data[i]?.color
  )
})

interface BarChartProps {
  data: { label: string; value: number; color?: string }[]
  title: string
  unit?: string
  horizontal?: boolean
}

const BarChartComponent = ({ data, title, unit = '', horizontal = false }: BarChartProps) => {
  const svgRef = useRef<SVGSVGElement>(null)
  const [tooltip, setTooltip] = useState<{ x: number; y: number; label: string; value: number } | null>(null)
  const prevDataRef = useRef<BarChartProps['data']>([])
  const renderedRef = useRef(false)

  const colors = useMemo(() => {
    const defaultColors = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
    return data.map((d, i) => d.color || defaultColors[i % defaultColors.length])
  }, [data])

  const shouldRedraw = useMemo(() => {
    if (!renderedRef.current) return true
    if (data.length !== prevDataRef.current.length) return true
    return data.some((d, i) =>
      d.label !== prevDataRef.current[i]?.label ||
      d.value !== prevDataRef.current[i]?.value
    )
  }, [data])

  useEffect(() => {
    if (!svgRef.current || data.length === 0) return
    if (!shouldRedraw) return

    const svg = svgRef.current
    const width = svg.clientWidth || 400
    const height = svg.clientHeight || 200
    const padding = { top: 20, right: 30, bottom: horizontal ? 30 : 60, left: horizontal ? 100 : 50 }
    const chartWidth = Math.max(0, width - padding.left - padding.right)
    const chartHeight = Math.max(0, height - padding.top - padding.bottom)

    if (chartWidth <= 0 || chartHeight <= 0) return

    while (svg.firstChild) {
      svg.removeChild(svg.firstChild)
    }

    const maxVal = Math.max(...data.map(d => d.value), 1)
    const fragment = document.createDocumentFragment()

    if (horizontal) {
      // Horizontal bar chart
      const barHeight = chartHeight / data.length * 0.7
      const barSpacing = chartHeight / data.length * 0.3

      data.forEach((item, i) => {
        const y = padding.top + i * (barHeight + barSpacing)
        const barWidth = (item.value / maxVal) * chartWidth

        // Bar
        const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
        rect.setAttribute('x', String(padding.left))
        rect.setAttribute('y', String(y))
        rect.setAttribute('width', String(barWidth))
        rect.setAttribute('height', String(barHeight))
        rect.setAttribute('fill', colors[i])
        rect.setAttribute('rx', '4')
        rect.setAttribute('style', 'cursor: pointer')

        rect.addEventListener('mouseenter', () => {
          setTooltip({ x: padding.left + barWidth + 5, y: y + barHeight / 2, label: item.label, value: item.value })
        })
        rect.addEventListener('mouseleave', () => {
          setTooltip(null)
        })

        fragment.appendChild(rect)

        // Label
        const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
        text.setAttribute('x', String(padding.left - 5))
        text.setAttribute('y', String(y + barHeight / 2))
        text.setAttribute('text-anchor', 'end')
        text.setAttribute('dominant-baseline', 'middle')
        text.setAttribute('fill', 'var(--text-primary)')
        text.setAttribute('font-size', '12')
        text.textContent = item.label.length > 15 ? item.label.slice(0, 15) + '...' : item.label
        fragment.appendChild(text)
      })
    } else {
      // Vertical bar chart
      const barWidth = chartWidth / data.length * 0.7
      const barSpacing = chartWidth / data.length * 0.3

      data.forEach((item, i) => {
        const x = padding.left + i * (barWidth + barSpacing)
        const barHeight = (item.value / maxVal) * chartHeight
        const y = padding.top + chartHeight - barHeight

        // Bar
        const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
        rect.setAttribute('x', String(x))
        rect.setAttribute('y', String(y))
        rect.setAttribute('width', String(barWidth))
        rect.setAttribute('height', String(barHeight))
        rect.setAttribute('fill', colors[i])
        rect.setAttribute('rx', '4')
        rect.setAttribute('style', 'cursor: pointer')

        rect.addEventListener('mouseenter', () => {
          setTooltip({ x: x + barWidth / 2, y: y - 10, label: item.label, value: item.value })
        })
        rect.addEventListener('mouseleave', () => {
          setTooltip(null)
        })

        fragment.appendChild(rect)

        // Label
        const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
        text.setAttribute('x', String(x + barWidth / 2))
        text.setAttribute('y', String(height - 5))
        text.setAttribute('text-anchor', 'middle')
        text.setAttribute('fill', 'var(--text-primary)')
        text.setAttribute('font-size', '10')
        text.textContent = item.label.length > 8 ? item.label.slice(0, 8) + '...' : item.label
        fragment.appendChild(text)
      })
    }

    svg.appendChild(fragment)

    prevDataRef.current = data
    renderedRef.current = true
  }, [data, colors, horizontal, shouldRedraw])

  return (
    <div class="chart-container">
      <div class="chart-header">
        <h3>{title}</h3>
      </div>
      <div class="chart-body">
        <svg ref={svgRef} class="chart-svg" />
        {tooltip && (
          <div
            class="chart-tooltip"
            style={{
              left: tooltip.x + 'px',
              top: tooltip.y + 'px',
            }}
          >
            <div class="tooltip-label">{tooltip.label}</div>
            <div class="tooltip-value">
              {tooltip.value.toFixed(2)}
              {unit}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export const BarChart = memo(BarChartComponent, (prev, next) => {
  if (prev.title !== next.title) return false
  if (prev.unit !== next.unit) return false
  if (prev.horizontal !== next.horizontal) return false
  if (prev.data.length !== next.data.length) return false
  return prev.data.every((d, i) =>
    d.label === next.data[i]?.label &&
    d.value === next.data[i]?.value &&
    d.color === next.data[i]?.color
  )
})
