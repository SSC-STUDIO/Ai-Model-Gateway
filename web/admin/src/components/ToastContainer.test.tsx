import { fireEvent, render, screen } from '@testing-library/preact'
import { describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '../i18n'
import { ToastContainer } from './ToastContainer'
import type { ToastItem } from '../hooks/useToast'

describe('ToastContainer', () => {
  it('renders toasts and forwards close events', () => {
    const onClose = vi.fn()
    const toast: ToastItem = {
      id: '1',
      type: 'success',
      message: 'Test message',
    }

    render(
      <I18nProvider>
        <ToastContainer toasts={[toast]} onClose={onClose} />
      </I18nProvider>
    )

    expect(screen.getByText('Test message')).toBeTruthy()

    const closeButton = screen.getByRole('button', { name: 'Close notification' })
    fireEvent.click(closeButton)
    expect(onClose).toHaveBeenCalledWith('1')
  })
})
