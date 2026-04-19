import { memo, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { ControlConfigView, ProviderHealthView } from '../../types'

interface ConfigTabProps {
  controlConfig: ControlConfigView | null
  providerHealth: ProviderHealthView[]
  snapshotInfo: {
    active_snapshot_id?: string
    provider_count?: number
    enabled_provider_count?: number
    unhealthy_provider_count?: number
    cooldown_provider_count?: number
  } | null
}

function formatDate(value: string | undefined): string {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}

const ConfigTabComponent = ({ controlConfig, providerHealth, snapshotInfo }: ConfigTabProps) => {
  const { t } = useI18n()

  const revision = controlConfig?.revision
  const policy = controlConfig?.policy

  const providerStatusList = useMemo(() => {
    if (!providerHealth || providerHealth.length === 0) return []
    return providerHealth.map((p) => ({
      name: p.name,
      status: p.status,
      healthy: p.healthy,
      latency: p.latency_ms,
      failures: p.consecutive_failures,
    }))
  }, [providerHealth])

  return (
    <section class="panel">
      <h2>{t('config.title')}</h2>

      {/* Current Revision */}
      <div class="config-section">
        <h3>{t('config.currentRevision')}</h3>
        {revision ? (
          <div class="config-card">
            <div class="config-row">
              <span class="config-label">{t('config.revisionId')}</span>
              <span class="config-value code">{revision.id}</span>
            </div>
            <div class="config-row">
              <span class="config-label">{t('config.createdAt')}</span>
              <span class="config-value">{formatDate(revision.created_at)}</span>
            </div>
            <div class="config-row">
              <span class="config-label">{t('config.createdBy')}</span>
              <span class="config-value">{revision.created_by || 'system'}</span>
            </div>
            {revision.description && (
              <div class="config-row">
                <span class="config-label">{t('config.description')}</span>
                <span class="config-value">{revision.description}</span>
              </div>
            )}
            {revision.is_active && (
              <div class="config-row">
                <span class="config-label">{t('config.status')}</span>
                <span class="config-value">
                  <span class="status-badge success">{t('config.active')}</span>
                </span>
              </div>
            )}
          </div>
        ) : (
          <div class="empty-state-box">
            <p class="empty-state-title">{t('config.noRevision')}</p>
          </div>
        )}
      </div>

      {/* Runtime Info */}
      {snapshotInfo && (
        <div class="config-section">
          <h3>{t('config.runtimeInfo')}</h3>
          <div class="config-card">
            <div class="config-row">
              <span class="config-label">{t('config.activeSnapshot')}</span>
              <span class="config-value code">{snapshotInfo.active_snapshot_id || '-'}</span>
            </div>
            <div class="config-row">
              <span class="config-label">{t('config.providerCount')}</span>
              <span class="config-value">{snapshotInfo.provider_count ?? 0}</span>
            </div>
            <div class="config-row">
              <span class="config-label">{t('overview.providersHealthy')}</span>
              <span class="config-value">{snapshotInfo.enabled_provider_count ?? 0}</span>
            </div>
            <div class="config-row">
              <span class="config-label">{t('overview.providersBlocked')}</span>
              <span class="config-value">{snapshotInfo.unhealthy_provider_count ?? 0}</span>
            </div>
            <div class="config-row">
              <span class="config-label">{t('overview.providersCooldown')}</span>
              <span class="config-value">{snapshotInfo.cooldown_provider_count ?? 0}</span>
            </div>
          </div>
        </div>
      )}

      {/* Policy */}
      {policy && (
        <div class="config-section">
          <h3>{t('config.policy')}</h3>
          <div class="config-card">
            <div class="config-row">
              <span class="config-label">{t('config.publishHistoryLimit')}</span>
              <span class="config-value">{policy.publish_history_limit}</span>
            </div>
          </div>
        </div>
      )}

      {/* Provider Health */}
      {providerStatusList.length > 0 && (
        <div class="config-section">
          <h3>{t('config.providerHealth')}</h3>
          <div class="config-table-wrapper">
            <table class="config-table">
              <thead>
                <tr>
                  <th>{t('config.providerName')}</th>
                  <th>{t('config.healthStatus')}</th>
                  <th>{t('config.latency')}</th>
                  <th>{t('config.failures')}</th>
                </tr>
              </thead>
              <tbody>
                {providerStatusList.map((p) => (
                  <tr key={p.name}>
                    <td class="code">{p.name}</td>
                    <td>
                      <span class={`status-badge ${p.healthy ? 'success' : 'warning'}`}>
                        {p.status}
                      </span>
                    </td>
                    <td>{p.latency ? `${p.latency}ms` : '-'}</td>
                    <td>{p.failures}</td>
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

export const ConfigTab = memo(ConfigTabComponent)
