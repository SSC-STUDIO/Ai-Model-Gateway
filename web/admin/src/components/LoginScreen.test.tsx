import { render, screen } from 'preact-testing-library';
import { LoginScreen } from './LoginScreen';
import { useI18n } from '../i18n';

jest.mock('../i18n', () => ({
  useI18n: jest.fn(),
}));

describe('LoginScreen', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('renders with default props', () => {
    const mockOnLogin = jest.fn().mockResolvedValue(true);
    const { rerender } = render(
      <LoginScreen
        loginBusy={false}
        sessionError={''}
        onClearError={() => {}}
        onLogin={mockOnLogin}
      />
    );

    expect(screen.getByText('Login')).toBeInTheDocument();
    expect(screen.getByLabelText('auth.hideToken')).toBeInTheDocument();
  });

  it('calls onLogin with token when submitted', async () => {
    const mockOnLogin = jest.fn().mockResolvedValue(true);
    const { rerender } = render(
      <LoginScreen
        loginBusy={false}
        sessionError={''}
        onClearError={() => {}}
        onLogin={mockOnLogin}
      />
    );

    const input = screen.getByPlaceholderText('auth.tokenPlaceholder');
    fireEvent.change(input, { target: { value: 'test-token' } });
    fireEvent.click(screen.getByText('auth.submit'));

    expect(mockOnLogin).toHaveBeenCalledWith('test-token');
  });
});