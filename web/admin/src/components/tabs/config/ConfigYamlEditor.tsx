import { useI18n } from '../../../i18n'

interface ConfigValidationResult {
  valid: boolean
  errors?: string[]
  warnings?: string[]
}

interface ValidationState {
  isValidating: boolean
  result: ConfigValidationResult | null
  error: string | null
}

interface ConfigYamlEditorProps {
  jsonValue: string
  onJsonChange: (e: Event) => void
  validation: ValidationState
  busy: boolean
  isUpdating: boolean
  onValidate: () => void
  onApplyChanges: () => void
}

export function ConfigYamlEditor({
  jsonValue,
  onJsonChange,
  validation,
  busy,
  isUpdating,
  onValidate,
  onApplyChanges,
}: ConfigYamlEditorProps) {
  const { t } = useI18n()

  return (
    <div class="config-section">
      <h3>{t('config.jsonEditor')}</h3>
      <div class="config-editor-wrapper">
        <textarea
          class="config-json-editor"
          value={jsonValue}
          onChange={onJsonChange}
          placeholder={t('config.jsonPlaceholder')}
          spellcheck={false}
        />
        <div class="config-editor-actions">
          <button
            type="button"
            class="primary"
            disabled={busy || isUpdating || !jsonValue.trim()}
            onClick={onApplyChanges}
          >
            {isUpdating ? t('config.updating') || 'Updating...' : t('config.applyChanges')}
          </button>
          <button
            type="button"
            disabled={busy || validation.isValidating}
            onClick={onValidate}
          >
            {validation.isValidating ? t('config.validating') || 'Validating...' : t('config.validate')}
          </button>
        </div>
        {validation.error && (
          <div class="validation-error">
            <span class="error-icon">⚠️</span>
            <span>{validation.error}</span>
          </div>
        )}
        {validation.result && (
          <div class={`validation-result ${validation.result.valid ? 'success' : 'error'}`}>
            {validation.result.errors?.map((err, i) => (
              <div key={i} class="validation-error-item">
                <span class="error-icon">✗</span>
                <span>{err}</span>
              </div>
            ))}
            {validation.result.warnings?.map((warn, i) => (
              <div key={i} class="validation-warning-item">
                <span class="warning-icon">✓</span>
                <span>{warn}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
