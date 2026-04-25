// Shared TypeScript types for the controld admin UI.

export type ControlTabKey = 'overview' | 'telemetry' | 'logs' | 'pricing' | 'benchmark' | 'config'

export type ConfigSubTab = 'current' | 'editor' | 'visual' | 'history'
export type AdminRole = 'admin' | 'viewer'

export type AnyRecord = Record<string, unknown>

export interface AdminSession {
  enabled: boolean
  authenticated: boolean
  name?: string
  role?: AdminRole | string
}

export interface DonutEntry {
  label: string
  value: number
  color: string
}

export interface TimeSeriesBucket {
  Bucket: string
  Requests: number
  Successes: number
  Failures: number
  AvgLatencyMs: number
  InputTokens: number
  OutputTokens: number
}

export interface TimeSeriesResponse {
  window_hours: number
  bucket_minutes: number
  points: TimeSeriesBucket[]
}

export interface RequestEntry {
  request_id?: string
  RequestID?: string
  time?: string
  Timestamp?: string
  method?: string
  path?: string
  Path?: string
  requested_model?: string
  RequestedModel?: string
  effective_model?: string
  EffectiveModel?: string
  status?: number
  StatusCode?: number
  upstream?: string
  Upstream?: string
  model?: string
  Model?: string
  route_mode?: string
  RouteMode?: string
  attempts?: number
  Attempts?: number
  latency_ms?: number
  LatencyMs?: number
  input_tokens?: number
  InputTokens?: number
  output_tokens?: number
  OutputTokens?: number
  cached?: boolean
  CachedPromptTokens?: number
  pricing_status?: string
  PricingStatus?: string
  total_cost_usd?: number
  TotalCostUSD?: number
  synthetic_kind?: string
  SyntheticKind?: string
  benchmark_run_id?: string
  BenchmarkRunID?: string
  benchmark_target_id?: string
  BenchmarkTargetID?: string
  benchmark_case_id?: string
  BenchmarkCaseID?: string
  error?: string
  Error?: string
}

export interface ErrorEntry {
  time?: string
  Timestamp?: string
  upstream?: string
  Upstream?: string
  model?: string
  Model?: string
  status?: number
  StatusCode?: number
  message?: string
  Message?: string
  count?: number
  Attempts?: number
}

export interface PricingCost {
  currency?: string
  prompt?: number
  completion?: number
  total?: number
  prompt_usd?: number
  completion_usd?: number
  total_usd?: number
}

export interface PricingCurrencySummary {
  currency: string
  prompt: number
  completion: number
  total: number
  cache_savings: number
  priced_models: number
}

export interface PricingSummary {
  currency: string
  prompt?: number
  completion?: number
  total?: number
  prompt_usd?: number
  completion_usd?: number
  total_usd?: number
  cached_prompt_tokens: number
  cache_savings?: number
  cache_savings_usd?: number
  priced_models: number
  unpriced_models: number
  exact_total_usd?: number
  estimated_total_usd?: number
  exact_requests?: number
  estimated_requests?: number
  exact_models?: number
  estimated_models?: number
  totals_by_currency?: PricingCurrencySummary[]
}

export interface PricingModelSummary {
  display_model: string
  requested_model?: string
  effective_model?: string
  upstream?: string
  provider?: string
  pricing_model?: string
  pricing_status?: string
  pricing_source_id?: string
  usage: {
    prompt_tokens: number
    cached_prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
  pricing?: {
    currency?: string
    input_per_1m?: number
    cached_input_per_1m?: number
    output_per_1m?: number
    input_per_1m_usd?: number
    cached_input_per_1m_usd?: number
    output_per_1m_usd?: number
    source?: string
    source_id?: string
    fx_rate_to_usd?: number
  }
  cost: PricingCost
}

export interface PricingSnapshot {
  source_url?: string
  updated_at?: string
  last_attempt_at?: string
  last_error?: string
  summary: PricingSummary
  models: PricingModelSummary[]
}

export interface BenchmarkModel {
  model: string
  requests: number
  successes: number
  failures: number
  input_tokens: number
  output_tokens: number
  avg_latency_ms: number
  p50_latency_ms: number
  p95_latency_ms: number
  p99_latency_ms: number
  max_latency_ms: number
  success_rate: number
  estimated_cost_usd: number
  exact_cost_usd?: number
  estimated_legacy_cost_usd?: number
}

export interface BenchmarkResponse {
  window_hours: number
  start_time?: string
  end_time?: string
  hours?: number
  benchmarks: BenchmarkModel[]
}

export interface VerificationBaselineSnapshot {
  snapshot_id: string
  kind: string
  source_name: string
  source_url?: string
  captured_at: string
  imported_at: string
  row_count: number
}

export interface VerificationRunCase {
  case_id: string
  dimension: string
  kind: string
  critical: boolean
  completed: boolean
  success: boolean
  score: number
  reason?: string
  status_code?: number
  latency_ms?: number
  prompt_tokens?: number
  cached_prompt_tokens?: number
  completion_tokens?: number
  cost_usd?: number
  provider_id?: string
  effective_model?: string
  route_mode?: string
  response_excerpt?: string
  error?: string
}

export interface VerificationRunTarget {
  target_id: string
  run_id: string
  status: string
  provider_id: string
  public_model: string
  effective_model?: string
  canonical_model_id?: string
  protocol: string
  protocol_adapter?: string
  suite_version: string
  judge_model?: string
  public_snapshot_id?: string
  vendor_snapshot_id?: string
  verdict?: string
  suspicion_score?: number
  public_gap?: number
  vendor_gap?: number
  completion_rate?: number
  critical_protocol_failures?: number
  reason_codes?: string[]
  dimension_scores?: Record<string, number>
  prompt_tokens?: number
  cached_prompt_tokens?: number
  completion_tokens?: number
  estimated_cost_usd?: number
  cases?: VerificationRunCase[]
  started_at: string
  completed_at?: string
  error?: string
}

export interface VerificationRunSummary {
  run_id: string
  status: string
  suite_version: string
  protocol: string
  public_snapshot_id?: string
  vendor_snapshot_id?: string
  started_at: string
  completed_at?: string
  target_count: number
  completed_targets: number
  error?: string
}

export interface VerificationRunDetail extends VerificationRunSummary {
  targets: VerificationRunTarget[]
}

export interface ControlStatusView {
  version?: string
  started_at?: string
  uptime?: string
  gateway_status?: string
  gateway_error?: string
  telemetry_status?: string
  telemetry_error?: string
  telemetry_version?: string
  telemetry_event_count?: number
  telemetry_last_checked_at?: string
  gateway_readiness?: string
  gateway_listener?: string
  active_snapshot_id?: string
  active_requests?: number
  provider_health_count?: number
  healthy_provider_count?: number
  unhealthy_provider_count?: number
  cooldown_provider_count?: number
  provider_health?: ProviderHealthView[]
  pricing?: PricingStatusView
}

export interface DataResponse {
  window_hours?: number
  window_label?: string
  summary?: {
    requests?: number
    successes?: number
    failures?: number
    avg_latency_ms?: number
  }
  models?: Array<{
    value: string
    requests: number
    successes: number
    failures: number
    input_tokens: number
    output_tokens: number
    avg_latency_ms: number
  }>
  upstreams?: Array<{
    value: string
    requests: number
    successes: number
    failures: number
    input_tokens: number
    output_tokens: number
    avg_latency_ms: number
  }>
  requests?: RequestEntry[]
  errors?: ErrorEntry[]
  pricing_economics?: PricingSnapshot
}

export interface ConfigVersionSummary {
  id: string
  filename: string
  created_at: string
  size: number
  created_by?: string
  description?: string
  is_active?: boolean
}

export interface ConfigHistoryResponse {
  versions: ConfigVersionSummary[]
}

export interface PublisherPolicyView {
  publish_history_limit: number
}

export interface ControlConfigView {
  revision: ConfigVersionSummary | null
  policy: PublisherPolicyView
  raw_yaml?: string
  config?: Record<string, unknown>
}

export interface ProviderHealthView {
  name: string
  healthy: boolean
  status: 'healthy' | 'cooldown' | 'unhealthy'
  last_check?: string
  last_success?: string
  consecutive_failures: number
  cooldown_until?: string
  latency_ms?: number
}

export interface PricingSourceStatus {
  id: string
  vendor: string
  url?: string
  enabled: boolean
  status?: string
  updated_at?: string
  last_attempt_at?: string
  last_error?: string
  model_count?: number
}

export interface PricingFXStatus {
  enabled: boolean
  source_url?: string
  base_currency?: string
  updated_at?: string
  last_attempt_at?: string
  last_error?: string
}

export interface PricingStatusView {
  source_url?: string
  updated_at?: string
  last_attempt_at?: string
  last_error?: string
  catalog_size?: number
  sources?: PricingSourceStatus[]
  fx?: PricingFXStatus
}

export interface AdminSessionView {
  enabled: boolean
  authenticated: boolean
  name?: string
  role?: string
}
