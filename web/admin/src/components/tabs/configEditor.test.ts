import { describe, expect, it } from 'vitest'
import {
  buildVisualConfig,
  createDefaultVisualConfigState,
  parseConfigDocument,
  providerModelsFromConfig,
  visualStateFromConfig,
  type VisualConfigState,
} from './configEditor'

function sampleState(): VisualConfigState {
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
      publish_history_limit: 128,
      bootstrap_token: 'secret-bootstrap',
      rate_limit_rps: 12,
      rate_limit_burst: 24,
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
    providers: [
      {
        id: '0',
        name: 'openai',
        base_url: 'https://api.openai.com/v1',
        anthropic_base_url: '',
        api_key: 'provider-secret',
        provider_class: 'quota_limited',
        enabled: true,
        weight: 100,
        timeout_ms: 180000,
        same_upstream_retries: 3,
        models: [
          { public_model: 'gpt-4o', upstream_model: 'gpt-4o' },
          { public_model: 'gpt-4.1', upstream_model: 'gpt-4.1' },
        ],
        headers: { 'x-extra': '1' },
      },
    ],
    bridge: {
      enabled: true,
      rules: [{ from: 'gpt-4o', to: 'gpt-4.1' }],
      exclude_user_agents: ['curl'],
    },
    fallback: {
      enabled: true,
      detect_repetition: true,
      models: { 'gpt-4o': 'gpt-4.1' },
    },
    interceptRules: [
      {
        name: 'rate-limit',
        enabled: true,
        status_codes: [429],
        message_keywords: ['rate'],
        action: 'retry',
      },
    ],
    telemetry: {
      sqlite_path: 'data/telemetry.db',
      retention_days: 3650,
    },
    pricing: {
      cache_path: 'data/pricing-cache.json',
      refresh_interval_hours: 6,
      request_timeout_ms: 30000,
    },
  }
}

describe('configEditor', () => {
  it('parses YAML config documents', () => {
    const parsed = parseConfigDocument(`
server:
  listen: ":18080"
providers:
  - name: openai
    models:
      - gpt-4o
`)

    expect(parsed).toMatchObject({
      server: { listen: ':18080' },
      providers: [{ name: 'openai', models: ['gpt-4o'] }],
    })
  })

  it('normalizes provider model lists from string arrays', () => {
    expect(providerModelsFromConfig(['gpt-4o', 'gpt-4.1'])).toEqual([
      { public_model: 'gpt-4o', upstream_model: 'gpt-4o' },
      { public_model: 'gpt-4.1', upstream_model: 'gpt-4.1' },
    ])
  })

  it('hydrates visual state from a complete config and preserves explicit zero and false values', () => {
    const state = visualStateFromConfig({
      server: {
        listen: ':19090',
        read_timeout_ms: 1,
        write_timeout_ms: 0,
        idle_timeout_ms: 0,
        max_body_bytes: 0,
      },
      admin: {
        enabled: false,
        language: 'en',
        publish_history_limit: 0,
        bootstrap_token: 'boot-secret',
        rate_limit: {
          requests_per_second: 0,
          burst: 0,
        },
      },
      routing: {
        strategy: 'least_latency',
        max_retries: 0,
        retry_backoff: {
          initial_ms: 0,
          max_ms: 0,
        },
        health: {
          enabled: false,
          interval_sec: 0,
          timeout_ms: 0,
          path: '/ready',
        },
        sticky_sessions: {
          enabled: false,
          ttl_sec: 0,
        },
        failure_policy: {
          threshold: 0,
          cooldown_sec: 0,
          passthrough_after_sec: 0,
        },
        intercepts: [
          {
            name: 'routing-rule',
            enabled: false,
            status_codes: [400, 503],
            message_keywords: ['quota', 'timeout'],
            action: 'fallback',
          },
        ],
      },
      providers: [
        {
          name: 'anthropic',
          base_url: 'https://provider.example/v1',
          anthropic_base_url: 'https://provider.example/anthropic',
          api_key: 'provider-secret',
          provider_class: 'quota_limited',
          enabled: false,
          weight: 0,
          timeout_ms: 0,
          same_retries: 0,
          models: [
            'claude-3-5-sonnet',
            { public_model: 'claude-public', upstream_model: 'claude-upstream' },
          ],
          headers: {
            'x-team': 'platform',
            ignored: 3,
          },
        },
      ],
      compat: {
        bridge: {
          enabled: true,
          rules: [{ from: 'gpt-4o', to: 'gpt-4.1' }],
          exclude_user_agents: ['curl'],
        },
        fallback: {
          enabled: true,
          detect_repetition: false,
          models: {
            'gpt-4o': 'gpt-4.1',
            ignored: 3,
          },
        },
      },
      telemetry: {
        sqlite_path: 'data/custom-telemetry.db',
        retention_days: 0,
      },
      pricing: {
        cache_path: 'data/custom-pricing-cache.json',
        refresh_interval_hours: 0,
        request_timeout_ms: 0,
      },
    })

    expect(state).toEqual({
      server: {
        listen: ':19090',
        read_timeout_ms: 1,
        write_timeout_ms: 0,
        idle_timeout_ms: 0,
        max_body_bytes: 0,
      },
      admin: {
        enabled: false,
        language: 'en',
        publish_history_limit: 0,
        bootstrap_token: 'boot-secret',
        rate_limit_rps: 0,
        rate_limit_burst: 0,
      },
      routing: {
        strategy: 'least_latency',
        max_retries: 0,
        retry_backoff_initial_ms: 0,
        retry_backoff_max_ms: 0,
        health_enabled: false,
        health_interval_sec: 0,
        health_timeout_ms: 0,
        health_path: '/ready',
        sticky_sessions_enabled: false,
        sticky_sessions_ttl_sec: 0,
        failure_policy_threshold: 0,
        failure_policy_cooldown_sec: 0,
        failure_policy_passthrough_after_sec: 0,
      },
      providers: [
        {
          id: '0',
          name: 'anthropic',
          base_url: 'https://provider.example/v1',
          anthropic_base_url: 'https://provider.example/anthropic',
          api_key: 'provider-secret',
          provider_class: 'quota_limited',
          enabled: false,
          weight: 0,
          timeout_ms: 0,
          same_upstream_retries: 0,
          models: [
            { public_model: 'claude-3-5-sonnet', upstream_model: 'claude-3-5-sonnet' },
            { public_model: 'claude-public', upstream_model: 'claude-upstream' },
          ],
          headers: { 'x-team': 'platform' },
        },
      ],
      bridge: {
        enabled: true,
        rules: [{ from: 'gpt-4o', to: 'gpt-4.1' }],
        exclude_user_agents: ['curl'],
      },
      fallback: {
        enabled: true,
        detect_repetition: false,
        models: { 'gpt-4o': 'gpt-4.1' },
      },
      interceptRules: [
        {
          name: 'routing-rule',
          enabled: false,
          status_codes: [400, 503],
          message_keywords: ['quota', 'timeout'],
          action: 'fallback',
        },
      ],
      telemetry: {
        sqlite_path: 'data/custom-telemetry.db',
        retention_days: 0,
      },
      pricing: {
        cache_path: 'data/custom-pricing-cache.json',
        refresh_interval_hours: 0,
        request_timeout_ms: 0,
      },
    })
  })

  it('keeps default visual state values when config sections are partial', () => {
    const defaults = createDefaultVisualConfigState()
    const state = visualStateFromConfig({
      server: {
        listen: ':19090',
      },
      admin: {
        rate_limit: {
          burst: 99,
        },
      },
      routing: {
        health: {
          path: '/ready',
        },
      },
      pricing: {
        cache_path: 'data/custom-pricing-cache.json',
      },
    })

    expect(state).toEqual({
      ...defaults,
      server: {
        ...defaults.server,
        listen: ':19090',
      },
      admin: {
        ...defaults.admin,
        rate_limit_burst: 99,
      },
      routing: {
        ...defaults.routing,
        health_path: '/ready',
      },
      pricing: {
        ...defaults.pricing,
        cache_path: 'data/custom-pricing-cache.json',
      },
    })
  })

  it('uses backend-compatible defaults for omitted provider and intercept fields', () => {
    const state = visualStateFromConfig({
      providers: [
        {
          name: 'openai',
          models: ['gpt-4o'],
        },
      ],
      routing: {
        intercepts: [
          {
            name: 'missing-enabled',
            status_codes: [429],
            action: 'retry',
          },
        ],
      },
    })

    expect(state.providers[0]).toMatchObject({
      enabled: true,
      weight: 1,
      timeout_ms: 30000,
    })
    expect(state.interceptRules[0]).toMatchObject({
      enabled: true,
    })
  })

  it('prefers routing intercepts over legacy compat intercepts', () => {
    const state = visualStateFromConfig({
      routing: {
        intercepts: [
          {
            name: 'routing-rule',
            enabled: true,
            status_codes: [429],
            message_keywords: ['rate'],
            action: 'retry',
          },
        ],
      },
      compat: {
        intercepts: [
          {
            name: 'compat-rule',
            enabled: true,
            status_codes: [500],
            message_keywords: ['legacy'],
            action: 'fail',
          },
        ],
      },
    })

    expect(state.interceptRules).toEqual([
      {
        name: 'routing-rule',
        enabled: true,
        status_codes: [429],
        message_keywords: ['rate'],
        action: 'retry',
      },
    ])
  })

  it('respects an explicitly empty routing intercept list over legacy compat intercepts', () => {
    const state = visualStateFromConfig({
      routing: {
        intercepts: [],
      },
      compat: {
        intercepts: [
          {
            name: 'compat-rule',
            enabled: true,
            status_codes: [500],
            message_keywords: ['legacy'],
            action: 'fail',
          },
        ],
      },
    })

    expect(state.interceptRules).toEqual([])
  })

  it('converts pricing refresh interval minutes to editor hours', () => {
    const state = visualStateFromConfig({
      pricing: {
        refresh_interval_minutes: 121,
        refresh_interval_hours: 12,
      },
    })

    expect(state.pricing.refresh_interval_hours).toBe(3)
  })

  it('preserves backend-only intercept fields during visual round trips', () => {
    const base = parseConfigDocument(`
routing:
  intercepts:
    - name: preserve-extra
      enabled: true
      paths:
        - /v1/chat/completions
      status_code_min: 500
      status_codes:
        - 429
      message_keywords:
        - quota
      action: retry
`)
    const state = visualStateFromConfig(base)
    state.interceptRules[0].action = 'fallback'

    const config = buildVisualConfig(base, state)
    expect(config.routing).toMatchObject({
      intercepts: [
        {
          name: 'preserve-extra',
          enabled: true,
          paths: ['/v1/chat/completions'],
          status_code_min: 500,
          status_codes: [429],
          message_keywords: ['quota'],
          action: 'fallback',
        },
      ],
    })
  })

  it('preserves hidden secrets and emits backend provider schema', () => {
    const base = parseConfigDocument(`
server:
  listen: ":18080"
admin:
  enabled: true
  bootstrap_token: secret-bootstrap
  cookie_signing_key: cookie-secret
  tokens:
    - name: viewer
      token: viewer-secret
      role: viewer
providers:
  - name: openai
    base_url: https://api.openai.com/v1
    api_key: provider-secret
    provider_class: quota_limited
    same_retries: 2
    models:
      - gpt-4o
routing:
  intercepts:
    - name: old-rule
      action: fail
compat:
  bridge:
    enabled: false
  fallback:
    enabled: false
pricing:
  cache_path: data/pricing-cache.json
  refresh_interval_minutes: 15
`)
    const config = buildVisualConfig(base, sampleState())

    expect(config.admin).toMatchObject({
      bootstrap_token: 'secret-bootstrap',
      cookie_signing_key: 'cookie-secret',
      tokens: [{ name: 'viewer', token: 'viewer-secret', role: 'viewer' }],
    })
    expect(config.providers).toEqual([
      expect.objectContaining({
        api_key: 'provider-secret',
        same_retries: 3,
        models: ['gpt-4o', 'gpt-4.1'],
        headers: { 'x-extra': '1' },
      }),
    ])
    expect((config.providers as Array<Record<string, unknown>>)[0].same_upstream_retries).toBeUndefined()
    expect(config.routing).toMatchObject({
      intercepts: [
        expect.objectContaining({
          name: 'rate-limit',
          status_codes: [429],
          message_keywords: ['rate'],
          action: 'retry',
        }),
      ],
    })
    expect(config.compat).toMatchObject({
      bridge: { enabled: true, rules: [{ from: 'gpt-4o', to: 'gpt-4.1' }] },
      fallback: { enabled: true, detect_repetition: true, models: { 'gpt-4o': 'gpt-4.1' } },
    })
    expect((config.compat as Record<string, unknown>).intercepts).toBeUndefined()
    expect(config.pricing).toMatchObject({
      refresh_interval_hours: 6,
      request_timeout_ms: 30000,
    })
    expect((config.pricing as Record<string, unknown>).refresh_interval_minutes).toBeUndefined()
  })

  it('rejects unsupported divergent public/upstream model mappings', () => {
    const state = sampleState()
    state.providers[0].models = [{ public_model: 'gpt-4o', upstream_model: 'real-gpt-4o' }]

    expect(() => buildVisualConfig({ providers: [] }, state)).toThrow(/only supports identical public\/upstream names/)
  })
})
