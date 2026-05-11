import { Component, type ComponentChildren } from 'preact'
import { I18nContext, type I18nContextValue } from '../i18n'

interface ErrorBoundaryProps {
  children: ComponentChildren
  fallback?: ComponentChildren
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  static contextType = I18nContext
  declare context: I18nContextValue | undefined

  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: { componentStack: string }) {
    console.error('[ErrorBoundary] caught error:', error, errorInfo.componentStack)

    // Fire-and-forget POST to control plane for server-side logging.
    try {
      fetch('/api/admin/client-error', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: error.message,
          stack: errorInfo.componentStack,
          source: 'ErrorBoundary',
          url: window.location.href,
        }),
      }).catch(() => {/* swallow — reporting failure must not break the fallback UI */})
    } catch {
      /* guard against environments where fetch is unavailable */
    }
  }

  handleReset = () => {
    this.setState({ hasError: false, error: null })
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }

      const t = this.context?.t ?? ((key: string) => key)

      return (
        <div class="error-boundary-fallback">
          <div class="error-boundary-icon">
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <circle cx="12" cy="12" r="10" />
              <line x1="12" y1="8" x2="12" y2="12" />
              <line x1="12" y1="16" x2="12.01" y2="16" />
            </svg>
          </div>
          <h3 class="error-boundary-title">{t('error.boundaryTitle')}</h3>
          <p class="error-boundary-message">
            {this.state.error?.message || t('error.boundaryMessage')}
          </p>
          <button
            type="button"
            class="error-boundary-retry"
            onClick={this.handleReset}
          >
            {t('error.boundaryRetry')}
          </button>
        </div>
      )
    }

    return this.props.children
  }
}
