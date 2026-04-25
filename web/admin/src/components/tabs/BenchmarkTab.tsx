import { memo, useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { BenchmarkResponse, ControlStatusView } from '../../types'
import { formatUsd } from '../../utils/formatting'
import { BenchmarkVerification } from '../BenchmarkVerification'
import { BarChart } from '../Charts'
import { Icon } from '../Icon'
import { ServiceStatePanel } from '../ServiceStatePanel'

const BENCHMARK_CHART_COLORS = {
  latency: '#2b4f7c',
  success: '#2f7b5b',
  warning: '#a5622a',
  danger: '#c24a3d',
  cost: '#168257',
}

interface BenchmarkTabProps {
  benchmark: BenchmarkResponse | null
  status?: ControlStatusView | null
  benchmarkHours: number
  benchmarkModels: string[]
  benchmarkLoading: boolean
  canWrite: boolean
  onHoursChange: (hours: number) => void
  onModelsChange: (models: string[]) => void
  onRefresh: () => void
  onRetry?: () => void
  onUnauthorized?: () => void
}

function formatStatusTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return value
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(parsed)
}

const BenchmarkTabComponent = ({
  benchmark,
  status,
  benchmarkHours,
  benchmarkModels,
  benchmarkLoading,
  canWrite,
  onHoursChange,
  onModelsChange,
  onRefresh,
  onRetry,
  onUnauthorized,
}: BenchmarkTabProps) => {
  const { t } = useI18n()
  const [modelInput, setModelInput] = useState(() => benchmarkModels.join(', '))
  const telemetryUnavailable = Boolean(status?.telemetry_status && status.telemetry_status !== 'connected')
  const hasBenchmarkData = (benchmark?.benchmarks?.length ?? 0) > 0

  useEffect(() => {
    setModelInput(benchmarkModels.join(', '))
  }, [benchmarkModels])

  const handleHoursChange = useCallback(
    (e: Event) => {
      onHoursChange(Number((e.currentTarget as HTMLSelectElement).value))
    },
    [onHoursChange]
  )

  const handleModelsChange = useCallback(
    (e: Event) => {
      const value = (e.currentTarget as HTMLInputElement).value
      setModelInput(value)
      onModelsChange(
        value
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean)
      )
    },
    [onModelsChange]
  )

  const handleExport = useCallback(() => {
    if (!benchmark) return
    const blob = new Blob([JSON.stringify(benchmark, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `benchmark-${new Date().toISOString().slice(0, 10)}.json`
    link.click()
    URL.revokeObjectURL(url)
  }, [benchmark])

  const avgLatencyData = useMemo(() => {
    if (!benchmark?.benchmarks) return []
    return benchmark.benchmarks.map((item) => ({ label: item.model, value: item.avg_latency_ms, color: BENCHMARK_CHART_COLORS.latency }))
  }, [benchmark?.benchmarks])

  const p50LatencyData = useMemo(() => {
    if (!benchmark?.benchmarks) return []
    return benchmark.benchmarks.map((item) => ({ label: item.model, value: item.p50_latency_ms, color: BENCHMARK_CHART_COLORS.latency }))
  }, [benchmark?.benchmarks])

  const p95LatencyData = useMemo(() => {
    if (!benchmark?.benchmarks) return []
    return benchmark.benchmarks.map((item) => ({ label: item.model, value: item.p95_latency_ms, color: BENCHMARK_CHART_COLORS.warning }))
  }, [benchmark?.benchmarks])

  const successRateData = useMemo(() => {
    if (!benchmark?.benchmarks) return []
    return benchmark.benchmarks.map((item) => {
      const value = item.success_rate * 100
      return {
        label: item.model,
        value,
        color: value >= 99 ? BENCHMARK_CHART_COLORS.success : value >= 95 ? BENCHMARK_CHART_COLORS.warning : BENCHMARK_CHART_COLORS.danger,
      }
    })
  }, [benchmark?.benchmarks])

  const costData = useMemo(() => {
    if (!benchmark?.benchmarks) return []
    return benchmark.benchmarks
      .filter((item) => item.estimated_cost_usd > 0)
      .map((item) => ({ label: item.model, value: item.estimated_cost_usd, color: BENCHMARK_CHART_COLORS.cost }))
  }, [benchmark?.benchmarks])

  return (
    <section class="panel">
      <h2>{t('benchmark.title')}</h2>

      <div class="panel-subsection">
        <div class="benchmark-toolbar">
          <label>
            {t('benchmark.timeRange')}
            <select value={benchmarkHours} onChange={handleHoursChange}>
              <option value={1}>{t('benchmark.last1h')}</option>
              <option value={6}>{t('benchmark.last6h')}</option>
              <option value={24}>{t('benchmark.last24h')}</option>
              <option value={168}>{t('benchmark.last7d')}</option>
            </select>
          </label>

          <label>
            {t('benchmark.modelFilter')}
            <input
              type="text"
              value={modelInput}
              placeholder={t('benchmark.modelFilterPlaceholder')}
              onInput={handleModelsChange}
            />
          </label>

          <button type="button" onClick={onRefresh} disabled={benchmarkLoading}>
            {benchmarkLoading ? t('benchmark.loading') : t('benchmark.refresh')}
          </button>

          <button type="button" onClick={handleExport} disabled={!benchmark || benchmarkLoading}>
            {t('benchmark.export')}
          </button>
        </div>
      </div>

      {benchmarkLoading && (
        <div class="panel-subsection">
          <div class="skeleton-grid-auto">
            <div class="skeleton chart-skeleton" />
            <div class="skeleton chart-skeleton" />
            <div class="skeleton chart-skeleton" />
            <div class="skeleton chart-skeleton" />
          </div>
        </div>
      )}

      {!benchmark && !benchmarkLoading && telemetryUnavailable && (
        <div class="panel-subsection">
          <ServiceStatePanel
            icon="benchmark"
            title={t('services.telemetryUnavailableTitle')}
            message={t('services.telemetryUnavailableMessage')}
            hint={t('services.telemetryUnavailableHint')}
            detail={status?.telemetry_error}
            actionLabel={t('common.retry')}
            onAction={onRetry ?? onRefresh}
            items={[
              {
                label: t('header.telemetry'),
                value: status?.telemetry_status ?? t('header.statusUnknown'),
                tone: status?.telemetry_status === 'error' ? 'error' : 'warning',
              },
              ...(status?.telemetry_last_checked_at
                ? [{ label: t('services.lastChecked'), value: formatStatusTime(status.telemetry_last_checked_at) }]
                : []),
            ]}
          />
        </div>
      )}

      {!benchmark && !benchmarkLoading && !telemetryUnavailable && (
        <div class="panel-subsection">
          <div class="empty-state-box">
            <div class="empty-state-icon"><Icon name="benchmark" size={30} /></div>
            <p class="empty-state-title">{t('benchmark.noData')}</p>
          </div>
        </div>
      )}

      {hasBenchmarkData && (
        <>
          <div class="panel-subsection">
            <div class="benchmark-charts-grid">
              <div class="benchmark-chart benchmark-chart-half">
                <BarChart data={avgLatencyData} title={t('benchmark.avgLatency')} unit=" ms" />
              </div>
              <div class="benchmark-chart benchmark-chart-half">
                <BarChart data={successRateData} title={t('benchmark.successRate')} unit="%" />
              </div>
              <div class="benchmark-chart benchmark-chart-half">
                <BarChart data={p50LatencyData} title={t('benchmark.p50Latency')} unit=" ms" horizontal />
              </div>
              <div class="benchmark-chart benchmark-chart-half">
                <BarChart data={p95LatencyData} title={t('benchmark.p95Latency')} unit=" ms" horizontal />
              </div>
              {costData.length > 0 && (
                <div class="benchmark-chart benchmark-chart-full">
                  <BarChart data={costData} title={t('benchmark.estimatedCost')} unit=" USD" />
                </div>
              )}
            </div>
          </div>

          <div class="panel-subsection">
            <h3>{t('benchmark.comparisonTable')}</h3>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>{t('benchmark.model')}</th>
                    <th>{t('benchmark.requests')}</th>
                    <th>{t('benchmark.successRate')}</th>
                    <th>{t('benchmark.avgLatency')}</th>
                    <th>{t('benchmark.p50Latency')}</th>
                    <th>{t('benchmark.p95Latency')}</th>
                    <th>{t('benchmark.p99Latency')}</th>
                    <th>{t('benchmark.maxLatency')}</th>
                    <th>{t('benchmark.tokens')}</th>
                    <th>{t('benchmark.estimatedCost')}</th>
                  </tr>
                </thead>
                <tbody>
                  {benchmark?.benchmarks.map((item) => (
                    <tr key={item.model}>
                      <td>{item.model}</td>
                      <td>{(item.requests ?? 0).toLocaleString()}</td>
                      <td
                        class={
                          (item.success_rate ?? 0) >= 0.99
                            ? 'status-ok'
                            : (item.success_rate ?? 0) >= 0.95
                              ? 'status-warn'
                              : 'status-error'
                        }
                      >
                        {((item.success_rate ?? 0) * 100).toFixed(2)}%
                      </td>
                      <td>{(item.avg_latency_ms ?? 0).toFixed(1)}ms</td>
                      <td>{(item.p50_latency_ms ?? 0).toFixed(1)}ms</td>
                      <td>{(item.p95_latency_ms ?? 0).toFixed(1)}ms</td>
                      <td>{(item.p99_latency_ms ?? 0).toFixed(1)}ms</td>
                      <td>{(item.max_latency_ms ?? 0).toFixed(1)}ms</td>
                      <td>{((item.input_tokens ?? 0) + (item.output_tokens ?? 0)).toLocaleString()}</td>
                      <td class="cost-value">{formatUsd(item.estimated_cost_usd ?? 0)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}

      {benchmark && !hasBenchmarkData && !benchmarkLoading && (
        <div class="panel-subsection">
          <div class="empty-state-box">
            <div class="empty-state-icon"><Icon name="benchmark" size={30} /></div>
            <p class="empty-state-title">{t('empty.noBenchmark')}</p>
          </div>
        </div>
      )}

      <BenchmarkVerification canWrite={canWrite} onUnauthorized={onUnauthorized} />
    </section>
  )
}

export const BenchmarkTab = memo(BenchmarkTabComponent)
