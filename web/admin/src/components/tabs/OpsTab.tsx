import { useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { fetchJSON } from '../../utils/fetch'
import { useI18n } from '../../i18n'
import { Icon, type IconName } from '../Icon'
import { asRecord, asArray, asBoolean } from '../../utils/controlApi'
import { formatAbsoluteTime } from '../../utils/formatting'
import { copyText } from '../../utils/clipboard'
import type { AnyRecord } from '../../types'

type OpsMode = 'audit' | 'probe' | 'diagnostics'

interface OpsTabProps {
  mode: OpsMode
  canWrite: boolean
  embedded?: boolean
  onUnauthorized?: () => void
}

interface AuditEvent {
  id?: string
  time?: string
  actor?: string
  role?: string
  source?: string
  action?: string
  resource?: string
  success?: boolean
  error?: string
  request_id?: string
  details?: AnyRecord
}

interface AuditPayload {
  events: AuditEvent[]
  count: number
}

interface ProbeResult {
  diagnostic?: boolean
  provider_id?: string
  model?: string
  status_code?: number
  latency_ms?: number
  healthy?: boolean
  error?: string
  response_excerpt?: string
  headers?: Record<string, string>
  probed_at?: string
}

interface DiagnosticsPayload {
  generated_at?: string
  redacted?: boolean
  status?: AnyRecord
  runtime?: AnyRecord
  audit_tail: AuditEvent[]
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : value == null ? '' : String(value)
}

function asNumber(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : undefined
  }
  return undefined
}

function pretty(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2)
}

function formatDate(value: unknown): string {
  return formatAbsoluteTime(asString(value) || null)
}

function normalizeAuditEvent(value: unknown): AuditEvent | null {
  const record = asRecord(value)
  if (!record) return null
  return {
    id: asString(record.id || record.ID),
    time: asString(record.time || record.Time),
    actor: asString(record.actor || record.Actor),
    role: asString(record.role || record.Role),
    source: asString(record.source || record.Source),
    action: asString(record.action || record.Action),
    resource: asString(record.resource || record.Resource),
    success: asBoolean(record.success ?? record.Success),
    error: asString(record.error || record.Error),
    request_id: asString(record.request_id || record.RequestID),
    details: asRecord(record.details || record.Details) ?? undefined,
  }
}

function normalizeAuditPayload(value: unknown): AuditPayload {
  const record = asRecord(value)
  const events = asArray(record?.events || record?.Events)
    .map(normalizeAuditEvent)
    .filter((event): event is AuditEvent => event !== null)
  return {
    events,
    count: asNumber(record?.count || record?.Count) ?? events.length,
  }
}

function normalizeProbeResult(value: unknown): ProbeResult | null {
  const record = asRecord(value)
  if (!record) return null
  const headersRecord = asRecord(record.headers || record.Headers)
  const headers: Record<string, string> = {}
  if (headersRecord) {
    for (const [key, headerValue] of Object.entries(headersRecord)) {
      headers[key] = asString(headerValue)
    }
  }
  return {
    diagnostic: asBoolean(record.diagnostic ?? record.Diagnostic),
    provider_id: asString(record.provider_id || record.ProviderID),
    model: asString(record.model || record.Model),
    status_code: asNumber(record.status_code || record.StatusCode),
    latency_ms: asNumber(record.latency_ms || record.LatencyMs),
    healthy: asBoolean(record.healthy ?? record.Healthy),
    error: asString(record.error || record.Error),
    response_excerpt: asString(record.response_excerpt || record.ResponseExcerpt),
    headers,
    probed_at: asString(record.probed_at || record.ProbedAt),
  }
}

function normalizeDiagnostics(value: unknown): DiagnosticsPayload {
  const record = asRecord(value)
  const auditTail = asArray(record?.audit_tail || record?.AuditTail)
    .map(normalizeAuditEvent)
    .filter((event): event is AuditEvent => event !== null)
  return {
    generated_at: asString(record?.generated_at || record?.GeneratedAt),
    redacted: asBoolean(record?.redacted ?? record?.Redacted),
    status: asRecord(record?.status || record?.Status) ?? undefined,
    runtime: asRecord(record?.runtime || record?.Runtime) ?? undefined,
    audit_tail: auditTail,
  }
}

function toneFromState(value: string): 'success' | 'warning' | 'error' | 'neutral' {
  const normalized = value.toLowerCase()
  if (['ok', 'ready', 'connected', 'healthy', 'serving', 'true'].includes(normalized)) return 'success'
  if (['degraded', 'starting', 'unknown', 'disconnected'].includes(normalized)) return 'warning'
  if (['error', 'failed', 'unhealthy', 'false'].includes(normalized)) return 'error'
  return 'neutral'
}

function titleForMode(mode: OpsMode, t: (key: string) => string): string {
  switch (mode) {
    case 'audit': return t('tabs.audit')
    case 'probe': return t('tabs.probe')
    case 'diagnostics': return t('tabs.diagnostics')
  }
}

function subtitleForMode(mode: OpsMode, t: (key: string) => string): string {
  switch (mode) {
    case 'audit': return t('ops.auditSubtitle')
    case 'probe': return t('ops.probeSubtitle')
    case 'diagnostics': return t('ops.diagnosticsSubtitle')
  }
}

const RUNTIME_FIELD_LABEL_KEY: Record<string, string> = {
  provider_count: 'ops.runtimeField.provider_count',
  enabled_provider_count: 'ops.runtimeField.enabled_provider_count',
  router_strategy: 'ops.runtimeField.router_strategy',
  health_enabled: 'ops.runtimeField.health_enabled',
  sticky_sessions_enabled: 'ops.runtimeField.sticky_sessions_enabled',
  bridge_enabled: 'ops.runtimeField.bridge_enabled',
  version: 'ops.runtimeField.version',
  product_version: 'ops.runtimeField.product_version',
  uptime: 'ops.runtimeField.uptime',
  gateway_status: 'ops.runtimeField.gateway_status',
  telemetry_status: 'ops.runtimeField.telemetry_status',
  gateway_readiness: 'ops.runtimeField.gateway_readiness',
  gateway_listener: 'ops.runtimeField.gateway_listener',
  gateway_last_auto_remediation_at: 'ops.runtimeField.gateway_last_auto_remediation_at',
  gateway_last_auto_remediation_reason: 'ops.runtimeField.gateway_last_auto_remediation_reason',
  active_snapshot_id: 'ops.runtimeField.active_snapshot_id',
  active_requests: 'ops.runtimeField.active_requests',
  provider_health_count: 'ops.runtimeField.provider_health_count',
  healthy_provider_count: 'ops.runtimeField.healthy_provider_count',
  unhealthy_provider_count: 'ops.runtimeField.unhealthy_provider_count',
  cooldown_provider_count: 'ops.runtimeField.cooldown_provider_count',
  telemetry_event_count: 'ops.runtimeField.telemetry_event_count',
  telemetry_last_checked_at: 'ops.runtimeField.telemetry_last_checked_at',
  telemetry_version: 'ops.runtimeField.telemetry_version',
  rpc_contract_version: 'ops.runtimeField.rpc_contract_version',
  startedAt: 'ops.runtimeField.started_at',
  started_at: 'ops.runtimeField.started_at',
  bundle_version: 'ops.runtimeField.bundle_version',
  bundle_manifest: 'ops.runtimeField.bundle_manifest',
  config_path: 'ops.runtimeField.config_path',
  data_dir: 'ops.runtimeField.data_dir',
  listen: 'ops.runtimeField.listen',
  gateway_socket: 'ops.runtimeField.gateway_socket',
  telemetry_socket: 'ops.runtimeField.telemetry_socket',
}

const RUNTIME_FIELD_ORDER = [
  'provider_count',
  'enabled_provider_count',
  'router_strategy',
  'health_enabled',
  'sticky_sessions_enabled',
  'bridge_enabled',
  'version',
  'product_version',
  'uptime',
  'gateway_status',
  'telemetry_status',
  'gateway_readiness',
  'gateway_listener',
  'active_snapshot_id',
  'active_requests',
  'provider_health_count',
  'healthy_provider_count',
  'unhealthy_provider_count',
  'cooldown_provider_count',
  'telemetry_event_count',
  'telemetry_last_checked_at',
  'telemetry_version',
  'rpc_contract_version',
  'startedAt',
  'started_at',
  'gateway_last_auto_remediation_at',
  'gateway_last_auto_remediation_reason',
] as const

const RUNTIME_FIELD_VALUE_KEY: Record<string, string> = {
  connected: 'ops.connected',
  disconnected: 'ops.disconnected',
  ready: 'ops.ready',
  serving: 'ops.serving',
  healthy: 'ops.healthy',
  cooldown: 'ops.cooldown',
  unhealthy: 'ops.unhealthy',
  unknown: 'ops.unknown',
}

function runtimeFieldLabel(key: string, t: (key: string) => string): string {
  const labelKey = RUNTIME_FIELD_LABEL_KEY[key]
  return labelKey ? t(labelKey) : key.replace(/_/g, ' ')
}

function runtimeFieldValue(value: unknown, t: (key: string) => string): string {
  if (typeof value === 'boolean') return value ? t('ops.yes') : t('ops.no')
  const text = asString(value)
  if (!text) return '-'
  const valueKey = RUNTIME_FIELD_VALUE_KEY[text.toLowerCase()]
  return valueKey ? t(valueKey) : text
}

function runtimeFieldOrder(key: string): number {
  const index = RUNTIME_FIELD_ORDER.indexOf(key as (typeof RUNTIME_FIELD_ORDER)[number])
  return index === -1 ? RUNTIME_FIELD_ORDER.length : index
}

/* ============ Shared Components ============ */

function OpsMetric({ icon, label, value, tone = 'neutral' }: { icon: IconName; label: string; value: string | number; tone?: string }) {
  return (
    <div class={`ops-metric ops-metric-${tone}`}>
      <span class="ops-metric-icon"><Icon name={icon} /></span>
      <span class="ops-metric-label">{label}</span>
      <strong class="ops-metric-value">{value}</strong>
    </div>
  )
}

function OpsEmpty({ title, detail, icon = 'file' }: { title: string; detail: string; icon?: IconName }) {
  return (
    <div class="ops-empty">
      <span class="ops-empty-icon"><Icon name={icon} /></span>
      <strong>{title}</strong>
      <span>{detail}</span>
    </div>
  )
}

function OpsSkeleton() {
  return (
    <div class="ops-skeleton-grid">
      <div class="skeleton ops-skeleton-block" />
      <div class="skeleton ops-skeleton-block" />
      <div class="skeleton ops-skeleton-wide" />
    </div>
  )
}

/* ============ Audit ============ */

function AuditTimeline({ events, t }: { events: AuditEvent[]; t: (key: string) => string }) {
  return (
    <div class="ops-timeline">
      {events.map((event, index) => {
        const ok = event.success !== false
        const details = event.details && Object.keys(event.details).length > 0 ? event.details : null
        return (
          <article key={event.id || `${event.action}-${index}`} class={`ops-event ${ok ? 'success' : 'error'}`}>
            <div class="ops-event-rail" />
            <div class="ops-event-main">
              <div class="ops-event-topline">
                <span class={`ops-event-state ${ok ? 'success' : 'error'}`}>{ok ? t('filterSuccess') : t('filterFailed')}</span>
                <span class="ops-event-time">{formatDate(event.time)}</span>
              </div>
              <div class="ops-event-title">{event.action || 'unknown action'}</div>
              <div class="ops-event-meta">
                <span>{event.resource || 'runtime'}</span>
                <span>{event.actor || 'system'}</span>
                {event.role ? <span>{event.role}</span> : null}
              </div>
              {event.error ? <p class="ops-event-error">{event.error}</p> : null}
              {details ? (
                <details class="ops-event-details">
                  <summary>{t('details')}</summary>
                  <pre>{pretty(details)}</pre>
                </details>
              ) : null}
            </div>
          </article>
        )
      })}
    </div>
  )
}

function AuditView({ payload, busy, t }: { payload: AuditPayload; busy: boolean; t: (key: string) => string }) {
  const [actorFilter, setActorFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState<'all' | 'success' | 'failed'>('all')

  const allEvents = payload.events

  const filteredEvents = useMemo(() => {
    const actor = actorFilter.trim().toLowerCase()
    const action = actionFilter.trim().toLowerCase()
    return allEvents.filter((event) => {
      if (actor && !asString(event.actor).toLowerCase().includes(actor)) return false
      if (action && !asString(event.action).toLowerCase().includes(action)) return false
      if (statusFilter === 'success' && event.success === false) return false
      if (statusFilter === 'failed' && event.success !== false) return false
      return true
    })
  }, [allEvents, actorFilter, actionFilter, statusFilter])

  const events = filteredEvents
  const successes = events.filter((event) => event.success !== false).length
  const failures = events.length - successes

  const actions = useMemo(() => {
    const counts = new Map<string, number>()
    for (const event of events) {
      const action = event.action || 'unknown'
      counts.set(action, (counts.get(action) ?? 0) + 1)
    }
    return Array.from(counts.entries()).sort((left, right) => right[1] - left[1]).slice(0, 7)
  }, [events])
  const maxActionCount = Math.max(1, ...actions.map(([, count]) => count))

  const hasFilters = actorFilter || actionFilter || statusFilter !== 'all'

  if (busy && events.length === 0) return <OpsSkeleton />
  if (allEvents.length === 0) {
    return <OpsEmpty icon="history" title={t('noAuditEvents')} detail={t('noAuditEventsHint')} />
  }

  return (
    <div class="ops-stack">
      <div class="ops-metric-grid">
        <OpsMetric icon="history" label={t('events')} value={payload.count} />
        <OpsMetric icon="shield" label={t('successful')} value={successes} tone="success" />
        <OpsMetric icon="close" label={t('failed')} value={failures} tone={failures > 0 ? 'error' : 'neutral'} />
        <OpsMetric icon="file" label={t('actionTypes')} value={actions.length} />
      </div>

      {/* Filter bar */}
      <div class="ops-filter-bar">
        <label class="ops-field">
          <span>{t('filterActor')}</span>
          <input
            value={actorFilter}
            placeholder={t('filterActor')}
            onInput={(event) => setActorFilter((event.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <label class="ops-field">
          <span>{t('filterAction')}</span>
          <input
            value={actionFilter}
            placeholder={t('filterAction')}
            onInput={(event) => setActionFilter((event.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <label class="ops-field">
          <span>{t('filterStatus')}</span>
          <select value={statusFilter} onChange={(event) => setStatusFilter((event.currentTarget as HTMLSelectElement).value as 'all' | 'success' | 'failed')}>
            <option value="all">{t('filterAll')}</option>
            <option value="success">{t('filterSuccess')}</option>
            <option value="failed">{t('filterFailed')}</option>
          </select>
        </label>
        <div class="ops-filter-actions">
          <button type="button" class="secondary" disabled={!hasFilters} onClick={() => { setActorFilter(''); setActionFilter(''); setStatusFilter('all') }}>
            {t('clearFilters')}
          </button>
        </div>
      </div>

      <div class="ops-audit-layout">
        <section class="ops-card">
          <div class="ops-card-header">
            <h3>{t('actionMix')}</h3>
            <span>{events.length} {t('recent')}</span>
          </div>
          <div class="ops-action-bars">
            {actions.map(([action, count]) => (
              <div class="ops-action-bar" key={action}>
                <div class="ops-action-bar-label">
                  <span>{action}</span>
                  <strong>{count}</strong>
                </div>
                <div class="ops-action-bar-track">
                  <span class="ops-action-bar-fill" style={{ width: `${Math.max(8, (count / maxActionCount) * 100)}%` }} />
                </div>
              </div>
            ))}
          </div>
        </section>

        <section class="ops-card ops-card-timeline">
          <div class="ops-card-header">
            <h3>{t('auditTrail')}</h3>
            <span>{hasFilters ? `${events.length} of ${allEvents.length}` : `${allEvents.length} total`}</span>
          </div>
          {events.length > 0 ? (
            <AuditTimeline events={events} t={t} />
          ) : (
            <OpsEmpty icon="search" title={t('noMatchingEvents')} detail={t('noMatchingEventsHint')} />
          )}
        </section>
      </div>
    </div>
  )
}

/* ============ Diagnostics ============ */

function RuntimeNode({ label, state, detail, icon }: { label: string; state: string; detail?: string; icon: IconName }) {
  const tone = toneFromState(state)
  return (
    <div class={`ops-runtime-node ${tone}`}>
      <span class="ops-runtime-icon"><Icon name={icon} /></span>
      <span class="ops-runtime-label">{label}</span>
      <strong>{state || 'unknown'}</strong>
      {detail ? <small>{detail}</small> : null}
    </div>
  )
}

function KeyValueGrid({ values, t, limit = 12 }: { values: AnyRecord; t: (key: string) => string; limit?: number }) {
  const entries = Object.entries(values)
    .filter(([, value]) => value != null && typeof value !== 'object')
    .sort(([left], [right]) => {
      const leftOrder = runtimeFieldOrder(left)
      const rightOrder = runtimeFieldOrder(right)
      return leftOrder === rightOrder ? left.localeCompare(right) : leftOrder - rightOrder
    })
    .slice(0, limit)
  if (entries.length === 0) return null
  return (
    <div class="ops-kv-grid">
      {entries.map(([key, value]) => (
        <div class="ops-kv" key={key}>
          <span>{runtimeFieldLabel(key, t)}</span>
          <strong>{runtimeFieldValue(value, t)}</strong>
        </div>
      ))}
    </div>
  )
}

function DiagnosticsView({ payload, raw, busy, t }: { payload: DiagnosticsPayload; raw: unknown; busy: boolean; t: (key: string) => string }) {
  const [copied, setCopied] = useState(false)
  const status = payload.status ?? {}
  const runtime = payload.runtime ?? {}
  const gatewayStatus = asString(status.gateway_status) || 'unknown'
  const telemetryStatus = asString(status.telemetry_status) || 'unknown'
  const version = asString(status.version || status.product_version) || asString(runtime.bundle_version) || '-'
  const configPaths = asRecord(runtime.config_paths)

  const rawJson = useMemo(() => pretty(raw), [raw])

  const handleCopy = useCallback(async () => {
    try {
      await copyText(rawJson)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // ignore
    }
  }, [rawJson])

  const handleDownload = useCallback(() => {
    const blob = new Blob([rawJson], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `diagnostics-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`
    a.click()
    URL.revokeObjectURL(url)
  }, [rawJson])

  if (busy && !payload.generated_at) return <OpsSkeleton />

  return (
    <div class="ops-stack">
      <div class="ops-metric-grid">
        <OpsMetric icon="shield" label={t('ops.bundle')} value={version} tone="success" />
        <OpsMetric icon="telemetry" label={t('header.telemetry')} value={telemetryStatus} tone={toneFromState(telemetryStatus)} />
        <OpsMetric icon="config" label={t('header.gateway')} value={gatewayStatus} tone={toneFromState(gatewayStatus)} />
        <OpsMetric icon="key" label={t('ops.redaction')} value={payload.redacted ? t('ops.on') : t('ops.unknown')} tone={payload.redacted ? 'success' : 'warning'} />
      </div>

      <section class="ops-card ops-runtime-card">
        <div class="ops-card-header">
          <h3>{t('runtimeTopology')}</h3>
          <span>{formatDate(payload.generated_at)}</span>
        </div>
        <div class="ops-runtime-flow">
          <RuntimeNode label="Control" state="serving" detail={asString(runtime.listen)} icon="shield" />
          <span class="ops-runtime-link"><Icon name="arrowRight" /></span>
          <RuntimeNode label="Gateway" state={gatewayStatus} detail={asString(runtime.gateway_socket)} icon="config" />
          <span class="ops-runtime-link"><Icon name="arrowRight" /></span>
          <RuntimeNode label="Telemetry" state={telemetryStatus} detail={asString(runtime.telemetry_socket)} icon="telemetry" />
        </div>
      </section>

      <div class="ops-diagnostics-layout">
        <section class="ops-card">
          <div class="ops-card-header">
            <h3>{t('runtimeStatus')}</h3>
            <span>{t('details')}</span>
          </div>
          <KeyValueGrid values={status} t={t} limit={24} />
        </section>

        <section class="ops-card">
          <div class="ops-card-header">
            <h3>{t('runtimePaths')}</h3>
            <span>local</span>
          </div>
          <KeyValueGrid values={runtime} t={t} />
          {configPaths ? (
            <div class="ops-path-list">
              {Object.entries(configPaths).map(([key, value]) => (
                <div class="ops-path-row" key={key}>
                  <span>{key}</span>
                  <code>{asString(value)}</code>
                </div>
              ))}
            </div>
          ) : null}
        </section>

        <section class="ops-card">
          <div class="ops-card-header">
            <h3>{t('auditTail')}</h3>
            <span>{payload.audit_tail.length} events</span>
          </div>
          {payload.audit_tail.length > 0 ? (
            <AuditTimeline events={payload.audit_tail.slice(0, 6)} t={t} />
          ) : (
            <OpsEmpty title={t('noAuditTail')} detail={t('noAuditTailHint')} />
          )}
        </section>
      </div>

      <details class="ops-json-details">
        <summary>
          {t('redactedDiagnostics')}
          <span class="ops-copy-actions">
            <button type="button" class="secondary" onClick={(e: Event) => { e.preventDefault(); void handleCopy() }}>
              {copied ? t('copied') : t('copy')}
            </button>
            <button type="button" class="secondary" onClick={(e: Event) => { e.preventDefault(); handleDownload() }}>
              {t('download')}
            </button>
          </span>
        </summary>
        <pre>{rawJson}</pre>
      </details>
    </div>
  )
}

/* ============ Probe ============ */

function ProbeView({
  canWrite,
  busy,
  provider,
  model,
  protocol,
  prompt,
  timeoutMs,
  result,
  onProvider,
  onModel,
  onProtocol,
  onPrompt,
  onTimeout,
  onRun,
  t,
}: {
  canWrite: boolean
  busy: boolean
  provider: string
  model: string
  protocol: string
  prompt: string
  timeoutMs: string
  result: ProbeResult | null
  onProvider: (value: string) => void
  onModel: (value: string) => void
  onProtocol: (value: string) => void
  onPrompt: (value: string) => void
  onTimeout: (value: string) => void
  onRun: (kind: 'provider' | 'model') => void
  t: (key: string) => string
}) {
  const healthy = result?.healthy === true
  const failed = result && !healthy
  const headers = result?.headers ? Object.entries(result.headers).slice(0, 6) : []

  return (
    <div class="ops-probe-layout">
      <section class="ops-card ops-probe-form">
        <div class="ops-card-header">
          <h3>{t('probeTarget')}</h3>
          <span>{canWrite ? t('ops.adminWrite') : t('ops.viewerReadOnly')}</span>
        </div>
        <label class="ops-field">
          <span>{t('ops.provider')}</span>
          <input value={provider} placeholder="provider id" onInput={(event) => onProvider((event.currentTarget as HTMLInputElement).value)} />
        </label>
        <label class="ops-field">
          <span>{t('ops.model')}</span>
          <input value={model} placeholder="public model" onInput={(event) => onModel((event.currentTarget as HTMLInputElement).value)} />
        </label>
        <div class="ops-field-row">
          <label class="ops-field">
            <span>{t('ops.protocol')}</span>
            <select value={protocol} onChange={(event) => onProtocol((event.currentTarget as HTMLSelectElement).value)}>
              <option value="">{t('ops.protocolAuto')}</option>
              <option value="openai_chat_completions">{t('ops.protocolOpenAI')}</option>
              <option value="anthropic_messages">{t('ops.protocolAnthropic')}</option>
            </select>
          </label>
          <label class="ops-field">
            <span>{t('ops.timeout')}</span>
            <input value={timeoutMs} inputMode="numeric" placeholder="30000" onInput={(event) => onTimeout((event.currentTarget as HTMLInputElement).value)} />
          </label>
        </div>
        <label class="ops-field">
          <span>{t('ops.prompt')}</span>
          <textarea value={prompt} rows={4} placeholder="Reply with exactly ok." onInput={(event) => onPrompt((event.currentTarget as HTMLTextAreaElement).value)} />
        </label>
        <div class="ops-probe-actions">
          <button type="button" disabled={busy || !canWrite || !provider.trim()} onClick={() => onRun('provider')}>
            {t('ops.probeProvider')}
          </button>
          <button type="button" class="secondary" disabled={busy || !canWrite || !model.trim()} onClick={() => onRun('model')}>
            {t('ops.probeModel')}
          </button>
        </div>
        {!canWrite ? (
          <p class="ops-alert warning">{t('ops.readOnlyWarning')}</p>
        ) : null}
      </section>

      <section class={`ops-card ops-probe-result ${healthy ? 'success' : failed ? 'error' : ''}`}>
        <div class="ops-card-header">
          <h3>{t('ops.probeResult')}</h3>
          {result?.probed_at ? <span>{formatDate(result.probed_at)}</span> : <span>{t('ops.waiting')}</span>}
        </div>
        {!result && !busy ? (
          <div class="ops-probe-placeholder">
            <div class="ops-probe-map">
              <span><Icon name="search" /> {t('ops.target')}</span>
              <Icon name="arrowRight" />
              <span><Icon name="shield" /> RPC</span>
              <Icon name="arrowRight" />
              <span><Icon name="telemetry" /> {t('ops.result')}</span>
            </div>
            <OpsEmpty icon="search" title={t('ops.noProbeResult')} detail={t('ops.noProbeResultHint')} />
          </div>
        ) : null}
        {busy ? <OpsSkeleton /> : null}
        {result && !busy ? (
          <div class="ops-result-content">
            <div class={`ops-result-ring ${healthy ? 'success' : 'error'}`}>
              <strong>{healthy ? 'OK' : 'FAIL'}</strong>
              <span>{result.status_code || '-'}</span>
            </div>
            <div class="ops-result-metrics">
              <OpsMetric icon="config" label={t('ops.provider')} value={result.provider_id || '-'} />
              <OpsMetric icon="file" label={t('ops.model')} value={result.model || '-'} />
              <OpsMetric icon="chart" label={t('telemetry.latency')} value={typeof result.latency_ms === 'number' ? `${result.latency_ms} ms` : '-'} />
              <OpsMetric icon="shield" label={t('ops.diagnostic')} value={result.diagnostic ? t('ops.yes') : t('ops.no')} tone={result.diagnostic ? 'success' : 'neutral'} />
            </div>
            {result.error ? <p class="ops-alert error">{result.error}</p> : null}
            {result.response_excerpt ? (
              <div class="ops-response-excerpt">
                <span>{t('ops.responseExcerpt')}</span>
                <pre>{result.response_excerpt}</pre>
              </div>
            ) : null}
            {headers.length > 0 ? (
              <div class="ops-header-grid">
                {headers.map(([key, value]) => (
                  <div class="ops-kv" key={key}>
                    <span>{key}</span>
                    <strong>{value}</strong>
                  </div>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}
      </section>
    </div>
  )
}

/* ============ Main ============ */

export function OpsTab({ mode, canWrite, embedded = false, onUnauthorized }: OpsTabProps) {
  const { t } = useI18n()
  const opsT = useCallback((key: string) => t(key.includes('.') ? key : `ops.${key}`), [t])
  const [data, setData] = useState<unknown>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [provider, setProvider] = useState('')
  const [model, setModel] = useState('')
  const [protocol, setProtocol] = useState('')
  const [prompt, setPrompt] = useState('')
  const [timeoutMs, setTimeoutMs] = useState('30000')

  const load = useCallback(async () => {
    if (mode === 'probe') return
    setBusy(true)
    setError('')
    try {
      const endpoint = mode === 'audit' ? '/api/admin/audit?limit=100' : '/api/admin/diagnostics'
      setData(await fetchJSON(endpoint, { onUnauthorized }))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }, [mode, onUnauthorized])

  useEffect(() => {
    void load()
  }, [load])

  const runProbe = useCallback(async (kind: 'provider' | 'model') => {
    setBusy(true)
    setError('')
    try {
      const timeout = Number(timeoutMs)
      setData(await fetchJSON(`/api/admin/probe/${kind}`, {
        method: 'POST',
        body: JSON.stringify({
          provider_id: provider.trim(),
          model: model.trim(),
          protocol: protocol.trim(),
          prompt: prompt.trim(),
          timeout_ms: Number.isFinite(timeout) ? timeout : undefined,
        }),
        onUnauthorized,
      }))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }, [model, onUnauthorized, prompt, protocol, provider, timeoutMs])

  const auditPayload = useMemo(() => normalizeAuditPayload(data), [data])
  const probeResult = useMemo(() => normalizeProbeResult(data), [data])
  const diagnosticsPayload = useMemo(() => normalizeDiagnostics(data), [data])

  return (
    <section class={`ops-surface ops-surface-${mode}`}>
      {!embedded ? (
        <div class="ops-toolbar">
          <div>
            <span class="workspace-kicker">
              <Icon name={mode === 'audit' ? 'history' : mode === 'probe' ? 'search' : 'file'} class="workspace-kicker-icon" />
              {titleForMode(mode, t)}
            </span>
            <h2>{titleForMode(mode, t)}</h2>
            <p>{subtitleForMode(mode, t)}</p>
          </div>
          {mode !== 'probe' ? (
            <button type="button" class="secondary" onClick={() => { void load() }} disabled={busy}>
              <Icon name="refresh" />
              {t('ops.refresh')}
            </button>
          ) : null}
        </div>
      ) : null}

      {error ? <p class="ops-alert error">{error}</p> : null}

      {mode === 'audit' ? <AuditView payload={auditPayload} busy={busy} t={opsT} /> : null}
      {mode === 'diagnostics' ? <DiagnosticsView payload={diagnosticsPayload} raw={data} busy={busy} t={opsT} /> : null}
      {mode === 'probe' ? (
        <ProbeView
          canWrite={canWrite}
          busy={busy}
          provider={provider}
          model={model}
          protocol={protocol}
          prompt={prompt}
          timeoutMs={timeoutMs}
          result={probeResult}
          onProvider={setProvider}
          onModel={setModel}
          onProtocol={setProtocol}
          onPrompt={setPrompt}
          onTimeout={setTimeoutMs}
          onRun={(kind) => { void runProbe(kind) }}
          t={opsT}
        />
      ) : null}
    </section>
  )
}
