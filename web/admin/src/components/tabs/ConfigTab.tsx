import { memo, useCallback, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { ControlConfigView, ProviderHealthView, ConfigHistoryResponse, ConfigVersionSummary, ConfigSubTab } from '../../types'

interface ConfigTabProps {
  controlConfig: ControlConfigView | null
  historyPayload: ConfigHistoryResponse
  providerHealth: ProviderHealthView[]
  snapshotInfo: {
    active_snapshot_id?: string
    provider_count?: number
    enabled_provider_count?: number
    unhealthy_provider_count?: number
    cooldown_provider_count?: number
  } | null
  selectedVersion: string
  selectedEntry: ConfigVersionSummary | null
  actionLabel: string
  actionDisabled: boolean
  onVersionChange: (version: string) => void
  onApplySelection: () => void
  busy: boolean
}

function formatDate(value: string | undefined): string {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}

const SUB_TABS: { key: ConfigSubTab; icon: string }[] = [
  { key: 'current', icon: '📄' },
  { key: 'editor', icon: '📝' },
  { key: 'visual', icon: '🎨' },
  { key: 'history', icon: '📋' },
]

const ConfigTabComponent = ({
  controlConfig,
  historyPayload,
  providerHealth,
  snapshotInfo,
  selectedVersion,
  selectedEntry,
  actionLabel,
  actionDisabled,
  onVersionChange,
  onApplySelection,
  busy,
}: ConfigTabProps) => {
  const { t } = useI18n()
  const [subTab, setSubTab] = useState<ConfigSubTab>('current')
  const [jsonValue, setJsonValue] = useState('')

  const revision = controlConfig?.revision
  const policy = controlConfig?.policy

  const historyEntries = useMemo(() => historyPayload?.versions ?? [], [historyPayload])

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

  const handleVersionChange = useCallback(
    (e: Event) => {
      onVersionChange((e.currentTarget as HTMLSelectElement).value)
    },
    [onVersionChange]
  )

  const handleApplySelection = useCallback(() => {
    onApplySelection()
  }, [onApplySelection])

  const handleJsonChange = useCallback((e: Event) => {
    setJsonValue((e.currentTarget as HTMLTextAreaElement).value)
  }, [])

  const renderSubTab = () => {
    switch (subTab) {
      case 'current':
        return (
          <>
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
          </>
        )

      case 'editor':
        return (
          <div class="config-section">
            <h3>{t('config.jsonEditor')}</h3>
            <div class="config-editor-wrapper">
              <textarea
                class="config-json-editor"
                value={jsonValue}
                onChange={handleJsonChange}
                placeholder={t('config.jsonPlaceholder')}
                spellcheck={false}
              />
              <div class="config-editor-actions">
                <button type="button" class="primary" disabled={busy || !jsonValue.trim()}>
                  {t('config.applyChanges')}
                </button>
                <button type="button" disabled={busy}>
                  {t('config.validate')}
                </button>
              </div>
            </div>
          </div>
        )

      case 'visual':
        return (
          <div class="config-section">
            <h3>{t('config.visualEditor')}</h3>
            <div class="visual-config-placeholder">
              <div class="empty-state-box">
                <div class="empty-state-icon">🎨</div>
                <p class="empty-state-title">{t('config.visualEditorComingSoon')}</p>
                <p class="empty-state-hint">{t('config.visualEditorHint')}</p>
              </div>
            </div>
          </div>
        )

      case 'history':
        if (historyEntries.length === 0) {
          return (
            <div class="config-section">
              <h3>{t('history.title')}</h3>
              <div class="empty-state-box">
                <div class="empty-state-icon">📋</div>
                <p class="empty-state-title">{t('empty.noHistory')}</p>
              </div>
            </div>
          )
        }

        return (
          <div class="config-section">
            <h3>{t('history.title')}</h3>

            <div class="history-toolbar">
              <label>
                {t('history.versionLabel')}
                <select value={selectedVersion} onChange={handleVersionChange}>
                  <option value="">{t('history.selectVersion')}</option>
                  {historyEntries.map((entry) => (
                    <option key={entry.id} value={entry.id}>
                      {entry.id}
                    </option>
                  ))}
                </select>
              </label>

              <button type="button" onClick={handleApplySelection} disabled={busy || actionDisabled}>
                {actionLabel}
              </button>
            </div>

            {selectedEntry && (
              <div class="config-card">
                <div class="config-row">
                  <span class="config-label">{t('history.revisionId')}</span>
                  <span class="config-value code">{selectedEntry.id}</span>
                </div>
                <div class="config-row">
                  <span class="config-label">{t('history.createdAt')}</span>
                  <span class="config-value">{formatDate(selectedEntry.created_at)}</span>
                </div>
                <div class="config-row">
                  <span class="config-label">{t('history.createdBy')}</span>
                  <span class="config-value">{selectedEntry.created_by || 'system'}</span>
                </div>
                {selectedEntry.description && (
                  <div class="config-row">
                    <span class="config-label">{t('history.description')}</span>
                    <span class="config-value">{selectedEntry.description}</span>
                  </div>
                )}
                <div class="config-row">
                  <span class="config-label">{t('history.status')}</span>
                  <span class="config-value">
                    <span class={`status-badge ${selectedEntry.is_active ? 'success' : ''}`}>
                      {selectedEntry.is_active ? t('history.activeBadge') : t('history.inactiveBadge')}
                    </span>
                  </span>
                </div>
              </div>
            )}
          </div>
        )
    }
  }

  return (
    <section class="panel">
      <div class="config-header">
        <h2>{t('config.title')}</h2>
        <div class="config-sub-tabs">
          {SUB_TABS.map((item) => (
            <button
              key={item.key}
              type="button"
              class={`config-sub-tab${subTab === item.key ? ' active' : ''}`}
              onClick={() => setSubTab(item.key)}
            >
              {item.icon} {t(`config.subTab.${item.key}`)}
            </button>
          ))}
        </div>
      </div>

      {renderSubTab()}
    </section>
  )
}

export const ConfigTab = memo(ConfigTabComponent)
