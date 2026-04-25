import type {
  AdminSessionView,
  AnyRecord,
  BenchmarkModel,
  BenchmarkResponse,
  ControlStatusView,
  ControlConfigView,
  ConfigHistoryResponse,
  ConfigVersionSummary,
  DataResponse,
  RequestEntry,
  ErrorEntry,
  PricingFXStatus,
  PricingSourceStatus,
  PricingStatusView,
  ProviderHealthView,
  TimeSeriesBucket,
  TimeSeriesResponse,
} from '../types'

export const FULL_WINDOW_HOURS = 24 * 365
const HISTORY_BUCKET_MINUTES = 7 * 24 * 60
const READINESS_LABELS: Record<number, string> = {
  0: 'unknown',
  1: 'starting',
  2: 'ready',
  3: 'draining',
  4: 'stopped',
}

interface TelemetryEventView {
  request_id: string
  timestamp: string
  path: string
  requested_model: string
  effective_model: string
  provider: string
  route_mode: string
  status_code: number
  latency_ms: number
  attempts: number
  input_tokens: number
  cached_prompt_tokens: number
  output_tokens: number
  pricing_status: string
  total_cost_usd: number
  synthetic_kind: string
  benchmark_run_id: string
  benchmark_target_id: string
  benchmark_case_id: string
  error: string
}

function asRecord(value: unknown): AnyRecord | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as AnyRecord : null
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function asNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function stringList(value: unknown): string[] {
  return asArray(value)
    .map((item) => asString(item))
    .filter((item): item is string => typeof item === 'string' && item.trim().length > 0)
}

function metricNumber(source: AnyRecord | null, upperKey: string, lowerKey: string): number {
  return asNumber(source?.[upperKey] ?? source?.[lowerKey]) ?? 0
}

function hasFutureTimestamp(value: string | undefined): boolean {
  if (!value) return false
  const timestamp = Date.parse(value)
  return Number.isFinite(timestamp) && timestamp > Date.now()
}

function providerHealthStatusSortValue(status: ProviderHealthView['status']): number {
  switch (status) {
    case 'unhealthy':
      return 0
    case 'cooldown':
      return 1
    default:
      return 2
  }
}

function normalizeProviderHealthEntry(name: string, value: unknown): ProviderHealthView | null {
  const normalizedName = name.trim()
  if (!normalizedName) return null

  if (typeof value === 'boolean') {
    return {
      name: normalizedName,
      healthy: value,
      status: value ? 'healthy' : 'unhealthy',
      consecutive_failures: 0,
    }
  }

  const record = asRecord(value)
  if (!record) return null

  const healthy = asBoolean(record.Healthy ?? record.healthy) ?? false
  const cooldownUntil = asString(record.CooldownUntil ?? record.cooldown_until)
  const status: ProviderHealthView['status'] = healthy
    ? 'healthy'
    : hasFutureTimestamp(cooldownUntil) ? 'cooldown' : 'unhealthy'

  return {
    name: asString(record.Name ?? record.name) ?? normalizedName,
    healthy,
    status,
    last_check: asString(record.LastCheck ?? record.last_check),
    last_success: asString(record.LastSuccess ?? record.last_success),
    consecutive_failures: metricNumber(record, 'ConsecutiveFailures', 'consecutive_failures'),
    cooldown_until: cooldownUntil,
    latency_ms: asNumber(record.LatencyMs ?? record.latency_ms),
  }
}

function normalizePricingSourceStatus(value: unknown): PricingSourceStatus | null {
  const record = asRecord(value)
  const id = asString(record?.id ?? record?.ID)
  const vendor = asString(record?.vendor ?? record?.Vendor)
  if (!id || !vendor) return null

  return {
    id,
    vendor,
    url: asString(record?.url ?? record?.URL),
    enabled: asBoolean(record?.enabled ?? record?.Enabled) ?? false,
    status: asString(record?.status ?? record?.Status),
    updated_at: asString(record?.updated_at ?? record?.UpdatedAt),
    last_attempt_at: asString(record?.last_attempt_at ?? record?.LastAttemptAt),
    last_error: asString(record?.last_error ?? record?.LastError),
    model_count: asNumber(record?.model_count ?? record?.ModelCount),
  }
}

function normalizePricingFXStatus(value: unknown): PricingFXStatus | undefined {
  const record = asRecord(value)
  if (!record) return undefined
  return {
    enabled: asBoolean(record?.enabled ?? record?.Enabled) ?? false,
    source_url: asString(record?.source_url ?? record?.SourceURL),
    base_currency: asString(record?.base_currency ?? record?.BaseCurrency),
    updated_at: asString(record?.updated_at ?? record?.UpdatedAt),
    last_attempt_at: asString(record?.last_attempt_at ?? record?.LastAttemptAt),
    last_error: asString(record?.last_error ?? record?.LastError),
  }
}

function normalizePricingStatusView(value: unknown): PricingStatusView | undefined {
  const record = asRecord(value)
  if (!record) return undefined

  return {
    source_url: asString(record?.source_url ?? record?.SourceURL),
    updated_at: asString(record?.updated_at ?? record?.UpdatedAt),
    last_attempt_at: asString(record?.last_attempt_at ?? record?.LastAttemptAt),
    last_error: asString(record?.last_error ?? record?.LastError),
    catalog_size: asNumber(record?.catalog_size ?? record?.CatalogSize),
    sources: asArray(record?.sources ?? record?.Sources)
      .map((item) => normalizePricingSourceStatus(item))
      .filter((item): item is PricingSourceStatus => item !== null),
    fx: normalizePricingFXStatus(record?.fx ?? record?.FX),
  }
}

function timeRangeHours(value: string | number): string {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return String(value)
  }
  if (value === 'all') {
    return String(FULL_WINDOW_HOURS)
  }
  return String(value)
}

function normalizeTimeBucket(value: unknown): TimeSeriesBucket | null {
  const record = asRecord(value)
  const bucket = asString(record?.Bucket ?? record?.bucket)
  if (!bucket) return null

  return {
    Bucket: bucket,
    Requests: metricNumber(record, 'Requests', 'requests'),
    Successes: metricNumber(record, 'Successes', 'successes'),
    Failures: metricNumber(record, 'Failures', 'failures'),
    AvgLatencyMs: metricNumber(record, 'AvgLatencyMs', 'avg_latency_ms'),
    InputTokens: metricNumber(record, 'InputTokens', 'input_tokens'),
    OutputTokens: metricNumber(record, 'OutputTokens', 'output_tokens'),
  }
}

function normalizeTelemetryEvent(value: unknown): TelemetryEventView | null {
  const record = asRecord(value)
  const timestamp = asString(record?.Timestamp ?? record?.timestamp)
  const path = asString(record?.Path ?? record?.path)
  if (!timestamp || !path) return null

  return {
    request_id: asString(record?.RequestID ?? record?.request_id) ?? '',
    timestamp,
    path,
    requested_model: asString(record?.RequestedModel ?? record?.requested_model) ?? '',
    effective_model: asString(record?.EffectiveModel ?? record?.effective_model) ?? '',
    provider: asString(record?.Provider ?? record?.provider) ?? '',
    route_mode: asString(record?.RouteMode ?? record?.route_mode) ?? '',
    status_code: metricNumber(record, 'StatusCode', 'status_code'),
    latency_ms: metricNumber(record, 'LatencyMs', 'latency_ms'),
    attempts: metricNumber(record, 'Attempts', 'attempts'),
    input_tokens: metricNumber(record, 'InputTokens', 'input_tokens'),
    cached_prompt_tokens: metricNumber(record, 'CachedPromptTokens', 'cached_prompt_tokens'),
    output_tokens: metricNumber(record, 'OutputTokens', 'output_tokens'),
    pricing_status: asString(record?.PricingStatus ?? record?.pricing_status) ?? '',
    total_cost_usd: asNumber(record?.PricingTotalCostUSD ?? record?.pricing_total_cost_usd) ?? 0,
    synthetic_kind: asString(record?.SyntheticKind ?? record?.synthetic_kind) ?? '',
    benchmark_run_id: asString(record?.BenchmarkRunID ?? record?.benchmark_run_id) ?? '',
    benchmark_target_id: asString(record?.BenchmarkTargetID ?? record?.benchmark_target_id) ?? '',
    benchmark_case_id: asString(record?.BenchmarkCaseID ?? record?.benchmark_case_id) ?? '',
    error: asString(record?.Error ?? record?.error) ?? '',
  }
}

function normalizeRevision(value: unknown): ConfigVersionSummary | null {
  const record = asRecord(value)
  const id = asString(record?.revision_id ?? record?.RevisionID ?? record?.id)
  if (!id) return null

  return {
    id,
    filename: id,
    created_at: asString(record?.created_at ?? record?.CreatedAt) ?? '',
    size: 0,
    created_by: asString(record?.created_by ?? record?.CreatedBy),
    description: asString(record?.description ?? record?.Description),
    is_active: asBoolean(record?.is_active ?? record?.IsActive),
  }
}

function normalizeBenchmarkModel(value: unknown): BenchmarkModel | null {
  const record = asRecord(value)
  const model = asString(record?.model ?? record?.Model)
  if (!model) return null

  const successRateRaw = asNumber(record?.success_rate ?? record?.SuccessRate) ?? 0
  const successRate = successRateRaw > 1 ? successRateRaw / 100 : successRateRaw

  return {
    model,
    requests: metricNumber(record, 'Requests', 'requests'),
    successes: metricNumber(record, 'Successes', 'successes'),
    failures: metricNumber(record, 'Failures', 'failures'),
    input_tokens: metricNumber(record, 'InputTokens', 'input_tokens'),
    output_tokens: metricNumber(record, 'OutputTokens', 'output_tokens'),
    avg_latency_ms: metricNumber(record, 'AvgLatencyMs', 'avg_latency_ms'),
    p50_latency_ms: metricNumber(record, 'P50LatencyMs', 'p50_latency_ms'),
    p95_latency_ms: metricNumber(record, 'P95LatencyMs', 'p95_latency_ms'),
    p99_latency_ms: metricNumber(record, 'P99LatencyMs', 'p99_latency_ms'),
    max_latency_ms: metricNumber(record, 'MaxLatencyMs', 'max_latency_ms'),
    success_rate: successRate,
    estimated_cost_usd: asNumber(record?.EstimatedCostUSD ?? record?.estimated_cost_usd) ?? 0,
    exact_cost_usd: asNumber(record?.ExactCostUSD ?? record?.exact_cost_usd),
    estimated_legacy_cost_usd: asNumber(record?.EstimatedLegacyCostUSD ?? record?.estimated_legacy_cost_usd),
  }
}

export function benchmarkURL(hours: number, models: string[]): string {
  const params = new URLSearchParams()
  params.set('hours', String(hours))

  const normalizedModels = [...models]
    .map((model) => model.trim())
    .filter(Boolean)
    .sort()
  if (normalizedModels.length > 0) {
    params.set('models', normalizedModels.join(','))
  }

  return `/api/admin/benchmark?${params.toString()}`
}

export function telemetryURL(hours: string): string {
  return `/api/admin/telemetry?hours=${encodeURIComponent(timeRangeHours(hours))}&limit=500`
}

export function logsURL(hours: string, limit = 500, offset = 0): string {
  return `/api/admin/telemetry?hours=${encodeURIComponent(timeRangeHours(hours))}&limit=${limit}&offset=${offset}`
}

export function telemetryTimeseriesURL(hours: string, bucket = ''): string {
  const resolvedHours = timeRangeHours(hours)
  const resolvedBucket = bucket.trim() !== ''
    ? bucket.trim()
    : String(hours === 'all' ? HISTORY_BUCKET_MINUTES : hours === '720' ? 1440 : 60)
  return `/api/admin/timeseries?hours=${encodeURIComponent(resolvedHours)}&bucket=${encodeURIComponent(resolvedBucket)}`
}

export function historyTimeseriesURL(): string {
  return `/api/admin/timeseries?hours=${FULL_WINDOW_HOURS}&bucket=${HISTORY_BUCKET_MINUTES}`
}

export function normalizeWindowHoursParam(value: string | number): string {
  return timeRangeHours(value)
}

export function normalizeControlStatus(payload: unknown): ControlStatusView | null {
  const record = asRecord(payload)
  if (!record) return null

  const gateway = asRecord(record.gateway)
  const providerHealth = asRecord(gateway?.ProviderHealth ?? gateway?.provider_health)
  const providerHealthItems = providerHealth
    ? Object.entries(providerHealth)
      .map(([name, value]) => normalizeProviderHealthEntry(name, value))
      .filter((item): item is ProviderHealthView => item !== null)
      .sort((left, right) => {
        const statusDiff = providerHealthStatusSortValue(left.status) - providerHealthStatusSortValue(right.status)
        if (statusDiff !== 0) return statusDiff
        return left.name.localeCompare(right.name)
      })
    : []
  const readinessRaw = gateway?.Readiness ?? gateway?.readiness
  const readiness = typeof readinessRaw === 'number'
    ? READINESS_LABELS[readinessRaw] ?? 'unknown'
    : asString(readinessRaw) ?? 'unknown'
  const healthyProviderCount = providerHealthItems.filter((item) => item.healthy).length
  const unhealthyProviderCount = providerHealthItems.filter((item) => !item.healthy).length
  const cooldownProviderCount = providerHealthItems.filter((item) => item.status === 'cooldown').length

  return {
    version: asString(record.version),
    started_at: asString(record.startedAt ?? record.started_at),
    uptime: asString(record.uptime),
    gateway_status: asString(record.gateway_status),
    gateway_error: asString(record.gateway_error),
    telemetry_status: asString(record.telemetry_status),
    telemetry_error: asString(record.telemetry_error),
    telemetry_version: asString(record.telemetry_version),
    telemetry_event_count: asNumber(record.telemetry_event_count),
    telemetry_last_checked_at: asString(record.telemetry_last_checked_at),
    gateway_readiness: readiness,
    gateway_listener: asString(gateway?.Listener ?? gateway?.listener),
    active_snapshot_id: asString(gateway?.ActiveSnapshotID ?? gateway?.active_snapshot_id),
    active_requests: asNumber(gateway?.ActiveRequests ?? gateway?.active_requests),
    provider_health_count: providerHealthItems.length,
    healthy_provider_count: healthyProviderCount,
    unhealthy_provider_count: unhealthyProviderCount,
    cooldown_provider_count: cooldownProviderCount,
    provider_health: providerHealthItems,
    pricing: normalizePricingStatusView(record.pricing ?? record.Pricing),
  }
}

export function normalizeAdminSession(payload: unknown): AdminSessionView | null {
  const record = asRecord(payload)
  if (!record) return null

  return {
    enabled: asBoolean(record.enabled) ?? true,
    authenticated: asBoolean(record.authenticated) ?? false,
    name: asString(record.name),
    role: asString(record.role),
  }
}

export function normalizeControlConfigResponse(payload: unknown): ControlConfigView | null {
  const record = asRecord(payload)
  if (!record) return null

  const revision = normalizeRevision(record.revision ?? record.Revision ?? record.current_revision)
  const policyRecord = asRecord(record.policy ?? record.Policy)

  return {
    revision,
    policy: {
      publish_history_limit: metricNumber(policyRecord, 'PublishHistoryLimit', 'publish_history_limit'),
    },
    raw_yaml: asString(record.raw_yaml ?? record.RawYAML),
    config: asRecord(record.config ?? record.Config) ?? undefined,
  }
}

export function normalizeOverviewResponse(payload: unknown, statusPayload?: unknown): AnyRecord | null {
  const record = asRecord(payload)
  if (!record) return null

  const windows = asRecord(record.Windows ?? record.windows)
  const runtime = asRecord(record.Runtime ?? record.runtime)
  const status = normalizeControlStatus(statusPayload)
  const normalized: AnyRecord = {}

  for (const key of ['last_1m', 'last_5m', 'last_1h', 'last_24h']) {
    const metrics = asRecord(windows?.[key] ?? record[key])
    normalized[key] = {
      requests: metricNumber(metrics, 'Requests', 'requests'),
      successes: metricNumber(metrics, 'Successes', 'successes'),
      failures: metricNumber(metrics, 'Failures', 'failures'),
      avg_latency_ms: metricNumber(metrics, 'AvgLatencyMs', 'avg_latency_ms'),
      input_tokens: metricNumber(metrics, 'InputTokens', 'input_tokens'),
      cached_prompt_tokens: metricNumber(metrics, 'CachedPromptTokens', 'cached_prompt_tokens'),
      output_tokens: metricNumber(metrics, 'OutputTokens', 'output_tokens'),
    }
  }

  normalized.runtime = {
    provider_count: metricNumber(runtime, 'ProviderCount', 'provider_count'),
    enabled_provider_count: metricNumber(runtime, 'EnabledProviderCount', 'enabled_provider_count'),
    router_strategy: asString(runtime?.RouterStrategy ?? runtime?.router_strategy) ?? '-',
    health_enabled: asBoolean(runtime?.HealthEnabled ?? runtime?.health_enabled) ?? false,
    sticky_sessions_enabled: asBoolean(runtime?.StickySessionsEnabled ?? runtime?.sticky_sessions_enabled) ?? false,
    bridge_enabled: asBoolean(runtime?.BridgeEnabled ?? runtime?.bridge_enabled) ?? false,
    version: status?.version ?? '-',
    uptime: status?.uptime ?? '-',
    gateway_status: status?.gateway_status ?? 'unknown',
    telemetry_status: status?.telemetry_status ?? 'unknown',
    gateway_readiness: status?.gateway_readiness ?? 'unknown',
    gateway_listener: status?.gateway_listener ?? '-',
    active_snapshot_id: status?.active_snapshot_id ?? '-',
    active_requests: status?.active_requests ?? 0,
    provider_health_count: status?.provider_health_count ?? 0,
    healthy_provider_count: status?.healthy_provider_count ?? 0,
    unhealthy_provider_count: status?.unhealthy_provider_count ?? 0,
    cooldown_provider_count: status?.cooldown_provider_count ?? 0,
  }

  normalized.provider_health = status?.provider_health ?? []
  normalized.available_models = stringList(record.AvailableModels ?? record.available_models)
  return normalized
}

export function normalizeTelemetryResponse(payload: unknown): DataResponse | null {
  const record = asRecord(payload)
  if (!record) return null

  const events = asArray(record.Events ?? record.events)
    .map((item) => normalizeTelemetryEvent(item))
    .filter((item): item is TelemetryEventView => item !== null)

  const requests: RequestEntry[] = []
  const errors: ErrorEntry[] = []
  const modelMap = new Map<string, { requests: number; successes: number; failures: number; input: number; output: number; latency: number }>()
  const upstreamMap = new Map<string, { requests: number; successes: number; failures: number; input: number; output: number; latency: number }>()

  let successes = 0
  let failures = 0
  let totalLatency = 0
  let latencySamples = 0

  for (const event of events) {
    const model = event.effective_model || event.requested_model || 'unknown'
    const upstream = event.provider || 'unknown'
    const isSuccess = event.status_code > 0 && event.status_code < 400

    requests.push({
      RequestID: event.request_id,
      Timestamp: event.timestamp,
      Path: event.path,
      RequestedModel: event.requested_model,
      EffectiveModel: event.effective_model,
      StatusCode: event.status_code,
      Upstream: upstream,
      Model: model,
      RouteMode: event.route_mode,
      Attempts: event.attempts,
      LatencyMs: event.latency_ms,
      InputTokens: event.input_tokens,
      CachedPromptTokens: event.cached_prompt_tokens,
      OutputTokens: event.output_tokens,
      cached: event.cached_prompt_tokens > 0,
      PricingStatus: event.pricing_status,
      TotalCostUSD: event.total_cost_usd,
      SyntheticKind: event.synthetic_kind,
      BenchmarkRunID: event.benchmark_run_id,
      BenchmarkTargetID: event.benchmark_target_id,
      BenchmarkCaseID: event.benchmark_case_id,
      Error: event.error,
    })

    if (event.error || event.status_code >= 400) {
      errors.push({
        Timestamp: event.timestamp,
        Upstream: upstream,
        Model: model,
        StatusCode: event.status_code,
        Message: event.error || `HTTP ${event.status_code}`,
        count: 1,
      })
    }

    const updateAgg = (
      target: Map<string, { requests: number; successes: number; failures: number; input: number; output: number; latency: number }>,
      key: string
    ) => {
      const current = target.get(key) ?? { requests: 0, successes: 0, failures: 0, input: 0, output: 0, latency: 0 }
      current.requests += 1
      current.successes += isSuccess ? 1 : 0
      current.failures += isSuccess ? 0 : 1
      current.input += event.input_tokens
      current.output += event.output_tokens
      current.latency += event.latency_ms
      target.set(key, current)
    }

    updateAgg(modelMap, model)
    updateAgg(upstreamMap, upstream)

    if (isSuccess) {
      successes += 1
    } else {
      failures += 1
    }
    if (event.latency_ms > 0) {
      totalLatency += event.latency_ms
      latencySamples += 1
    }
  }

  const toDistribution = (
    source: Map<string, { requests: number; successes: number; failures: number; input: number; output: number; latency: number }>
  ) => Array.from(source.entries()).map(([value, agg]) => ({
    value,
    requests: agg.requests,
    successes: agg.successes,
    failures: agg.failures,
    input_tokens: agg.input,
    output_tokens: agg.output,
    avg_latency_ms: agg.requests > 0 ? agg.latency / agg.requests : 0,
  }))

  return {
    window_hours: asNumber(record.WindowHours ?? record.window_hours),
    summary: {
      requests: events.length,
      successes,
      failures,
      avg_latency_ms: latencySamples > 0 ? totalLatency / latencySamples : 0,
    },
    models: toDistribution(modelMap),
    upstreams: toDistribution(upstreamMap),
    requests,
    errors,
    pricing_economics: (asRecord(record.pricing_economics ?? record.PricingEconomics ?? record.Pricing) ?? undefined) as DataResponse['pricing_economics'],
  }
}

export function normalizeTimeSeriesResponse(payload: unknown): TimeSeriesResponse | null {
  const record = asRecord(payload)
  if (!record) return null

  const points = asArray(record.points ?? record.Points ?? record.Buckets ?? record.buckets)
    .map((item) => normalizeTimeBucket(item))
    .filter((item): item is TimeSeriesBucket => item !== null)
    .sort((left, right) => Date.parse(left.Bucket) - Date.parse(right.Bucket))

  return {
    window_hours: asNumber(record.window_hours ?? record.WindowHours) ?? 24,
    bucket_minutes: asNumber(record.bucket_minutes ?? record.BucketMinutes) ?? 5,
    points,
  }
}

export function normalizeBenchmarkResponse(payload: unknown): BenchmarkResponse | null {
  const record = asRecord(payload)
  if (!record) return null

  const benchmarks = asArray(record.benchmarks ?? record.Benchmarks)
    .map((item) => normalizeBenchmarkModel(item))
    .filter((item): item is BenchmarkModel => item !== null)

  const windowHours = asNumber(record.window_hours ?? record.WindowHours) ?? 24

  return {
    window_hours: windowHours,
    hours: windowHours,
    benchmarks,
  }
}

export function normalizeConfigHistoryResponse(payload: unknown): ConfigHistoryResponse {
  const versions = Array.isArray(payload)
    ? payload
    : asArray(asRecord(payload)?.versions ?? asRecord(payload)?.items)

  return {
    versions: versions
      .map((item) => normalizeRevision(item))
      .filter((item): item is ConfigVersionSummary => item !== null),
  }
}
