import { fireEvent, render } from '@testing-library/preact'
import { describe, expect, it, vi } from 'vitest'
import { ChartFrame } from './ChartFrame'

describe('ChartFrame', () => {
  it('renders an accessible svg with a stable viewBox', () => {
    const { getByRole } = render(
      <ChartFrame width={320} height={160} ariaLabel="Latency chart" className="custom-chart">
        <circle cx="10" cy="10" r="4" />
      </ChartFrame>
    )

    const svg = getByRole('img', { name: 'Latency chart' })
    expect(svg.getAttribute('viewBox')).toBe('0 0 320 160')
    expect(svg.getAttribute('preserveAspectRatio')).toBe('none')
    expect(svg.getAttribute('class')).toBe('chart-svg custom-chart')
    expect(svg.querySelector('circle')).toBeTruthy()
  })

  it('derives a default label from the title and forwards pointer handlers', () => {
    const onMouseLeave = vi.fn()

    const { getByRole } = render(
      <ChartFrame width={100} height={100} title="Requests" onMouseLeave={onMouseLeave}>
        <path d="M 0 0 L 10 10" />
      </ChartFrame>
    )

    const svg = getByRole('img', { name: 'Requests chart' })
    fireEvent.mouseLeave(svg)
    expect(onMouseLeave).toHaveBeenCalledTimes(1)
  })
})
