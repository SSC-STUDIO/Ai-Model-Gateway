import { memo, useCallback, useState, useRef, useEffect } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { AnyRecord } from '../../types'

const SENSITIVE_KEYS = new Set([
  'bootstrap_token',
  'cookie_signing_key',
  'api_key',
  'secret',
  'password',
  'token',
])

function isSensitive(key: string): boolean {
  const lower = key.toLowerCase()
  return SENSITIVE_KEYS.has(lower) || lower.includes('secret') || lower.includes('password')
}

interface SettingsTabProps {
  configView: AnyRecord | null
  configText: string
  setConfigText: (text: string) => void
  onSave: () => Promise<boolean>
  busy: boolean
}

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

type ViewMode = 'structured' | 'json'

// Toast component
function Toast({ message, type, onClose }: { message: string; type: 'success' | 'error'; onClose: () => void }) {
  useEffect(() => {
    const timer = setTimeout(onClose, 4000)
    return () => clearTimeout(timer)
  }, [onClose])

  return (
    <div class={`settings-toast settings-toast-${type}`}>
      <span>{message}</span>
      <button type="button" class="settings-toast-close" onClick={onClose}>
        ×
      </button>
    </div>
  )
}

// Confirm dialog
function ConfirmDialog({
  title,
  message,
  confirmLabel,
  cancelLabel,
  onConfirm,
  onCancel,
}: {
  title: string
  message: string
  confirmLabel: string
  cancelLabel: string
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <div class="settings-overlay" onClick={onCancel}>
      <div class="settings-dialog" onClick={(e) => e.stopPropagation()}>
        <h3>{title}</h3>
        <p>{message}</p>
        <div class="settings-dialog-actions">
          <button type="button" class="secondary" onClick={onCancel}>
            {cancelLabel}
          </button>
          <button type="button" onClick={onConfirm}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

// Section card with collapsible content
function SectionCard({
  sectionKey,
  data,
  onChange,
  t,
}: {
  sectionKey: string
  data: unknown
  onChange: (sectionKey: string, path: string[], value: unknown) => void
  t: (key: string) => string
}) {
  const [collapsed, setCollapsed] = useState(false)

  return (
    <article class="config-card settings-section-card">
      <h3
        class="settings-section-header"
        onClick={() => setCollapsed(!collapsed)}
      >
        <span class="settings-collapse-icon">{collapsed ? '+' : '-'}</span>
        {sectionKey}
      </h3>
      {!collapsed && (
        <div class="settings-fields">
          <FieldRenderer
            value={data}
            path={[]}
            sectionKey={sectionKey}
            onChange={onChange}
            t={t}
            depth={0}
          />
        </div>
      )}
    </article>
  )
}

// Recursive field renderer
function FieldRenderer({
  value,
  path,
  sectionKey,
  onChange,
  t,
  depth,
}: {
  value: unknown
  path: string[]
  sectionKey: string
  onChange: (sectionKey: string, path: string[], value: unknown) => void
  t: (key: string) => string
  depth: number
}) {
  if (value === null || value === undefined) {
    return <span class="muted">null</span>
  }

  if (Array.isArray(value)) {
    return (
      <div class="settings-array-field">
        <p class="muted" style={{ fontSize: '0.8rem', margin: '4px 0' }}>
          {t('settings.readOnlyArray')}
        </p>
        <pre style={{ maxHeight: '200px', overflow: 'auto' }}>{pretty(value)}</pre>
      </div>
    )
  }

  if (typeof value === 'object') {
    return (
      <div class="settings-nested" style={{ paddingLeft: depth > 0 ? '16px' : '0' }}>
        {Object.entries(value as Record<string, unknown>).map(([key, val]) => {
          const fieldPath = [...path, key]
          const fieldType = typeof val

          if (Array.isArray(val)) {
            return (
              <div key={key} class="settings-field-row">
                <label class="settings-field-label">{key}</label>
                <FieldRenderer
                  value={val}
                  path={fieldPath}
                  sectionKey={sectionKey}
                  onChange={onChange}
                  t={t}
                  depth={depth + 1}
                />
              </div>
            )
          }

          if (val !== null && fieldType === 'object') {
            return (
              <div key={key} class="settings-field-group">
                <label class="settings-field-label settings-field-label-group">{key}</label>
                <FieldRenderer
                  value={val}
                  path={fieldPath}
                  sectionKey={sectionKey}
                  onChange={onChange}
                  t={t}
                  depth={depth + 1}
                />
              </div>
            )
          }

          if (fieldType === 'boolean') {
            return (
              <div key={key} class="settings-field-row">
                <label class="settings-field-label">{key}</label>
                <label class="settings-toggle-wrap">
                  <input
                    type="checkbox"
                    checked={val as boolean}
                    onChange={(e) =>
                      onChange(sectionKey, fieldPath, (e.currentTarget as HTMLInputElement).checked)
                    }
                  />
                  <span class="settings-toggle-label">{val ? 'true' : 'false'}</span>
                </label>
              </div>
            )
          }

          if (fieldType === 'number') {
            return (
              <div key={key} class="settings-field-row">
                <label class="settings-field-label">{key}</label>
                <input
                  type="number"
                  value={val as number}
                  onInput={(e) =>
                    onChange(sectionKey, fieldPath, Number((e.currentTarget as HTMLInputElement).value))
                  }
                  class="settings-field-input"
                />
              </div>
            )
          }

          // String field
          const sensitive = isSensitive(key)
          return (
            <div key={key} class="settings-field-row">
              <label class="settings-field-label">
                {key}
                {sensitive && <span class="settings-sensitive-badge">{t('settings.sensitive')}</span>}
              </label>
              <input
                type={sensitive ? 'password' : 'text'}
                value={val == null ? '' : String(val)}
                onInput={(e) => onChange(sectionKey, fieldPath, (e.currentTarget as HTMLInputElement).value)}
                class="settings-field-input"
              />
            </div>
          )
        })}
      </div>
    )
  }

  // Primitive at top level (shouldn't usually happen)
  return <span>{String(value)}</span>
}

const SettingsTabComponent = ({
  configView,
  configText,
  setConfigText,
  onSave,
  busy,
}: SettingsTabProps) => {
  const { t } = useI18n()
  const [viewMode, setViewMode] = useState<ViewMode>('structured')
  const [showConfirm, setShowConfirm] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  // Local mutable copy for structured editing
  const [structuredData, setStructuredData] = useState<AnyRecord | null>(null)
  const initializedRef = useRef(false)

  // Sync structuredData from configView when it changes
  useEffect(() => {
    if (configView) {
      setStructuredData(JSON.parse(JSON.stringify(configView)))
      initializedRef.current = true
    }
  }, [configView])

  const handleInput = useCallback(
    (e: Event) => {
      setConfigText((e.currentTarget as HTMLTextAreaElement).value)
    },
    [setConfigText]
  )

  const handleStructuredChange = useCallback(
    (sectionKey: string, path: string[], value: unknown) => {
      setStructuredData((prev) => {
        if (!prev) return prev
        const next = JSON.parse(JSON.stringify(prev)) as AnyRecord
        let target = next[sectionKey] as Record<string, unknown>
        for (let i = 0; i < path.length - 1; i++) {
          target = target[path[i]] as Record<string, unknown>
        }
        target[path[path.length - 1]] = value
        // Keep configText in sync
        setConfigText(pretty(next))
        return next
      })
    },
    [setConfigText]
  )

  const handleSaveClick = useCallback(() => {
    // When in structured mode, sync configText from structuredData before confirming
    if (viewMode === 'structured' && structuredData) {
      setConfigText(pretty(structuredData))
    }
    setShowConfirm(true)
  }, [viewMode, structuredData, setConfigText])

  const handleConfirmSave = useCallback(async () => {
    setShowConfirm(false)
    const success = await onSave()
    if (success) {
      setToast({ message: t('settings.saveSuccess'), type: 'success' })
    } else {
      setToast({ message: t('settings.saveError'), type: 'error' })
    }
  }, [onSave, t])

  const handleCancelConfirm = useCallback(() => {
    setShowConfirm(false)
  }, [])

  const handleDismissToast = useCallback(() => {
    setToast(null)
  }, [])

  // Sync structuredData when switching from JSON to structured view
  const handleViewToggle = useCallback(
    (mode: ViewMode) => {
      if (mode === 'structured' && viewMode === 'json') {
        try {
          setStructuredData(JSON.parse(configText) as AnyRecord)
        } catch {
          // Invalid JSON, stay on JSON view
          return
        }
      }
      setViewMode(mode)
    },
    [viewMode, configText]
  )

  return (
    <section class="panel">
      <div class="settings-header">
        <h2>{t('settings.title')}</h2>
        <div class="settings-view-toggle">
          <button
            type="button"
            class={`settings-toggle-btn${viewMode === 'structured' ? ' active' : ''}`}
            onClick={() => handleViewToggle('structured')}
          >
            {t('settings.structured')}
          </button>
          <button
            type="button"
            class={`settings-toggle-btn${viewMode === 'json' ? ' active' : ''}`}
            onClick={() => handleViewToggle('json')}
          >
            {t('settings.advancedJson')}
          </button>
        </div>
      </div>

      {viewMode === 'structured' && structuredData && (
        <div class="config-sections">
          {Object.entries(structuredData).map(([key, value]) => (
            <SectionCard
              key={key}
              sectionKey={key}
              data={value}
              onChange={handleStructuredChange}
              t={t}
            />
          ))}
        </div>
      )}

      {viewMode === 'json' && (
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
        </div>
      )}

      <div style={{ marginTop: '16px' }}>
        <button type="button" onClick={handleSaveClick} disabled={busy || configText.trim() === ''}>
          {t('settings.saveConfig')}
        </button>
      </div>

      {showConfirm && (
        <ConfirmDialog
          title={t('settings.confirmTitle')}
          message={t('settings.confirmMessage')}
          confirmLabel={t('settings.confirmYes')}
          cancelLabel={t('settings.confirmCancel')}
          onConfirm={handleConfirmSave}
          onCancel={handleCancelConfirm}
        />
      )}

      {toast && <Toast message={toast.message} type={toast.type} onClose={handleDismissToast} />}
    </section>
  )
}

export const SettingsTab = memo(SettingsTabComponent)
