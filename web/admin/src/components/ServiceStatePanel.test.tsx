import { fireEvent, render, screen } from '@testing-library/preact'
import { describe, expect, it, vi } from 'vitest'
import { ServiceStatePanel } from './ServiceStatePanel'

describe('ServiceStatePanel', () => {
  it('renders status copy, badges, detail, children, and action', () => {
    const onAction = vi.fn()

    render(
      <ServiceStatePanel
        icon="server"
        title="Gateway unavailable"
        message="The control plane cannot reach gatewayd."
        hint="Check the listener and service logs."
        detail="connect ECONNREFUSED 127.0.0.1:8080"
        items={[
          { label: 'Gateway', value: 'offline', tone: 'error' },
          { label: 'Telemetry', value: 'healthy', tone: 'success' },
        ]}
        actionLabel="Retry"
        onAction={onAction}
      >
        <span>Last checked just now</span>
      </ServiceStatePanel>
    )

    expect(screen.getByText('Gateway unavailable')).toBeTruthy()
    expect(screen.getByText('The control plane cannot reach gatewayd.')).toBeTruthy()
    expect(screen.getByText('Check the listener and service logs.')).toBeTruthy()
    expect(screen.getByText('Gateway: offline')).toBeTruthy()
    expect(screen.getByText('Telemetry: healthy')).toBeTruthy()
    expect(screen.getByText('connect ECONNREFUSED 127.0.0.1:8080')).toBeTruthy()
    expect(screen.getByText('Last checked just now')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onAction).toHaveBeenCalledTimes(1)
  })

  it('does not render an incomplete action', () => {
    render(
      <ServiceStatePanel
        icon="server"
        title="Gateway unavailable"
        message="The control plane cannot reach gatewayd."
        actionLabel="Retry"
      />
    )

    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull()
  })
})
