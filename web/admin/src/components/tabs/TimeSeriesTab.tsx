import { memo, useMemo } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { TimeSeriesResponse } from '../../types'

interface TimeSeriesTabProps {
  timeseries: TimeSeriesResponse | null
}

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

const TimeSeriesTabComponent = ({ timeseries }: TimeSeriesTabProps) => {
  const { t } = useI18n()

  const displayData = useMemo(() => timeseries, [timeseries])

  return (
    <section class="panel">
      <h2>{t('timeseries.title')}</h2>
      <pre>{pretty(displayData)}</pre>
    </section>
  )
}

export const TimeSeriesTab = memo(TimeSeriesTabComponent)
