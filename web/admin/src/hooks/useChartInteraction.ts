import { useCallback, useEffect, useRef, useState } from 'preact/compat'
import { clamp, clampActiveIndex, getNearestIndexFromClientX } from '../utils/charting'

export interface ChartInteractionOptions {
  dataLength: number
  onPointChange?: (index: number) => void
  onClear?: () => void
}

export interface ChartInteractionState {
  activeIndex: number | null
  setActiveIndex: (index: number | null) => void
  handleKeyDown: (e: KeyboardEvent) => void
  handleMouseMove: (e: MouseEvent, svgRect: DOMRect) => void
  handleMouseLeave: () => void
  handleFocus: () => void
  handleBlur: () => void
}

export function useChartInteraction(options: ChartInteractionOptions): ChartInteractionState {
  const { dataLength, onPointChange, onClear } = options
  const [activeIndex, setActiveIndex] = useState<number | null>(null)

  const onPointChangeRef = useRef(onPointChange)
  onPointChangeRef.current = onPointChange
  const onClearRef = useRef(onClear)
  onClearRef.current = onClear

  useEffect(() => {
    setActiveIndex((prev) => {
      const next = clampActiveIndex(prev, dataLength)
      if (next !== null && next !== prev) onPointChangeRef.current?.(next)
      return next
    })
  }, [dataLength])

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (dataLength === 0) return
      setActiveIndex((prev) => {
        let next: number | null = prev
        if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') {
          event.preventDefault()
          next = clamp((prev ?? dataLength - 1) - 1, 0, dataLength - 1)
        } else if (event.key === 'ArrowRight' || event.key === 'ArrowUp') {
          event.preventDefault()
          next = clamp((prev ?? -1) + 1, 0, dataLength - 1)
        } else if (event.key === 'Home') {
          event.preventDefault()
          next = 0
        } else if (event.key === 'End') {
          event.preventDefault()
          next = dataLength - 1
        }
        if (next !== prev) {
          if (next !== null) onPointChangeRef.current?.(next)
          else onClearRef.current?.()
        }
        return next
      })
    },
    [dataLength]
  )

  const handleMouseMove = useCallback(
    (event: MouseEvent, svgRect: DOMRect) => {
      if (dataLength === 0) return
      const nextIndex = getNearestIndexFromClientX(event.clientX, svgRect.left, svgRect.width, dataLength)
      setActiveIndex(nextIndex)
      if (nextIndex !== null) onPointChangeRef.current?.(nextIndex)
      else onClearRef.current?.()
    },
    [dataLength]
  )

  const handleMouseLeave = useCallback(() => {
    setActiveIndex(null)
    onClearRef.current?.()
  }, [])

  const handleFocus = useCallback(() => {
    setActiveIndex((prev) => {
      if (prev !== null) return prev
      const next = dataLength > 0 ? dataLength - 1 : null
      if (next !== null) onPointChangeRef.current?.(next)
      return next
    })
  }, [dataLength])

  const handleBlur = useCallback(() => {
    setActiveIndex(null)
    onClearRef.current?.()
  }, [])

  return {
    activeIndex,
    setActiveIndex,
    handleKeyDown,
    handleMouseMove,
    handleMouseLeave,
    handleFocus,
    handleBlur,
  }
}
