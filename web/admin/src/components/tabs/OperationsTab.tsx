import { useCallback, useEffect, useMemo, useState } from 'preact/compat'
import type { AnyRecord, ControlStatusView, ProviderHealthView } from '../../types'
import { fetchJSON } from '../../utils/fetch'
import { normalizeControlStatus } from '../../utils/controlApi'
import { useI18n } from '../../i18n'
import { Icon, type IconName } from '../Icon'
import { OpsTab } from './OpsTab'

type OpsWorkspaceMode = 'runtime' | 'probe' | 'audit' | 'diagnostics'
type Tone = 'success' | 'warning' | 'error' | 'neutral'

interface OperationsTabProps {
  canWrite: boolean
  onUnauthorized?: () => void
}

interface RuntimePayload {
  raw: unknown
  status: ControlStatusView | null
  runtime?: AnyRecord
}

interface AuditEvent {
  id?: string
  time?: string
  actor?: string
  action?: string
  resource?: string
  success?: boolean
  error?: string
  request_id?: string
}

interface AuditSummary {
  events: AuditEvent[]
  count: number
}

interface PreflightCheck {
  name: string
  ok: boolean
  detail?: string
}

interface PreflightPayload {
  ok: boolean
  checks: PreflightCheck[]
}

interface OpsSummaryItemProps {
  icon: IconName
  label: string
  value: string | number
  detail?: string
  tone?: Tone
}

function asRecord(value: unknown): AnyRecord | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as AnyRecord : null
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : value == null ? '' : String(value)
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function asNumber(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : undefined
  }
  return undefined
}

function formatDate(value: unknown): string {
  const raw = asString(value)
  if (!raw) return '-'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return raw
  return date.toLocaleString()
}

function toneFromState(value: string): Tone {
  const normalized = value.toLowerCase()
  if (['ok', 'ready', 'connected', 'healthy', 'serving', 'true'].includes(normalized)) return 'success'
  if (['degraded', 'starting', 'draining', 'unknown', 'disconnected', 'cooldown'].includes(normalized)) return 'warning'
  if (['error', 'failed', 'unhealthy', 'false', 'stopped'].includes(normalized)) return 'error'
  return 'neutral'
}

function normalizeAuditEvent(value: unknown): AuditEvent | null {
  const record = asRecord(value)
  if (!record) return null
  return {
    id: asString(record.id || record.ID),
    time: asString(record.time || record.Time),
    actor: asString(record.actor || record.Actor),
    action: asString(record.action || record.Action),
    resource: asString(record.resource || record.Resource),
    success: asBoolean(record.success ?? record.Success),
    error: asString(record.error || record.Error),
    request_id: asString(record.request_id || record.RequestID),
  }
}

function normalizeAuditSummary(value: unknown): AuditSummary {
  const directEvents = asArray(value)
  const record = asRecord(value)
  const events = (directEvents.length > 0 ? directEvents : asArray(record?.events || record?.Events))
    .map(normalizeAuditEvent)
    .filter((event): event is AuditEvent => event !== null)
  return {
    events,
    count: asNumber(record?.count || record?.Count) ?? events.length,
  }
}

function normalizeRuntimePayload(value: unknown): RuntimePayload {
  const record = asRecord(value)
  return {
    raw: value,
    status: normalizeControlStatus(value),
    runtime: asRecord(record?.runtime || record?.Runtime) ?? undefined,
  }
}

function normalizePreflight(value: unknown): PreflightPayload {
  const record = asRecord(value)
  const checks: PreflightCheck[] = asArray(record?.checks || record?.Checks)
    .map((item): PreflightCheck | null => {
      const check = asRecord(item)
      if (!check) return null
      const detail = asString(check.detail || check.Detail)
      return {
        name: asString(check.name || check.Name),
        ok: asBoolean(check.ok ?? check.OK) ?? false,
        detail: detail || undefined,
      }
    })
    .filter((item): item is PreflightCheck => item !== null && item.name.length > 0)
  return {
    ok: asBoolean(record?.ok ?? record?.OK) ?? checks.every((check) => check.ok),
    checks,
  }
}

function runtimeValue(runtime: AnyRecord | undefined, keys: string[]): string {
  if (!runtime) return ''
  for (const key of keys) {
    const value = runtime[key]
    if (typeof value !== 'object' && value != null && asString(value)) return asString(value)
  }
  return ''
}

function configPathValues(runtime: AnyRecord | undefined): Array<[string, string]> {
  const paths = asRecord(runtime?.config_paths || runtime?.ConfigPaths)
  if (!paths) return []
  return Object.entries(paths)
    .map(([key, value]) => [key.replace(/_/g, ' '), asString(value)] as [string, string])
    .filter(([, value]) => value.length > 0)
    .slice(0, 6)
}

function OpsSummaryItem({ icon, label, value, detail, tone = 'neutral' }: OpsSummaryItemProps) {
  return (
    <article class={`ops-command-metric ${tone}`}>
      <span class="ops-command-metric-icon"><Icon name={icon} /></span>
      <span class="ops-command-metric-label">{label}</span>
      <strong>{value}</strong>
      {detail ? <small>{detail}</small> : null}
    </article>
  )
}

function TopologyNode({
  icon,
  label,
  state,
  detail,
  meta,
}: {
  icon: IconName
  label: string
  state: string
  detail?: string
  meta?: string
}) {
  const tone = toneFromState(state || 'unknown')
  return (
    <article class={`ops-topology-node ${tone}`}>
      <div class="ops-topology-node-head">
        <span class="ops-topology-icon"><Icon name={icon} /></span>
        <span class={`ops-node-pulse ${tone}`} />
      </div>
      <div class="ops-topology-node-copy">
        <span>{label}</span>
        <strong>{state || 'unknown'}</strong>
        {detail ? <small title={detail}>{detail}</small> : null}
        {meta ? <em>{meta}</em> : null}
      </div>
    </article>
  )
}

function FlowLink() {
  return (
    <div class="ops-topology-link" aria-hidden="true">
      <span />
      <Icon name="arrowRight" />
    </div>
  )
}

function ProviderTile({ provider }: { provider: ProviderHealthView }) {
  const tone: Tone = provider.status === 'healthy' ? 'success' : provider.status === 'cooldown' ? 'warning' : 'error'
  return (
    <article class={`ops-provider-tile ${tone}`}>
      <div class="ops-provider-tile-head">
        <strong title={provider.name}>{provider.name}</strong>
        <span>{provider.status}</span>
      </div>
      <div class="ops-provider-tile-meta">
        <span>{typeof provider.latency_ms === 'number' ? `${provider.latency_ms} ms` : '-'}</span>
        <span>{provider.consecutive_failures} failures</span>
      </div>
    </article>
  )
}

function ProviderHealthPanel({ providers, t }: { providers: ProviderHealthView[]; t: (key: string) => string }) {
  const total = providers.length
  const healthy = providers.filter((provider) => provider.status === 'healthy').length
  const cooldown = providers.filter((provider) => provider.status === 'cooldown').length
  const unhealthy = providers.filter((provider) => provider.status === 'unhealthy').length
  const sorted = providers.slice().sort((left, right) => {
    const score = (provider: ProviderHealthView) => provider.status === 'unhealthy' ? 0 : provider.status === 'cooldown' ? 1 : 2
    const statusDiff = score(left) - score(right)
    if (statusDiff !== 0) return statusDiff
    return left.name.localeCompare(right.name)
  })

  const percent = (value: number) => total > 0 ? Math.max(0, (value / total) * 100) : 0

  return (
    <section class="ops-provider-panel">
      <div class="ops-section-heading">
        <div>
          <span class="workspace-kicker"><Icon name="activity" class="workspace-kicker-icon" />{t('ops.providerHealth')}</span>
          <h3>{healthy}/{total} {t('ops.providersReady')}</h3>
        </div>
        <span class={`status-badge ${unhealthy > 0 ? 'error' : cooldown > 0 ? 'warning' : total > 0 ? 'success' : 'neutral'}`}>
          {unhealthy > 0 ? t('ops.attentionNeeded') : cooldown > 0 ? t('overview.statusCooldown') : t('overview.statusHealthy')}
        </span>
      </div>

      <div class="ops-health-strip" aria-label={t('ops.providerHealth')}>
        <span class="success" style={{ width: `${percent(healthy)}%` }} />
        <span class="warning" style={{ width: `${percent(cooldown)}%` }} />
        <span class="error" style={{ width: `${percent(unhealthy)}%` }} />
      </div>

      <div class="ops-provider-counts">
        <span><strong>{healthy}</strong>{t('ops.healthy')}</span>
        <span><strong>{cooldown}</strong>{t('ops.cooldown')}</span>
        <span><strong>{unhealthy}</strong>{t('ops.unhealthy')}</span>
      </div>

      {sorted.length > 0 ? (
        <div class="ops-provider-grid">
          {sorted.slice(0, 8).map((provider) => <ProviderTile key={provider.name} provider={provider} />)}
        </div>
      ) : (
        <div class="ops-compact-empty">{t('overview.providerNoData')}</div>
      )}
    </section>
  )
}

function PreflightPanel({
  preflight,
  busy,
  onRun,
  canWrite,
  t,
}: {
  preflight: PreflightPayload | null
  busy: boolean
  onRun: () => void
  canWrite: boolean
  t: (key: string) => string
}) {
  return (
    <section class="ops-preflight-panel">
      <div class="ops-section-heading">
        <div>
          <span class="workspace-kicker"><Icon name="check" class="workspace-kicker-icon" />{t('ops.preflight')}</span>
          <h3>{preflight ? (preflight.ok ? t('ops.preflightPassed') : t('ops.preflightFailed')) : t('ops.preflightReady')}</h3>
        </div>
        <button type="button" disabled={busy || !canWrite} onClick={onRun}>
          <Icon name="check" />
          {busy ? t('ops.running') : t('ops.runPreflight')}
        </button>
      </div>
      {!canWrite ? <p class="ops-alert warning">{t('ops.readOnlyPreflight')}</p> : null}
      {preflight ? (
        <div class="ops-check-list">
          {preflight.checks.map((check) => (
            <div class={`ops-check-row ${check.ok ? 'success' : 'error'}`} key={check.name}>
              <span><Icon name={check.ok ? 'check' : 'close'} /></span>
              <strong>{check.name.replace(/_/g, ' ')}</strong>
              <small>{check.detail || (check.ok ? 'ok' : 'failed')}</small>
            </div>
          ))}
        </div>
      ) : (
        <p class="ops-compact-empty">{t('ops.preflightHint')}</p>
      )}
    </section>
  )
}

function RecentOpsPanel({ audit, t }: { audit: AuditSummary; t: (key: string) => string }) {
  const events = audit.events.slice(0, 3)
  return (
    <section class="ops-recent-panel">
      <div class="ops-section-heading">
        <div>
          <span class="workspace-kicker"><Icon name="history" class="workspace-kicker-icon" />{t('tabs.audit')}</span>
          <h3>{audit.count} {t('ops.recentEvents')}</h3>
        </div>
      </div>
      {events.length > 0 ? (
        <div class="ops-recent-list">
          {events.map((event, index) => (
            <article class={`ops-recent-row ${event.success === false ? 'error' : 'success'}`} key={event.id || `${event.action}-${index}`}>
              <span class="ops-recent-dot" />
              <div>
                <strong>{event.action || 'unknown'}</strong>
                <small>{event.actor || 'system'} · {event.resource || 'runtime'}</small>
              </div>
              <time>{formatDate(event.time)}</time>
            </article>
          ))}
        </div>
      ) : (
        <p class="ops-compact-empty">{t('ops.noAuditEventsHint')}</p>
      )}
    </section>
  )
}

function RuntimeCommandCenter({
  payload,
  audit,
  preflight,
  preflightBusy,
  onPreflight,
  t,
  canWrite,
}: {
  payload: RuntimePayload
  audit: AuditSummary
  preflight: PreflightPayload | null
  preflightBusy: boolean
  onPreflight: () => void
  t: (key: string) => string
  canWrite: boolean
}) {
  const status = payload.status
  const runtime = payload.runtime
  const providers = status?.provider_health ?? []
  const gatewayState = status?.gateway_readiness || status?.gateway_status || 'unknown'
  const gatewayDetail = status?.gateway_listener || runtimeValue(runtime, ['gateway_listen', 'gateway_addr', 'gateway_socket'])
  const telemetryState = status?.telemetry_status || 'unknown'
  const telemetryDetail = runtimeValue(runtime, ['telemetry_socket', 'telemetry_query_socket', 'telemetry_addr'])
  const controlDetail = runtimeValue(runtime, ['listen', 'control_listen', 'admin_listen'])
  const version = status?.version || runtimeValue(runtime, ['bundle_version', 'product_version', 'version']) || '-'
  const snapshot = status?.active_snapshot_id || '-'
  const configPaths = configPathValues(runtime)
  const readyProviders = providers.filter((provider) => provider.healthy).length
  const providerState = providers.length === 0
    ? 'unknown'
    : (status?.unhealthy_provider_count ?? 0) > 0 ? 'unhealthy'
    : (status?.cooldown_provider_count ?? 0) > 0 ? 'cooldown'
    : 'ready'

  return (
    <div class="ops-command-layout">
      <section class="ops-command-main">
        <div class="ops-section-heading">
          <div>
            <span class="workspace-kicker"><Icon name="server" class="workspace-kicker-icon" />{t('ops.commandTopology')}</span>
            <h3>{t('ops.runtimeFlow')}</h3>
          </div>
          <span class={`status-badge ${toneFromState(gatewayState)}`}>
            {t('header.gateway')}: {gatewayState}
          </span>
        </div>

        <div class="ops-topology-canvas">
          <TopologyNode icon="shield" label={t('ops.controlPlane')} state="serving" detail={controlDetail} meta={version} />
          <FlowLink />
          <TopologyNode icon="server" label={t('ops.gatewayPlane')} state={gatewayState} detail={gatewayDetail} meta={snapshot} />
          <FlowLink />
          <TopologyNode icon="database" label={t('ops.telemetryPlane')} state={telemetryState} detail={telemetryDetail} meta={status?.telemetry_event_count != null ? `${status.telemetry_event_count} events` : status?.telemetry_version} />
          <FlowLink />
          <TopologyNode icon="activity" label={t('ops.providerPlane')} state={providerState} detail={providers.length ? `${readyProviders}/${providers.length} ${t('ops.ready')}` : t('ops.providerHealth')} meta={providers.length ? t('ops.liveRouting') : ''} />
        </div>

        {configPaths.length > 0 ? (
          <div class="ops-path-ribbon">
            {configPaths.map(([key, value]) => (
              <span key={key} title={value}>
                <strong>{key}</strong>
                <code>{value}</code>
              </span>
            ))}
          </div>
        ) : null}
      </section>

      <ProviderHealthPanel providers={providers} t={t} />
      <PreflightPanel preflight={preflight} busy={preflightBusy} canWrite={canWrite} onRun={onPreflight} t={t} />
      <RecentOpsPanel audit={audit} t={t} />
    </div>
  )
}

export function OperationsTab({ canWrite, onUnauthorized }: OperationsTabProps) {
  const { t } = useI18n()
  const [mode, setMode] = useState<OpsWorkspaceMode>('runtime')
  const [runtimePayload, setRuntimePayload] = useState<RuntimePayload>(() => normalizeRuntimePayload(null))
  const [audit, setAudit] = useState<AuditSummary>({ events: [], count: 0 })
  const [runtimeBusy, setRuntimeBusy] = useState(false)
  const [preflightBusy, setPreflightBusy] = useState(false)
  const [runtimeError, setRuntimeError] = useState('')
  const [auditError, setAuditError] = useState('')
  const [preflight, setPreflight] = useState<PreflightPayload | null>(null)

  const loadRuntime = useCallback(async () => {
    setRuntimeBusy(true)
    setRuntimeError('')
    try {
      setRuntimePayload(normalizeRuntimePayload(await fetchJSON('/api/admin/runtime/status', { onUnauthorized })))
    } catch (err) {
      setRuntimeError(err instanceof Error ? err.message : String(err))
    } finally {
      setRuntimeBusy(false)
    }
  }, [onUnauthorized])

  const loadAudit = useCallback(async () => {
    setAuditError('')
    try {
      setAudit(normalizeAuditSummary(await fetchJSON('/api/admin/audit?limit=20', { onUnauthorized })))
    } catch (err) {
      setAuditError(err instanceof Error ? err.message : String(err))
    }
  }, [onUnauthorized])

  const runPreflight = useCallback(async () => {
    setPreflightBusy(true)
    setRuntimeError('')
    try {
      setPreflight(normalizePreflight(await fetchJSON('/api/admin/runtime/preflight', {
        method: 'POST',
        onUnauthorized,
      })))
      await Promise.all([loadRuntime(), loadAudit()])
    } catch (err) {
      setRuntimeError(err instanceof Error ? err.message : String(err))
    } finally {
      setPreflightBusy(false)
    }
  }, [loadAudit, loadRuntime, onUnauthorized])

  useEffect(() => {
    void Promise.all([loadRuntime(), loadAudit()])
  }, [loadAudit, loadRuntime])

  const status = runtimePayload.status
  const providers = status?.provider_health ?? []
  const gatewayState = status?.gateway_readiness || status?.gateway_status || 'unknown'
  const telemetryState = status?.telemetry_status || 'unknown'
  const failedAuditCount = audit.events.filter((event) => event.success === false).length

  const summaryItems = useMemo<OpsSummaryItemProps[]>(() => [
    {
      icon: 'shield',
      label: t('ops.bundle'),
      value: status?.version || '-',
      detail: status?.uptime ? `${t('ops.uptime')} ${status.uptime}` : t('ops.runtimeStatus'),
      tone: status?.version ? 'success' : 'neutral',
    },
    {
      icon: 'server',
      label: t('header.gateway'),
      value: gatewayState,
      detail: status?.gateway_listener || status?.gateway_error || t('ops.noRuntimeDetail'),
      tone: toneFromState(gatewayState),
    },
    {
      icon: 'database',
      label: t('header.telemetry'),
      value: telemetryState,
      detail: status?.telemetry_event_count != null ? `${status.telemetry_event_count} ${t('ops.events')}` : status?.telemetry_error || t('ops.noRuntimeDetail'),
      tone: toneFromState(telemetryState),
    },
    {
      icon: 'activity',
      label: t('ops.providers'),
      value: `${providers.filter((provider) => provider.healthy).length}/${providers.length}`,
      detail: `${status?.cooldown_provider_count ?? 0} ${t('ops.cooldown')} · ${status?.unhealthy_provider_count ?? 0} ${t('ops.unhealthy')}`,
      tone: (status?.unhealthy_provider_count ?? 0) > 0 ? 'error' : (status?.cooldown_provider_count ?? 0) > 0 ? 'warning' : providers.length > 0 ? 'success' : 'neutral',
    },
    {
      icon: 'clock',
      label: t('ops.activeRequests'),
      value: status?.active_requests ?? 0,
      detail: `${failedAuditCount} ${t('ops.recentFailures')}`,
      tone: failedAuditCount > 0 ? 'warning' : 'neutral',
    },
  ], [audit.events, gatewayState, providers, status, t, telemetryState])

  const workspaceItems: Array<{ mode: OpsWorkspaceMode; icon: IconName; label: string; detail: string }> = [
    { mode: 'runtime', icon: 'server', label: t('ops.runtime'), detail: t('ops.runtimeWorkspaceHint') },
    { mode: 'probe', icon: 'search', label: t('tabs.probe'), detail: t('ops.probeWorkspaceHint') },
    { mode: 'audit', icon: 'history', label: t('tabs.audit'), detail: t('ops.auditWorkspaceHint') },
    { mode: 'diagnostics', icon: 'file', label: t('tabs.diagnostics'), detail: t('ops.diagnosticsWorkspaceHint') },
  ]

  return (
    <section class="panel workspace-page ops-command-page">
      <div class="workspace-hero workspace-hero-ops ops-command-hero">
        <div class="workspace-hero-copy">
          <span class="workspace-kicker">{t('ops.commandCenter')}</span>
          <h2 class="workspace-title">{t('tabs.ops')}</h2>
          <p class="workspace-subtitle">{t('ops.commandCenterSubtitle')}</p>
        </div>
        <div class="workspace-hero-meta ops-command-actions">
          <span class={`status-badge ${canWrite ? 'success' : 'neutral'}`}>
            {canWrite ? t('ops.adminWrite') : t('ops.viewerReadOnly')}
          </span>
          <button type="button" class="secondary" disabled={runtimeBusy} onClick={() => { void Promise.all([loadRuntime(), loadAudit()]) }}>
            <Icon name="refresh" />
            {runtimeBusy ? t('ops.refreshing') : t('ops.refresh')}
          </button>
          <button type="button" class="secondary" onClick={() => setMode('diagnostics')}>
            <Icon name="download" />
            {t('ops.generateDiagnostics')}
          </button>
        </div>
      </div>

      <div class="ops-command-metrics">
        {summaryItems.map((item) => <OpsSummaryItem key={item.label} {...item} />)}
      </div>

      {runtimeError ? <p class="ops-alert error">{runtimeError}</p> : null}
      {auditError ? <p class="ops-alert warning">{auditError}</p> : null}

      <nav class="ops-workspace-switch" aria-label={t('ops.workspace')}>
        {workspaceItems.map((item) => (
          <button
            key={item.mode}
            type="button"
            class={`ops-workspace-switch-btn${mode === item.mode ? ' active' : ''}`}
            onClick={() => setMode(item.mode)}
          >
            <Icon name={item.icon} />
            <span>
              <strong>{item.label}</strong>
              <small>{item.detail}</small>
            </span>
          </button>
        ))}
      </nav>

      <section class="ops-workspace-panel">
        {mode === 'runtime' ? (
          runtimeBusy && !runtimePayload.status ? (
            <div class="ops-skeleton-grid">
              <div class="skeleton ops-skeleton-block" />
              <div class="skeleton ops-skeleton-block" />
              <div class="skeleton ops-skeleton-wide" />
            </div>
          ) : (
            <RuntimeCommandCenter
              payload={runtimePayload}
              audit={audit}
              preflight={preflight}
              preflightBusy={preflightBusy}
              onPreflight={() => { void runPreflight() }}
              t={t}
              canWrite={canWrite}
            />
          )
        ) : null}
        {mode === 'probe' ? <OpsTab mode="probe" canWrite={canWrite} embedded onUnauthorized={onUnauthorized} /> : null}
        {mode === 'audit' ? <OpsTab mode="audit" canWrite={canWrite} embedded onUnauthorized={onUnauthorized} /> : null}
        {mode === 'diagnostics' ? <OpsTab mode="diagnostics" canWrite={canWrite} embedded onUnauthorized={onUnauthorized} /> : null}
      </section>
    </section>
  )
}
