import { useCallback, useMemo, useState } from 'preact/compat'
import { invalidateCache } from './useCachedFetch'
import { useI18n } from '../i18n'
import { useToast } from './useToast'
import { fetchJSON } from '../utils/fetch'
import type { ConfigHistoryResponse, ConfigVersionSummary } from '../types'

function selectedRevisionAction(
  history: ConfigHistoryResponse | null,
  selectedRevision: string
): 'publish' | 'rollback' | null {
  const entries = history?.versions ?? []
  if (!selectedRevision || entries.length === 0) return null

  const activeIndex = entries.findIndex((entry) => entry.is_active)
  const selectedIndex = entries.findIndex((entry) => entry.id === selectedRevision)
  const selectedEntry = selectedIndex >= 0 ? entries[selectedIndex] : null
  if (!selectedEntry || selectedEntry.is_active) return null
  if (activeIndex === -1 || selectedIndex < activeIndex) return 'publish'
  return 'rollback'
}

function getActionLabel(action: 'publish' | 'rollback' | null, t: (key: string) => string): string {
  if (action === 'publish') return t('history.publish')
  if (action === 'rollback') return t('history.rollbackToSelected')
  return t('history.current')
}



export interface HistoryActionsResult {
  action: 'publish' | 'rollback' | null
  actionLabel: string
  currentEntry: ConfigVersionSummary | null
  apply: () => Promise<void>
  busy: boolean
  error: string
}

/**
 * Provides publish/rollback mutation logic for the History tab.
 *
 * Automatically derives whether the selected revision should be published
 * (moved forward) or rolled back (moved backward) relative to the active
 * revision. On success, invalidates the admin cache and refetches
 * overview, status, and history data.
 *
 * @param selectedRevision  Currently selected revision ID.
 * @param historyPayload    Full config history (used to locate active vs selected).
 * @param refetchOverview   Refetch callback for overview data.
 * @param refetchStatus     Refetch callback for status data.
 * @param refetchHistory    Refetch callback for history data.
 * @param onUnauthorized    Callback invoked when the mutation returns 401.
 * @returns Action type, label, current entry, apply handler, and busy/error state.
 */
export function useHistoryActions(
  selectedRevision: string,
  historyPayload: ConfigHistoryResponse | null,
  refetchOverview: () => Promise<unknown>,
  refetchStatus: () => Promise<unknown>,
  refetchHistory: () => Promise<unknown>,
  onUnauthorized?: () => void
): HistoryActionsResult {
  const { t } = useI18n()
  const { addToast } = useToast()
  const [actionBusy, setActionBusy] = useState(false)
  const [actionError, setActionError] = useState('')

  const historyAction = useMemo(
    () => selectedRevisionAction(historyPayload, selectedRevision),
    [historyPayload, selectedRevision]
  )

  const currentHistoryEntry = useMemo<ConfigVersionSummary | null>(
    () => historyPayload?.versions.find((entry) => entry.id === selectedRevision) ?? null,
    [historyPayload, selectedRevision]
  )

  const apply = useCallback(async () => {
    if (!selectedRevision || !historyAction) return

    setActionBusy(true)
    setActionError('')
    try {
      await fetchJSON(
        historyAction === 'publish' ? '/api/admin/config/publish' : '/api/admin/config/rollback',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ revision_id: selectedRevision }),
          onUnauthorized,
        }
      )
      invalidateCache(/\/api\/admin\/(overview|status|config\/history)/)
      await Promise.all([refetchOverview(), refetchStatus(), refetchHistory()])
      addToast(
        historyAction === 'publish' ? t('history.publishSuccess') : t('history.rollbackSuccess'),
        'success'
      )
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setActionError(message)
      addToast(
        `${historyAction === 'publish' ? t('history.publishFailed') : t('history.rollbackFailed')} ${message}`,
        'error'
      )
    } finally {
      setActionBusy(false)
    }
  }, [addToast, historyAction, onUnauthorized, refetchHistory, refetchOverview, refetchStatus, selectedRevision, t])

  const label = useMemo(() => getActionLabel(historyAction, t), [historyAction, t])

  return {
    action: historyAction,
    actionLabel: label,
    currentEntry: currentHistoryEntry,
    apply,
    busy: actionBusy,
    error: actionError,
  }
}
