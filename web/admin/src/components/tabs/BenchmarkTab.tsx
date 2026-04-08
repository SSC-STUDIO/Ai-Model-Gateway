import { memo, useMemo, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import { BarChart } from '../Charts'
import type { BenchmarkResponse } from '../../types'

interface BenchmarkTabProps {
  benchmark: BenchmarkResponse | null
  benchmarkHours: number
  benchmarkModels: string[]
  benchmarkLoading: boolean
  onHoursChange: (hours: number) => void
  onModelsChange: (models: string[]) => void
  onRefresh: () => void
}

function formatUsd(value: number): string {
  if (value < 0.01 && value > 0) return '<$0.01'
  if (value >= 1000) return `$${(value / 1000).toFixed(2)}K`
  return `$${value.toFixed(2)}`
}

const BenchmarkTabComponent = ({
  benchmark,
  benchmarkHours,
  benchmarkModels: _benchmarkModels,
  benchmarkLoading,
  onHoursChange,
  onModelsChange,
  onRefresh,
}: BenchmarkTabProps) => {
  const { t } = useI18n()

  const handleHoursChange = useCallback(
    (e: Event) => {
      const hours = Number((e.currentTarget as HTMLSelectElement).value)
      onHoursChange(hours)
    },
    [onHoursChange]
  )

  const handleModelsChange = useCallback(
    (e: Event) => {
      const value = (e.currentTarget as HTMLInputElement).value
      const models = value
        .split(',')
        .map((m) => m.trim())
        .filter(Boolean)
      onModelsChange(models)
    },
    [onModelsChange]
  )

  const handleExport = useCallback(() => {
    if (!benchmark) return
    const dataStr = JSON.stringify(benchmark, null, 2)
    const blob = new Blob([dataStr], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `benchmark-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
    URL.revokeObjectURL(url)
  }, [benchmark])

  const avgLatencyData = useMemo(() => {
    if (!benchmark?.models) return []
    return benchmark.models.map((m) => ({
      label: m.model,
      value: m.avg_latency_ms,
      color: '#3b82f6',
    }))
  }, [benchmark?.models])

  const p50LatencyData = useMemo(() => {
    if (!benchmark?.models) return []
    return benchmark.models.map((m) => ({
      label: m.model,
      value: m.p50_latency_ms,
      color: '#22c55e',
    }))
  }, [benchmark?.models])

  const p95LatencyData = useMemo(() => {
    if (!benchmark?.models) return []
    return benchmark.models.map((m) => ({
      label: m.model,
      value: m.p95_latency_ms,
      color: '#f59e0b',
    }))
  }, [benchmark?.models])

  const successRateData = useMemo(() => {
    if (!benchmark?.models) return []
    return benchmark.models.map((m) => ({
      label: m.model,
      value: m.success_rate * 100,
      color: m.success_rate >= 0.99 ? '#22c55e' : m.success_rate >= 0.95 ? '#f59e0b' : '#ef4444',
    }))
  }, [benchmark?.models])

  const costData = useMemo(() => {
    if (!benchmark?.models) return []
    return benchmark.models
      .filter((m) => m.estimated_cost_usd > 0)
      .map((m) => ({
        label: m.model,
        value: m.estimated_cost_usd,
        color: '#8b5cf6',
      }))
  }, [benchmark?.models])

  return (
    <section class="panel">
      <h2>{t('benchmark.title')}</h2>

      <div class="panel-subsection">
        <div
          class="benchmark-toolbar"
          style={{
            display: 'flex',
            gap: '16px',
            flexWrap: 'wrap',
            alignItems: 'flex-end',
          }}
        >
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
              placeholder={t('benchmark.modelFilterPlaceholder')}
              onChange={handleModelsChange}
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
          <p class="muted"><span class="loading-dots"></span> {t('benchmark.loading')}</p>
        </div>
      )}

      {benchmark && benchmark.models.length > 0 && (
        <>
          <div class="panel-subsection">
            <h3>{t('benchmark.latencyComparison')}</h3>
            <div class="charts-grid charts-grid-single">
              <BarChart data={avgLatencyData} title={t('benchmark.avgLatency')} unit=" ms" />
            </div>
            <div class="charts-grid" style={{ marginTop: '16px' }}>
              <BarChart
                data={p50LatencyData}
                title={t('benchmark.p50Latency')}
                unit=" ms"
                horizontal
              />
              <BarChart
                data={p95LatencyData}
                title={t('benchmark.p95Latency')}
                unit=" ms"
                horizontal
              />
            </div>
          </div>

          <div class="panel-subsection">
            <h3>{t('benchmark.successRateComparison')}</h3>
            <div class="charts-grid charts-grid-single">
              <BarChart data={successRateData} title={t('benchmark.successRate')} unit="%" />
            </div>
          </div>

          <div class="panel-subsection">
            <h3>{t('benchmark.costComparison')}</h3>
            <div class="charts-grid charts-grid-single">
              <BarChart data={costData} title={t('benchmark.estimatedCost')} unit=" USD" />
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
                  {benchmark.models.map((m) => (
                    <tr key={m.model}>
                      <td>{m.model}</td>
                      <td>{m.requests.toLocaleString()}</td>
                      <td
                        class={
                          m.success_rate >= 0.99
                            ? 'status-ok'
                            : m.success_rate >= 0.95
                              ? 'status-warn'
                              : 'status-error'
                        }
                      >
                        {(m.success_rate * 100).toFixed(2)}%
                      </td>
                      <td>{m.avg_latency_ms.toFixed(1)}ms</td>
                      <td>{m.p50_latency_ms.toFixed(1)}ms</td>
                      <td>{m.p95_latency_ms.toFixed(1)}ms</td>
                      <td>{m.p99_latency_ms.toFixed(1)}ms</td>
                      <td>{m.max_latency_ms.toFixed(1)}ms</td>
                      <td>{(m.input_tokens + m.output_tokens).toLocaleString()}</td>
                      <td class="cost-value">{formatUsd(m.estimated_cost_usd)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}

      {benchmark && benchmark.models.length === 0 && !benchmarkLoading && (
        <div class="panel-subsection">
          <p class="empty-state">{t('empty.noBenchmark')}</p>
        </div>
      )}
    </section>
  )
}

export const BenchmarkTab = memo(BenchmarkTabComponent)
