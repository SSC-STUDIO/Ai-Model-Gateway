import { memo, useCallback, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { ConfigHistoryResponse, ConfigVersionSummary, ControlConfigView } from '../../types'

interface HistoryTabProps {
  controlConfig: ControlConfigView | null
  historyPayload: ConfigHistoryResponse | null
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

const HistoryTabComponent = ({
  controlConfig,
  historyPayload,
  selectedVersion,
  selectedEntry,
  actionLabel,
  actionDisabled,
  onVersionChange,
  onApplySelection,
  busy,
}: HistoryTabProps) => {
  const { t } = useI18n()

  const historyEntries = useMemo(() => historyPayload?.versions ?? [], [historyPayload])

  const handleVersionChange = useCallback(
    (e: Event) => {
      onVersionChange((e.currentTarget as HTMLSelectElement).value)
    },
    [onVersionChange]
  )

  const handleApplySelection = useCallback(() => {
    onApplySelection()
  }, [onApplySelection])

  if (historyEntries.length === 0) {
    return (
      <section class="panel">
        <h2>{t('history.title')}</h2>
        <div class="empty-state-box">
          <div class="empty-state-icon">📋</div>
          <p class="empty-state-title">{t('empty.noHistory')}</p>
        </div>
      </section>
    )
  }

  return (
    <section class="panel">
      <h2>{t('history.title')}</h2>

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

      <div class="panel-subsection split">
        <div>
          <h3>{t('history.entries')}</h3>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('history.revisionId')}</th>
                  <th>{t('history.createdAt')}</th>
                  <th>{t('history.createdBy')}</th>
                  <th>{t('history.status')}</th>
                </tr>
              </thead>
              <tbody>
                {historyEntries.map((entry) => (
                  <tr
                    key={entry.id}
                    class={`data-row${entry.id === selectedVersion ? ' active' : ''}`}
                    tabIndex={0}
                    role="button"
                    aria-pressed={entry.id === selectedVersion}
                    onClick={() => onVersionChange(entry.id)}
                    onKeyDown={(e: KeyboardEvent) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        onVersionChange(entry.id)
                      }
                    }}
                  >
                    <td>{entry.id}</td>
                    <td>{formatDate(entry.created_at)}</td>
                    <td>{entry.created_by ?? '-'}</td>
                    <td>
                      <span class={`status-badge ${entry.is_active ? 'success' : 'neutral'}`}>
                        {entry.is_active ? t('history.activeBadge') : t('history.inactiveBadge')}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        <div>
          <h3>{t('history.currentPolicy')}</h3>
          <div class="metrics-grid" style={{ marginBottom: '16px' }}>
            <article class="metric-card">
              <div class="metric-label">{t('history.currentActiveRevision')}</div>
              <div class="metric-value" style={{ fontSize: '0.95rem', wordBreak: 'break-word' }}>
                {controlConfig?.revision?.id ?? '-'}
              </div>
            </article>
            <article class="metric-card">
              <div class="metric-label">{t('history.publishHistoryLimit')}</div>
              <div class="metric-value" style={{ fontSize: '1rem' }}>
                {typeof controlConfig?.policy.publish_history_limit === 'number'
                  ? controlConfig.policy.publish_history_limit.toLocaleString()
                  : '-'}
              </div>
            </article>
          </div>

          <h3>{t('history.selectedRevision')}</h3>
          {selectedEntry ? (
            <div class="metrics-grid">
              <article class="metric-card">
                <div class="metric-label">{t('history.revisionId')}</div>
                <div class="metric-value" style={{ fontSize: '0.95rem', wordBreak: 'break-word' }}>
                  {selectedEntry.id}
                </div>
              </article>
              <article class="metric-card">
                <div class="metric-label">{t('history.createdAt')}</div>
                <div class="metric-value" style={{ fontSize: '1rem' }}>
                  {formatDate(selectedEntry.created_at)}
                </div>
              </article>
              <article class="metric-card">
                <div class="metric-label">{t('history.createdBy')}</div>
                <div class="metric-value" style={{ fontSize: '1rem' }}>
                  {selectedEntry.created_by ?? '-'}
                </div>
              </article>
              <article class="metric-card">
                <div class="metric-label">{t('history.status')}</div>
                <div class="metric-value" style={{ fontSize: '1rem' }}>
                  {selectedEntry.is_active ? t('history.activeBadge') : t('history.inactiveBadge')}
                </div>
              </article>
              <article class="metric-card" style={{ gridColumn: '1 / -1' }}>
                <div class="metric-label">{t('history.description')}</div>
                <div class="metric-value" style={{ fontSize: '1rem' }}>
                  {selectedEntry.description?.trim() || t('history.noDescription')}
                </div>
              </article>
            </div>
          ) : (
            <div class="empty-state-box">
              <div class="empty-state-icon">🧭</div>
              <p class="empty-state-title">{t('history.selectVersion')}</p>
            </div>
          )}
        </div>
      </div>
    </section>
  )
}

export const HistoryTab = memo(HistoryTabComponent)
