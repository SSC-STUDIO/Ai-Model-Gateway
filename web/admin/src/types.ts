// Shared TypeScript types for AI Model Gateway Admin UI

export type TabKey = 'overview' | 'telemetry' | 'timeseries' | 'settings' | 'history' | 'probe' | 'logs' | 'benchmark' | 'audit'
export type AnyRecord = Record<string, unknown>

export interface TimeSeriesPoint {
  timestamp: number
  value: number
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
  time: string
  method: string
  path: string
  status: number
  upstream: string
  model: string
  attempts: number
  latency_ms: number
  input_tokens?: number
  output_tokens?: number
  cached?: boolean
}

export interface ErrorEntry {
  time: string
  upstream: string
  model: string
  status: number
  message: string
  count: number
}

export interface PricingCost {
  prompt_usd: number
  completion_usd: number
  total_usd: number
}

export interface PricingSummary {
  currency: string
  prompt_usd: number
  completion_usd: number
  total_usd: number
  cached_prompt_tokens: number
  cache_savings_usd: number
  priced_models: number
  unpriced_models: number
}

export interface PricingModelSummary {
  display_model: string
  requested_model?: string
  effective_model?: string
  pricing_model?: string
  usage: {
    prompt_tokens: number
    cached_prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
  pricing?: {
    input_per_1m_usd: number
    cached_input_per_1m_usd?: number
    output_per_1m_usd: number
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
  start_time: string
  end_time: string
  hours: number
  models: BenchmarkModel[]
}

export interface DataResponse {
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

export interface ProbeProvider {
  name: string
  base_url: string
  anthropic_base_url: string
  api_key: string
  provider_class: string
  models: string
  timeout_ms: string
  enabled: boolean
}

export interface AuditEntry {
  id: number
  timestamp: string
  action: string
  actor: string
  role: string
  details: string
  source_ip: string
}

export interface AuditLogResponse {
  items: AuditEntry[]
  total: number
}
