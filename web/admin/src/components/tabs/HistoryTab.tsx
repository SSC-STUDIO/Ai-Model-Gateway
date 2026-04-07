import { memo, useMemo, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { AnyRecord } from '../../types'

interface HistoryTabProps {
  historyPayload: unknown
  selectedVersion: string
  historyDiff: unknown
  onVersionChange: (version: string) => void
  onRollback: () => void
  busy: boolean
}

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

function versionIdOf(item: unknown): string {
  if (!item || typeof item !== 'object') return ''
  const record = item as AnyRecord
  const raw = record.version_id ?? record.versionId ?? record.id
  return typeof raw === 'string' ? raw : ''
}

const HistoryTabComponent = ({
  historyPayload,
  selectedVersion,
  historyDiff,
  onVersionChange,
  onRollback,
  busy,
}: HistoryTabProps) => {
  const { t } = useI18n()

  const historyEntries = useMemo(() => {
    if (Array.isArray(historyPayload)) return historyPayload
    if (historyPayload && typeof historyPayload === 'object') {
      const items = (historyPayload as AnyRecord).items
      if (Array.isArray(items)) return items
    }
    return [] as unknown[]
  }, [historyPayload])

  const handleVersionChange = useCallback(
    (e: Event) => {
      onVersionChange((e.currentTarget as HTMLSelectElement).value)
    },
    [onVersionChange]
  )

  const handleRollback = useCallback(() => {
    onRollback()
  }, [onRollback])

  return (
    <section class="panel">
      <h2>{t('history.title')}</h2>
      <div class="history-toolbar">
        <label>
          {t('history.versionLabel')}
          <select value={selectedVersion} onChange={handleVersionChange}>
            <option value="">{t('history.selectVersion')}</option>
            {historyEntries.map((entry) => {
              const versionId = versionIdOf(entry)
              return (
                <option key={versionId || pretty(entry)} value={versionId}>
                  {versionId || pretty(entry)}
                </option>
              )
            })}
          </select>
        </label>
        <button type="button" onClick={handleRollback} disabled={busy || selectedVersion === ''}>
          {t('history.rollback')}
        </button>
      </div>
      <div class="panel-subsection split">
        <div>
          <h3>{t('history.entries')}</h3>
          <pre>{pretty(historyPayload)}</pre>
        </div>
        <div>
          <h3>{t('history.diff')}</h3>
          <pre>{pretty(historyDiff)}</pre>
        </div>
      </div>
    </section>
  )
}

export const HistoryTab = memo(HistoryTabComponent)
