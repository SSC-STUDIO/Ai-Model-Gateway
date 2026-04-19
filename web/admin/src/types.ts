// Shared TypeScript types for the controld admin UI.

export type ControlTabKey = 'overview' | 'telemetry' | 'timeseries' | 'benchmark' | 'config'

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
  time?: string
  Timestamp?: string
  method?: string
  path?: string
  Path?: string
  status?: number
  StatusCode?: number
  upstream?: string
  Upstream?: string
  model?: string
  Model?: string
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
  totals_by_currency?: PricingCurrencySummary[]
}

export interface PricingModelSummary {
  display_model: string
  requested_model?: string
  effective_model?: string
  upstream?: string
  provider?: string
  pricing_model?: string
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
}

export interface BenchmarkResponse {
  window_hours: number
  start_time?: string
  end_time?: string
  hours?: number
  benchmarks: BenchmarkModel[]
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

export interface AdminSessionView {
  enabled: boolean
  authenticated: boolean
  name?: string
  role?: string
}
