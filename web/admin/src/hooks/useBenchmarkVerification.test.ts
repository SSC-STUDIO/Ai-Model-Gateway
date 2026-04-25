import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/preact'
import { statusTone, useBenchmarkVerification } from './useBenchmarkVerification'

function jsonResponse(payload: unknown) {
  return {
    ok: true,
    text: vi.fn().mockResolvedValue(JSON.stringify(payload)),
  }
}

describe('useBenchmarkVerification', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the latest run and falls back to legacy telemetry matching', async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)

      if (url === '/api/admin/benchmark/baselines') {
        return jsonResponse({
          snapshots: [
            {
              snapshot_id: 'public-1',
              kind: 'public_standard',
              source_name: 'public baseline',
              captured_at: '2026-04-01T00:00:00Z',
              imported_at: '2026-04-02T00:00:00Z',
              row_count: 12,
            },
          ],
        })
      }

      if (url === '/api/admin/benchmark/runs?limit=20') {
        return jsonResponse({
          runs: [
            {
              run_id: 'run-1',
              status: 'completed',
              suite_version: 'general_protocol_v1',
              protocol: 'auto',
              started_at: '2026-04-25T00:00:00Z',
              target_count: 1,
              completed_targets: 1,
            },
          ],
        })
      }

      if (url === '/api/admin/benchmark/runs/run-1') {
        return jsonResponse({
          run_id: 'run-1',
          status: 'completed',
          suite_version: 'general_protocol_v1',
          protocol: 'auto',
          started_at: '2026-04-25T00:00:00Z',
          target_count: 1,
          completed_targets: 1,
          targets: [
            {
              target_id: 'target-1',
              run_id: 'run-1',
              status: 'pass',
              provider_id: 'provider-a',
              public_model: 'gpt-4o',
              effective_model: 'vendor-model',
              protocol: 'auto',
              suite_version: 'general_protocol_v1',
              cases: [],
              started_at: '2026-04-25T00:00:00Z',
            },
          ],
        })
      }

      if (url.includes('/api/admin/benchmark/runs/run-1/telemetry?') && url.includes('target_id=target-1')) {
        return jsonResponse({ events: [] })
      }

      if (url.includes('/api/admin/benchmark/runs/run-1/telemetry?') && url.includes('providers=provider-a') && url.includes('models=gpt-4o')) {
        return jsonResponse({
          events: [
            {
              request_id: 'req-wrong',
              timestamp: '2026-04-25T00:00:30Z',
              path: '/v1/chat/completions',
              requested_model: 'gpt-4o',
              effective_model: 'other-vendor-model',
              provider: 'provider-a',
              route_mode: 'proxy',
              status_code: 200,
              latency_ms: 80,
              attempts: 1,
              input_tokens: 4,
              cached_prompt_tokens: 0,
              output_tokens: 8,
              pricing_status: 'priced',
              total_cost_usd: 0.002,
              synthetic_kind: 'benchmark',
              benchmark_run_id: 'run-1',
              benchmark_target_id: '',
              benchmark_case_id: 'wrong-case',
              error: '',
            },
            {
              request_id: 'req-1',
              timestamp: '2026-04-25T00:01:00Z',
              path: '/v1/chat/completions',
              requested_model: 'gpt-4o',
              effective_model: 'vendor-model',
              provider: 'provider-a',
              route_mode: 'proxy',
              status_code: 200,
              latency_ms: 120,
              attempts: 1,
              input_tokens: 10,
              cached_prompt_tokens: 0,
              output_tokens: 20,
              pricing_status: 'priced',
              total_cost_usd: 0.01,
              synthetic_kind: 'benchmark',
              benchmark_run_id: 'run-1',
              benchmark_target_id: '',
              benchmark_case_id: 'case-1',
              error: '',
            },
          ],
        })
      }

      throw new Error(`unexpected fetch ${url}`)
    })

    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useBenchmarkVerification({}))

    await waitFor(() => expect(result.current.selectedRun?.run_id).toBe('run-1'))
    await waitFor(() => expect(result.current.publicSnapshotID).toBe('public-1'))
    await waitFor(() => expect(result.current.selectedRunTelemetryIdentityMode).toBe('legacy'))

    expect(result.current.selectedTarget?.target_id).toBe('target-1')
    expect(result.current.selectedTelemetryRequests).toHaveLength(1)
    expect(result.current.selectedTelemetryRequests[0]?.RequestID).toBe('req-1')
  })

  it('keeps explicit empty baseline selection across refreshes', async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)

      if (url === '/api/admin/benchmark/baselines') {
        return jsonResponse({
          snapshots: [
            {
              snapshot_id: 'public-1',
              kind: 'public_standard',
              source_name: 'public baseline',
              captured_at: '2026-04-01T00:00:00Z',
              imported_at: '2026-04-02T00:00:00Z',
              row_count: 12,
            },
          ],
        })
      }

      if (url === '/api/admin/benchmark/runs?limit=20') {
        return jsonResponse({
          runs: [
            {
              run_id: 'run-1',
              status: 'completed',
              suite_version: 'general_protocol_v1',
              protocol: 'auto',
              started_at: '2026-04-25T00:00:00Z',
              target_count: 0,
              completed_targets: 0,
            },
          ],
        })
      }

      if (url === '/api/admin/benchmark/runs/run-1') {
        return jsonResponse({
          run_id: 'run-1',
          status: 'completed',
          suite_version: 'general_protocol_v1',
          protocol: 'auto',
          started_at: '2026-04-25T00:00:00Z',
          target_count: 0,
          completed_targets: 0,
          targets: [],
        })
      }

      if (url.includes('/api/admin/benchmark/runs/run-1/telemetry?')) {
        return jsonResponse({ events: [] })
      }

      throw new Error(`unexpected fetch ${url}`)
    })

    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useBenchmarkVerification({}))

    await waitFor(() => expect(result.current.publicSnapshotID).toBe('public-1'))

    await act(async () => {
      result.current.setPublicSnapshotID('')
    })
    await waitFor(() => expect(result.current.publicSnapshotID).toBe(''))

    await act(async () => {
      await result.current.loadVerification()
    })

    expect(result.current.publicSnapshotID).toBe('')
  })

  it('keeps a newly started run selected after refreshing verification lists', async () => {
    let created = false
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)

      if (url === '/api/admin/benchmark/baselines') {
        return jsonResponse({ snapshots: [] })
      }

      if (url === '/api/admin/benchmark/runs?limit=20') {
        return jsonResponse({
          runs: [
            {
              run_id: 'run-old',
              status: 'completed',
              suite_version: 'general_protocol_v1',
              protocol: 'auto',
              started_at: '2026-04-24T00:00:00Z',
              target_count: 1,
              completed_targets: 1,
            },
            ...(created ? [{
              run_id: 'run-new',
              status: 'completed',
              suite_version: 'general_protocol_v1',
              protocol: 'auto',
              started_at: '2026-04-25T00:00:00Z',
              target_count: 1,
              completed_targets: 1,
            }] : []),
          ],
        })
      }

      if (url === '/api/admin/benchmark/runs' && init?.method === 'POST') {
        created = true
        return jsonResponse({
          run_id: 'run-new',
          status: 'completed',
          suite_version: 'general_protocol_v1',
          protocol: 'auto',
          started_at: '2026-04-25T00:00:00Z',
          target_count: 1,
          completed_targets: 1,
          targets: [],
        })
      }

      if (url === '/api/admin/benchmark/runs/run-old' || url === '/api/admin/benchmark/runs/run-new') {
        const runID = url.endsWith('run-new') ? 'run-new' : 'run-old'
        return jsonResponse({
          run_id: runID,
          status: 'completed',
          suite_version: 'general_protocol_v1',
          protocol: 'auto',
          started_at: '2026-04-25T00:00:00Z',
          target_count: 1,
          completed_targets: 1,
          targets: [],
        })
      }

      if (url.includes('/api/admin/benchmark/runs/') && url.includes('/telemetry?')) {
        return jsonResponse({ events: [] })
      }

      throw new Error(`unexpected fetch ${url}`)
    })

    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useBenchmarkVerification({}))

    await waitFor(() => expect(result.current.selectedRun?.run_id).toBe('run-old'))

    await act(async () => {
      await result.current.startVerification()
    })

    await waitFor(() => expect(result.current.selectedRun?.run_id).toBe('run-new'))
  })

  it('clears the selected baseline file and bumps the input key after import', async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)

      if (url === '/api/admin/benchmark/baselines') {
        return jsonResponse({ snapshots: [] })
      }

      if (url === '/api/admin/benchmark/runs?limit=20') {
        return jsonResponse({ runs: [] })
      }

      if (url === '/api/admin/benchmark/baselines/import' && init?.method === 'POST') {
        return jsonResponse({ ok: true })
      }

      throw new Error(`unexpected fetch ${url}`)
    })

    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useBenchmarkVerification({}))

    await waitFor(() => expect(result.current.loadingLists).toBe(false))

    await act(async () => {
      result.current.setBaselineFile(new File(['[]'], 'baseline.json', { type: 'application/json' }))
    })

    expect(result.current.baselineFile?.name).toBe('baseline.json')
    expect(result.current.baselineFileInputKey).toBe(0)

    await act(async () => {
      await result.current.importBaseline()
    })

    expect(result.current.baselineFile).toBeNull()
    expect(result.current.baselineFileInputKey).toBe(1)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/admin/benchmark/baselines/import',
      expect.objectContaining({ method: 'POST' })
    )
  })
})

describe('benchmark verification status mapping', () => {
  it('maps verification verdicts to visual tones', () => {
    expect(statusTone('normal')).toBe('success')
    expect(statusTone('suspect')).toBe('warning')
    expect(statusTone('incomplete')).toBe('warning')
    expect(statusTone('cancelled')).toBe('warning')
    expect(statusTone('highly_suspect')).toBe('error')
  })
})
