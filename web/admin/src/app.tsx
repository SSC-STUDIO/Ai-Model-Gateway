import { useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from './i18n'
import { ThemeToggle } from './theme/ThemeToggle'
import { LanguageSelector } from './theme/LanguageSelector'
import { ErrorBoundary } from './components/ErrorBoundary'
import { ToastContainer } from './components/ToastContainer'
import { BrandMark } from './components/BrandMark'
import { LoginScreen } from './components/LoginScreen'
import { Icon, type IconName } from './components/Icon'
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
import type { ControlTabKey, PrimaryTabKey } from './types'
import { OverviewTab, MonitoringTab, TelemetryTab, BenchmarkTab, ConfigTab, LogsTab, PricingTab, OpsTab, OperationsTab } from './components/tabs'
import { fetchJSON } from './utils/fetch'
import {
  benchmarkURL,
  normalizeWindowHoursParam,
  telemetryTimeseriesURL,
  telemetryURL,
  logsURL,
} from './utils/controlApi'

const DEFAULT_ADMIN_PATH = '/admin'
const LOGIN_PATH = '/admin/login'

const tabPaths: Record<ControlTabKey, string> = {
  overview: '/admin',
  monitoring: '/admin/monitoring',
  telemetry: '/admin/telemetry',
  logs: '/admin/logs',
  pricing: '/admin/pricing',
  benchmark: '/admin/benchmark',
  config: '/admin/config',
  ops: '/admin/ops',
  audit: '/admin/audit',
  probe: '/admin/probe',
  diagnostics: '/admin/diagnostics',
}

// Primary tab icons used in the top navigation.
const PRIMARY_TAB_ICONS: Record<PrimaryTabKey, IconName> = {
  overview: 'overview',
  monitoring: 'telemetry',
  logs: 'logs',
  ops: 'shield',
  config: 'config',
  benchmark: 'benchmark',
}

// Navigation structure: primary tabs with optional sub-tabs
interface NavItem {
  key: PrimaryTabKey
  label: string
}

function getNavItems(t: (key: string) => string): NavItem[] {
  return [
    { key: 'overview', label: t('tabs.overview') },
    { key: 'monitoring', label: t('tabs.monitoring') },
    { key: 'benchmark', label: t('tabs.benchmark') },
    { key: 'ops', label: t('tabs.ops') },
    { key: 'config', label: t('tabs.config') },
    { key: 'logs', label: t('tabs.logs') },
  ]
}

function getPrimaryTab(tab: ControlTabKey): PrimaryTabKey {
  switch (tab) {
    case 'overview':
      return 'overview'
    case 'monitoring':
      return 'monitoring'
    case 'telemetry':
    case 'pricing':
      return 'monitoring'
    case 'logs':
      return 'logs'
    case 'benchmark':
      return 'benchmark'
    case 'config':
      return 'config'
    case 'ops':
      return 'ops'
    case 'audit':
    case 'probe':
    case 'diagnostics':
      return 'ops'
    default:
      // Exhaustive check: if TypeScript complains here, a case is missing
      return 'ops'
  }
}

function inferTab(pathname: string): ControlTabKey {
  if (pathname.endsWith('/monitoring')) return 'monitoring'
  if (pathname.endsWith('/telemetry')) return 'telemetry'
  if (pathname.endsWith('/logs')) return 'logs'
  if (pathname.endsWith('/pricing')) return 'pricing'
  if (pathname.endsWith('/benchmark')) return 'benchmark'
  if (pathname.endsWith('/config')) return 'config'
  if (pathname.endsWith('/ops')) return 'ops'
  if (pathname.endsWith('/audit')) return 'audit'
  if (pathname.endsWith('/probe')) return 'probe'
  if (pathname.endsWith('/diagnostics')) return 'diagnostics'
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

function primeCacheSilently(url: string, ttl: number) {
  void primeCache(url, { ttl }).catch(() => {})
}

function isTelemetryConnectionError(message: string): boolean {
  const value = message.trim().toLowerCase()
  if (!value) return false
  return value.includes('telemetry')
    || value.includes('connection is shut down')
    || value.includes('broken pipe')
    || value.includes('unexpected eof')
    || value === 'eof'
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
  const [refreshInterval, setRefreshInterval] = usePersistentState<number>('admin-refresh-interval', 30000)
  const [telemetryHours, setTelemetryHours] = useUrlState<string>('hours', '168')
  const [telemetryBucket, setTelemetryBucket] = useUrlState<string>('bucket', '1')
  const [logsHours, setLogsHours] = useUrlState<string>('logsHours', '24')
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
    benchmarkModels,
    benchmarkLoading,
    logs,
    configError,
    overviewError,
    statusError,
    telemetryError,
    telemetryTimeseriesError,
    historyError,
    benchmarkError,
    logsError,
    refetchOverview,
    refetchStatus,
    refetchTelemetry,
    refetchTelemetryTimeseries,
    refetchConfig,
    refetchHistory,
    refetchBenchmark,
    refetchLogs,
  } = useControlData(tab, telemetryHours, telemetryBucket, logsHours, canAccessAdmin, handleUnauthorized)

  const navItems = useMemo(() => getNavItems(t), [t])

  const prefetchTabResources = useCallback((targetTab: ControlTabKey) => {
    switch (targetTab) {
      case 'overview':
        primeCacheSilently('/api/admin/overview', 30000)
        primeCacheSilently('/api/admin/status', 30000)
        break
      case 'monitoring':
        primeCacheSilently(telemetryURL(telemetryHours), 30000)
        primeCacheSilently(telemetryTimeseriesURL(telemetryHours, telemetryBucket), 30000)
        break
      case 'telemetry':
      case 'pricing':
        primeCacheSilently(telemetryURL(telemetryHours), 30000)
        primeCacheSilently(telemetryTimeseriesURL(telemetryHours, telemetryBucket), 30000)
        break
      case 'logs':
        primeCacheSilently(logsURL(logsHours), 30000)
        break
      case 'config':
        primeCacheSilently('/api/admin/config', 60000)
        primeCacheSilently('/api/admin/status', 30000)
        primeCacheSilently('/api/admin/config/history', 60000)
        break
      case 'benchmark':
        primeCacheSilently(benchmarkURL(Number(normalizeWindowHoursParam(telemetryHours)), [], 'upstream'), 30000)
        primeCacheSilently(benchmarkURL(Number(normalizeWindowHoursParam(telemetryHours)), [], 'model'), 30000)
        break
      case 'audit':
        primeCacheSilently('/api/admin/audit?limit=100', 30000)
        break
      case 'ops':
        primeCacheSilently('/api/admin/runtime/status', 30000)
        primeCacheSilently('/api/admin/audit?limit=20', 30000)
        break
      case 'diagnostics':
        primeCacheSilently('/api/admin/diagnostics', 30000)
        break
      case 'probe':
        break
    }
  }, [telemetryHours, telemetryBucket, logsHours])

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
      ? navItems.find((item) => item.key === getPrimaryTab(tab))?.label ?? t('header.title')
      : t('auth.title')
    document.title = `${title} - AI-Model-Gateway Admin`
  }, [canAccessAdmin, tab, navItems, t])

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
    refetchLogs,
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

  const refreshPricingStatus = useCallback(async () => {
    await fetchJSON('/api/admin/pricing/refresh', {
      method: 'POST',
    })
    await Promise.all([refetchStatus(), refetchTelemetry()])
  }, [refetchStatus, refetchTelemetry])

  const retryTelemetryState = useCallback(async () => {
    await Promise.all([
      refetchStatus(),
      refetchOverview(),
      refetchTelemetry(),
      refetchTelemetryTimeseries(),
      refetchBenchmark(),
      refetchLogs(),
    ])
  }, [refetchBenchmark, refetchLogs, refetchOverview, refetchStatus, refetchTelemetry, refetchTelemetryTimeseries])

  const handleBenchmarkRefresh = useCallback(() => {
    void refetchBenchmark()
  }, [refetchBenchmark])

  const handleLogin = useCallback(async (token: string) => {
    const result = await login(token)
    return Boolean(result?.authenticated)
  }, [login])

  const handleLogout = useCallback(() => {
    clearError()
    void logout()
  }, [clearError, logout])

  const activeError = useMemo(() => {
    if (!canAccessAdmin) return ''
    if (sessionError) return sessionError
    if (tab === 'overview') return overviewError?.message ?? statusError?.message ?? ''
    if (tab === 'monitoring') return telemetryError?.message ?? telemetryTimeseriesError?.message ?? ''
    if (tab === 'telemetry') return telemetryError?.message ?? telemetryTimeseriesError?.message ?? ''
    if (tab === 'logs') return logsError?.message ?? ''
    if (tab === 'pricing') return telemetryError?.message ?? ''
    if (tab === 'config') return configError?.message ?? statusError?.message ?? historyError?.message ?? actionError
    if (tab === 'benchmark') return benchmarkError?.message ?? ''
    if (tab === 'ops') return ''
    if (tab === 'audit' || tab === 'probe' || tab === 'diagnostics') return ''
    return ''
  }, [
    actionError,
    benchmarkError,
    canAccessAdmin,
    configError?.message,
    historyError?.message,
    logsError?.message,
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
  const inlineTelemetryState = Boolean(
    status?.telemetry_status
    && status.telemetry_status !== 'connected'
    && (tab === 'overview' || tab === 'monitoring' || tab === 'telemetry' || tab === 'pricing' || tab === 'logs' || tab === 'benchmark')
  )
  const hideActiveError = inlineTelemetryState && isTelemetryConnectionError(activeError)

  const refreshControls = useMemo(() => (
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
  ), [t, refreshInterval])

  const tabContent = (() => {
    switch (tab) {
      case 'overview':
        return (
          <>
            {refreshControls}
            <OverviewTab
              overview={overview}
              telemetryStatus={status?.telemetry_status}
              telemetryError={status?.telemetry_error}
              telemetryLastCheckedAt={status?.telemetry_last_checked_at}
              onRetry={() => { void retryTelemetryState() }}
            />
          </>
        )
      case 'monitoring':
        return (
          <MonitoringTab
            telemetry={telemetry}
            timeseries={telemetryTimeseries}
            status={status}
            telemetryHours={telemetryHours}
            onTelemetryHoursChange={setTelemetryHours}
            telemetryBucket={telemetryBucket}
            onTelemetryBucketChange={setTelemetryBucket}
            onRefreshPricing={refreshPricingStatus}
            onRetry={() => { void retryTelemetryState() }}
            refreshControls={refreshControls}
          />
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
              bucketMinutes={parseInt(telemetryBucket, 10) || 1}
              onBucketChange={setTelemetryBucket}
              telemetryStatus={status?.telemetry_status}
              telemetryError={status?.telemetry_error}
              telemetryLastCheckedAt={status?.telemetry_last_checked_at}
              onRetry={() => { void retryTelemetryState() }}
            />
          </>
        )
      case 'logs':
        return (
          <>
            {refreshControls}
            <LogsTab
              // telemetry prop holds logs data (consistent with DataResponse type naming)
              telemetry={logs}
              hours={logsHours}
              onHoursChange={setLogsHours}
              telemetryStatus={status?.telemetry_status}
              telemetryError={status?.telemetry_error}
              telemetryLastCheckedAt={status?.telemetry_last_checked_at}
              onRetry={() => { void retryTelemetryState() }}
            />
          </>
        )
      case 'pricing':
        return (
          <>
            {refreshControls}
            <PricingTab
              telemetry={telemetry}
              status={status}
              onRefreshPricing={refreshPricingStatus}
              onRetry={() => { void retryTelemetryState() }}
            />
          </>
        )
      case 'config':
        return (
          <ConfigTab
            controlConfig={controlConfig}
            historyPayload={historyPayload}
            providerHealth={status?.provider_health ?? []}
            snapshotInfo={{
              active_snapshot_id: status?.active_snapshot_id,
              provider_count: status?.provider_health_count,
              enabled_provider_count: status?.healthy_provider_count,
              unhealthy_provider_count: status?.unhealthy_provider_count,
              cooldown_provider_count: status?.cooldown_provider_count,
            }}
            selectedVersion={selectedRevision}
            selectedEntry={currentHistoryEntry}
            actionLabel={canWrite ? historyActionLabel : t('history.readOnly')}
            actionDisabled={!canWrite || !historyAction || actionBusy}
            onVersionChange={setSelectedRevision}
            onApplySelection={applySelectedRevision}
            busy={actionBusy}
            onConfigUpdated={() => {
              refetchConfig()
              primeCacheSilently('/api/admin/status', 0)
            }}
          />
        )
      case 'benchmark':
        return (
          <BenchmarkTab
            benchmark={benchmark}
            modelBenchmark={benchmarkModels}
            loading={benchmarkLoading}
            hours={telemetryHours}
            onHoursChange={setTelemetryHours}
            status={status}
            canWrite={canWrite}
            onRefresh={handleBenchmarkRefresh}
            onRetry={() => { void retryTelemetryState() }}
            onUnauthorized={handleUnauthorized}
          />
        )
      case 'ops':
        return <OperationsTab canWrite={canWrite} onUnauthorized={handleUnauthorized} />
      case 'audit':
        return <OpsTab mode="audit" canWrite={canWrite} onUnauthorized={handleUnauthorized} />
      case 'probe':
        return <OpsTab mode="probe" canWrite={canWrite} onUnauthorized={handleUnauthorized} />
      case 'diagnostics':
        return <OpsTab mode="diagnostics" canWrite={canWrite} onUnauthorized={handleUnauthorized} />
    }
  })()

  if (sessionLoading) {
    return <main class="app-shell login-shell" />
  }

  if (!canAccessAdmin) {
    return (
      <>
        <main class="app-shell login-shell">
          <LoginScreen
            loginBusy={loginBusy}
            sessionError={sessionError}
            onClearError={clearError}
            onLogin={handleLogin}
          />
        </main>

        <ToastContainer toasts={toasts} onClose={removeToast} />
      </>
    )
  }

  return (
    <>
      <main class="app-shell">
        <header class="topbar">
          <div class="topbar-brand">
            <BrandMark />
            <div>
              <h1>{t('header.title')}</h1>
              <p class="muted">{status?.version ? `${status.version} · ${status.uptime ?? '-'}` : t('header.subtitle')}</p>
            </div>
          </div>

          <div class="topbar-nav">
            <div class="tabbar">
              {navItems.map((item) => {
                const primaryTab = getPrimaryTab(tab)
                const isActive = primaryTab === item.key
                return (
                  <button
                    key={item.key}
                    type="button"
                    class={`tab${isActive ? ' active' : ''}`}
                    onClick={() => handleTabChange(item.key as ControlTabKey)}
                    onMouseEnter={() => prefetchTabResources(item.key as ControlTabKey)}
                    onFocus={() => prefetchTabResources(item.key as ControlTabKey)}
                    title={item.label}
                    aria-current={isActive ? 'page' : undefined}
                  >
                    <Icon name={PRIMARY_TAB_ICONS[item.key]} class="tab-icon" />
                    <span>{item.label}</span>
                  </button>
                )
              })}
            </div>
          </div>

          <div class="topbar-right">
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
                  <button type="button" class="logout-btn" onClick={handleLogout} disabled={logoutBusy}>
                    {logoutBusy ? t('auth.loggingOut') : t('auth.logout')}
                  </button>
                </>
              ) : (
                <span class="status-badge neutral">{t('auth.disabled')}</span>
              )}
            </div>
          </div>
        </header>

        {activeError && !hideActiveError && (
          <section class="panel">
            <p class="error">{activeError}</p>
          </section>
        )}

        <div key={tab} class="tab-content-wrapper">
          <ErrorBoundary>
            {tabContent}
          </ErrorBoundary>
        </div>
      </main>

      <ToastContainer toasts={toasts} onClose={removeToast} />
    </>
  )
}
