import type { ComponentChildren } from 'preact'
import { useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { ControlStatusView, DataResponse, TimeSeriesResponse } from '../../types'
import { Icon } from '../Icon'
import { WorkspaceBand } from '../WorkspaceBand'
import { PricingTab } from './PricingTab'
import { TelemetryTab } from './TelemetryTab'

interface MonitoringTabProps {
  mode: 'traffic' | 'cost'
  onModeChange: (mode: 'traffic' | 'cost') => void
  telemetry: DataResponse | null
  timeseries: TimeSeriesResponse | null
  status?: ControlStatusView | null
  telemetryHours: string
  onTelemetryHoursChange: (hours: string) => void
  telemetryBucket: string
  onTelemetryBucketChange: (bucket: string) => void
  onRefreshPricing?: () => Promise<void> | void
  onRetry?: () => Promise<void> | void
  refreshControls?: ComponentChildren
}

function gatewayTone(status?: ControlStatusView | null): 'success' | 'warning' | 'error' | 'neutral' {
  if (status?.gateway_status === 'connected') {
    return status.gateway_readiness === 'ready' ? 'success' : 'warning'
  }
  return status?.gateway_status === 'error' ? 'error' : 'neutral'
}

function telemetryTone(status?: ControlStatusView | null): 'success' | 'warning' | 'error' | 'neutral' {
  if (status?.telemetry_status === 'connected') return 'success'
  return status?.telemetry_status === 'error' ? 'error' : 'warning'
}

export function MonitoringTab({
  mode,
  onModeChange,
  telemetry,
  timeseries,
  status,
  telemetryHours,
  onTelemetryHoursChange,
  telemetryBucket,
  onTelemetryBucketChange,
  onRefreshPricing,
  onRetry,
  refreshControls,
}: MonitoringTabProps) {
  const { t } = useI18n()
  const handleRetry = useCallback(() => {
    if (onRetry) void onRetry()
  }, [onRetry])

  const selectMode = useCallback((nextMode: 'traffic' | 'cost') => {
    onModeChange(nextMode)
    if (nextMode === 'cost' && telemetryHours !== 'all') {
      onTelemetryHoursChange('all')
    }
  }, [onModeChange, onTelemetryHoursChange, telemetryHours])

  return (
    <section class="panel workspace-page">
      <div class="workspace-hero workspace-hero-monitoring">
        <div class="workspace-hero-copy">
          <span class="workspace-kicker">{t('tabs.monitoring')}</span>
          <h2 class="workspace-title">{t('tabs.monitoring')}</h2>
          <p class="workspace-subtitle">{[t('telemetry.title'), t('tabs.pricing')].join(' / ')}</p>
        </div>
        <div class="workspace-hero-meta">
          <span class={`status-badge ${gatewayTone(status)}`}>
            {t('header.gateway')}: {status?.gateway_readiness ?? status?.gateway_status ?? t('header.statusUnknown')}
          </span>
          <span class={`status-badge ${telemetryTone(status)}`}>
            {t('header.telemetry')}: {status?.telemetry_status ?? t('header.statusUnknown')}
          </span>
          {typeof status?.active_requests === 'number' ? (
            <span class="status-badge neutral">{t('ops.activeRequests')}: {status.active_requests}</span>
          ) : null}
        </div>
      </div>

      {refreshControls}

      <div class="workspace-nav workspace-nav-segmented" role="tablist" aria-label={t('tabs.monitoring')}>
        <button
          type="button"
          class={`workspace-nav-btn${mode === 'traffic' ? ' active' : ''}`}
          role="tab"
          aria-selected={mode === 'traffic'}
          onClick={() => selectMode('traffic')}
        >
          <Icon name="telemetry" class="tab-icon" />
          <span>{t('telemetry.title')}</span>
        </button>
        <button
          type="button"
          class={`workspace-nav-btn${mode === 'cost' ? ' active' : ''}`}
          role="tab"
          aria-selected={mode === 'cost'}
          onClick={() => selectMode('cost')}
        >
          <Icon name="pricing" class="tab-icon" />
          <span>{t('tabs.pricing')}</span>
        </button>
      </div>

      {mode === 'traffic' ? (
        <WorkspaceBand
          id="monitoring-telemetry"
          icon="telemetry"
          kicker={t('tabs.monitoring')}
          title={t('telemetry.title')}
          detail={[t('telemetry.requests'), t('telemetry.latency'), t('telemetry.successRate')].join(' / ')}
        >
          <TelemetryTab
            telemetry={telemetry}
            timeseries={timeseries}
            hours={telemetryHours}
            onHoursChange={onTelemetryHoursChange}
            bucketMinutes={parseInt(telemetryBucket, 10) || 1}
            onBucketChange={onTelemetryBucketChange}
            telemetryStatus={status?.telemetry_status}
            telemetryError={status?.telemetry_error}
            telemetryLastCheckedAt={status?.telemetry_last_checked_at}
            onRetry={onRetry ? handleRetry : undefined}
            hideTitle
          />
        </WorkspaceBand>
      ) : (
        <WorkspaceBand
          id="monitoring-pricing"
          icon="pricing"
          kicker={t('tabs.monitoring')}
          title={t('tabs.pricing')}
          detail={[t('telemetry.totalCost'), t('telemetry.cacheSavings'), t('pricing.costSummary')].join(' / ')}
        >
          <PricingTab
            telemetry={telemetry}
            status={status}
            hours={telemetryHours}
            onHoursChange={onTelemetryHoursChange}
            onRefreshPricing={onRefreshPricing}
            onRetry={onRetry ? handleRetry : undefined}
            hideTitle
          />
        </WorkspaceBand>
      )}
    </section>
  )
}
