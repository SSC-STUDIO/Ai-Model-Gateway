import { render } from '@testing-library/preact'
import { describe, expect, it } from 'vitest'
import { Icon } from './Icon'

describe('Icon', () => {
  it('renders an accessible icon when a title is provided', () => {
    const { getByRole } = render(<Icon name="overview" title="Overview" />)
    expect(getByRole('img', { name: 'Overview' })).toBeTruthy()
  })

  it('renders a decorative icon as aria-hidden by default', () => {
    const { container } = render(<Icon name="telemetry" />)
    const icon = container.querySelector('svg')
    expect(icon).toBeTruthy()
    expect(icon?.getAttribute('aria-hidden')).toBe('true')
  })
})
