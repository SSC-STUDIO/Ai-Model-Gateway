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

interface DiffLine {
  kind: 'context' | 'add' | 'remove'
  text: string
}

interface DiffResponse {
  version?: { id: string; filename: string; created_at: string; size: number }
  summary?: { added_lines: number; removed_lines: number; changed_blocks: number }
  lines?: DiffLine[]
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

function parseDiffResponse(raw: unknown): DiffResponse | null {
  if (!raw || typeof raw !== 'object') return null
  const obj = raw as AnyRecord
  if (!Array.isArray(obj.lines)) return null
  return obj as unknown as DiffResponse
}

function DiffViewer({ diff }: { diff: DiffResponse }) {
  const { t } = useI18n()
  const lines = diff.lines ?? []
  const summary = diff.summary

  let addNum = 0
  let removeNum = 0

  return (
    <div>
      {summary && (
        <div class="diff-summary">
          <span class="diff-stat diff-stat-add">+{summary.added_lines} {t('history.linesAdded')}</span>
          <span class="diff-stat diff-stat-remove">-{summary.removed_lines} {t('history.linesRemoved')}</span>
          <span class="diff-stat">{summary.changed_blocks} {t('history.blocks')}</span>
        </div>
      )}
      <div class="diff-container">
        {lines.map((line, i) => {
          let leftNum = ''
          let rightNum = ''
          if (line.kind === 'context') {
            removeNum++
            addNum++
            leftNum = String(removeNum)
            rightNum = String(addNum)
          } else if (line.kind === 'remove') {
            removeNum++
            leftNum = String(removeNum)
          } else if (line.kind === 'add') {
            addNum++
            rightNum = String(addNum)
          }
          return (
            <div key={i} class={`diff-line diff-line-${line.kind}`}>
              <span class="diff-line-num diff-line-num-old">{leftNum}</span>
              <span class="diff-line-num diff-line-num-new">{rightNum}</span>
              <span class="diff-line-prefix">{line.kind === 'add' ? '+' : line.kind === 'remove' ? '-' : ' '}</span>
              <span class="diff-line-text">{line.text}</span>
            </div>
          )
        })}
      </div>
    </div>
  )
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

  const parsedDiff = useMemo(() => parseDiffResponse(historyDiff), [historyDiff])

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
          {parsedDiff ? <DiffViewer diff={parsedDiff} /> : <pre>{pretty(historyDiff)}</pre>}
        </div>
      </div>
    </section>
  )
}

export const HistoryTab = memo(HistoryTabComponent)
