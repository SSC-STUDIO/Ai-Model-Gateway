import { render } from 'preact-testing-library'
import { ErrorBoundary } from './ErrorBoundary'

test('renders component without error', () => {
  render(<ErrorBoundary><div data-testid="test"></div></ErrorBoundary>)
  // The component should render children when there is no error
  expect(screen.getByTestId('test')).toBeInTheDocument()
})