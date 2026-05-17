import { parse as parseYAML } from 'yaml'

export type ConfigRecord = Record<string, unknown>

export interface ProviderModelMapping {
  public_model: string
  upstream_model: string
}

export interface ProviderEditorConfig {
  id: string
  name: string
  base_url: string
  anthropic_base_url: string
  api_key: string
  provider_class: string
  enabled: boolean
  weight: number
  timeout_ms: number
  same_upstream_retries: number
  rate_limit_enabled: boolean
  rate_limit_rps: number
  rate_limit_burst: number
  models: ProviderModelMapping[]
  headers: Record<string, string>
}

export interface RoutingEditorConfig {
  strategy: string
  max_retries: number
  retry_backoff_initial_ms: number
  retry_backoff_max_ms: number
  health_enabled: boolean
  health_interval_sec: number
  health_timeout_ms: number
  health_path: string
  sticky_sessions_enabled: boolean
  sticky_sessions_ttl_sec: number
  failure_policy_threshold: number
  failure_policy_cooldown_sec: number
  failure_policy_passthrough_after_sec: number
}

export interface ServerEditorConfig {
  listen: string
  read_timeout_ms: number
  write_timeout_ms: number
  idle_timeout_ms: number
  max_body_bytes: number
}

export interface AdminEditorConfig {
  enabled: boolean
  language: string
  publish_history_limit: number
  bootstrap_token: string
  rate_limit_rps: number
  rate_limit_burst: number
}

export interface BridgeRule {
  from: string
  to: string
}

export interface BridgeEditorConfig {
  enabled: boolean
  rules: BridgeRule[]
  exclude_user_agents: string[]
}

export interface FallbackEditorConfig {
  enabled: boolean
  detect_repetition: boolean
  models: Record<string, string>
}

export interface TelemetryEditorConfig {
  sqlite_path: string
  retention_days: number
}

export interface PricingEditorConfig {
  cache_path: string
  refresh_interval_hours: number
  request_timeout_ms: number
}

export interface InterceptEditorRule {
  name: string
  enabled: boolean
  status_codes: number[]
  message_keywords: string[]
  action: string
}

export interface VisualConfigState {
  server: ServerEditorConfig
  admin: AdminEditorConfig
  routing: RoutingEditorConfig
  providers: ProviderEditorConfig[]
  bridge: BridgeEditorConfig
  fallback: FallbackEditorConfig
  interceptRules: InterceptEditorRule[]
  telemetry: TelemetryEditorConfig
  pricing: PricingEditorConfig
}

function isRecord(value: unknown): value is ConfigRecord {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function asRecordArray(value: unknown): ConfigRecord[] {
  return Array.isArray(value) ? value.filter((item): item is ConfigRecord => isRecord(item)) : []
}

function stringValue(value: unknown, fallback: string): string {
  return typeof value === 'string' ? value : fallback
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function booleanValue(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

function stringArrayValue(value: unknown, fallback: string[] = []): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : fallback
}

function stringRecordValue(value: unknown): Record<string, string> {
  if (!isRecord(value)) return {}
  return Object.fromEntries(
    Object.entries(value).filter((entry): entry is [string, string] => typeof entry[1] === 'string')
  )
}

function cloneRecord(value: ConfigRecord | null | undefined): ConfigRecord {
  if (!value) return {}
  return JSON.parse(JSON.stringify(value)) as ConfigRecord
}

function cloneStringMap(value: Record<string, string>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(value).filter(([key]) => key.trim() !== '')
  )
}

function trimString(value: string): string {
  return value.trim()
}

export function createDefaultVisualConfigState(): VisualConfigState {
  return {
    server: {
      listen: ':18080',
      read_timeout_ms: 30000,
      write_timeout_ms: 0,
      idle_timeout_ms: 120000,
      max_body_bytes: 104857600,
    },
    admin: {
      enabled: true,
      language: 'zh',
      publish_history_limit: 256,
      bootstrap_token: '',
      rate_limit_rps: 10,
      rate_limit_burst: 20,
    },
    routing: {
      strategy: 'health_weighted_rr',
      max_retries: 2,
      retry_backoff_initial_ms: 3000,
      retry_backoff_max_ms: 30000,
      health_enabled: true,
      health_interval_sec: 10,
      health_timeout_ms: 5000,
      health_path: '/v1/models',
      sticky_sessions_enabled: true,
      sticky_sessions_ttl_sec: 1800,
      failure_policy_threshold: 8,
      failure_policy_cooldown_sec: 60,
      failure_policy_passthrough_after_sec: 600,
    },
    providers: [],
    bridge: {
      enabled: false,
      rules: [],
      exclude_user_agents: [],
    },
    fallback: {
      enabled: false,
      detect_repetition: true,
      models: {},
    },
    interceptRules: [],
    telemetry: {
      sqlite_path: 'data/telemetry.db',
      retention_days: 3650,
    },
    pricing: {
      cache_path: 'data/pricing-cache.json',
      refresh_interval_hours: 12,
      request_timeout_ms: 30000,
    },
  }
}

export function parseConfigDocument(content: string): ConfigRecord | null {
  const trimmed = content.trim()
  if (!trimmed) return null

  const parseObject = (value: unknown): ConfigRecord | null => {
    if (!isRecord(value)) return null
    return value
  }

  try {
    return parseObject(JSON.parse(trimmed))
  } catch {
    return parseObject(parseYAML(trimmed))
  }
}

export function providerModelsFromConfig(value: unknown): ProviderModelMapping[] {
  if (!Array.isArray(value)) return []

  return value
    .map((entry) => {
      if (typeof entry === 'string') {
        const model = trimString(entry)
        if (!model) return null
        return { public_model: model, upstream_model: model }
      }
      if (!isRecord(entry)) return null

      const publicModel = typeof entry.public_model === 'string' ? trimString(entry.public_model) : ''
      const upstreamModel = typeof entry.upstream_model === 'string' ? trimString(entry.upstream_model) : ''
      const resolved = publicModel || upstreamModel
      if (!resolved) return null
      return {
        public_model: publicModel || resolved,
        upstream_model: upstreamModel || resolved,
      }
    })
    .filter((entry): entry is ProviderModelMapping => entry !== null)
}

export function visualStateFromConfig(config: ConfigRecord | null | undefined): VisualConfigState {
  const state = createDefaultVisualConfigState()
  if (!config) return state

  const serverCfg = isRecord(config.server) ? config.server : null
  if (serverCfg) {
    state.server = {
      listen: stringValue(serverCfg.listen, state.server.listen),
      read_timeout_ms: numberValue(serverCfg.read_timeout_ms, state.server.read_timeout_ms),
      write_timeout_ms: numberValue(serverCfg.write_timeout_ms, state.server.write_timeout_ms),
      idle_timeout_ms: numberValue(serverCfg.idle_timeout_ms, state.server.idle_timeout_ms),
      max_body_bytes: numberValue(serverCfg.max_body_bytes, state.server.max_body_bytes),
    }
  }

  const adminCfg = isRecord(config.admin) ? config.admin : null
  if (adminCfg) {
    const rateLimit = isRecord(adminCfg.rate_limit) ? adminCfg.rate_limit : null
    state.admin = {
      enabled: booleanValue(adminCfg.enabled, state.admin.enabled),
      language: stringValue(adminCfg.language, state.admin.language),
      publish_history_limit: numberValue(adminCfg.publish_history_limit, state.admin.publish_history_limit),
      bootstrap_token: stringValue(adminCfg.bootstrap_token, state.admin.bootstrap_token),
      rate_limit_rps: numberValue(rateLimit?.requests_per_second, state.admin.rate_limit_rps),
      rate_limit_burst: numberValue(rateLimit?.burst, state.admin.rate_limit_burst),
    }
  }

  const routingCfg = isRecord(config.routing) ? config.routing : null
  if (routingCfg) {
    const healthCfg = isRecord(routingCfg.health) ? routingCfg.health : null
    const stickyCfg = isRecord(routingCfg.sticky_sessions) ? routingCfg.sticky_sessions : null
    const retryBackoff = isRecord(routingCfg.retry_backoff) ? routingCfg.retry_backoff : null
    const failurePolicy = isRecord(routingCfg.failure_policy) ? routingCfg.failure_policy : null
    state.routing = {
      strategy: stringValue(routingCfg.strategy, state.routing.strategy),
      max_retries: numberValue(routingCfg.max_retries, state.routing.max_retries),
      retry_backoff_initial_ms: numberValue(retryBackoff?.initial_ms, state.routing.retry_backoff_initial_ms),
      retry_backoff_max_ms: numberValue(retryBackoff?.max_ms, state.routing.retry_backoff_max_ms),
      health_enabled: booleanValue(healthCfg?.enabled, state.routing.health_enabled),
      health_interval_sec: numberValue(healthCfg?.interval_sec, state.routing.health_interval_sec),
      health_timeout_ms: numberValue(healthCfg?.timeout_ms, state.routing.health_timeout_ms),
      health_path: stringValue(healthCfg?.path, state.routing.health_path),
      sticky_sessions_enabled: booleanValue(stickyCfg?.enabled, state.routing.sticky_sessions_enabled),
      sticky_sessions_ttl_sec: numberValue(stickyCfg?.ttl_sec, state.routing.sticky_sessions_ttl_sec),
      failure_policy_threshold: numberValue(failurePolicy?.threshold, state.routing.failure_policy_threshold),
      failure_policy_cooldown_sec: numberValue(failurePolicy?.cooldown_sec, state.routing.failure_policy_cooldown_sec),
      failure_policy_passthrough_after_sec: numberValue(failurePolicy?.passthrough_after_sec, state.routing.failure_policy_passthrough_after_sec),
    }
  }

  const providers = asRecordArray(config.providers)
  if (providers.length > 0) {
    state.providers = providers.map((provider, index) => {
      const rateLimit = isRecord(provider.rate_limit) ? provider.rate_limit : null
      return {
        id: String(index),
        name: stringValue(provider.name, `Provider ${index + 1}`),
        base_url: stringValue(provider.base_url, ''),
        anthropic_base_url: stringValue(provider.anthropic_base_url, ''),
        api_key: stringValue(provider.api_key, ''),
        provider_class: stringValue(provider.provider_class, ''),
        enabled: booleanValue(provider.enabled, true),
        weight: numberValue(provider.weight, 1),
        timeout_ms: numberValue(provider.timeout_ms, 30000),
        same_upstream_retries: numberValue(provider.same_retries ?? provider.same_upstream_retries, 2),
        rate_limit_enabled: booleanValue(rateLimit?.enabled, false),
        rate_limit_rps: numberValue(rateLimit?.requests_per_second, 0),
        rate_limit_burst: numberValue(rateLimit?.burst, 0),
        models: providerModelsFromConfig(provider.models),
        headers: stringRecordValue(provider.headers),
      }
    })
  }

  const compatCfg = isRecord(config.compat) ? config.compat : null
  const bridgeCfg = isRecord(compatCfg?.bridge) ? compatCfg.bridge : null
  if (bridgeCfg) {
    state.bridge = {
      enabled: booleanValue(bridgeCfg.enabled, state.bridge.enabled),
      rules: asRecordArray(bridgeCfg.rules).map((rule) => ({
        from: stringValue(rule.from, ''),
        to: stringValue(rule.to, ''),
      })),
      exclude_user_agents: stringArrayValue(bridgeCfg.exclude_user_agents),
    }
  }

  const fallbackCfg = isRecord(compatCfg?.fallback) ? compatCfg.fallback : null
  if (fallbackCfg) {
    state.fallback = {
      enabled: booleanValue(fallbackCfg.enabled, state.fallback.enabled),
      detect_repetition: booleanValue(fallbackCfg.detect_repetition, state.fallback.detect_repetition),
      models: stringRecordValue(fallbackCfg.models),
    }
  }

  const intercepts = Array.isArray(routingCfg?.intercepts)
    ? asRecordArray(routingCfg?.intercepts)
    : asRecordArray(compatCfg?.intercepts)
  state.interceptRules = intercepts.map((rule) => ({
    name: stringValue(rule.name, ''),
    enabled: booleanValue(rule.enabled, true),
    status_codes: Array.isArray(rule.status_codes)
      ? rule.status_codes.filter((item): item is number => typeof item === 'number' && Number.isFinite(item))
      : [],
    message_keywords: stringArrayValue(rule.message_keywords),
    action: stringValue(rule.action, ''),
  }))

  const telemetryCfg = isRecord(config.telemetry) ? config.telemetry : null
  if (telemetryCfg) {
    state.telemetry = {
      sqlite_path: stringValue(telemetryCfg.sqlite_path, state.telemetry.sqlite_path),
      retention_days: numberValue(telemetryCfg.retention_days, state.telemetry.retention_days),
    }
  }

  const pricingCfg = isRecord(config.pricing) ? config.pricing : null
  if (pricingCfg) {
    const refreshHours = typeof pricingCfg.refresh_interval_minutes === 'number' && pricingCfg.refresh_interval_minutes > 0
      ? Math.max(1, Math.ceil(pricingCfg.refresh_interval_minutes / 60))
      : numberValue(pricingCfg.refresh_interval_hours, state.pricing.refresh_interval_hours)
    state.pricing = {
      cache_path: stringValue(pricingCfg.cache_path, state.pricing.cache_path),
      refresh_interval_hours: refreshHours,
      request_timeout_ms: numberValue(pricingCfg.request_timeout_ms, state.pricing.request_timeout_ms),
    }
  }

  return state
}

function compileProviderModels(providerName: string, models: ProviderModelMapping[]): string[] {
  const compiled: string[] = []
  for (const model of models) {
    const publicModel = trimString(model.public_model)
    const upstreamModel = trimString(model.upstream_model)
    if (!publicModel && !upstreamModel) {
      continue
    }
    if (publicModel && upstreamModel && publicModel !== upstreamModel) {
      throw new Error(
        `Provider ${providerName || 'unnamed'} has model mapping ${publicModel} -> ${upstreamModel}, but the current config schema only supports identical public/upstream names.`
      )
    }
    compiled.push(publicModel || upstreamModel)
  }
  return compiled
}

export function buildVisualConfig(baseConfig: ConfigRecord | null, state: VisualConfigState): ConfigRecord {
  if (!baseConfig) {
    throw new Error('Visual editor requires the full raw YAML config so secret fields can be preserved.')
  }

  const config = cloneRecord(baseConfig)

  const server = isRecord(config.server) ? cloneRecord(config.server) : {}
  server.listen = state.server.listen
  server.read_timeout_ms = state.server.read_timeout_ms
  server.write_timeout_ms = state.server.write_timeout_ms
  server.idle_timeout_ms = state.server.idle_timeout_ms
  server.max_body_bytes = state.server.max_body_bytes
  config.server = server

  const admin = isRecord(config.admin) ? cloneRecord(config.admin) : {}
  admin.enabled = state.admin.enabled
  admin.language = state.admin.language
  admin.publish_history_limit = state.admin.publish_history_limit
  admin.bootstrap_token = state.admin.bootstrap_token
  const rateLimit = isRecord(admin.rate_limit) ? cloneRecord(admin.rate_limit) : {}
  rateLimit.requests_per_second = state.admin.rate_limit_rps
  rateLimit.burst = state.admin.rate_limit_burst
  admin.rate_limit = rateLimit
  config.admin = admin

  const routing = isRecord(config.routing) ? cloneRecord(config.routing) : {}
  routing.strategy = state.routing.strategy
  routing.max_retries = state.routing.max_retries
  const retryBackoff = isRecord(routing.retry_backoff) ? cloneRecord(routing.retry_backoff) : {}
  retryBackoff.initial_ms = state.routing.retry_backoff_initial_ms
  retryBackoff.max_ms = state.routing.retry_backoff_max_ms
  routing.retry_backoff = retryBackoff
  const health = isRecord(routing.health) ? cloneRecord(routing.health) : {}
  health.enabled = state.routing.health_enabled
  health.interval_sec = state.routing.health_interval_sec
  health.timeout_ms = state.routing.health_timeout_ms
  health.path = state.routing.health_path
  routing.health = health
  const stickySessions = isRecord(routing.sticky_sessions) ? cloneRecord(routing.sticky_sessions) : {}
  stickySessions.enabled = state.routing.sticky_sessions_enabled
  stickySessions.ttl_sec = state.routing.sticky_sessions_ttl_sec
  routing.sticky_sessions = stickySessions
  const failurePolicy = isRecord(routing.failure_policy) ? cloneRecord(routing.failure_policy) : {}
  failurePolicy.threshold = state.routing.failure_policy_threshold
  failurePolicy.cooldown_sec = state.routing.failure_policy_cooldown_sec
  failurePolicy.passthrough_after_sec = state.routing.failure_policy_passthrough_after_sec
  routing.failure_policy = failurePolicy
  const baseCompatForIntercepts = isRecord(config.compat) ? config.compat : null
  const routingHadIntercepts = Array.isArray(routing.intercepts)
  const baseIntercepts = routingHadIntercepts
    ? routing.intercepts as unknown[]
    : Array.isArray(baseCompatForIntercepts?.intercepts) ? baseCompatForIntercepts.intercepts as unknown[] : []
  routing.intercepts = state.interceptRules.map((rule, index) => {
    const existing = isRecord(baseIntercepts[index]) ? cloneRecord(baseIntercepts[index] as ConfigRecord) : {}
    existing.name = rule.name
    existing.enabled = rule.enabled
    existing.status_codes = [...rule.status_codes]
    existing.message_keywords = [...rule.message_keywords]
    existing.action = rule.action
    return existing
  })
  config.routing = routing

  const baseProviders = Array.isArray(config.providers) ? config.providers : []
  config.providers = state.providers.map((provider, index) => {
    const existing = isRecord(baseProviders[index]) ? cloneRecord(baseProviders[index] as ConfigRecord) : {}
    existing.name = provider.name
    existing.base_url = provider.base_url
    existing.anthropic_base_url = provider.anthropic_base_url
    existing.api_key = provider.api_key
    existing.provider_class = provider.provider_class
    existing.enabled = provider.enabled
    existing.weight = provider.weight
    existing.timeout_ms = provider.timeout_ms
    existing.same_retries = provider.same_upstream_retries
    delete existing.same_upstream_retries
    const providerRateLimit = isRecord(existing.rate_limit) ? cloneRecord(existing.rate_limit) : {}
    providerRateLimit.enabled = provider.rate_limit_enabled
    providerRateLimit.requests_per_second = provider.rate_limit_rps
    providerRateLimit.burst = provider.rate_limit_burst
    existing.rate_limit = providerRateLimit
    existing.models = compileProviderModels(provider.name, provider.models)
    existing.headers = cloneStringMap(provider.headers)
    return existing
  })

  const compat = isRecord(config.compat) ? cloneRecord(config.compat) : {}
  const bridge = isRecord(compat.bridge) ? cloneRecord(compat.bridge) : {}
  bridge.enabled = state.bridge.enabled
  bridge.rules = state.bridge.rules.map((rule) => ({ from: rule.from, to: rule.to }))
  bridge.exclude_user_agents = [...state.bridge.exclude_user_agents]
  compat.bridge = bridge
  const fallback = isRecord(compat.fallback) ? cloneRecord(compat.fallback) : {}
  fallback.enabled = state.fallback.enabled
  fallback.detect_repetition = state.fallback.detect_repetition
  fallback.models = cloneStringMap(state.fallback.models)
  compat.fallback = fallback
  delete compat.intercepts
  config.compat = compat

  const telemetry = isRecord(config.telemetry) ? cloneRecord(config.telemetry) : {}
  telemetry.sqlite_path = state.telemetry.sqlite_path
  telemetry.retention_days = state.telemetry.retention_days
  config.telemetry = telemetry

  const pricing = isRecord(config.pricing) ? cloneRecord(config.pricing) : {}
  pricing.cache_path = state.pricing.cache_path
  pricing.refresh_interval_hours = state.pricing.refresh_interval_hours
  delete pricing.refresh_interval_minutes
  pricing.request_timeout_ms = state.pricing.request_timeout_ms
  config.pricing = pricing

  return config
}
