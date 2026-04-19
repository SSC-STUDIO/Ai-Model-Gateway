import { useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from './i18n'
import { ThemeToggle } from './theme/ThemeToggle'
import { LanguageSelector } from './theme/LanguageSelector'
import { ToastContainer } from './components/ToastContainer'
import {
  primeCache,
  useAdminSession,
  useUrlState,
  usePersistentState,
  usePageVisibility,
  useToast,
  useControlData,
  useHistoryActions,
  useAutoRefresh,
} from './hooks'
import type { ControlTabKey } from './types'
import { OverviewTab, TelemetryTab, TimeSeriesTab, HistoryTab, BenchmarkTab, ConfigTab } from './components/tabs'
import {
  benchmarkURL,
  historyTimeseriesURL,
  telemetryTimeseriesURL,
  telemetryURL,
} from './utils/controlApi'

const DEFAULT_ADMIN_PATH = '/admin'
const LOGIN_PATH = '/admin/login'

const tabPaths: Record<ControlTabKey, string> = {
  overview: '/admin',
  telemetry: '/admin/telemetry',
  timeseries: '/admin/timeseries',
  benchmark: '/admin/benchmark',
  config: '/admin/config',
  history: '/admin/history',
}

const TAB_ICONS: Record<ControlTabKey, string> = {
  overview: '\u{1F4CA}',
  telemetry: '\u{1F4C8}',
  timeseries: '\u{1F4C9}',
  benchmark: '\u26A1',
  config: '\u2699\uFE0F',
  history: '\u{1F4CB}',
}

function inferTab(pathname: string): ControlTabKey {
  if (pathname.endsWith('/telemetry')) return 'telemetry'
  if (pathname.endsWith('/timeseries')) return 'timeseries'
  if (pathname.endsWith('/benchmark')) return 'benchmark'
  if (pathname.endsWith('/config')) return 'config'
  if (pathname.endsWith('/history')) return 'history'
  return 'overview'
}

function defaultAdminNext(next: string | null | undefined): string {
  const value = (next ?? '').trim()
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.startsWith(LOGIN_PATH)) {
    return DEFAULT_ADMIN_PATH
  }
  return value
}

function roleLabel(role: string | undefined, t: (key: string) => string): string {
  return role === 'viewer' ? t('auth.roleViewer') : t('auth.roleAdmin')
}

function BrandMark() {
  return (
    <svg width="36" height="36" viewBox="0 0 96 96" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <rect width="96" height="96" rx="24" fill="#0B0C0C" />
      <path d="M24 68V28H38L48 52L58 28H72V68H62V46L54 66H42L34 46V68H24Z" fill="#7EE7D6" />
      <circle cx="73" cy="24" r="8" fill="#F1B866" />
    </svg>
  )
}

export function App() {
  const { t } = useI18n()
  const { toasts, removeToast } = useToast()
  const {
    session,
    loading: sessionLoading,
    loginBusy,
    logoutBusy,
    error: sessionError,
    clearError,
    refreshSession,
    login,
    logout,
  } = useAdminSession()

  const [tab, setTab] = useState<ControlTabKey>(() => inferTab(window.location.pathname))
  const [loginToken, setLoginToken] = useState('')
  const [refreshInterval, setRefreshInterval] = usePersistentState<number>('admin-refresh-interval', 30000)
  const [telemetryHours, setTelemetryHours] = useUrlState<string>('hours', '168')
  const [benchmarkHours, setBenchmarkHours] = useUrlState<number>('benchmarkHours', 168)
  const [benchmarkModels, setBenchmarkModels] = useUrlState<string[]>('models', [])
  const [selectedRevision, setSelectedRevision] = useState('')
  const isPageVisible = usePageVisibility()

  const routeKey = `${window.location.pathname}${window.location.search}`
  const isLoginRoute = window.location.pathname === LOGIN_PATH
  const nextAdminPath = isLoginRoute
    ? defaultAdminNext(new URLSearchParams(window.location.search).get('next'))
    : defaultAdminNext(routeKey)
  const authEnabled = session?.enabled ?? true
  const isAuthenticated = Boolean(session?.authenticated)
  const canAccessAdmin = !authEnabled || isAuthenticated
  const canWrite = !authEnabled || session?.role !== 'viewer'

  const navigate = useCallback((target: string, mode: 'push' | 'replace' = 'push') => {
    const url = new URL(target, window.location.origin)
    const href = `${url.pathname}${url.search}${url.hash}`
    const state = { tab: inferTab(url.pathname) }
    if (mode === 'replace') {
      window.history.replaceState(state, '', href)
    } else {
      window.history.pushState(state, '', href)
    }
    setTab(inferTab(url.pathname))
  }, [])

  const handleUnauthorized = useCallback(() => {
    void refreshSession()
  }, [refreshSession])

  const {
    status,
    overview,
    telemetry,
    telemetryTimeseries,
    controlConfig,
    historyPayload,
    benchmark,
    benchmarkLoading,
    configError,
    overviewError,
    statusError,
    telemetryError,
    telemetryTimeseriesError,
    historyError,
    benchmarkError,
    refetchOverview,
    refetchStatus,
    refetchTelemetry,
    refetchTelemetryTimeseries,
    refetchConfig,
    refetchHistory,
    refetchBenchmark,
  } = useControlData(tab, telemetryHours, benchmarkHours, benchmarkModels, canAccessAdmin, handleUnauthorized)

  const tabLabels = useMemo(
    () => [
      { key: 'overview' as ControlTabKey, label: t('tabs.overview') },
      { key: 'telemetry' as ControlTabKey, label: t('tabs.telemetry') },
      { key: 'timeseries' as ControlTabKey, label: t('tabs.timeseries') },
      { key: 'benchmark' as ControlTabKey, label: t('tabs.benchmark') },
      { key: 'config' as ControlTabKey, label: t('tabs.config') },
      { key: 'history' as ControlTabKey, label: t('tabs.history') },
    ],
    [t]
  )

  const prefetchTabResources = useCallback((targetTab: ControlTabKey) => {
    switch (targetTab) {
      case 'overview':
        void primeCache('/api/admin/overview', { ttl: 30000 })
        void primeCache('/api/admin/status', { ttl: 30000 })
        break
      case 'telemetry':
        void primeCache(telemetryURL(telemetryHours), { ttl: 30000 })
        void primeCache(telemetryTimeseriesURL(telemetryHours), { ttl: 30000 })
        break
      case 'timeseries':
        void primeCache('/api/admin/timeseries?hours=168&bucket=5', { ttl: 30000 })
        void primeCache(historyTimeseriesURL(), { ttl: 60000 })
        break
      case 'config':
        void primeCache('/api/admin/config', { ttl: 60000 })
        void primeCache('/api/admin/status', { ttl: 30000 })
        break
      case 'history':
        void primeCache('/api/admin/config', { ttl: 60000 })
        void primeCache('/api/admin/config/history', { ttl: 60000 })
        break
      case 'benchmark':
        void primeCache(benchmarkURL(benchmarkHours, benchmarkModels), { ttl: 30000 })
        break
    }
  }, [telemetryHours, benchmarkHours, benchmarkModels])

  const handleTabChange = useCallback((nextTab: ControlTabKey) => {
    navigate(tabPaths[nextTab] + window.location.search, 'push')
    prefetchTabResources(nextTab)
  }, [navigate, prefetchTabResources])

  useEffect(() => {
    if (!historyPayload.versions.length) {
      setSelectedRevision('')
      return
    }
    if (historyPayload.versions.some((entry) => entry.id === selectedRevision)) {
      return
    }
    const active = historyPayload.versions.find((entry) => entry.is_active)
    setSelectedRevision(active?.id ?? historyPayload.versions[0].id)
  }, [historyPayload, selectedRevision])

  useEffect(() => {
    const title = canAccessAdmin
      ? tabLabels.find((entry) => entry.key === tab)?.label ?? t('header.title')
      : t('auth.title')
    document.title = `${title} - AI-Model-Gateway Admin`
  }, [canAccessAdmin, tab, tabLabels, t])

  useEffect(() => {
    const handler = () => {
      setTab(inferTab(window.location.pathname))
    }
    window.addEventListener('popstate', handler)
    return () => window.removeEventListener('popstate', handler)
  }, [])

  useEffect(() => {
    if (sessionLoading) return
    if (canAccessAdmin && isLoginRoute) {
      navigate(nextAdminPath, 'replace')
    }
  }, [canAccessAdmin, isLoginRoute, navigate, nextAdminPath, routeKey, sessionLoading])

  useAutoRefresh(refreshInterval, isPageVisible && canAccessAdmin, tab, {
    refetchOverview,
    refetchStatus,
    refetchTelemetry,
    refetchTelemetryTimeseries,
    refetchConfig,
    refetchHistory,
    refetchBenchmark,
  })

  const {
    action: historyAction,
    actionLabel: historyActionLabel,
    currentEntry: currentHistoryEntry,
    apply: applySelectedRevision,
    busy: actionBusy,
    error: actionError,
  } = useHistoryActions(
    selectedRevision,
    historyPayload,
    refetchOverview,
    refetchStatus,
    refetchHistory,
    handleUnauthorized
  )

  const handleBenchmarkRefresh = useCallback(() => {
    void refetchBenchmark()
  }, [refetchBenchmark])

  const handleLoginSubmit = useCallback(async (event: Event) => {
    event.preventDefault()
    const result = await login(loginToken)
    if (result?.authenticated) {
      setLoginToken('')
    }
  }, [login, loginToken])

  const handleLogout = useCallback(() => {
    clearError()
    void logout()
  }, [clearError, logout])

  const activeError = useMemo(() => {
    if (!canAccessAdmin) return ''
    if (sessionError) return sessionError
    if (tab === 'overview') return overviewError?.message ?? statusError?.message ?? ''
    if (tab === 'telemetry') return telemetryError?.message ?? telemetryTimeseriesError?.message ?? ''
    if (tab === 'config') return configError?.message ?? statusError?.message ?? ''
    if (tab === 'history') return configError?.message ?? historyError?.message ?? actionError
    if (tab === 'benchmark') return benchmarkError?.message ?? ''
    return ''
  }, [
    actionError,
    benchmarkError,
    canAccessAdmin,
    configError?.message,
    historyError?.message,
    overviewError?.message,
    sessionError,
    statusError?.message,
    tab,
    telemetryError?.message,
    telemetryTimeseriesError?.message,
  ])

  const gatewayTone = status?.gateway_status === 'connected'
    ? status.gateway_readiness === 'ready' ? 'success' : 'warning'
    : status?.gateway_status === 'error' ? 'error' : 'neutral'
  const telemetryTone = status?.telemetry_status === 'connected' ? 'success' : 'neutral'

  const refreshControls = (
    <div class="auto-refresh-controls">
      <span class="auto-refresh-label">{t('autoRefresh.interval')}:</span>
      {([0, 10000, 30000, 60000] as const).map((ms) => (
        <button
          key={ms}
          type="button"
          class={`auto-refresh-btn${refreshInterval === ms ? ' active' : ''}`}
          onClick={() => setRefreshInterval(ms)}
        >
          {ms === 0 ? t('autoRefresh.off') : `${ms / 1000}s`}
        </button>
      ))}
      {refreshInterval > 0 && (
        <span class="auto-refresh-indicator">{t('autoRefresh.active')}</span>
      )}
    </div>
  )

  const tabContent = useMemo(() => {
    switch (tab) {
      case 'overview':
        return (
          <>
            {refreshControls}
            <OverviewTab overview={overview} />
          </>
        )
      case 'telemetry':
        return (
          <>
            {refreshControls}
            <TelemetryTab
              telemetry={telemetry}
              timeseries={telemetryTimeseries}
              hours={telemetryHours}
              onHoursChange={setTelemetryHours}
            />
          </>
        )
      case 'timeseries':
        return <TimeSeriesTab />
      case 'config':
        return (
          <ConfigTab
            controlConfig={controlConfig}
            providerHealth={status?.provider_health ?? []}
            snapshotInfo={{
              active_snapshot_id: status?.active_snapshot_id,
              provider_count: status?.provider_health_count,
              enabled_provider_count: status?.healthy_provider_count,
              unhealthy_provider_count: status?.unhealthy_provider_count,
              cooldown_provider_count: status?.cooldown_provider_count,
            }}
          />
        )
      case 'history':
        return (
          <HistoryTab
            controlConfig={controlConfig}
            historyPayload={historyPayload}
            selectedVersion={selectedRevision}
            selectedEntry={currentHistoryEntry}
            actionLabel={canWrite ? historyActionLabel : t('history.readOnly')}
            actionDisabled={!canWrite || !historyAction || actionBusy}
            onVersionChange={setSelectedRevision}
            onApplySelection={applySelectedRevision}
            busy={actionBusy}
          />
        )
      case 'benchmark':
        return (
          <BenchmarkTab
            benchmark={benchmark}
            benchmarkHours={benchmarkHours}
            benchmarkModels={benchmarkModels}
            benchmarkLoading={benchmarkLoading}
            onHoursChange={setBenchmarkHours}
            onModelsChange={setBenchmarkModels}
            onRefresh={handleBenchmarkRefresh}
          />
        )
    }
  }, [
    actionBusy,
    applySelectedRevision,
    benchmark,
    benchmarkHours,
    benchmarkLoading,
    benchmarkModels,
    canWrite,
    controlConfig,
    currentHistoryEntry,
    handleBenchmarkRefresh,
    historyAction,
    historyActionLabel,
    historyPayload,
    overview,
    refreshControls,
    selectedRevision,
    setTelemetryHours,
    setBenchmarkHours,
    setBenchmarkModels,
    status,
    t,
    tab,
    telemetry,
    telemetryHours,
    telemetryTimeseries,
  ])

  const bgOrbs = (
    <>
      <div class="bg-orb bg-orb-1" aria-hidden="true" />
      <div class="bg-orb bg-orb-2" aria-hidden="true" />
      <div class="bg-orb bg-orb-3" aria-hidden="true" />
    </>
  )

  if (sessionLoading || !canAccessAdmin) {
    return (
      <>
        {bgOrbs}
        <main class="app-shell login-shell">
          <section class="login-panel">
            <div class="login-panel-toolbar">
              <LanguageSelector />
              <ThemeToggle />
            </div>
            <div class="login-brand">
              <BrandMark />
              <div>
                <div class="login-eyebrow">{t('header.title')}</div>
                <h1>{t('auth.title')}</h1>
              </div>
            </div>

            <p class="muted">
              {sessionLoading ? t('auth.checking') : t('auth.subtitle')}
            </p>

            {!sessionLoading && (
              <form class="login-form" onSubmit={handleLoginSubmit}>
                <label>
                  {t('auth.tokenLabel')}
                  <input
                    type="password"
                    value={loginToken}
                    placeholder={t('auth.tokenPlaceholder')}
                    autoComplete="current-password"
                    autoFocus
                    disabled={loginBusy}
                    onInput={(event) => {
                      clearError()
                      setLoginToken((event.currentTarget as HTMLInputElement).value)
                    }}
                  />
                </label>

                <button type="submit" disabled={loginBusy || !loginToken.trim()}>
                  {loginBusy ? t('auth.submitting') : t('auth.submit')}
                </button>
              </form>
            )}

            {sessionError && <p class="error">{sessionError}</p>}
            {!sessionLoading && <p class="muted login-help">{t('auth.hint')}</p>}
          </section>
        </main>

        <ToastContainer toasts={toasts} onClose={removeToast} />
      </>
    )
  }

  return (
    <>
      {bgOrbs}
      <main class="app-shell">
        <header class="topbar">
          <div class="topbar-brand">
            <BrandMark />
            <div>
              <h1>{t('header.title')}</h1>
              <p class="muted">{status?.version ? `${status.version} · ${status.uptime ?? '-'}` : t('header.subtitle')}</p>
            </div>
          </div>

          <div class="topbar-actions">
            <div class="tabbar">
              {tabLabels.map((item) => (
                <button
                  key={item.key}
                  type="button"
                  class={`tab${tab === item.key ? ' active' : ''}`}
                  onClick={() => handleTabChange(item.key)}
                  onMouseEnter={() => prefetchTabResources(item.key)}
                  onFocus={() => prefetchTabResources(item.key)}
                >
                  {TAB_ICONS[item.key]} {item.label}
                </button>
              ))}
            </div>

            <span class={`status-badge ${gatewayTone}`}>
              {t('header.gateway')}: {status?.gateway_readiness ?? status?.gateway_status ?? t('header.statusUnknown')}
            </span>
            <span class={`status-badge ${telemetryTone}`}>
              {t('header.telemetry')}: {status?.telemetry_status ?? t('header.statusUnknown')}
            </span>

            <div class="header-controls">
              <LanguageSelector />
              <ThemeToggle />
            </div>

            <div class="session-controls">
              {authEnabled ? (
                <>
                  <span class="status-badge neutral">
                    {session?.name ?? t('auth.sessionCurrent')} · {roleLabel(session?.role, t)}
                  </span>
                  <button type="button" class="secondary" onClick={handleLogout} disabled={logoutBusy}>
                    {logoutBusy ? t('auth.loggingOut') : t('auth.logout')}
                  </button>
                </>
              ) : (
                <span class="status-badge neutral">{t('auth.disabled')}</span>
              )}
            </div>
          </div>
        </header>

        {activeError && (
          <section class="panel">
            <p class="error">{activeError}</p>
          </section>
        )}

        <div key={tab} class="tab-content-wrapper">
          {tabContent}
        </div>
      </main>

      <ToastContainer toasts={toasts} onClose={removeToast} />
    </>
  )
}
