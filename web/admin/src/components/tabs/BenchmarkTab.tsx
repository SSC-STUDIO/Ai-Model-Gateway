import { memo, useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { BenchmarkModel, BenchmarkResponse, ControlStatusView, DonutEntry, VerificationRunDetail, VerificationRunSummary, VerificationRunTarget } from '../../types'
import { formatInteger, formatUsd } from '../../utils/formatting'
import { fetchJSON } from '../../utils/fetch'
import { BarChart, DonutChart } from '../Charts'
import { Icon } from '../Icon'
import { ServiceStatePanel } from '../ServiceStatePanel'
import { BenchmarkVerification } from '../BenchmarkVerification'
import { WorkspaceBand } from '../WorkspaceBand'

interface BenchmarkTabProps {
  benchmark: BenchmarkResponse | null
  modelBenchmark: BenchmarkResponse | null
  loading: boolean
  hours: string
  onHoursChange: (hours: string) => void
  status?: ControlStatusView | null
  canWrite: boolean
  onRefresh: () => void
  onRetry?: () => void
  onUnauthorized?: () => void
}

const BENCHMARK_WINDOW_OPTIONS = [
  { value: '24', key: 'benchmark.last24h' },
  { value: '168', key: 'benchmark.last7d' },
  { value: '720', key: 'benchmark.last30d' },
  { value: 'all', key: 'pricing.allHistory' },
]

const DONUT_COLORS = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
const TERMINAL_CAPABILITY_STATUSES = new Set(['completed', 'incomplete'])
const DIMENSION_ORDER = ['reasoning', 'coding_proxy', 'instruction', 'tool_json', 'stream_protocol']

interface CapabilityRankingRow {
  key: string
  rank: number
  target: VerificationRunTarget
  score: number | null
  dimensions: Array<[string, number]>
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

function toPercent(rate: number): number {
  return rate > 1 ? rate : rate * 100
}

function formatPercent(rate: number): string {
  return `${toPercent(rate).toFixed(1)}%`
}

function formatLatency(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '-'
  return `${value.toFixed(1)} ms`
}

function modelTokens(item: BenchmarkModel): number {
  return item.input_tokens + (item.cached_prompt_tokens ?? 0) + item.output_tokens
}

function benchmarkLabel(item: BenchmarkModel): string {
  return item.upstream || item.label || item.model || '-'
}

function formatScore(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return value.toFixed(1)
}

function formatGap(value: number | undefined, available: boolean): string {
  if (!available || typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return `${value.toFixed(1)} pts`
}

function dimensionLabel(value: string): string {
  return value.replace(/_/g, ' ')
}

function dimensionEntries(scores: Record<string, number> | undefined): Array<[string, number]> {
  if (!scores) return []
  return Object.entries(scores)
    .filter(([, value]) => typeof value === 'number' && Number.isFinite(value))
    .sort(([left], [right]) => {
      const leftIndex = DIMENSION_ORDER.indexOf(left)
      const rightIndex = DIMENSION_ORDER.indexOf(right)
      if (leftIndex !== -1 || rightIndex !== -1) {
        return (leftIndex === -1 ? Number.MAX_SAFE_INTEGER : leftIndex) - (rightIndex === -1 ? Number.MAX_SAFE_INTEGER : rightIndex)
      }
      return left.localeCompare(right)
    })
}

function fallbackOverallScore(target: VerificationRunTarget): number | null {
  if (typeof target.overall_score === 'number' && Number.isFinite(target.overall_score) && target.overall_score > 0) {
    return target.overall_score
  }
  const dimensions = dimensionEntries(target.dimension_scores)
  if (dimensions.length === 0) return null
  return dimensions.reduce((sum, [, value]) => sum + value, 0) / dimensions.length
}

function hasBaselineGap(target: VerificationRunTarget, kind: 'public' | 'vendor'): boolean {
  const snapshotID = kind === 'public' ? target.public_snapshot_id : target.vendor_snapshot_id
  if (!snapshotID) return false
  const missingCode = kind === 'public' ? 'public_baseline_missing_for_model' : 'vendor_baseline_missing_for_model'
  const reasons = target.reason_codes ?? []
  return !reasons.includes(missingCode) && !reasons.includes('no_baseline_rows_for_target')
}

function latestTerminalRun(runs: VerificationRunSummary[]): VerificationRunSummary | null {
  const terminalRuns = runs.filter((run) => TERMINAL_CAPABILITY_STATUSES.has(run.status))
  const completed = terminalRuns.find((run) => run.status === 'completed')
  if (completed) return completed
  return terminalRuns.find((run) => run.status === 'incomplete') ?? null
}

function buildCapabilityRows(run: VerificationRunDetail | null): CapabilityRankingRow[] {
  if (!run?.targets?.length) return []
  const bestByRoute = new Map<string, CapabilityRankingRow>()
  for (const target of run.targets) {
    const key = `${target.provider_id}::${target.public_model}`
    const score = fallbackOverallScore(target)
    const dimensions = dimensionEntries(target.dimension_scores)
    const existing = bestByRoute.get(key)
    if (!existing || (score ?? -1) > (existing.score ?? -1)) {
      bestByRoute.set(key, { key, rank: 0, target, score, dimensions })
    }
  }
  return Array.from(bestByRoute.values())
    .sort((left, right) => {
      const scoreDiff = (right.score ?? -1) - (left.score ?? -1)
      if (scoreDiff !== 0) return scoreDiff
      const completionDiff = (right.target.completion_rate ?? 0) - (left.target.completion_rate ?? 0)
      if (completionDiff !== 0) return completionDiff
      return left.key.localeCompare(right.key)
    })
    .map((row, index) => ({ ...row, rank: index + 1 }))
}

const BenchmarkTabComponent = ({
  benchmark,
  modelBenchmark,
  loading,
  hours,
  onHoursChange,
  status,
  canWrite,
  onRefresh,
  onRetry,
  onUnauthorized,
}: BenchmarkTabProps) => {
  const { t } = useI18n()
  const telemetryUnavailable = Boolean(status?.telemetry_status && status.telemetry_status !== 'connected')
  const [capabilityRuns, setCapabilityRuns] = useState<VerificationRunSummary[]>([])
  const [capabilityRun, setCapabilityRun] = useState<VerificationRunDetail | null>(null)
  const [capabilityLoading, setCapabilityLoading] = useState(false)
  const [capabilityError, setCapabilityError] = useState('')
  const [mode, setMode] = useState<'upstream' | 'capability'>('upstream')

  const loadCapabilityRun = useCallback(async () => {
    setCapabilityLoading(true)
    setCapabilityError('')
    try {
      const runPayload = await fetchJSON<{ runs: VerificationRunSummary[] }>('/api/admin/benchmark/runs?limit=20', { onUnauthorized })
      const runs = runPayload.runs ?? []
      setCapabilityRuns(runs)
      const selectedRun = latestTerminalRun(runs)
      if (!selectedRun) {
        setCapabilityRun(null)
        return
      }
      const detail = await fetchJSON<VerificationRunDetail>(`/api/admin/benchmark/runs/${encodeURIComponent(selectedRun.run_id)}`, { onUnauthorized })
      setCapabilityRun(detail)
    } catch (error) {
      setCapabilityRun(null)
      setCapabilityError(error instanceof Error ? error.message : 'Failed to load benchmark run')
    } finally {
      setCapabilityLoading(false)
    }
  }, [onUnauthorized])

  useEffect(() => {
    if (!telemetryUnavailable) {
      void loadCapabilityRun()
    }
  }, [loadCapabilityRun, telemetryUnavailable])

  const handleRefresh = useCallback(() => {
    onRefresh()
    void loadCapabilityRun()
  }, [loadCapabilityRun, onRefresh])

  const handleRetry = useCallback(() => {
    onRetry?.()
    handleRefresh()
  }, [handleRefresh, onRetry])

  const rows = useMemo(() => {
    const source = benchmark?.benchmarks ?? []
    return source
      .filter((item) => item.requests > 0)
      .slice()
      .sort((left, right) => {
        const successDiff = right.success_rate - left.success_rate
        if (successDiff !== 0) return successDiff
        const latencyDiff = (left.p95_latency_ms || left.avg_latency_ms) - (right.p95_latency_ms || right.avg_latency_ms)
        if (latencyDiff !== 0) return latencyDiff
        return right.requests - left.requests
      })
  }, [benchmark])

  const modelRows = useMemo(() => {
    const source = modelBenchmark?.benchmarks ?? []
    return source
      .filter((item) => item.requests > 0)
      .slice()
      .sort((left, right) => right.requests - left.requests || left.model.localeCompare(right.model))
  }, [modelBenchmark])

  const capabilityRows = useMemo(() => buildCapabilityRows(capabilityRun), [capabilityRun])

  const summary = useMemo(() => {
    const upstreamCount = rows.length
    const requests = rows.reduce((sum, item) => sum + item.requests, 0)
    const successes = rows.reduce((sum, item) => sum + item.successes, 0)
    const avgLatency = requests > 0
      ? rows.reduce((sum, item) => sum + (item.avg_latency_ms * item.requests), 0) / requests
      : 0
    const tokens = rows.reduce((sum, item) => sum + modelTokens(item), 0)
    const estimatedCost = rows.reduce((sum, item) => sum + item.estimated_cost_usd, 0)
    return {
      upstreamCount,
      requests,
      successRate: requests > 0 ? (successes / requests) * 100 : 0,
      avgLatency,
      tokens,
      estimatedCost,
    }
  }, [rows])

  const successRateBars = useMemo(
    () => rows.slice(0, 12).map((item) => ({ label: benchmarkLabel(item), value: toPercent(item.success_rate) })),
    [rows]
  )
  const latencyBars = useMemo(
    () => rows.slice(0, 12).map((item) => ({ label: benchmarkLabel(item), value: item.p95_latency_ms > 0 ? item.p95_latency_ms : item.avg_latency_ms })),
    [rows]
  )
  const costBars = useMemo(
    () => rows.slice(0, 12).map((item) => ({ label: benchmarkLabel(item), value: item.estimated_cost_usd })),
    [rows]
  )
  const requestDonut = useMemo<DonutEntry[]>(
    () => rows.slice(0, 8).map((item, index) => ({ label: benchmarkLabel(item), value: item.requests, color: DONUT_COLORS[index % DONUT_COLORS.length] })),
    [rows]
  )

  if (telemetryUnavailable) {
    return (
      <section class="panel benchmark-reset-page">
        <h2>{t('benchmark.title')}</h2>
        <ServiceStatePanel
          icon="benchmark"
          title={t('services.telemetryUnavailableTitle')}
          message={t('services.telemetryUnavailableMessage')}
          hint={t('services.telemetryUnavailableHint')}
          detail={status?.telemetry_error}
          actionLabel={t('common.retry')}
          onAction={handleRetry}
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
      </section>
    )
  }

  return (
    <section class="panel workspace-page benchmark-reset-page">
      <div class="workspace-hero benchmark-reset-hero">
        <div class="workspace-hero-copy">
          <span class="workspace-kicker">{t('benchmark.title')}</span>
          <h2 class="workspace-title">{t('benchmark.pageTitle')}</h2>
          <p class="workspace-subtitle">{t('benchmark.officialComparisonHint')}</p>
        </div>
        <div class="workspace-hero-meta">
          <span class={`status-badge ${canWrite ? 'success' : 'neutral'}`}>
            {canWrite ? t('ops.adminWrite') : t('ops.viewerReadOnly')}
          </span>
          <span class="status-badge neutral">{t('benchmark.currentWindow')}: {hours === 'all' ? t('pricing.allHistory') : `${hours}h`}</span>
        </div>
      </div>

      <div class="workspace-nav workspace-nav-segmented" role="tablist" aria-label={t('tabs.benchmark')}>
        <button
          type="button"
          class={`workspace-nav-btn${mode === 'upstream' ? ' active' : ''}`}
          role="tab"
          aria-selected={mode === 'upstream'}
          onClick={() => setMode('upstream')}
        >
          <Icon name="telemetry" class="tab-icon" />
          <span>{t('benchmark.modeUpstream')}</span>
        </button>
        <button
          type="button"
          class={`workspace-nav-btn${mode === 'capability' ? ' active' : ''}`}
          role="tab"
          aria-selected={mode === 'capability'}
          onClick={() => setMode('capability')}
        >
          <Icon name="benchmark" class="tab-icon" />
          <span>{t('benchmark.modeCapability')}</span>
        </button>
      </div>

      {mode === 'upstream' ? (
        <WorkspaceBand
          id="benchmark-upstream"
          icon="telemetry"
          kicker={t('benchmark.title')}
          title={t('benchmark.modeUpstream')}
          detail={[t('benchmark.upstreamSuccessRateComparison'), t('benchmark.upstreamLatencyComparison'), t('benchmark.upstreamCostComparison')].join(' / ')}
        >
          <div class="timeseries-header benchmark-workspace-toolbar">
            <h3>{t('benchmark.title')}</h3>
            <div class="timeseries-controls">
              <div class="timeseries-selector">
                <span>{t('benchmark.timeRange')}:</span>
                {BENCHMARK_WINDOW_OPTIONS.map((option) => (
                  <button
                    key={option.value}
                    type="button"
                    class={`ts-btn${hours === option.value ? ' active' : ''}`}
                    onClick={() => onHoursChange(option.value)}
                  >
                    {t(option.key)}
                  </button>
                ))}
              </div>
              <button type="button" class="secondary" onClick={handleRefresh}>
                <Icon name="refresh" />
                {t('benchmark.refresh')}
              </button>
            </div>
          </div>

          {loading && !benchmark ? (
            <div class="skeleton-grid">
              <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
              <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
              <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
              <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
            </div>
          ) : null}

          {!loading && rows.length === 0 ? (
            <div class="empty-state-box">
              <div class="empty-state-icon"><Icon name="benchmark" size={30} /></div>
              <p class="empty-state-title">{t('benchmark.upstreamNoData')}</p>
              <p class="empty-state-hint">{t('benchmark.upstreamPerformanceHint')}</p>
            </div>
          ) : null}

          {rows.length > 0 ? (
            <>
              <div class="benchmark-summary-grid">
                <article class="benchmark-summary-card">
                  <span>{t('benchmark.observedUpstreams')}</span>
                  <strong>{formatInteger(summary.upstreamCount)}</strong>
                </article>
                <article class="benchmark-summary-card">
                  <span>{t('benchmark.requests')}</span>
                  <strong>{formatInteger(summary.requests)}</strong>
                </article>
                <article class="benchmark-summary-card">
                  <span>{t('benchmark.successRate')}</span>
                  <strong>{formatPercent(summary.successRate)}</strong>
                </article>
                <article class="benchmark-summary-card">
                  <span>{t('benchmark.avgLatency')}</span>
                  <strong>{formatLatency(summary.avgLatency)}</strong>
                </article>
                <article class="benchmark-summary-card">
                  <span>{t('benchmark.tokens')}</span>
                  <strong>{formatInteger(summary.tokens)}</strong>
                </article>
                <article class="benchmark-summary-card">
                  <span>{t('benchmark.estimatedCost')}</span>
                  <strong>{formatUsd(summary.estimatedCost)}</strong>
                </article>
              </div>

              <div class="benchmark-charts-grid">
                <div class="benchmark-chart benchmark-chart-half">
                  <BarChart data={successRateBars} title={t('benchmark.upstreamSuccessRateComparison')} unit="%" horizontal />
                </div>
                <div class="benchmark-chart benchmark-chart-half">
                  <BarChart data={latencyBars} title={t('benchmark.upstreamLatencyComparison')} unit=" ms" horizontal />
                </div>
                <div class="benchmark-chart benchmark-chart-half">
                  <DonutChart data={requestDonut} title={t('benchmark.requests')} singleRowLegend={requestDonut.length > 4} />
                </div>
                <div class="benchmark-chart benchmark-chart-half">
                  <BarChart data={costBars} title={t('benchmark.upstreamCostComparison')} unit=" $" horizontal />
                </div>
              </div>

              <section class="panel-subsection benchmark-table-section">
                <div class="panel-header">
                  <h3>{t('benchmark.upstreamRanking')}</h3>
                </div>
                <div class="table-wrap benchmark-table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>{t('benchmark.upstream')}</th>
                        <th>{t('benchmark.requests')}</th>
                        <th>{t('benchmark.successRate')}</th>
                        <th>{t('benchmark.avgLatency')}</th>
                        <th>{t('benchmark.p95Latency')}</th>
                        <th>{t('benchmark.tokens')}</th>
                        <th>{t('benchmark.estimatedCost')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {rows.map((item) => (
                        <tr key={benchmarkLabel(item)}>
                          <td>{benchmarkLabel(item)}</td>
                          <td>{formatInteger(item.requests)}</td>
                          <td>{formatPercent(item.success_rate)}</td>
                          <td>{formatLatency(item.avg_latency_ms)}</td>
                          <td>{formatLatency(item.p95_latency_ms)}</td>
                          <td>{formatInteger(modelTokens(item))}</td>
                          <td>{formatUsd(item.estimated_cost_usd)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>

              <section class="panel-subsection benchmark-table-section">
                <div class="panel-header">
                  <h3>{t('benchmark.modelDetails')}</h3>
                </div>
                {modelRows.length === 0 ? (
                  <div class="benchmark-inline-state">{t('benchmark.noModelDetails')}</div>
                ) : (
                  <div class="table-wrap benchmark-table-wrap">
                    <table>
                      <thead>
                        <tr>
                          <th>{t('benchmark.model')}</th>
                          <th>{t('benchmark.requests')}</th>
                          <th>{t('benchmark.successRate')}</th>
                          <th>{t('benchmark.avgLatency')}</th>
                          <th>{t('benchmark.p95Latency')}</th>
                          <th>{t('benchmark.tokens')}</th>
                          <th>{t('benchmark.estimatedCost')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {modelRows.map((item) => (
                          <tr key={item.model}>
                            <td>{item.model}</td>
                            <td>{formatInteger(item.requests)}</td>
                            <td>{formatPercent(item.success_rate)}</td>
                            <td>{formatLatency(item.avg_latency_ms)}</td>
                            <td>{formatLatency(item.p95_latency_ms)}</td>
                            <td>{formatInteger(modelTokens(item))}</td>
                            <td>{formatUsd(item.estimated_cost_usd)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </section>
            </>
          ) : null}
        </WorkspaceBand>
      ) : (
        <WorkspaceBand
          id="benchmark-capability"
          icon="benchmark"
          kicker={t('benchmark.title')}
          title={t('benchmark.modeCapability')}
          detail={[t('benchmark.officialComparison'), t('benchmark.scoreMatrix'), t('benchmark.verification.title')].join(' / ')}
        >
          <div class="timeseries-header benchmark-workspace-toolbar">
            <div>
              <h3>{t('benchmark.capability.title')}</h3>
              <p class="benchmark-toolbar-copy">{t('benchmark.capability.subtitle')}</p>
            </div>
            <button type="button" class="secondary" onClick={handleRefresh}>
              <Icon name="refresh" />
              {t('benchmark.refresh')}
            </button>
          </div>

          <BenchmarkVerification canWrite={canWrite} onRunStarted={loadCapabilityRun} onUnauthorized={onUnauthorized} />

          <section class="panel-subsection benchmark-table-section benchmark-capability-section">
            <div class="panel-header">
              <div>
                <h3>{t('benchmark.capability.title')}</h3>
                <p>{capabilityRun ? `${t('benchmark.capability.run')}: ${capabilityRun.run_id}` : t('benchmark.capability.subtitle')}</p>
              </div>
              <div class="benchmark-capability-meta">
                {capabilityRun ? <span class="status-badge neutral">{t('benchmark.capability.status')}: {capabilityRun.status}</span> : null}
                {capabilityRun?.completed_at ? <span class="status-badge neutral">{formatStatusTime(capabilityRun.completed_at)}</span> : null}
              </div>
            </div>
            {capabilityLoading && !capabilityRun ? (
              <div class="benchmark-inline-state">{t('benchmark.capability.loading')}</div>
            ) : capabilityError ? (
              <div class="empty-state-box">
                <div class="empty-state-icon"><Icon name="benchmark" size={30} /></div>
                <p class="empty-state-title">{t('benchmark.capability.error')}</p>
                <p class="empty-state-hint">{capabilityError}</p>
              </div>
            ) : capabilityRuns.length === 0 ? (
              <div class="empty-state-box">
                <div class="empty-state-icon"><Icon name="benchmark" size={30} /></div>
                <p class="empty-state-title">{t('benchmark.capability.noRuns')}</p>
                <p class="empty-state-hint">{t('benchmark.capability.noRunsHint')}</p>
              </div>
            ) : !capabilityRun ? (
              <div class="empty-state-box">
                <div class="empty-state-icon"><Icon name="benchmark" size={30} /></div>
                <p class="empty-state-title">{t('benchmark.capability.noTerminalRun')}</p>
                <p class="empty-state-hint">{t('benchmark.capability.noTerminalRunHint')}</p>
              </div>
            ) : capabilityRows.length === 0 ? (
              <div class="empty-state-box">
                <div class="empty-state-icon"><Icon name="benchmark" size={30} /></div>
                <p class="empty-state-title">{t('benchmark.capability.noTargets')}</p>
                <p class="empty-state-hint">{t('benchmark.capability.noTargetsHint')}</p>
              </div>
            ) : (
              <div class="table-wrap benchmark-table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>{t('benchmark.capability.rank')}</th>
                      <th>{t('benchmark.upstream')}</th>
                      <th>{t('benchmark.model')}</th>
                      <th>{t('benchmark.capability.score')}</th>
                      <th>{t('benchmark.capability.completion')}</th>
                      <th>{t('benchmark.capability.verdict')}</th>
                      <th>{t('benchmark.capability.publicGap')}</th>
                      <th>{t('benchmark.capability.vendorGap')}</th>
                      <th>{t('benchmark.capability.dimensions')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {capabilityRows.map((row) => (
                      <tr key={row.key}>
                        <td>#{row.rank}</td>
                        <td>{row.target.provider_id || '-'}</td>
                        <td>{row.target.public_model || '-'}</td>
                        <td><span class="benchmark-score-pill">{formatScore(row.score)}</span></td>
                        <td>{formatPercent(row.target.completion_rate ?? 0)}</td>
                        <td><span class={`status-badge ${row.target.verdict === 'normal' ? 'success' : row.target.verdict === 'highly_suspect' ? 'error' : 'warning'}`}>{row.target.verdict || '-'}</span></td>
                        <td>{formatGap(row.target.public_gap, hasBaselineGap(row.target, 'public'))}</td>
                        <td>{formatGap(row.target.vendor_gap, hasBaselineGap(row.target, 'vendor'))}</td>
                        <td>
                          <div class="benchmark-dimension-list">
                            {row.dimensions.slice(0, 5).map(([dimension, score]) => (
                              <span key={dimension}>{dimensionLabel(dimension)} {formatScore(score)}</span>
                            ))}
                            {row.dimensions.length === 0 ? <span>-</span> : null}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </WorkspaceBand>
      )}

    </section>
  )
}

export const BenchmarkTab = memo(BenchmarkTabComponent)
