import type { ComponentChildren } from 'preact'
import { useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { ControlStatusView, DataResponse, TimeSeriesResponse } from '../../types'
import { Icon, type IconName } from '../Icon'
import { PricingTab } from './PricingTab'
import { TelemetryTab } from './TelemetryTab'

interface MonitoringTabProps {
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

interface WorkspaceBandProps {
  id: string
  icon: IconName
  kicker: string
  title: string
  detail: string
  children: ComponentChildren
}

function WorkspaceBand({ id, icon, kicker, title, detail, children }: WorkspaceBandProps) {
  return (
    <section id={id} class="workspace-band">
      <div class="workspace-band-header">
        <div class="workspace-band-copy">
          <span class="workspace-kicker">
            <Icon name={icon} class="workspace-kicker-icon" />
            {kicker}
          </span>
          <h3 class="workspace-band-title">{title}</h3>
          <p class="workspace-band-detail">{detail}</p>
        </div>
      </div>
      {children}
    </section>
  )
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
  const [mode, setMode] = useState<'traffic' | 'cost'>('traffic')

  return (
    <section class="panel workspace-page">
      <div class="workspace-hero workspace-hero-monitoring">
        <div class="workspace-hero-copy">
          <span class="workspace-kicker">{t('tabs.monitoring')}</span>
          <h2 class="workspace-title">{t('tabs.monitoring')}</h2>
          <p class="workspace-subtitle">{[t('telemetry.title'), t('tabs.pricing')].join(' · ')}</p>
        </div>
        <div class="workspace-hero-meta">
          <span class={`status-badge ${gatewayTone(status)}`}>
            {t('header.gateway')}: {status?.gateway_readiness ?? status?.gateway_status ?? t('header.statusUnknown')}
          </span>
          <span class={`status-badge ${telemetryTone(status)}`}>
            {t('header.telemetry')}: {status?.telemetry_status ?? t('header.statusUnknown')}
          </span>
          {typeof status?.active_requests === 'number' ? (
            <span class="status-badge neutral">Active: {status.active_requests}</span>
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
          onClick={() => setMode('traffic')}
        >
          <Icon name="telemetry" class="tab-icon" />
          <span>{t('telemetry.title')}</span>
        </button>
        <button
          type="button"
          class={`workspace-nav-btn${mode === 'cost' ? ' active' : ''}`}
          role="tab"
          aria-selected={mode === 'cost'}
          onClick={() => {
            setMode('cost')
            if (telemetryHours !== 'all') {
              onTelemetryHoursChange('all')
            }
          }}
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
          detail={[t('telemetry.requests'), t('telemetry.latency'), t('telemetry.successRate')].join(' · ')}
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
            onRetry={onRetry ? () => { void onRetry() } : undefined}
          />
        </WorkspaceBand>
      ) : (
        <WorkspaceBand
          id="monitoring-pricing"
          icon="pricing"
          kicker={t('tabs.monitoring')}
          title={t('tabs.pricing')}
          detail={[t('telemetry.totalCost'), t('telemetry.cacheSavings'), t('pricing.costSummary')].join(' · ')}
        >
          <PricingTab
            telemetry={telemetry}
            status={status}
            hours={telemetryHours}
            onHoursChange={onTelemetryHoursChange}
            onRefreshPricing={onRefreshPricing}
            onRetry={onRetry ? () => { void onRetry() } : undefined}
          />
        </WorkspaceBand>
      )}
    </section>
  )
}
