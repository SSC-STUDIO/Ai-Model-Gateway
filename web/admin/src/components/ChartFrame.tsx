import type { ComponentChildren, RefObject } from 'preact'
import { memo } from 'preact/compat'

interface ChartFrameProps {
  width: number
  height: number
  title?: string
  children: ComponentChildren
  ariaLabel?: string
  onKeyDown?: (e: KeyboardEvent) => void
  onMouseMove?: (e: MouseEvent) => void
  onMouseLeave?: () => void
  svgRef?: RefObject<SVGSVGElement>
  className?: string
  preserveAspectRatio?: string
}

const ChartFrameComponent = ({
  width,
  height,
  title,
  children,
  ariaLabel,
  onKeyDown,
  onMouseMove,
  onMouseLeave,
  svgRef,
  className,
  preserveAspectRatio = 'none',
}: ChartFrameProps) => {
  const label = ariaLabel || (title ? `${title} chart` : 'Data chart')
  const classNames = ['chart-svg', className].filter(Boolean).join(' ')

  return (
    <svg
      ref={svgRef}
      class={classNames}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio={preserveAspectRatio}
      role="img"
      aria-label={label}
      onKeyDown={onKeyDown}
      onMouseMove={onMouseMove}
      onMouseLeave={onMouseLeave}
    >
      {children}
    </svg>
  )
}

export const ChartFrame = memo(ChartFrameComponent)
