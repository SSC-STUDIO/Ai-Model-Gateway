import { memo, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'

interface ProbeProvider {
  name: string
  base_url: string
  anthropic_base_url: string
  api_key: string
  provider_class: string
  models: string
  timeout_ms: string
  enabled: boolean
}

interface ProbeTabProps {
  probeProvider: ProbeProvider
  setProbeProvider: (provider: ProbeProvider | ((prev: ProbeProvider) => ProbeProvider)) => void
  probeResult: unknown
  onRunProbe: (e: Event) => void
  busy: boolean
}

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

const ProbeTabComponent = ({
  probeProvider,
  setProbeProvider,
  probeResult,
  onRunProbe,
  busy,
}: ProbeTabProps) => {
  const { t } = useI18n()

  const updateField = useCallback(
    (field: keyof ProbeProvider, value: string | boolean) => {
      setProbeProvider((prev) => ({ ...prev, [field]: value }))
    },
    [setProbeProvider]
  )

  return (
    <section class="panel">
      <h2>{t('probe.title')}</h2>
      <form class="probe-form" onSubmit={onRunProbe}>
        <label>
          {t('probe.name')}
          <input
            value={probeProvider.name}
            onInput={(e) => updateField('name', (e.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <label>
          {t('probe.baseUrl')}
          <input
            value={probeProvider.base_url}
            onInput={(e) => updateField('base_url', (e.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <label>
          {t('probe.anthropicBaseUrl')}
          <input
            value={probeProvider.anthropic_base_url}
            onInput={(e) =>
              updateField('anthropic_base_url', (e.currentTarget as HTMLInputElement).value)
            }
          />
        </label>
        <label>
          {t('probe.apiKey')}
          <input
            type="password"
            value={probeProvider.api_key}
            onInput={(e) => updateField('api_key', (e.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <label>
          {t('probe.providerClass')}
          <select
            value={probeProvider.provider_class}
            onChange={(e) =>
              updateField('provider_class', (e.currentTarget as HTMLSelectElement).value)
            }
          >
            <option value="quota_limited">quota_limited</option>
            <option value="free">free</option>
          </select>
        </label>
        <label>
          {t('probe.models')}
          <input
            value={probeProvider.models}
            onInput={(e) => updateField('models', (e.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <label>
          {t('probe.timeoutMs')}
          <input
            value={probeProvider.timeout_ms}
            onInput={(e) => updateField('timeout_ms', (e.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <button type="submit" disabled={busy || probeProvider.base_url.trim() === ''}>
          {t('probe.runProbe')}
        </button>
      </form>
      <div class="panel-subsection">
        <h3>{t('probe.result')}</h3>
        <pre>{pretty(probeResult)}</pre>
      </div>
    </section>
  )
}

export const ProbeTab = memo(ProbeTabComponent)
