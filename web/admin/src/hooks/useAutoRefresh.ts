import { useEffect } from 'preact/compat'
import type { ControlTabKey } from '../types'

/**
 * Sets up a tab-aware polling interval that refetches only the data
 * relevant to the currently visible tab.
 *
 * Polling is automatically paused when:
 * - `refreshInterval` is 0 (user turned auto-refresh off).
 * - `isPageVisible` is false (tab is backgrounded or hidden).
 *
 * @param refreshInterval  Interval in milliseconds (0 = off).
 * @param isPageVisible    Whether the page is currently visible.
 * @param tab              Active tab — determines which refetchers to call.
 * @param refetchers       Object containing all available refetch callbacks.
 */
export function useAutoRefresh(
  refreshInterval: number,
  isPageVisible: boolean,
  tab: ControlTabKey,
  refetchers: {
    refetchOverview: () => Promise<unknown>
    refetchStatus: () => Promise<unknown>
    refetchTelemetry: () => Promise<unknown>
    refetchTelemetryTimeseries: () => Promise<unknown>
    refetchConfig: () => Promise<unknown>
    refetchHistory: () => Promise<unknown>
    refetchBenchmark: () => Promise<unknown>
  }
): void {
  const { refetchOverview, refetchStatus, refetchTelemetry, refetchTelemetryTimeseries, refetchConfig, refetchHistory, refetchBenchmark } = refetchers

  useEffect(() => {
    if (refreshInterval === 0 || !isPageVisible) return
    const interval = window.setInterval(() => {
      if (tab === 'overview') {
        void refetchOverview()
        void refetchStatus()
      } else if (tab === 'telemetry') {
        void refetchTelemetry()
        void refetchTelemetryTimeseries()
      } else if (tab === 'history') {
        void refetchConfig()
        void refetchHistory()
      } else if (tab === 'benchmark') {
        void refetchBenchmark()
      }
    }, refreshInterval)

    return () => {
      window.clearInterval(interval)
    }
  }, [
    isPageVisible,
    refetchConfig,
    refetchHistory,
    refetchOverview,
    refetchStatus,
    refetchTelemetry,
    refetchTelemetryTimeseries,
    refetchBenchmark,
    refreshInterval,
    tab,
  ])
}
