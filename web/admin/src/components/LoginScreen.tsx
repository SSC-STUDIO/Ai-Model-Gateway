import { useCallback, useState } from 'preact/compat'
import { useI18n } from '../i18n'
import { LanguageSelector } from '../theme/LanguageSelector'
import { ThemeToggle } from '../theme/ThemeToggle'
import { BrandMark } from './BrandMark'
import { Icon } from './Icon'

interface LoginScreenProps {
  loginBusy: boolean
  sessionError: string
  onClearError: () => void
  onLogin: (token: string) => Promise<boolean>
}

export function LoginScreen({ loginBusy, sessionError, onClearError, onLogin }: LoginScreenProps) {
  const { t } = useI18n()
  const [token, setToken] = useState('')
  const [showToken, setShowToken] = useState(false)

  const handleSubmit = useCallback(async (event: Event) => {
    event.preventDefault()
    const authenticated = await onLogin(token)
    if (authenticated) {
      setToken('')
      setShowToken(false)
    }
  }, [onLogin, token])

  return (
    <section class="login-panel" aria-labelledby="login-title">
      <div class="login-visual" aria-hidden="true">
        <div class="login-visual-grid" />
        <div class="login-visual-stage">
          <div class="login-orbit">
            <span />
            <span />
            <span />
          </div>
          <div class="login-brand-mark login-brand-mark-large">
            <BrandMark />
          </div>
        </div>
        <div class="login-signal-stack">
          <div>
            <Icon name="shield" size={16} />
            <span>{t('auth.controlPlane')}</span>
          </div>
          <div>
            <Icon name="key" size={16} />
            <span>{t('auth.roleTokens')}</span>
          </div>
          <div>
            <Icon name="config" size={16} />
            <span>{t('auth.signedSession')}</span>
          </div>
        </div>
      </div>

      <div class="login-content">
        <div class="login-panel-toolbar">
          <LanguageSelector />
          <ThemeToggle />
        </div>
        <div class="login-brand">
          <div class="login-brand-mark login-brand-mark-compact">
            <BrandMark />
          </div>
          <div class="login-brand-text">
            <div class="login-eyebrow">{t('header.title')}</div>
            <h1 id="login-title">{t('auth.title')}</h1>
          </div>
        </div>

        <p class="muted login-subtitle">{t('auth.subtitle')}</p>

        <form class="login-form" onSubmit={handleSubmit}>
          <label class="login-token-label">
            <span>{t('auth.tokenLabel')}</span>
            <span class="login-token-field">
              <Icon name="key" size={17} />
              <input
                type={showToken ? 'text' : 'password'}
                value={token}
                placeholder={t('auth.tokenPlaceholder')}
                autoComplete="current-password"
                autoFocus
                disabled={loginBusy}
                onInput={(event) => {
                  onClearError()
                  setToken((event.currentTarget as HTMLInputElement).value)
                }}
              />
              <button
                class="login-token-toggle"
                type="button"
                onClick={() => setShowToken((value) => !value)}
                disabled={loginBusy}
                aria-label={showToken ? t('auth.hideToken') : t('auth.showToken')}
                title={showToken ? t('auth.hideToken') : t('auth.showToken')}
              >
                <Icon name={showToken ? 'eyeOff' : 'eye'} size={17} />
              </button>
            </span>
          </label>

          <button class="login-submit" type="submit" disabled={loginBusy || !token.trim()}>
            <span>{loginBusy ? t('auth.submitting') : t('auth.submit')}</span>
            <Icon name="arrowRight" size={17} />
          </button>
        </form>

        {sessionError && <p class="error login-error">{sessionError}</p>}
        <p class="muted login-help">{t('auth.hint')}</p>
      </div>
    </section>
  )
}
