import { memo, useState, useEffect, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { AuditLogResponse } from '../../types'

interface AuditTabProps {
  enabled: boolean
}

const PAGE_SIZE = 50

const AuditTabComponent = ({ enabled }: AuditTabProps) => {
  const { t } = useI18n()
  const [data, setData] = useState<AuditLogResponse | null>(null)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(false)

  const fetchPage = useCallback(async (pageOffset: number) => {
    setLoading(true)
    try {
      const resp = await fetch(`/api/admin/audit-log?limit=${PAGE_SIZE}&offset=${pageOffset}`, {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      })
      if (resp.ok) {
        setData(await resp.json() as AuditLogResponse)
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (enabled) void fetchPage(offset)
  }, [enabled, offset, fetchPage])

  const totalPages = data ? Math.max(1, Math.ceil(data.total / PAGE_SIZE)) : 1
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  return (
    <section class="panel">
      <h2>{t('audit.title')}</h2>
      {loading && <p class="muted">{t('audit.loading')}</p>}
      {data && data.items.length === 0 && !loading && (
        <p class="muted">{t('audit.empty')}</p>
      )}
      {data && data.items.length > 0 && (
        <>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('audit.time')}</th>
                  <th>{t('audit.action')}</th>
                  <th>{t('audit.actor')}</th>
                  <th>{t('audit.role')}</th>
                  <th>{t('audit.details')}</th>
                  <th>{t('audit.ip')}</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((entry) => (
                  <tr key={entry.id}>
                    <td>{entry.timestamp}</td>
                    <td>{entry.action}</td>
                    <td>{entry.actor || '-'}</td>
                    <td>{entry.role || '-'}</td>
                    <td>{entry.details || '-'}</td>
                    <td>{entry.source_ip || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div class="pagination">
            <button
              type="button"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
            >
              {t('audit.prev')}
            </button>
            <span>{t('audit.page')} {currentPage} / {totalPages} ({data.total} {t('audit.totalEntries')})</span>
            <button
              type="button"
              disabled={offset + PAGE_SIZE >= data.total}
              onClick={() => setOffset(offset + PAGE_SIZE)}
            >
              {t('audit.next')}
            </button>
          </div>
        </>
      )}
    </section>
  )
}

export const AuditTab = memo(AuditTabComponent)