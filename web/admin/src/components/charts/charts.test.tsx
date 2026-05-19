import { fireEvent, render, screen } from '@testing-library/preact'
import { beforeEach, describe, expect, it } from 'vitest'
import { I18nProvider } from '../../i18n'
import { BarChart } from './BarChart'
import { DonutChart } from './DonutChart'
import { EmptyChart } from './EmptyChart'
import { LineChart } from './LineChart'

function renderWithI18n(children: preact.ComponentChildren) {
  localStorage.setItem('admin-locale', 'en')
  return render(<I18nProvider>{children}</I18nProvider>)
}

describe('chart components', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('renders a localized empty chart state', () => {
    renderWithI18n(<EmptyChart title="Requests" message="No requests yet" hint="Traffic will appear here." />)

    expect(screen.getByRole('img', { name: 'Empty chart' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Requests' })).toBeTruthy()
    expect(screen.getByText('No requests yet')).toBeTruthy()
    expect(screen.getByText('Traffic will appear here.')).toBeTruthy()
  })

  it('renders the bar chart summary and filters invalid values', () => {
    const { container } = renderWithI18n(
      <BarChart
        title="Requests by model"
        unit=" req"
        data={[
          { label: 'gpt-5', value: 120 },
          { label: ' ', value: 99 },
          { label: 'gpt-4.1', value: Number.NaN },
          { label: 'gpt-4o-mini', value: 35 },
        ]}
      />
    )

    expect(screen.getByRole('heading', { name: 'Requests by model' })).toBeTruthy()
    expect(screen.getByText('Max')).toBeTruthy()
    expect(screen.getAllByText('120 req')).toHaveLength(2)
    expect(screen.getByRole('img', { name: 'Requests by model chart' })).toBeTruthy()
    expect(container.querySelectorAll('.bar-rect')).toHaveLength(2)
  })

  it('shows the bar empty state when all values are invalid', () => {
    renderWithI18n(<BarChart title="Requests by model" data={[{ label: ' ', value: Number.NaN }]} />)

    expect(screen.getByRole('img', { name: 'Empty chart' })).toBeTruthy()
    expect(screen.getByText('No bar data yet')).toBeTruthy()
  })

  it('renders donut totals, collapses overflow segments, and responds to keyboard focus', () => {
    const data = Array.from({ length: 10 }, (_, index) => ({
      label: `provider-${index}`,
      value: 10 - index,
      color: `#${(index + 1).toString().repeat(6).slice(0, 6)}`,
    }))

    const { container } = renderWithI18n(<DonutChart title="Provider share" data={data} />)

    expect(screen.getByRole('heading', { name: 'Provider share' })).toBeTruthy()
    expect(screen.getByText('Total')).toBeTruthy()
    expect(screen.getAllByText('55.0')).toHaveLength(2)
    expect(screen.getByText('Other')).toBeTruthy()
    expect(screen.getAllByText('3')).toHaveLength(2)

    const chartBody = container.querySelector('.donut-body') as HTMLElement
    fireEvent.focus(chartBody)
    fireEvent.keyDown(chartBody, { key: 'ArrowRight' })
    expect(screen.getAllByText('provider-0')).toHaveLength(2)
    expect(screen.getAllByText('10.0')).toHaveLength(2)
  })

  it('renders line chart latest value and switches to current value on keyboard navigation', () => {
    const { container } = renderWithI18n(
      <LineChart
        title="Latency"
        unit="ms"
        data={[
          { timestamp: 1_700_000_000_000, value: 20 },
          { timestamp: 1_700_000_060_000, value: 25.5 },
        ]}
      />
    )

    expect(screen.getByRole('heading', { name: 'Latency' })).toBeTruthy()
    expect(screen.getByText('Latest')).toBeTruthy()
    expect(screen.getAllByText('25.5ms')).toHaveLength(2)
    expect(screen.getByRole('img', { name: 'Latency chart' })).toBeTruthy()

    const chartBody = container.querySelector('.chart-body.interactive') as HTMLElement
    fireEvent.focus(chartBody)
    fireEvent.keyDown(chartBody, { key: 'ArrowRight' })
    expect(screen.getByText('Current')).toBeTruthy()
  })

  it('shows the line empty state for invalid data', () => {
    renderWithI18n(<LineChart title="Latency" data={[{ timestamp: Number.NaN, value: 20 }]} />)

    expect(screen.getByRole('img', { name: 'Empty chart' })).toBeTruthy()
    expect(screen.getByText('No trend data yet')).toBeTruthy()
  })
})
