import { useI18n } from '../../../i18n'
import type { ConfigVersionSummary } from '../../../types'
import { formatAbsoluteTime } from '../../../utils/formatting'
import { Icon } from '../../Icon'

interface ConfigHistoryProps {
  historyEntries: ConfigVersionSummary[]
  selectedVersion: string
  selectedEntry: ConfigVersionSummary | null
  actionLabel: string
  actionDisabled: boolean
  busy: boolean
  onVersionChange: (version: string) => void
  onApplySelection: () => void
}

export function ConfigHistory({
  historyEntries,
  selectedVersion,
  selectedEntry,
  actionLabel,
  actionDisabled,
  busy,
  onVersionChange,
  onApplySelection,
}: ConfigHistoryProps) {
  const { t } = useI18n()

  if (historyEntries.length === 0) {
    return (
      <div class="config-section">
        <h3>{t('history.title')}</h3>
        <div class="empty-state-box">
          <div class="empty-state-icon"><Icon name="history" size={30} /></div>
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
          <select
            value={selectedVersion}
            onChange={(e) => onVersionChange((e.currentTarget as HTMLSelectElement).value)}
          >
            <option value="">{t('history.selectVersion')}</option>
            {historyEntries.map((entry) => (
              <option key={entry.id} value={entry.id}>
                {entry.id}
              </option>
            ))}
          </select>
        </label>

        <button type="button" onClick={onApplySelection} disabled={busy || actionDisabled}>
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
            <span class="config-value">{formatAbsoluteTime(selectedEntry.created_at)}</span>
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