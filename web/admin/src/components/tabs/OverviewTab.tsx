import { memo, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { AnyRecord } from '../../types'

interface OverviewTabProps {
  overview: AnyRecord | null
}

function metricValue(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}

const OverviewTabComponent = ({ overview }: OverviewTabProps) => {
  const { t } = useI18n()

  const runtimeEntries = useMemo(() => {
    const runtime = overview?.runtime as AnyRecord | undefined
    return runtime ? Object.entries(runtime) : []
  }, [overview])

  const availableModels = useMemo(() => {
    const models = overview?.available_models
    return Array.isArray(models) ? models : []
  }, [overview])

  const windowKeys = useMemo(() => ['last_1m', 'last_5m', 'last_1h', 'last_24h'] as const, [])

  if (!overview) {
    return (
      <section class="panel">
        <h2>{t('overview.title')}</h2>
        <p class="muted">{t('overview.loading')}</p>
      </section>
    )
  }

  return (
    <section class="panel">
      <h2>{t('overview.title')}</h2>
      <div class="panel-subsection">
        <h3>{t('overview.windows')}</h3>
        <div class="metrics-grid">
          {windowKeys.map((key) => {
            const windowData = overview[key] as AnyRecord | undefined
            if (!windowData) return null
            return (
              <article key={key} class="config-card">
                <h3>{t(`overview.${key}`)}</h3>
                <div class="metrics-grid">
                  <div class="metric-card">
                    <div class="metric-label">{t('overview.requests')}</div>
                    <div class="metric-value">{metricValue(windowData.requests)}</div>
                  </div>
                  <div class="metric-card">
                    <div class="metric-label">{t('overview.successes')}</div>
                    <div class="metric-value">{metricValue(windowData.successes)}</div>
                  </div>
                  <div class="metric-card">
                    <div class="metric-label">{t('overview.failures')}</div>
                    <div class="metric-value">{metricValue(windowData.failures)}</div>
                  </div>
                  <div class="metric-card">
                    <div class="metric-label">{t('overview.avgLatency')}</div>
                    <div class="metric-value">
                      {typeof windowData.avg_latency_ms === 'number'
                        ? `${windowData.avg_latency_ms.toFixed(1)}ms`
                        : metricValue(windowData.avg_latency_ms)}
                    </div>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      </div>

      <div class="panel-subsection split">
        <div>
          <h3>{t('overview.runtime')}</h3>
          <div class="metrics-grid">
            {runtimeEntries.map(([key, value]) => (
              <article key={key} class="metric-card">
                <div class="metric-label">{key}</div>
                <div class="metric-value">{metricValue(value)}</div>
              </article>
            ))}
          </div>
        </div>
        <div>
          <h3>{t('overview.availableModels')}</h3>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('overview.model')}</th>
                </tr>
              </thead>
              <tbody>
                {availableModels.length > 0 ? (
                  availableModels.map((model) => (
                    <tr key={String(model)}>
                      <td>{String(model)}</td>
                    </tr>
                  ))
                ) : (
                  <tr>
                    <td>{t('overview.noModels')}</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </section>
  )
}

export const OverviewTab = memo(OverviewTabComponent)
