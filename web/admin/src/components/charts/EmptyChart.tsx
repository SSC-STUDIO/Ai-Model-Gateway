import { useI18n } from '../../i18n'
import { Icon } from '../Icon'

export function EmptyChart({
  title,
  message = 'No data available',
  hint = 'This chart will update automatically once data arrives.',
}: {
  title: string
  message?: string
  hint?: string
}) {
  const { t } = useI18n()
  return (
    <div class="chart-container" role="img" aria-label={t('charts.emptyAriaLabel')}>
      <div class="chart-header">
        <h3>{title}</h3>
      </div>
      <div class="chart-body chart-empty">
        <div class="empty-state-icon" style={{ width: '56px', height: '56px' }}><Icon name="chart" size={26} /></div>
        <div class="chart-empty-title">{message}</div>
        <div class="chart-empty-hint">{hint}</div>
      </div>
    </div>
  )
}
