import { render } from 'preact-testing-library'
import { Icon } from './Icon'

test('renders overview icon without error', () => {
  const { getByRole } = render(<Icon name="overview" />)
  expect(getByRole('img')).toBeInTheDocument()
})

test('renders telemetry icon without error', () => {
  const { getByRole } = render(<Icon name="telemetry" />
  expect(getByRole('img')).toBeInTheDocument()
})