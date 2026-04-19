import { useCallback, useState } from 'preact/compat'

export interface TooltipState {
  visible: boolean
  x: number
  y: number
  content: string
  meta?: string
}

export function useChartTooltip(): {
  tooltip: TooltipState
  show: (x: number, y: number, content: string, meta?: string) => void
  hide: () => void
} {
  const [tooltip, setTooltip] = useState<TooltipState>({
    visible: false,
    x: 0,
    y: 0,
    content: '',
  })

  const show = useCallback((x: number, y: number, content: string, meta?: string) => {
    setTooltip({ visible: true, x, y, content, meta })
  }, [])

  const hide = useCallback(() => {
    setTooltip((prev) => ({ ...prev, visible: false }))
  }, [])

  return { tooltip, show, hide }
}
