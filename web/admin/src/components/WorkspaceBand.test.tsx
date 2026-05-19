import { render, screen } from '@testing-library/preact'
import { describe, expect, it } from 'vitest'
import { WorkspaceBand } from './WorkspaceBand'

describe('WorkspaceBand', () => {
  it('renders a navigable workspace section with header copy and children', () => {
    const { container } = render(
      <WorkspaceBand
        id="monitoring"
        icon="activity"
        kicker="Live signals"
        title="Monitoring"
        detail="Request health, latency, and upstream distribution."
      >
        <button type="button">Refresh</button>
      </WorkspaceBand>
    )

    const section = container.querySelector('section#monitoring')
    expect(section).toBeTruthy()
    expect(section?.getAttribute('class')).toBe('workspace-band')
    expect(screen.getByText('Live signals')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Monitoring' })).toBeTruthy()
    expect(screen.getByText('Request health, latency, and upstream distribution.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeTruthy()
  })
})
