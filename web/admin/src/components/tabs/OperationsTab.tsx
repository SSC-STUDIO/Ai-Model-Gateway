import { useCallback, useEffect, useMemo, useState } from 'preact/compat'
import type { AnyRecord, ControlStatusView, ProviderHealthView } from '../../types'
import { fetchJSON } from '../../utils/fetch'
import { normalizeControlStatus } from '../../utils/controlApi'
import { useI18n } from '../../i18n'
import { Icon, type IconName } from '../Icon'
import { OpsTab } from './OpsTab'

type OpsWorkspaceMode = 'runtime' | 'updates' | 'probe' | 'audit' | 'diagnostics'
type Tone = 'success' | 'warning' | 'error' | 'neutral'

interface OperationsTabProps {
  mode: OpsWorkspaceMode
  canWrite: boolean
  onModeChange: (mode: OpsWorkspaceMode) => void
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

interface UpdateVerifyReport {
  ok?: boolean
  issues?: string[]
}

interface UpdateStatus {
  current_version?: string
  platform?: string
  repository?: string
  install_dir?: string
  state_dir?: string
  download_dir?: string
  latest_version?: string
  latest_tag?: string
  update_available?: boolean
  asset_name?: string
  asset_url?: string
  release_url?: string
  published_at?: string
  last_checked_at?: string
  last_check_error?: string
  cached_bundle_dir?: string
  cached_archive_path?: string
  cached_version?: string
  cached_verify?: UpdateVerifyReport
  last_applied_at?: string
  last_apply_error?: string
  last_backup_dir?: string
  last_rolled_back_at?: string
  last_rollback_error?: string
  message?: string
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

function normalizeUpdateStatus(value: unknown): UpdateStatus {
  const record = asRecord(value)
  const verify = asRecord(record?.cached_verify || record?.CachedVerify)
  return {
    current_version: asString(record?.current_version || record?.CurrentVersion),
    platform: asString(record?.platform || record?.Platform),
    repository: asString(record?.repository || record?.Repository),
    install_dir: asString(record?.install_dir || record?.InstallDir),
    state_dir: asString(record?.state_dir || record?.StateDir),
    download_dir: asString(record?.download_dir || record?.DownloadDir),
    latest_version: asString(record?.latest_version || record?.LatestVersion),
    latest_tag: asString(record?.latest_tag || record?.LatestTag),
    update_available: asBoolean(record?.update_available ?? record?.UpdateAvailable),
    asset_name: asString(record?.asset_name || record?.AssetName),
    asset_url: asString(record?.asset_url || record?.AssetURL),
    release_url: asString(record?.release_url || record?.ReleaseURL),
    published_at: asString(record?.published_at || record?.PublishedAt),
    last_checked_at: asString(record?.last_checked_at || record?.LastCheckedAt),
    last_check_error: asString(record?.last_check_error || record?.LastCheckError),
    cached_bundle_dir: asString(record?.cached_bundle_dir || record?.CachedBundleDir),
    cached_archive_path: asString(record?.cached_archive_path || record?.CachedArchivePath),
    cached_version: asString(record?.cached_version || record?.CachedVersion),
    cached_verify: verify ? {
      ok: asBoolean(verify.ok ?? verify.OK),
      issues: asArray(verify.issues || verify.Issues).map((item) => asString(item)).filter(Boolean),
    } : undefined,
    last_applied_at: asString(record?.last_applied_at || record?.LastAppliedAt),
    last_apply_error: asString(record?.last_apply_error || record?.LastApplyError),
    last_backup_dir: asString(record?.last_backup_dir || record?.LastBackupDir),
    last_rolled_back_at: asString(record?.last_rolled_back_at || record?.LastRolledBackAt),
    last_rollback_error: asString(record?.last_rollback_error || record?.LastRollbackError),
    message: asString(record?.message || record?.Message),
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

function UpdatePanel({
  status,
  busy,
  error,
  canWrite,
  manualBundle,
  onManualBundle,
  onCheck,
  onFetch,
  onApply,
  onDryRun,
  onRollback,
  t,
}: {
  status: UpdateStatus
  busy: string
  error: string
  canWrite: boolean
  manualBundle: string
  onManualBundle: (value: string) => void
  onCheck: () => void
  onFetch: () => void
  onApply: () => void
  onDryRun: () => void
  onRollback: () => void
  t: (key: string) => string
}) {
  const updateAvailable = status.update_available === true
  const verifyOK = status.cached_verify?.ok === true
  const latest = status.latest_version || '-'
  const current = status.current_version || '-'
  const cachedBundle = status.cached_bundle_dir || ''
  const manualReady = manualBundle.trim().length > 0
  const canApply = canWrite && !busy && (verifyOK || manualReady)

  return (
    <div class="ops-update-layout">
      <section class="ops-update-main">
        <div class="ops-section-heading">
          <div>
            <span class="workspace-kicker"><Icon name="download" class="workspace-kicker-icon" />{t('ops.updates')}</span>
            <h3>{updateAvailable ? t('ops.updateAvailable') : t('ops.updateStatus')}</h3>
          </div>
          <span class={`status-badge ${updateAvailable ? 'warning' : status.last_check_error ? 'error' : 'success'}`}>
            {updateAvailable ? t('ops.updateAvailable') : status.last_check_error ? t('ops.updateCheckFailed') : t('ops.updateCurrent')}
          </span>
        </div>

        <div class="ops-update-version-grid">
          <OpsSummaryItem icon="shield" label={t('ops.currentVersion')} value={current} detail={status.platform || '-'} tone="neutral" />
          <OpsSummaryItem icon="download" label={t('ops.latestVersion')} value={latest} detail={status.latest_tag || status.repository || '-'} tone={updateAvailable ? 'warning' : 'success'} />
          <OpsSummaryItem icon="file" label={t('ops.bundleAsset')} value={status.asset_name || '-'} detail={status.published_at ? formatDate(status.published_at) : status.repository || '-'} tone={status.asset_name ? 'success' : 'neutral'} />
          <OpsSummaryItem icon="check" label={t('ops.bundleVerify')} value={verifyOK ? t('ops.verified') : t('ops.notVerified')} detail={cachedBundle || t('ops.noCachedBundle')} tone={verifyOK ? 'success' : 'neutral'} />
        </div>

        {error ? <p class="ops-alert error">{error}</p> : null}
        {status.message ? <p class="ops-alert success">{status.message}</p> : null}
        {status.last_check_error ? <p class="ops-alert error">{status.last_check_error}</p> : null}
        {status.last_apply_error ? <p class="ops-alert error">{status.last_apply_error}</p> : null}
        {status.last_rollback_error ? <p class="ops-alert error">{status.last_rollback_error}</p> : null}
        {!canWrite ? <p class="ops-alert warning">{t('ops.updateReadOnly')}</p> : null}

        <div class="ops-update-actions">
          <button type="button" onClick={onCheck} disabled={Boolean(busy)}>
            <Icon name="refresh" />
            {busy === 'check' ? t('ops.checkingUpdate') : t('ops.checkForUpdates')}
          </button>
          <button type="button" class="secondary" onClick={onFetch} disabled={!canWrite || Boolean(busy) || !updateAvailable}>
            <Icon name="download" />
            {busy === 'fetch' ? t('ops.downloadingUpdate') : t('ops.downloadUpdate')}
          </button>
          <button type="button" class="secondary" onClick={onDryRun} disabled={!canApply}>
            <Icon name="check" />
            {busy === 'dry-run' ? t('ops.verifyingUpdate') : t('ops.verifyApply')}
          </button>
          <button type="button" onClick={onApply} disabled={!canApply}>
            <Icon name="upload" />
            {busy === 'apply' ? t('ops.applyingUpdate') : t('ops.applyUpdate')}
          </button>
          <button type="button" class="secondary" onClick={onRollback} disabled={!canWrite || Boolean(busy) || !status.last_backup_dir}>
            <Icon name="history" />
            {busy === 'rollback' ? t('ops.rollingBack') : t('ops.rollbackUpdate')}
          </button>
        </div>
      </section>

      <section class="ops-update-side">
        <div class="ops-section-heading">
          <div>
            <span class="workspace-kicker"><Icon name="file" class="workspace-kicker-icon" />{t('ops.manualBundle')}</span>
            <h3>{t('ops.localBundlePath')}</h3>
          </div>
        </div>
        <label class="ops-field">
          <span>{t('ops.bundleDirectory')}</span>
          <input
            value={manualBundle}
            placeholder={cachedBundle || 'D:\\path\\to\\bundle'}
            onInput={(event) => onManualBundle((event.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <div class="ops-path-list">
          <div class="ops-path-row">
            <span>{t('ops.installDir')}</span>
            <code>{status.install_dir || '-'}</code>
          </div>
          <div class="ops-path-row">
            <span>{t('ops.stateDir')}</span>
            <code>{status.state_dir || '-'}</code>
          </div>
          <div class="ops-path-row">
            <span>{t('ops.cachedArchive')}</span>
            <code>{status.cached_archive_path || '-'}</code>
          </div>
          <div class="ops-path-row">
            <span>{t('ops.lastBackup')}</span>
            <code>{status.last_backup_dir || '-'}</code>
          </div>
        </div>
        {status.cached_verify?.issues?.length ? (
          <div class="ops-update-issues">
            <strong>{t('ops.verifyIssues')}</strong>
            {status.cached_verify.issues.map((issue) => <span key={issue}>{issue}</span>)}
          </div>
        ) : null}
      </section>
    </div>
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
                <small>{event.actor || 'system'} / {event.resource || 'runtime'}</small>
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

export function OperationsTab({ mode, canWrite, onModeChange, onUnauthorized }: OperationsTabProps) {
  const { t } = useI18n()
  const [runtimePayload, setRuntimePayload] = useState<RuntimePayload>(() => normalizeRuntimePayload(null))
  const [audit, setAudit] = useState<AuditSummary>({ events: [], count: 0 })
  const [runtimeBusy, setRuntimeBusy] = useState(false)
  const [preflightBusy, setPreflightBusy] = useState(false)
  const [updateBusy, setUpdateBusy] = useState('')
  const [runtimeError, setRuntimeError] = useState('')
  const [auditError, setAuditError] = useState('')
  const [updateError, setUpdateError] = useState('')
  const [preflight, setPreflight] = useState<PreflightPayload | null>(null)
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus>(() => normalizeUpdateStatus(null))
  const [manualBundle, setManualBundle] = useState('')

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

  const loadUpdateStatus = useCallback(async () => {
    setUpdateError('')
    try {
      setUpdateStatus(normalizeUpdateStatus(await fetchJSON('/api/admin/update/status', { onUnauthorized })))
    } catch (err) {
      setUpdateError(err instanceof Error ? err.message : String(err))
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

  const runUpdateAction = useCallback(async (
    action: 'check' | 'fetch' | 'dry-run' | 'apply' | 'rollback'
  ) => {
    setUpdateBusy(action)
    setUpdateError('')
    try {
      let endpoint = '/api/admin/update/check'
      let body: Record<string, unknown> | undefined
      if (action === 'fetch') {
        endpoint = '/api/admin/update/fetch'
      } else if (action === 'dry-run' || action === 'apply') {
        endpoint = '/api/admin/update/apply'
        body = {
          bundle_dir: manualBundle.trim() || undefined,
          download: !manualBundle.trim() && !updateStatus.cached_bundle_dir,
          dry_run: action === 'dry-run',
        }
      } else if (action === 'rollback') {
        endpoint = '/api/admin/update/rollback'
      }
      setUpdateStatus(normalizeUpdateStatus(await fetchJSON(endpoint, {
        method: 'POST',
        body: body ? JSON.stringify(body) : undefined,
        onUnauthorized,
      })))
      await loadAudit()
    } catch (err) {
      setUpdateError(err instanceof Error ? err.message : String(err))
    } finally {
      setUpdateBusy('')
    }
  }, [loadAudit, manualBundle, onUnauthorized, updateStatus.cached_bundle_dir])

  useEffect(() => {
    void Promise.all([loadRuntime(), loadAudit(), loadUpdateStatus()])
  }, [loadAudit, loadRuntime, loadUpdateStatus])

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
      detail: `${status?.cooldown_provider_count ?? 0} ${t('ops.cooldown')} / ${status?.unhealthy_provider_count ?? 0} ${t('ops.unhealthy')}`,
      tone: (status?.unhealthy_provider_count ?? 0) > 0 ? 'error' : (status?.cooldown_provider_count ?? 0) > 0 ? 'warning' : providers.length > 0 ? 'success' : 'neutral',
    },
    {
      icon: 'download',
      label: t('ops.updates'),
      value: updateStatus.update_available ? t('ops.updateAvailableShort') : t('ops.updateCurrentShort'),
      detail: updateStatus.latest_version || updateStatus.last_checked_at || t('ops.notChecked'),
      tone: updateStatus.update_available ? 'warning' : updateStatus.last_check_error ? 'error' : 'neutral',
    },
    {
      icon: 'clock',
      label: t('ops.activeRequests'),
      value: status?.active_requests ?? 0,
      detail: `${failedAuditCount} ${t('ops.recentFailures')}`,
      tone: failedAuditCount > 0 ? 'warning' : 'neutral',
    },
  ], [audit.events, gatewayState, providers, status, t, telemetryState, updateStatus])

  const workspaceItems: Array<{ mode: OpsWorkspaceMode; icon: IconName; label: string; detail: string }> = [
    { mode: 'runtime', icon: 'server', label: t('ops.runtime'), detail: t('ops.runtimeWorkspaceHint') },
    { mode: 'updates', icon: 'download', label: t('ops.updates'), detail: t('ops.updatesWorkspaceHint') },
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
          <button type="button" class="secondary" onClick={() => onModeChange('diagnostics')}>
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
            onClick={() => onModeChange(item.mode)}
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
        {mode === 'updates' ? (
          <UpdatePanel
            status={updateStatus}
            busy={updateBusy}
            error={updateError}
            canWrite={canWrite}
            manualBundle={manualBundle}
            onManualBundle={setManualBundle}
            onCheck={() => { void runUpdateAction('check') }}
            onFetch={() => { void runUpdateAction('fetch') }}
            onDryRun={() => { void runUpdateAction('dry-run') }}
            onApply={() => { void runUpdateAction('apply') }}
            onRollback={() => { void runUpdateAction('rollback') }}
            t={t}
          />
        ) : null}
        {mode === 'probe' ? <OpsTab mode="probe" canWrite={canWrite} embedded onUnauthorized={onUnauthorized} /> : null}
        {mode === 'audit' ? <OpsTab mode="audit" canWrite={canWrite} embedded onUnauthorized={onUnauthorized} /> : null}
        {mode === 'diagnostics' ? <OpsTab mode="diagnostics" canWrite={canWrite} embedded onUnauthorized={onUnauthorized} /> : null}
      </section>
    </section>
  )
}
