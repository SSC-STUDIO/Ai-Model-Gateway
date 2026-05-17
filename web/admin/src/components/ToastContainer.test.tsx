import { render } from 'preact-testing-library'
import { ToastContainer } from './ToastContainer'
import type { ToastItem } from '../hooks/useToast'

test('renders ToastContainer with toasts', () => {
  const dummyToast: ToastItem = {
    id: '1',
    type: 'success',
    message: 'Test message',
    onClose: jest.fn(),
  }
  const { getByText, getByLabelText } = render(
    <ToastContainer toasts={[dummyToast]} onClose={() => {}} />
  )
  expect(getByText('Test message')).toBeInTheDocument()
  expect(getByLabelText('close')).toBeInTheDocument()
})