import { memo, useMemo, useState, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import { DonutChart } from '../Charts'
import { Icon } from '../Icon'
import { ServiceStatePanel } from '../ServiceStatePanel'
import type {
  ControlStatusView,
  DataResponse,
  DonutEntry,
  PricingCost,
  PricingCurrencySummary,
  PricingModelSummary,
  PricingSummary,
} from '../../types'
import { formatUsd, formatInteger } from '../../utils/formatting'

interface PricingTabProps {
  telemetry: DataResponse | null
  status?: ControlStatusView | null
  hours?: string
  onHoursChange?: (hours: string) => void
  onRefreshPricing?: () => Promise<void> | void
  onRetry?: () => Promise<void> | void
  hideTitle?: boolean
}

const PROVIDER_OPTIONS = [
  { key: 'all', label: 'All' },
  { key: 'openai', label: 'OpenAI' },
  { key: 'anthropic', label: 'Anthropic' },
  { key: 'glm', label: 'GLM' },
  { key: 'kimi', label: 'Kimi' },
  { key: 'minimax', label: 'MiniMax' },
  { key: 'gemini', label: 'Gemini' },
  { key: 'step', label: 'Step' },
  { key: 'deepseek', label: 'DeepSeek' },
]

function normalizeCurrency(currency: string | null | undefined): string {
  return currency && currency.trim() ? currency.trim().toUpperCase() : 'USD'
}

function formatMoney(value: number | null | undefined, currency: string | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'

  const normalized = normalizeCurrency(currency)
  if (normalized === 'USD') return formatUsd(value)

  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: normalized,
      maximumFractionDigits: value >= 1000 ? 1 : 2,
    }).format(value)
  } catch {
    return `${value.toFixed(2)} ${normalized}`
  }
}

function costCurrency(cost: PricingCost | null | undefined, pricingCurrency?: string | null): string {
  return normalizeCurrency(cost?.currency ?? pricingCurrency)
}

function costTotal(cost: PricingCost | null | undefined): number {
  if (typeof cost?.total === 'number' && Number.isFinite(cost.total)) return cost.total
  if (typeof cost?.total_usd === 'number' && Number.isFinite(cost.total_usd)) return cost.total_usd

  const prompt = typeof cost?.prompt === 'number' && Number.isFinite(cost.prompt)
    ? cost.prompt
    : cost?.prompt_usd
  const completion = typeof cost?.completion === 'number' && Number.isFinite(cost.completion)
    ? cost.completion
    : cost?.completion_usd

  return (prompt ?? 0) + (completion ?? 0)
}

function primaryPricingTotals(summary: PricingSummary | undefined): PricingCurrencySummary[] {
  if (!summary) return []
  if (Array.isArray(summary.totals_by_currency) && summary.totals_by_currency.length > 0) {
    return summary.totals_by_currency
  }

  const total = typeof summary.total === 'number' && Number.isFinite(summary.total)
    ? summary.total
    : summary.total_usd ?? 0
  const prompt = typeof summary.prompt === 'number' && Number.isFinite(summary.prompt)
    ? summary.prompt
    : summary.prompt_usd ?? 0
  const completion = typeof summary.completion === 'number' && Number.isFinite(summary.completion)
    ? summary.completion
    : summary.completion_usd ?? 0
  const cacheSavings = typeof summary.cache_savings === 'number' && Number.isFinite(summary.cache_savings)
    ? summary.cache_savings
    : summary.cache_savings_usd ?? 0

  if (total <= 0 && prompt <= 0 && completion <= 0 && cacheSavings <= 0) return []

  return [{
    currency: normalizeCurrency(summary.currency),
    prompt,
    completion,
    total,
    cache_savings: cacheSavings,
    priced_models: summary.priced_models,
  }]
}

function pricingToDonut(models: PricingModelSummary[], currency: string): DonutEntry[] {
  const colors = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#64748b']
  if (!Array.isArray(models)) return []
  return models
    .filter((m) => costCurrency(m?.cost, m?.pricing?.currency) === currency && costTotal(m?.cost) > 0)
    .map((m, i) => ({
      label: m?.display_model ?? 'unknown',
      value: costTotal(m?.cost),
      color: colors[i % colors.length],
    }))
}

function matchesProvider(model: PricingModelSummary, provider: string): boolean {
  if (provider === 'all') return true
  const p = model?.provider?.toLowerCase() ?? ''
  const u = model?.upstream?.toLowerCase() ?? ''
  const d = model?.display_model?.toLowerCase() ?? ''
  const e = model?.effective_model?.toLowerCase() ?? ''
  const pm = model?.pricing_model?.toLowerCase() ?? ''
  const haystack = `${p} ${u} ${d} ${e} ${pm}`
  return haystack.includes(provider.toLowerCase())
}

function formatStatusTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return value
  if (new Date(parsed).getUTCFullYear() < 2000) return '-'
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(parsed)
}

function formatPricingTimestamp(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return value
  if (new Date(parsed).getUTCFullYear() < 2000) return '-'
  return new Date(parsed).toLocaleString()
}

const PricingTabComponent = ({ telemetry, status, hours = 'all', onHoursChange, onRefreshPricing, onRetry, hideTitle = false }: PricingTabProps) => {
  const { t } = useI18n()
  const [activeProvider, setActiveProvider] = useState('all')
  const [refreshing, setRefreshing] = useState(false)

  const pricingEconomics = telemetry?.pricing_economics
  const pricingStatus = status?.pricing

  const filteredModels = useMemo(() => {
    const all = pricingEconomics?.models ?? []
    if (activeProvider === 'all') return all
    return all.filter((m) => matchesProvider(m, activeProvider))
  }, [pricingEconomics?.models, activeProvider])

  // Merge same model entries (group by display_model)
  const mergedModels = useMemo(() => {
    type Merged = PricingModelSummary & { _providers?: Set<string> }
    const groups = new Map<string, Merged>()
    for (const m of filteredModels) {
      const modelKey = m?.display_model ?? 'unknown'
      const currency = costCurrency(m?.cost, m?.pricing?.currency)
      const key = `${modelKey}\x00${currency}`
      const existing = groups.get(key)
      if (existing) {
        // Accumulate usage
        const pt = (existing.usage?.prompt_tokens ?? 0) + (m?.usage?.prompt_tokens ?? 0)
        const ct = (existing.usage?.cached_prompt_tokens ?? 0) + (m?.usage?.cached_prompt_tokens ?? 0)
        const cpt = (existing.usage?.completion_tokens ?? 0) + (m?.usage?.completion_tokens ?? 0)
        existing.usage = { prompt_tokens: pt, cached_prompt_tokens: ct, completion_tokens: cpt, total_tokens: pt + cpt }
        // Accumulate cost (same currency guaranteed by key)
        const existingCost = costTotal(existing.cost)
        const newCost = costTotal(m?.cost)
        if (existing.cost) existing.cost.total = existingCost + newCost
        if (m?.provider) existing._providers?.add(m.provider)
        if (m?.upstream) existing._providers?.add(m.upstream)
      } else {
        groups.set(key, {
          ...m,
          _providers: new Set([m?.provider ?? '', m?.upstream ?? ''].filter(Boolean)),
        })
      }
    }
    return Array.from(groups.values())
      .sort((a, b) => costTotal(b?.cost) - costTotal(a?.cost))
  }, [filteredModels])

  const pricingModels = useMemo(
    () => mergedModels.slice(0, 20),
    [mergedModels]
  )
  const pricingTotals = useMemo(
    () => primaryPricingTotals(pricingEconomics?.summary),
    [pricingEconomics?.summary]
  )
  const pricingCharts = useMemo(() => {
    if (!mergedModels.length || pricingTotals.length === 0) return []
    return pricingTotals
      .map((total) => ({
        currency: total.currency,
        data: pricingToDonut(mergedModels, total.currency),
      }))
      .filter((group) => group.data.length > 0)
  }, [pricingTotals, mergedModels])
  const pricingSourceSummary = useMemo(() => {
    const sources = pricingStatus?.sources ?? []
    return sources.reduce(
      (summary, source) => {
        if (!source.enabled) {
          summary.disabled += 1
        } else if ((source.status ?? 'ready') === 'error') {
          summary.error += 1
        } else {
          summary.ready += 1
        }
        return summary
      },
      { ready: 0, error: 0, disabled: 0 }
    )
  }, [pricingStatus?.sources])

  const hasData = pricingTotals.length > 0 || pricingModels.length > 0
  const telemetryUnavailable = status?.telemetry_status && status.telemetry_status !== 'connected'

  const handleProviderChange = useCallback((key: string) => {
    setActiveProvider(key)
  }, [])

  const handleRefreshPricing = useCallback(async () => {
    if (!onRefreshPricing) return
    setRefreshing(true)
    try {
      await onRefreshPricing()
    } finally {
      setRefreshing(false)
    }
  }, [onRefreshPricing])

  if (telemetryUnavailable) {
    return (
      <section class="panel">
        {!hideTitle ? <h2>{t('pricing.title')}</h2> : null}
        <ServiceStatePanel
          icon="pricing"
          title={t('services.telemetryUnavailableTitle')}
          message={t('services.telemetryUnavailableMessage')}
          hint={t('services.telemetryUnavailableHint')}
          detail={status?.telemetry_error}
          actionLabel={onRetry ? t('common.retry') : undefined}
          onAction={onRetry ? () => { void onRetry() } : undefined}
          items={[
            { label: t('header.telemetry'), value: status.telemetry_status ?? t('header.statusUnknown'), tone: status.telemetry_status === 'error' ? 'error' : 'warning' },
            ...(status?.telemetry_last_checked_at ? [{ label: t('services.lastChecked'), value: formatStatusTime(status.telemetry_last_checked_at) }] : []),
          ]}
        />
      </section>
    )
  }

  if (!telemetry) {
    return (
      <section class="panel">
        {!hideTitle ? <h2>{t('pricing.title')}</h2> : null}
        <div class="skeleton-grid" style={{ marginTop: '20px' }}>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
          <div class="skeleton skeleton-card"><div style={{ padding: '18px' }}><div class="skeleton skeleton-label" /><div class="skeleton skeleton-metric" /></div></div>
        </div>
      </section>
    )
  }

  if (!hasData) {
    return (
      <section class="panel">
        {!hideTitle ? <h2>{t('pricing.title')}</h2> : null}
        <div class="empty-state-box">
          <div class="empty-state-icon"><Icon name="pricing" size={30} /></div>
          <p class="empty-state-title">{t('pricing.noData')}</p>
          <p class="empty-state-hint">{t('pricing.noDataHint')}</p>
        </div>
      </section>
    )
  }

  return (
    <section class="panel">
      {!hideTitle ? <h2>{t('pricing.title')}</h2> : null}

      <div class="panel-subsection">
        <div class="timeseries-header" style={{ marginBottom: '12px' }}>
          <h3>{t('pricing.costSummary')}</h3>
          <div class="timeseries-controls">
            {onHoursChange ? (
              <div class="timeseries-selector">
                <span>{t('timeseries.timeRange')}:</span>
                <button type="button" class={`ts-btn${hours === '24' ? ' active' : ''}`} onClick={() => onHoursChange('24')}>
                  {t('benchmark.last24h')}
                </button>
                <button type="button" class={`ts-btn${hours === '168' ? ' active' : ''}`} onClick={() => onHoursChange('168')}>
                  {t('benchmark.last7d')}
                </button>
                <button type="button" class={`ts-btn${hours === 'all' ? ' active' : ''}`} onClick={() => onHoursChange('all')}>
                  {t('pricing.allHistory')}
                </button>
              </div>
            ) : null}
            <div class="timeseries-selector">
              <span>{t('pricing.provider')}:</span>
              {PROVIDER_OPTIONS.map((opt) => (
                <button
                  key={opt.key}
                  type="button"
                  class={`ts-btn${activeProvider === opt.key ? ' active' : ''}`}
                  onClick={() => handleProviderChange(opt.key)}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>
        </div>
        <div class="metrics-grid panel-stagger">
          {pricingTotals.map((total) => (
            <article key={total.currency} class="metric-card">
              <div class="metric-label">{t('pricing.totalCost')} ({total.currency})</div>
              <div class="metric-value">{formatMoney(total.total, total.currency)}</div>
            </article>
          ))}
          {pricingTotals.length === 1 && (
            <>
              <article class="metric-card">
                <div class="metric-label">{t('pricing.promptCost')}</div>
                <div class="metric-value">{formatMoney(pricingTotals[0].prompt, pricingTotals[0].currency)}</div>
              </article>
              <article class="metric-card">
                <div class="metric-label">{t('pricing.completionCost')}</div>
                <div class="metric-value">{formatMoney(pricingTotals[0].completion, pricingTotals[0].currency)}</div>
              </article>
              <article class="metric-card">
                <div class="metric-label">{t('pricing.cacheSavings')}</div>
                <div class="metric-value">{formatMoney(pricingTotals[0].cache_savings, pricingTotals[0].currency)}</div>
              </article>
            </>
          )}
          <article class="metric-card">
            <div class="metric-label">{t('pricing.cachedTokens')}</div>
            <div class="metric-value">
              {(pricingEconomics?.summary.cached_prompt_tokens ?? 0).toLocaleString()}
            </div>
          </article>
          <article class="metric-card">
            <div class="metric-label">{t('pricing.pricedModels')}</div>
            <div class="metric-value">
              {pricingEconomics?.summary.priced_models} / {pricingEconomics?.summary.unpriced_models}
            </div>
          </article>
          {typeof pricingEconomics?.summary.exact_total_usd === 'number' ? (
            <article class="metric-card">
              <div class="metric-label">Exact USD</div>
              <div class="metric-value">{formatUsd(pricingEconomics.summary.exact_total_usd ?? 0)}</div>
            </article>
          ) : null}
          {typeof pricingEconomics?.summary.estimated_total_usd === 'number' ? (
            <article class="metric-card">
              <div class="metric-label">Legacy Estimated USD</div>
              <div class="metric-value">{formatUsd(pricingEconomics.summary.estimated_total_usd ?? 0)}</div>
            </article>
          ) : null}
        </div>
      </div>

      {(pricingStatus?.sources?.length || pricingStatus?.fx) && (
        <div class="panel-subsection">
          <div class="timeseries-header" style={{ marginBottom: '12px' }}>
            <h3>{t('pricing.sources')}</h3>
            {onRefreshPricing ? (
              <button type="button" class="ts-btn active" onClick={() => void handleRefreshPricing()} disabled={refreshing}>
                {refreshing ? t('pricing.refreshing') : t('pricing.refreshNow')}
              </button>
            ) : null}
          </div>
          <div class="metrics-grid pricing-source-grid panel-stagger" style={{ marginBottom: '16px' }}>
            <article class="metric-card">
              <div class="metric-label">{t('pricing.catalogSize')}</div>
              <div class="metric-value">{formatInteger(pricingStatus?.catalog_size ?? 0)}</div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('pricing.readySources')}</div>
              <div class="metric-value">{pricingSourceSummary.ready}</div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('pricing.sourceErrors')}</div>
              <div class="metric-value">{pricingSourceSummary.error}</div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('pricing.lastUpdate')}</div>
              <div class="metric-value">{formatPricingTimestamp(pricingStatus?.updated_at)}</div>
            </article>
          </div>
          {pricingStatus?.sources?.length ? (
            <details class="pricing-source-details">
              <summary>
                <span>{t('pricing.sourceDetails')}</span>
                <span>{pricingStatus.sources.length} {t('charts.donutItems')}</span>
              </summary>
              <div class="table-wrap">
                <table class="pricing-source-table">
                  <thead>
                    <tr>
                      <th>{t('pricing.source')}</th>
                      <th>{t('pricing.status')}</th>
                      <th>{t('pricing.models')}</th>
                      <th>{t('pricing.updated')}</th>
                      <th>{t('pricing.error')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pricingStatus.sources.map((source) => (
                      <tr key={source.id} class="data-row">
                        <td>
                          <div class="table-cell-stack">
                            <span class="table-cell-primary">{source.vendor}</span>
                            <span class="table-cell-secondary mono">{source.id}</span>
                          </div>
                        </td>
                        <td>{source.enabled ? (source.status ?? 'ready') : 'disabled'}</td>
                        <td>{formatInteger(source.model_count ?? 0)}</td>
                        <td>{formatPricingTimestamp(source.updated_at)}</td>
                        <td class="source-error-cell" title={source.last_error || undefined}>
                          {source.last_error ?? '-'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </details>
          ) : null}
        </div>
      )}

      {pricingCharts.length > 0 && (
        <div class="panel-subsection">
          <h3>{t('pricing.costByModel')}</h3>
          <div class="charts-grid panel-stagger">
            {pricingCharts.map((chart) => (
              <DonutChart
                key={chart.currency}
                data={chart.data}
                title={`${t('pricing.costByModel')} (${chart.currency})`}
                singleRowLegend={chart.data.length > 4}
              />
            ))}
          </div>
        </div>
      )}

      {pricingModels.length > 0 && (
        <div class="panel-subsection">
          <h3>{t('pricing.modelBreakdown')}</h3>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('pricing.model')}</th>
                  <th>{t('pricing.promptTokens')}</th>
                  <th>{t('pricing.completionTokens')}</th>
                  <th>{t('pricing.totalCost')}</th>
                </tr>
              </thead>
              <tbody>
                {pricingModels.map((m, idx) => (
                  <tr key={idx} class="data-row">
                    <td>
                      <div class="table-cell-stack" title={m?.display_model ?? 'unknown'}>
                        <span class="table-cell-primary">{m?.display_model ?? 'unknown'}</span>
                        <span class="table-cell-secondary">
                          {m?._providers?.size
                            ? [...m._providers].filter(Boolean).join(' / ')
                            : [m?.effective_model, m?.upstream ?? m?.provider, m?.pricing_model]
                              .filter(Boolean)
                              .join(' / ')}
                        </span>
                      </div>
                    </td>
                    <td>
                      <div class="table-cell-stack mono">
                        <span class="table-cell-primary">{formatInteger(m?.usage?.prompt_tokens ?? 0)}</span>
                        {m?.usage?.cached_prompt_tokens ? (
                          <span class="table-cell-secondary">cached {formatInteger(m.usage.cached_prompt_tokens)}</span>
                        ) : null}
                      </div>
                    </td>
                    <td>{formatInteger(m?.usage?.completion_tokens ?? 0)}</td>
                    <td class="cost-value">{formatMoney(costTotal(m?.cost), costCurrency(m?.cost, m?.pricing?.currency))}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  )
}

export const PricingTab = memo(PricingTabComponent)
