import { useMemo } from 'preact/compat'
import { useCachedFetch } from './useCachedFetch'
import type { AnyRecord, ConfigHistoryResponse, ControlConfigView, DataResponse, TimeSeriesResponse, ControlTabKey } from '../types'
import {
  benchmarkURL,
  normalizeBenchmarkResponse,
  normalizeControlConfigResponse,
  normalizeConfigHistoryResponse,
  normalizeControlStatus,
  normalizeOverviewResponse,
  normalizeTelemetryResponse,
  normalizeTimeSeriesResponse,
  telemetryTimeseriesURL,
  telemetryURL,
  logsURL,
} from '../utils/controlApi'

export interface ControlDataResult {
  status: ReturnType<typeof normalizeControlStatus>
  overview: AnyRecord | null
  telemetry: DataResponse | null
  telemetryTimeseries: TimeSeriesResponse | null
  controlConfig: ControlConfigView | null
  historyPayload: ConfigHistoryResponse
  benchmark: ReturnType<typeof normalizeBenchmarkResponse>
  benchmarkLoading: boolean
  logs: DataResponse | null
  configError: Error | null
  overviewError: Error | null
  statusError: Error | null
  telemetryError: Error | null
  telemetryTimeseriesError: Error | null
  historyError: Error | null
  benchmarkError: Error | null
  logsError: Error | null
  refetchOverview: () => Promise<unknown>
  refetchStatus: () => Promise<unknown>
  refetchTelemetry: () => Promise<unknown>
  refetchTelemetryTimeseries: () => Promise<unknown>
  refetchConfig: () => Promise<unknown>
  refetchHistory: () => Promise<unknown>
  refetchBenchmark: () => Promise<unknown>
  refetchLogs: () => Promise<unknown>
}

/**
 * Orchestrates all admin data fetching for the current tab.
 *
 * Each API call is lazy: requests are only made when the tab is active
 * (`enabled && tab === '<name>'`) or when `enabled` is true globally.
 * All responses are normalized through `controlApi` helpers.
 *
 * @param tab            Currently active tab — controls which endpoints are active.
 * @param telemetryHours Hours window passed to telemetry endpoints.
 * @param benchmarkHours Hours window passed to the benchmark endpoint.
 * @param benchmarkModels Model filter list for the benchmark endpoint.
 * @param enabled        Global kill-switch (e.g. false while auth is resolving).
 * @param onUnauthorized Callback invoked when any request returns 401.
 * @returns Normalized data, loading flags, errors, and refetchers for every endpoint.
 */
export function useControlData(
  tab: ControlTabKey,
  telemetryHours: string,
  telemetryBucket: string,
  benchmarkHours: number,
  benchmarkModels: string[],
  logsHours: string,
  enabled = true,
  onUnauthorized?: () => void
): ControlDataResult {
  const telemetryDataURL = useMemo(() => telemetryURL(telemetryHours), [telemetryHours])
  const telemetryChartURL = useMemo(
    () => telemetryTimeseriesURL(telemetryHours, telemetryBucket),
    [telemetryHours, telemetryBucket]
  )
  const logsDataURL = useMemo(() => logsURL(logsHours), [logsHours])

  const { data: rawOverview, error: overviewError, refetch: refetchOverview } = useCachedFetch<unknown>(
    '/api/admin/overview',
    { ttl: 30000, enabled, onUnauthorized }
  )
  const { data: rawStatus, error: statusError, refetch: refetchStatus } = useCachedFetch<unknown>(
    '/api/admin/status',
    { ttl: 30000, enabled, onUnauthorized }
  )
  const telemetryEnabled = enabled && (tab === 'telemetry' || tab === 'pricing')
  const { data: rawTelemetry, error: telemetryError, refetch: refetchTelemetry } = useCachedFetch<unknown>(
    telemetryDataURL,
    { ttl: 30000, enabled: telemetryEnabled, onUnauthorized }
  )
  const { data: rawTelemetryTimeseries, error: telemetryTimeseriesError, refetch: refetchTelemetryTimeseries } =
    useCachedFetch<unknown>(telemetryChartURL, { ttl: 30000, enabled: telemetryEnabled, onUnauthorized })
  const { data: rawConfig, error: configError, refetch: refetchConfig } = useCachedFetch<unknown>(
    '/api/admin/config',
    { ttl: 60000, enabled: enabled && tab === 'config', onUnauthorized }
  )
  const { data: rawHistory, error: historyError, refetch: refetchHistory } = useCachedFetch<unknown>(
    '/api/admin/config/history',
    { ttl: 60000, enabled: enabled && tab === 'config', onUnauthorized }
  )

  const benchmarkDataURL = useMemo(() => benchmarkURL(benchmarkHours, benchmarkModels), [benchmarkHours, benchmarkModels])
  const { data: rawBenchmark, error: benchmarkError, loading: benchmarkLoading, refetch: refetchBenchmark } =
    useCachedFetch<unknown>(benchmarkDataURL, { ttl: 30000, enabled: enabled && tab === 'benchmark', onUnauthorized })

  const { data: rawLogs, error: logsError, refetch: refetchLogs } = useCachedFetch<unknown>(
    logsDataURL,
    { ttl: 30000, enabled: enabled && tab === 'logs', onUnauthorized }
  )

  const status = useMemo(() => normalizeControlStatus(rawStatus), [rawStatus])
  const overview = useMemo<AnyRecord | null>(() => normalizeOverviewResponse(rawOverview, rawStatus), [rawOverview, rawStatus])
  const telemetry = useMemo<DataResponse | null>(() => normalizeTelemetryResponse(rawTelemetry), [rawTelemetry])
  const telemetryTimeseries = useMemo<TimeSeriesResponse | null>(
    () => normalizeTimeSeriesResponse(rawTelemetryTimeseries),
    [rawTelemetryTimeseries]
  )
  const controlConfig = useMemo<ControlConfigView | null>(() => normalizeControlConfigResponse(rawConfig), [rawConfig])
  const historyPayload = useMemo<ConfigHistoryResponse>(() => normalizeConfigHistoryResponse(rawHistory), [rawHistory])
  const benchmark = useMemo(() => normalizeBenchmarkResponse(rawBenchmark), [rawBenchmark])
  const logs = useMemo<DataResponse | null>(() => normalizeTelemetryResponse(rawLogs), [rawLogs])

  return {
    status,
    overview,
    telemetry,
    telemetryTimeseries,
    controlConfig,
    historyPayload,
    benchmark,
    benchmarkLoading,
    logs,
    configError,
    overviewError,
    statusError,
    telemetryError,
    telemetryTimeseriesError,
    historyError,
    benchmarkError,
    logsError,
    refetchOverview,
    refetchStatus,
    refetchTelemetry,
    refetchTelemetryTimeseries,
    refetchConfig,
    refetchHistory,
    refetchBenchmark,
    refetchLogs,
  }
}
