import { render, screen } from '@testing-library/preact'
import { describe, expect, it } from 'vitest'
import { ErrorBoundary } from './ErrorBoundary'

describe('ErrorBoundary', () => {
  it('renders children when no error is thrown', () => {
    render(
      <ErrorBoundary>
        <div data-testid="child">ok</div>
      </ErrorBoundary>
    )

    const child = screen.getByTestId('child')
    expect(child).toBeTruthy()
    expect(child.textContent).toBe('ok')
  })
})
