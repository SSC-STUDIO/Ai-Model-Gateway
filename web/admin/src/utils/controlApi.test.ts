import { describe, it, expect } from 'vitest'
import {
  benchmarkURL,
  normalizeControlConfigResponse,
  telemetryURL,
  telemetryTimeseriesURL,
  normalizeControlStatus,
  normalizeOverviewResponse,
  normalizeTelemetryResponse,
  normalizeTimeSeriesResponse,
  normalizeBenchmarkResponse,
  normalizeConfigHistoryResponse,
} from './controlApi'

describe('controlApi', () => {
  describe('benchmarkURL', () => {
    it('builds URL with hours only', () => {
      expect(benchmarkURL(24, [])).toBe('/api/admin/benchmark?hours=24')
    })

    it('builds URL with hours and sorted models', () => {
      expect(benchmarkURL(168, ['gpt-4o', 'gpt-4o-mini'])).toBe(
        '/api/admin/benchmark?hours=168&models=gpt-4o%2Cgpt-4o-mini'
      )
    })

    it('ignores empty model strings', () => {
      expect(benchmarkURL(24, ['', 'gpt-4o', '  '])).toBe(
        '/api/admin/benchmark?hours=24&models=gpt-4o'
      )
    })
  })

  describe('telemetryURL', () => {
    it('encodes hours', () => {
      expect(telemetryURL('168')).toContain('hours=168')
      expect(telemetryURL('168')).toContain('limit=500')
    })

    it('resolves "all" to full window hours', () => {
      expect(telemetryURL('all')).toContain('hours=8760')
    })
  })

  describe('telemetryTimeseriesURL', () => {
    it('uses 60min bucket for default hours', () => {
      expect(telemetryTimeseriesURL('168')).toContain('bucket=60')
    })

    it('uses 1440min bucket for 720 hours', () => {
      expect(telemetryTimeseriesURL('720')).toContain('bucket=1440')
    })

    it('uses 10080min bucket for "all"', () => {
      expect(telemetryTimeseriesURL('all')).toContain('bucket=10080')
    })
  })

  describe('normalizeControlStatus', () => {
    it('returns null for non-object payload', () => {
      expect(normalizeControlStatus(null)).toBeNull()
      expect(normalizeControlStatus('string')).toBeNull()
    })

    it('normalizes gateway readiness from number', () => {
      const result = normalizeControlStatus({
        gateway: { readiness: 2 },
      })
      expect(result?.gateway_readiness).toBe('ready')
    })

    it('normalizes gateway readiness from string', () => {
      const result = normalizeControlStatus({
        gateway: { readiness: 'draining' },
      })
      expect(result?.gateway_readiness).toBe('draining')
    })

    it('extracts provider health count', () => {
      const result = normalizeControlStatus({
        gateway: { provider_health: { a: true, b: false } },
      })
      expect(result?.provider_health_count).toBe(2)
    })

    it('normalizes detailed provider health entries', () => {
      const result = normalizeControlStatus({
        gateway: {
          provider_health: {
            'provider-b': {
              Healthy: false,
              ConsecutiveFailures: 3,
              CooldownUntil: '2099-01-01T00:00:00Z',
              LatencyMs: 420,
            },
            'provider-a': {
              Healthy: true,
              LastCheck: '2026-01-01T00:00:00Z',
              LastSuccess: '2026-01-01T00:00:00Z',
              LatencyMs: 120,
            },
          },
        },
      })

      expect(result?.healthy_provider_count).toBe(1)
      expect(result?.unhealthy_provider_count).toBe(1)
      expect(result?.cooldown_provider_count).toBe(1)
      expect(result?.provider_health?.[0]).toMatchObject({
        name: 'provider-b',
        status: 'cooldown',
        consecutive_failures: 3,
        latency_ms: 420,
      })
      expect(result?.provider_health?.[1]).toMatchObject({
        name: 'provider-a',
        status: 'healthy',
      })
    })
  })

  describe('normalizeOverviewResponse', () => {
    it('returns null for null payload', () => {
      expect(normalizeOverviewResponse(null)).toBeNull()
    })

    it('normalizes window metrics with mixed casing', () => {
      const payload = {
        windows: {
          last_1m: { Requests: 10, successes: 8, Failures: 2 },
        },
      }
      const result = normalizeOverviewResponse(payload)
      expect(result?.last_1m).toEqual({
        requests: 10,
        successes: 8,
        failures: 2,
        avg_latency_ms: 0,
        input_tokens: 0,
        cached_prompt_tokens: 0,
        output_tokens: 0,
      })
    })

    it('normalizes available models from string list', () => {
      const result = normalizeOverviewResponse({
        AvailableModels: ['gpt-4o', 'gpt-4o-mini'],
      })
      expect(result?.available_models).toEqual(['gpt-4o', 'gpt-4o-mini'])
    })

    it('includes normalized provider health details from status payload', () => {
      const result = normalizeOverviewResponse(
        { windows: { last_1m: { requests: 1 } } },
        {
          gateway: {
            provider_health: {
              alpha: { healthy: true, latency_ms: 90 },
              beta: { healthy: false, consecutive_failures: 2 },
            },
          },
        }
      )

      expect(result?.provider_health).toEqual([
        expect.objectContaining({ name: 'beta', status: 'unhealthy' }),
        expect.objectContaining({ name: 'alpha', status: 'healthy' }),
      ])
      expect(result?.runtime).toMatchObject({
        provider_health_count: 2,
        healthy_provider_count: 1,
        unhealthy_provider_count: 1,
      })
    })
  })

  describe('normalizeControlConfigResponse', () => {
    it('returns null for non-object payload', () => {
      expect(normalizeControlConfigResponse(null)).toBeNull()
      expect(normalizeControlConfigResponse('string')).toBeNull()
    })

    it('normalizes revision and publisher policy', () => {
      const result = normalizeControlConfigResponse({
        revision: {
          revision_id: 'rev-1',
          created_at: '2026-04-18T00:00:00Z',
          is_active: true,
        },
        policy: {
          publish_history_limit: 64,
        },
      })

      expect(result).toEqual({
        revision: expect.objectContaining({
          id: 'rev-1',
          is_active: true,
        }),
        policy: {
          publish_history_limit: 64,
        },
      })
    })
  })

  describe('normalizeTelemetryResponse', () => {
    it('returns null for null payload', () => {
      expect(normalizeTelemetryResponse(null)).toBeNull()
    })

    it('aggregates events into requests and errors', () => {
      const payload = {
        events: [
          {
            timestamp: '2024-01-01T00:00:00Z',
            path: '/v1/chat',
            requested_model: 'gpt-4o',
            effective_model: 'gpt-4o',
            provider: 'openai',
            status_code: 200,
            latency_ms: 100,
            attempts: 1,
            input_tokens: 10,
            cached_prompt_tokens: 0,
            output_tokens: 20,
            error: '',
          },
          {
            timestamp: '2024-01-01T00:01:00Z',
            path: '/v1/chat',
            requested_model: 'gpt-4o',
            effective_model: 'gpt-4o',
            provider: 'openai',
            status_code: 500,
            latency_ms: 50,
            attempts: 2,
            input_tokens: 5,
            cached_prompt_tokens: 0,
            output_tokens: 0,
            error: 'timeout',
          },
        ],
      }
      const result = normalizeTelemetryResponse(payload)
      expect(result!.summary!.requests).toBe(2)
      expect(result!.summary!.successes).toBe(1)
      expect(result!.summary!.failures).toBe(1)
      expect(result!.requests!.length).toBe(2)
      expect(result!.errors!.length).toBe(1)
    })
  })

  describe('normalizeTimeSeriesResponse', () => {
    it('returns null for null payload', () => {
      expect(normalizeTimeSeriesResponse(null)).toBeNull()
    })

    it('normalizes and sorts points by timestamp', () => {
      const payload = {
        buckets: [
          { bucket: '2024-01-02T00:00:00Z', requests: 5 },
          { bucket: '2024-01-01T00:00:00Z', requests: 3 },
        ],
      }
      const result = normalizeTimeSeriesResponse(payload)
      expect(result?.points.length).toBe(2)
      expect(result?.points[0].Bucket).toBe('2024-01-01T00:00:00Z')
      expect(result?.points[1].Bucket).toBe('2024-01-02T00:00:00Z')
    })
  })

  describe('normalizeBenchmarkResponse', () => {
    it('returns null for null payload', () => {
      expect(normalizeBenchmarkResponse(null)).toBeNull()
    })

    it('normalizes success rate >1 as percentage', () => {
      const payload = {
        benchmarks: [{ model: 'gpt-4o', success_rate: 99.5 }],
      }
      const result = normalizeBenchmarkResponse(payload)
      expect(result?.benchmarks[0].success_rate).toBe(0.995)
    })

    it('keeps success rate <=1 as-is', () => {
      const payload = {
        benchmarks: [{ model: 'gpt-4o', success_rate: 0.985 }],
      }
      const result = normalizeBenchmarkResponse(payload)
      expect(result?.benchmarks[0].success_rate).toBe(0.985)
    })
  })

  describe('normalizeConfigHistoryResponse', () => {
    it('normalizes flat array payload', () => {
      const payload = [
        { revision_id: 'rev-1', created_at: '2024-01-01', is_active: true },
        { revision_id: 'rev-2', created_at: '2024-01-02', is_active: false },
      ]
      const result = normalizeConfigHistoryResponse(payload)
      expect(result.versions.length).toBe(2)
      expect(result.versions[0].id).toBe('rev-1')
      expect(result.versions[0].is_active).toBe(true)
    })

    it('normalizes nested versions object', () => {
      const payload = {
        versions: [
          { id: 'rev-3', created_at: '2024-01-03' },
        ],
      }
      const result = normalizeConfigHistoryResponse(payload)
      expect(result.versions.length).toBe(1)
      expect(result.versions[0].id).toBe('rev-3')
    })

    it('filters out invalid entries', () => {
      const payload = [
        { revision_id: 'rev-1' },
        null,
        { revision_id: '' },
      ]
      const result = normalizeConfigHistoryResponse(payload)
      expect(result.versions.length).toBe(1)
    })
  })
})
