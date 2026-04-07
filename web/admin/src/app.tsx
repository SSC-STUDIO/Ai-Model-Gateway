import { useEffect, useMemo, useState, useCallback, lazy, Suspense } from 'preact/compat'
import { useI18n } from './i18n'
import { ThemeToggle } from './theme/ThemeToggle'
import { LanguageSelector } from './theme/LanguageSelector'
import { LogViewer } from './components/LogViewer'
import { useCachedFetch, invalidateCache } from './hooks'
import type { TabKey, AnyRecord, DataResponse, TimeSeriesResponse, BenchmarkResponse, ProbeProvider } from './types'

// Lazy load tab components for code splitting
const OverviewTab = lazy(() => import('./components/tabs').then(m => ({ default: m.OverviewTab })))
const TelemetryTab = lazy(() => import('./components/tabs').then(m => ({ default: m.TelemetryTab })))
const SettingsTab = lazy(() => import('./components/tabs').then(m => ({ default: m.SettingsTab })))
const HistoryTab = lazy(() => import('./components/tabs').then(m => ({ default: m.HistoryTab })))
const ProbeTab = lazy(() => import('./components/tabs').then(m => ({ default: m.ProbeTab })))
const TimeSeriesTab = lazy(() => import('./components/tabs').then(m => ({ default: m.TimeSeriesTab })))
const BenchmarkTab = lazy(() => import('./components/tabs').then(m => ({ default: m.BenchmarkTab })))

const tabPaths: Record<TabKey, string> = {
  overview: '/admin',
  telemetry: '/admin/telemetry',
  timeseries: '/admin/timeseries',
  settings: '/admin/settings',
  history: '/admin/history',
  probe: '/admin/probe',
  logs: '/admin/logs',
  benchmark: '/admin/benchmark',
}

function inferTab(pathname: string): TabKey {
  if (pathname.endsWith('/telemetry')) return 'telemetry'
  if (pathname.endsWith('/timeseries')) return 'timeseries'
  if (pathname.endsWith('/settings')) return 'settings'
  if (pathname.endsWith('/history')) return 'history'
  if (pathname.endsWith('/probe')) return 'probe'
  if (pathname.endsWith('/logs')) return 'logs'
  if (pathname.endsWith('/benchmark')) return 'benchmark'
  return 'overview'
}

async function readJSON<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const resp = await fetch(input, {
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(text || `${resp.status} ${resp.statusText}`)
  }
  return (await resp.json()) as T
}

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

function versionIdOf(item: unknown): string {
  if (!item || typeof item !== 'object') return ''
  const record = item as AnyRecord
  const raw = record.version_id ?? record.versionId ?? record.id
  return typeof raw === 'string' ? raw : ''
}

function firstHistoryVersion(payload: unknown): string {
  if (Array.isArray(payload)) return versionIdOf(payload[0])
  if (payload && typeof payload === 'object') {
    const items = (payload as AnyRecord).items
    if (Array.isArray(items)) return versionIdOf(items[0])
  }
  return ''
}

export function App() {
  const { t } = useI18n()

  const tabLabels = useMemo(
    () => [
      { key: 'overview' as TabKey, label: t('tabs.overview') },
      { key: 'telemetry' as TabKey, label: t('tabs.telemetry') },
      { key: 'benchmark' as TabKey, label: t('tabs.benchmark') },
      { key: 'timeseries' as TabKey, label: t('tabs.timeseries') },
      { key: 'settings' as TabKey, label: t('tabs.settings') },
      { key: 'history' as TabKey, label: t('tabs.history') },
      { key: 'probe' as TabKey, label: t('tabs.probe') },
      { key: 'logs' as TabKey, label: t('tabs.logs') },
    ],
    [t]
  )

  const [tab, setTab] = useState<TabKey>(() => inferTab(window.location.pathname))
  const [authed, setAuthed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [token, setToken] = useState('')

  // Use cached fetch for data that can be cached
  const { data: overview, refetch: refetchOverview } = useCachedFetch<AnyRecord>('/api/admin/overview', {
    ttl: 30000,
    enabled: authed,
  })

  const { data: telemetry, refetch: refetchTelemetry } = useCachedFetch<DataResponse>('/api/admin/data', {
    ttl: 30000,
    enabled: authed && (tab === 'telemetry' || tab === 'overview'),
  })

  const { data: timeseries, refetch: refetchTimeseries } = useCachedFetch<TimeSeriesResponse>('/api/admin/timeseries', {
    ttl: 30000,
    enabled: authed && (tab === 'telemetry' || tab === 'timeseries'),
  })

  const { data: configView, refetch: refetchConfig } = useCachedFetch<AnyRecord>('/api/admin/config', {
    ttl: 60000,
    enabled: authed && tab === 'settings',
  })

  const { data: historyPayload, refetch: refetchHistory } = useCachedFetch<unknown>('/api/admin/config/history', {
    ttl: 60000,
    enabled: authed && tab === 'history',
  })

  const [configText, setConfigText] = useState('')
  const [selectedVersion, setSelectedVersion] = useState('')
  const [historyDiff, setHistoryDiff] = useState<unknown>(null)

  const [probeProvider, setProbeProvider] = useState<ProbeProvider>({
    name: 'manual-probe',
    base_url: '',
    anthropic_base_url: '',
    api_key: '',
    provider_class: 'quota_limited',
    models: 'gpt-4o',
    timeout_ms: '10000',
    enabled: true,
  })
  const [probeResult, setProbeResult] = useState<unknown>(null)

  const [benchmark, setBenchmark] = useState<BenchmarkResponse | null>(null)
  const [benchmarkHours, setBenchmarkHours] = useState(24)
  const [benchmarkModels, setBenchmarkModels] = useState<string[]>([])
  const [benchmarkLoading, setBenchmarkLoading] = useState(false)

  // Update URL when tab changes
  useEffect(() => {
    window.history.replaceState(null, '', tabPaths[tab])
  }, [tab])

  // Bootstrap authentication check
  useEffect(() => {
    void bootstrap()
  }, [])

  // Load initial config text when config view changes
  useEffect(() => {
    if (configView) {
      setConfigText(pretty(configView))
    }
  }, [configView])

  // Load history diff when history payload changes
  useEffect(() => {
    if (historyPayload && tab === 'history') {
      const firstVersion = firstHistoryVersion(historyPayload)
      if (firstVersion) {
        setSelectedVersion(firstVersion)
        void loadHistoryDiff(firstVersion)
      } else {
        setSelectedVersion('')
        setHistoryDiff(null)
      }
    }
  }, [historyPayload, tab])

  // Debounced tab change handler
  const handleTabChange = useCallback((newTab: TabKey) => {
    setTab(newTab)
    // Invalidate relevant caches when switching tabs to get fresh data
    if (newTab === 'overview') {
      invalidateCache(/\/api\/admin\/overview/)
      void refetchOverview()
    } else if (newTab === 'telemetry') {
      invalidateCache(/\/api\/admin\/(data|timeseries)/)
      void refetchTelemetry()
      void refetchTimeseries()
    } else if (newTab === 'timeseries') {
      invalidateCache(/\/api\/admin\/timeseries/)
      void refetchTimeseries()
    } else if (newTab === 'settings') {
      invalidateCache(/\/api\/admin\/config$/)
      void refetchConfig()
    } else if (newTab === 'history') {
      invalidateCache(/\/api\/admin\/config\/history/)
      void refetchHistory()
    }
  }, [refetchOverview, refetchTelemetry, refetchTimeseries, refetchConfig, refetchHistory])

  async function bootstrap() {
    try {
      await readJSON<AnyRecord>('/api/admin/overview')
      setAuthed(true)
    } catch {
      setAuthed(false)
    }
  }

  async function loadBenchmark() {
    setBenchmarkLoading(true)
    try {
      const params = new URLSearchParams()
      params.append('hours', String(benchmarkHours))
      if (benchmarkModels.length > 0) {
        benchmarkModels.forEach((m) => params.append('models', m))
      }
      const data = await readJSON<BenchmarkResponse>(`/api/admin/models/benchmark?${params.toString()}`)
      setBenchmark(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBenchmarkLoading(false)
    }
  }

  // Load benchmark when tab is activated
  useEffect(() => {
    if (tab === 'benchmark' && authed) {
      void loadBenchmark()
    }
  }, [tab, authed, benchmarkHours, benchmarkModels])

  async function submitLogin(event: Event) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await readJSON('/api/admin/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      })
      setAuthed(true)
      setToken('')
      // Invalidate all caches on login
      invalidateCache()
      await refetchOverview()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const logout = useCallback(async () => {
    setBusy(true)
    try {
      await readJSON('/api/admin/auth/logout', { method: 'POST' })
      setAuthed(false)
      setBenchmark(null)
      setProbeResult(null)
      setError('')
      // Clear all caches on logout
      invalidateCache()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }, [])

  async function saveConfig() {
    setBusy(true)
    setError('')
    try {
      const payload = JSON.parse(configText) as AnyRecord
      const updated = await readJSON<AnyRecord>('/api/admin/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      invalidateCache(/\/api\/admin\/config/)
      await refetchConfig()
      setConfigText(pretty(updated))
      handleTabChange('history')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function loadHistoryDiff(versionId: string) {
    setSelectedVersion(versionId)
    if (!versionId) {
      setHistoryDiff(null)
      return
    }
    try {
      setHistoryDiff(await readJSON(`/api/admin/config/history/${encodeURIComponent(versionId)}/diff`))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleVersionChange = useCallback((versionId: string) => {
    void loadHistoryDiff(versionId)
  }, [])

  async function rollbackConfig() {
    if (!selectedVersion) return
    setBusy(true)
    setError('')
    try {
      const updated = await readJSON<AnyRecord>('/api/admin/config/rollback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version_id: selectedVersion }),
      })
      invalidateCache(/\/api\/admin\/config/)
      await refetchConfig()
      setConfigText(pretty(updated))
      handleTabChange('history')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const handleRollback = useCallback(() => {
    void rollbackConfig()
  }, [selectedVersion])

  async function runProbe(event: Event) {
    event.preventDefault()
    setBusy(true)
    setError('')
    setProbeResult(null)
    try {
      const result = await readJSON('/api/admin/upstreams/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          upstream: {
            name: probeProvider.name,
            base_url: probeProvider.base_url,
            anthropic_base_url: probeProvider.anthropic_base_url || undefined,
            api_key: probeProvider.api_key,
            provider_class: probeProvider.provider_class,
            models: probeProvider.models
              .split(',')
              .map((item) => item.trim())
              .filter(Boolean),
            timeout_ms: Number(probeProvider.timeout_ms) || 10000,
            enabled: probeProvider.enabled,
          },
        }),
      })
      setProbeResult(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const handleRunProbe = useCallback((e: Event) => {
    void runProbe(e)
  }, [probeProvider])

  const handleBenchmarkRefresh = useCallback(() => {
    void loadBenchmark()
  }, [benchmarkHours, benchmarkModels])

  const handleBenchmarkHoursChange = useCallback((hours: number) => {
    setBenchmarkHours(hours)
  }, [])

  const handleBenchmarkModelsChange = useCallback((models: string[]) => {
    setBenchmarkModels(models)
  }, [])

  // Tab content renderer with Suspense
  const renderTabContent = () => {
    const fallback = (
      <section class="panel">
        <p class="muted">{t('common.loading')}</p>
      </section>
    )

    switch (tab) {
      case 'overview':
        return (
          <Suspense fallback={fallback}>
            <OverviewTab overview={overview ?? null} />
          </Suspense>
        )
      case 'telemetry':
        return (
          <Suspense fallback={fallback}>
            <TelemetryTab telemetry={telemetry ?? null} timeseries={timeseries ?? null} />
          </Suspense>
        )
      case 'timeseries':
        return (
          <Suspense fallback={fallback}>
            <TimeSeriesTab timeseries={timeseries ?? null} />
          </Suspense>
        )
      case 'settings':
        return (
          <Suspense fallback={fallback}>
            <SettingsTab
              configView={configView ?? null}
              configText={configText}
              setConfigText={setConfigText}
              onSave={saveConfig}
              busy={busy}
            />
          </Suspense>
        )
      case 'history':
        return (
          <Suspense fallback={fallback}>
            <HistoryTab
              historyPayload={historyPayload ?? null}
              selectedVersion={selectedVersion}
              historyDiff={historyDiff}
              onVersionChange={handleVersionChange}
              onRollback={handleRollback}
              busy={busy}
            />
          </Suspense>
        )
      case 'probe':
        return (
          <Suspense fallback={fallback}>
            <ProbeTab
              probeProvider={probeProvider}
              setProbeProvider={setProbeProvider}
              probeResult={probeResult}
              onRunProbe={handleRunProbe}
              busy={busy}
            />
          </Suspense>
        )
      case 'benchmark':
        return (
          <Suspense fallback={fallback}>
            <BenchmarkTab
              benchmark={benchmark}
              benchmarkHours={benchmarkHours}
              benchmarkModels={benchmarkModels}
              benchmarkLoading={benchmarkLoading}
              onHoursChange={handleBenchmarkHoursChange}
              onModelsChange={handleBenchmarkModelsChange}
              onRefresh={handleBenchmarkRefresh}
            />
          </Suspense>
        )
      case 'logs':
        return (
          <section class="panel">
            <h2>{t('logs.title')}</h2>
            <LogViewer maxEntries={1000} />
          </section>
        )
      default:
        return null
    }
  }

  if (!authed) {
    return (
      <main class="app-shell login-shell">
        <section class="panel login-panel">
          <h1>{t('login.title')}</h1>
          <p class="muted">{t('login.hint')}</p>
          <form class="login-form" onSubmit={submitLogin}>
            <label>
              {t('login.tokenLabel')}
              <input
                type="password"
                value={token}
                onInput={(event) => setToken((event.currentTarget as HTMLInputElement).value)}
                placeholder={t('login.tokenPlaceholder')}
              />
            </label>
            <button type="submit" disabled={busy || token.trim() === ''}>
              {busy ? t('login.signingIn') : t('login.signIn')}
            </button>
          </form>
          {error && <p class="error">{error}</p>}
        </section>
      </main>
    )
  }

  return (
    <main class="app-shell">
      <header class="topbar">
        <div>
          <h1>{t('header.title')}</h1>
          <p class="muted">{t('header.subtitle')}</p>
        </div>
        <div class="topbar-actions">
          <div class="tabbar">
            {tabLabels.map((item) => (
              <button
                key={item.key}
                type="button"
                class={`tab${tab === item.key ? ' active' : ''}`}
                onClick={() => handleTabChange(item.key)}
              >
                {item.label}
              </button>
            ))}
          </div>
          <div class="header-controls">
            <LanguageSelector />
            <ThemeToggle />
          </div>
          <button type="button" onClick={logout} disabled={busy}>
            {t('actions.logout')}
          </button>
        </div>
      </header>

      {error && (
        <section class="panel">
          <p class="error">{error}</p>
        </section>
      )}

      {renderTabContent()}
    </main>
  )
}
