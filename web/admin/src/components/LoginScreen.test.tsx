import { fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '../i18n'
import { LoginScreen } from './LoginScreen'

function installMatchMediaMock() {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
}

function renderLoginScreen(props?: Partial<preact.ComponentProps<typeof LoginScreen>>) {
  const onLogin = props?.onLogin ?? vi.fn().mockResolvedValue(true)
  const onClearError = props?.onClearError ?? vi.fn()

  return {
    onLogin,
    onClearError,
    ...render(
      <I18nProvider>
        <LoginScreen
          loginBusy={props?.loginBusy ?? false}
          sessionError={props?.sessionError ?? ''}
          onClearError={onClearError}
          onLogin={onLogin}
        />
      </I18nProvider>
    ),
  }
}

describe('LoginScreen', () => {
  beforeEach(() => {
    localStorage.clear()
    installMatchMediaMock()
  })

  it('renders the localized sign-in form', () => {
    renderLoginScreen()

    expect(screen.getByRole('heading', { name: 'Admin Sign In' })).toBeTruthy()
    expect(screen.getByPlaceholderText('Paste admin or viewer token')).toBeTruthy()
    expect(screen.getByLabelText('Show token')).toBeTruthy()
  })

  it('submits the entered token and clears session errors while typing', async () => {
    const { onLogin, onClearError } = renderLoginScreen({ sessionError: 'bad token' })

    const input = screen.getByPlaceholderText('Paste admin or viewer token') as HTMLInputElement
    fireEvent.input(input, { target: { value: 'test-token' } })

    expect(onClearError).toHaveBeenCalledTimes(1)
    expect(input.value).toBe('test-token')

    const submitButton = screen.getByRole('button', { name: 'Sign In' }) as HTMLButtonElement
    expect(submitButton.disabled).toBe(false)

    fireEvent.click(submitButton)

    await waitFor(() => {
      expect(onLogin).toHaveBeenCalledWith('test-token')
    })
  })
})
