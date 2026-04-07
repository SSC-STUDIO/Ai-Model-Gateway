import { memo, useCallback } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { AnyRecord } from '../../types'

interface SettingsTabProps {
  configView: AnyRecord | null
  configText: string
  setConfigText: (text: string) => void
  onSave: () => void
  busy: boolean
}

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

const SettingsTabComponent = ({
  configView,
  configText,
  setConfigText,
  onSave,
  busy,
}: SettingsTabProps) => {
  const { t } = useI18n()

  const handleInput = useCallback(
    (e: Event) => {
      setConfigText((e.currentTarget as HTMLTextAreaElement).value)
    },
    [setConfigText]
  )

  const handleSave = useCallback(() => {
    onSave()
  }, [onSave])

  return (
    <section class="panel">
      <h2>{t('settings.title')}</h2>
      {configView && (
        <div class="config-sections">
          {Object.entries(configView).map(([key, value]) => (
            <article key={key} class="config-card">
              <h3>{key}</h3>
              <pre>{pretty(value)}</pre>
            </article>
          ))}
        </div>
      )}
      <div class="panel-subsection">
        <h3>{t('settings.editableJson')}</h3>
        <textarea
          value={configText}
          onInput={handleInput}
          style={{
            minHeight: '360px',
            width: '100%',
            borderRadius: '8px',
            border: '1px solid var(--input-border)',
            padding: '10px',
            fontFamily: 'Consolas, monospace',
            fontSize: '0.9rem',
          }}
        />
        <div>
          <button type="button" onClick={handleSave} disabled={busy || configText.trim() === ''}>
            {t('settings.saveConfig')}
          </button>
        </div>
      </div>
    </section>
  )
}

export const SettingsTab = memo(SettingsTabComponent)
