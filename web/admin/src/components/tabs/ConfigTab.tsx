import { fetchJSON } from '../../utils/fetch'
import { memo, useCallback, useEffect, useMemo, useState } from 'preact/compat'
import { useI18n } from '../../i18n'
import type { ControlConfigView, ProviderHealthView, ConfigHistoryResponse, ConfigVersionSummary, ConfigSubTab } from '../../types'
import { formatAbsoluteTime } from '../../utils/formatting'
import {
  buildVisualConfig,
  createDefaultVisualConfigState,
  parseConfigDocument,
  visualStateFromConfig,
  type AdminEditorConfig as AdminConfig,
  type BridgeEditorConfig as BridgeConfig,
  type BridgeRule,
  type FallbackEditorConfig as FallbackConfig,
  type InterceptEditorRule as InterceptRule,
  type PricingEditorConfig as PricingConfig,
  type ProviderEditorConfig as ProviderConfig,
  type RoutingEditorConfig as RoutingConfig,
  type ServerEditorConfig as ServerConfig,
  type TelemetryEditorConfig as TelemetryConfig,
} from './configEditor'
import { Icon, type IconName } from '../Icon'
import { ConfigHistory } from './config/ConfigHistory'
import { ConfigYamlEditor } from './config/ConfigYamlEditor'

// API response types for validation and update
interface ConfigValidationResult {
  valid: boolean
  errors?: string[]
  warnings?: string[]
}

interface ConfigUpdateResult {
  success: boolean
  snapshot_id?: string
  revision_id?: string
  published_at?: string
  error?: string
}

interface ConfigTabProps {
  controlConfig: ControlConfigView | null
  historyPayload: ConfigHistoryResponse
  providerHealth: ProviderHealthView[]
  snapshotInfo: {
    active_snapshot_id?: string
    provider_count?: number
    enabled_provider_count?: number
    unhealthy_provider_count?: number
    cooldown_provider_count?: number
  } | null
  selectedVersion: string
  selectedEntry: ConfigVersionSummary | null
  actionLabel: string
  actionDisabled: boolean
  onVersionChange: (version: string) => void
  onApplySelection: () => void
  busy: boolean
  onConfigUpdated?: () => void
}

function formatDate(value: string | undefined): string {
  return formatAbsoluteTime(value ?? null)
}

const SUB_TABS: { key: ConfigSubTab; icon: IconName }[] = [
  { key: 'current', icon: 'file' },
  { key: 'editor', icon: 'edit' },
  { key: 'visual', icon: 'palette' },
  { key: 'history', icon: 'history' },
]

// Validation state
interface ValidationState {
  isValidating: boolean
  result: ConfigValidationResult | null
  error: string | null
}

const ConfigTabComponent = ({
  controlConfig,
  historyPayload,
  providerHealth,
  snapshotInfo,
  selectedVersion,
  selectedEntry,
  actionLabel,
  actionDisabled,
  onVersionChange,
  onApplySelection,
  busy,
  onConfigUpdated,
}: ConfigTabProps) => {
  const { t } = useI18n()
  const [subTab, setSubTab] = useState<ConfigSubTab>('current')
  const [jsonValue, setJsonValue] = useState('')
  const [validation, setValidation] = useState<ValidationState>({
    isValidating: false,
    result: null,
    error: null,
  })
  const [isUpdating, setIsUpdating] = useState(false)
  const defaultVisualState = useMemo(() => createDefaultVisualConfigState(), [])

  // Visual editor state
  const [providers, setProviders] = useState<ProviderConfig[]>(() => defaultVisualState.providers)
  const [routing, setRouting] = useState<RoutingConfig>(() => defaultVisualState.routing)
  const [server, setServer] = useState<ServerConfig>(() => defaultVisualState.server)
  const [admin, setAdmin] = useState<AdminConfig>(() => defaultVisualState.admin)
  const [bridge, setBridge] = useState<BridgeConfig>(() => defaultVisualState.bridge)
  const [fallback, setFallback] = useState<FallbackConfig>(() => defaultVisualState.fallback)
  const [telemetry, setTelemetry] = useState<TelemetryConfig>(() => defaultVisualState.telemetry)
  const [pricing, setPricing] = useState<PricingConfig>(() => defaultVisualState.pricing)
  const [interceptRules, setInterceptRules] = useState<InterceptRule[]>(() => defaultVisualState.interceptRules)
  const [activeProviderId, setActiveProviderId] = useState<string | null>(null)

  const revision = controlConfig?.revision
  const policy = controlConfig?.policy
  const rawYaml = controlConfig?.raw_yaml
  const configData = controlConfig?.config
  const visualConfigBase = useMemo(() => parseConfigDocument(rawYaml ?? ''), [rawYaml])
  const visualConfigSource = useMemo(() => {
    if (visualConfigBase) {
      return visualConfigBase
    }
    if (configData && typeof configData === 'object' && !Array.isArray(configData)) {
      return configData as Record<string, unknown>
    }
    return null
  }, [visualConfigBase, configData])

  // Load raw YAML into editor when config changes
  useEffect(() => {
    if (rawYaml) {
      setJsonValue(rawYaml)
    }
  }, [rawYaml])

  // Load config data into visual editor when config changes
  useEffect(() => {
    if (visualConfigSource) {
      const nextState = visualStateFromConfig(visualConfigSource)
      setServer(nextState.server)
      setAdmin(nextState.admin)
      setRouting(nextState.routing)
      setProviders(nextState.providers)
      setBridge(nextState.bridge)
      setFallback(nextState.fallback)
      setInterceptRules(nextState.interceptRules)
      setTelemetry(nextState.telemetry)
      setPricing(nextState.pricing)
    }
  }, [visualConfigSource])

  const historyEntries = useMemo(() => historyPayload?.versions ?? [], [historyPayload])

  const providerStatusList = useMemo(() => {
    if (!providerHealth || providerHealth.length === 0) return []
    return providerHealth.map((p) => ({
      name: p.name,
      status: p.status,
      healthy: p.healthy,
      latency: p.latency_ms,
      failures: p.consecutive_failures,
    }))
  }, [providerHealth])

  // Health lookup by name for visual editor
  const providerHealthMap = useMemo(() => {
    const map = new Map<string, { healthy: boolean; status: string; latency: number }>()
    for (const p of providerHealth ?? []) {
      map.set(p.name, { healthy: p.healthy, status: p.status, latency: p.latency_ms ?? 0 })
    }
    return map
  }, [providerHealth])

  // Max weight across all providers for scale indicator
  const maxWeight = useMemo(() => {
    return providers.reduce((max, p) => Math.max(max, p.weight), 0)
  }, [providers])

  const handleApplySelection = useCallback(() => {
    onApplySelection()
  }, [onApplySelection])

  const handleJsonChange = useCallback((e: Event) => {
    setJsonValue((e.currentTarget as HTMLTextAreaElement).value)
    // Clear validation when content changes
    setValidation({ isValidating: false, result: null, error: null })
  }, [])

  // Parse YAML/JSON config to object
  const parseConfigContent = useCallback((content: string): Record<string, unknown> | null => {
    return parseConfigDocument(content)
  }, [])

  // Validate config via API
  const handleValidate = useCallback(async () => {
    const config = parseConfigContent(jsonValue)
    if (!config) {
      setValidation({
        isValidating: false,
        result: { valid: false, errors: ['Config content is empty'] },
        error: null,
      })
      return
    }

    setValidation({ isValidating: true, result: null, error: null })

    try {
      const result = (await fetchJSON('/api/admin/config/validate', {
        method: 'POST',
        body: JSON.stringify({ config }),
      })) as ConfigValidationResult
      setValidation({ isValidating: false, result, error: null })
    } catch (err) {
      setValidation({
        isValidating: false,
        result: null,
        error: err instanceof Error ? err.message : 'Validation request failed',
      })
    }
  }, [jsonValue, parseConfigContent])

  // Apply changes via API
  const handleApplyChanges = useCallback(async () => {
    const config = parseConfigContent(jsonValue)
    if (!config) {
      setValidation({
        isValidating: false,
        result: { valid: false, errors: ['Config content is empty'] },
        error: null,
      })
      return
    }

    setIsUpdating(true)
    setValidation({ isValidating: false, result: null, error: null })

    try {
      const response = await fetch('/api/admin/config/update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          config,
          description: 'Updated via admin UI',
        }),
      })

      if (!response.ok) {
        const errorText = await response.text()
        setValidation({
          isValidating: false,
          result: null,
          error: `Update failed: ${response.status} ${errorText}`,
        })
        setIsUpdating(false)
        return
      }

      const result = (await response.json()) as ConfigUpdateResult

      if (result.success) {
        setValidation({
          isValidating: false,
          result: { valid: true, warnings: [`Config updated successfully. Revision: ${result.revision_id}`] },
          error: null,
        })
        // Refresh the page to show updated config
        onConfigUpdated?.()
      } else {
        setValidation({
          isValidating: false,
          result: { valid: false, errors: [result.error || 'Update failed'] },
          error: null,
        })
      }
    } catch (err) {
      setValidation({
        isValidating: false,
        result: null,
        error: err instanceof Error ? err.message : 'Update request failed',
      })
    } finally {
      setIsUpdating(false)
    }
  }, [jsonValue, parseConfigContent])

  // Validate visual editor config
  const handleVisualValidate = useCallback(async () => {
    let config: Record<string, unknown>
    try {
      config = buildVisualConfig(visualConfigBase, {
        server,
        admin,
        routing,
        providers,
        bridge,
        fallback,
        interceptRules,
        telemetry,
        pricing,
      })
    } catch (err) {
      setValidation({
        isValidating: false,
        result: null,
        error: err instanceof Error ? err.message : 'Visual config build failed',
      })
      return
    }

    setValidation({ isValidating: true, result: null, error: null })

    try {
      const result = (await fetchJSON('/api/admin/config/validate', {
        method: 'POST',
        body: JSON.stringify({ config }),
      })) as ConfigValidationResult
      setValidation({ isValidating: false, result, error: null })
    } catch (err) {
      setValidation({
        isValidating: false,
        result: null,
        error: err instanceof Error ? err.message : 'Validation request failed',
      })
    }
  }, [visualConfigBase, server, admin, routing, providers, bridge, fallback, interceptRules, telemetry, pricing])

  // Apply visual editor changes
  const handleVisualApplyChanges = useCallback(async () => {
    let config: Record<string, unknown>
    try {
      config = buildVisualConfig(visualConfigBase, {
        server,
        admin,
        routing,
        providers,
        bridge,
        fallback,
        interceptRules,
        telemetry,
        pricing,
      })
    } catch (err) {
      setValidation({
        isValidating: false,
        result: null,
        error: err instanceof Error ? err.message : 'Visual config build failed',
      })
      return
    }

    setIsUpdating(true)
    setValidation({ isValidating: false, result: null, error: null })

    try {
      const response = await fetch('/api/admin/config/update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          config,
          description: 'Updated via admin UI visual editor',
        }),
      })

      if (!response.ok) {
        const errorText = await response.text()
        setValidation({
          isValidating: false,
          result: null,
          error: `Update failed: ${response.status} ${errorText}`,
        })
        setIsUpdating(false)
        return
      }

      const result = (await response.json()) as ConfigUpdateResult

      if (result.success) {
        setValidation({
          isValidating: false,
          result: { valid: true, warnings: [`Config updated successfully. Revision: ${result.revision_id}`] },
          error: null,
        })
        // Refresh the page to show updated config
        onConfigUpdated?.()
      } else {
        setValidation({
          isValidating: false,
          result: { valid: false, errors: [result.error || 'Update failed'] },
          error: null,
        })
      }
    } catch (err) {
      setValidation({
        isValidating: false,
        result: null,
        error: err instanceof Error ? err.message : 'Update request failed',
      })
    } finally {
      setIsUpdating(false)
    }
  }, [visualConfigBase, server, admin, routing, providers, bridge, fallback, interceptRules, telemetry, pricing])

  // Visual editor handlers
  const handleRoutingChange = useCallback((key: keyof RoutingConfig, value: string | number | boolean) => {
    setRouting((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleServerChange = useCallback((key: keyof ServerConfig, value: string | number) => {
    setServer((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleAdminChange = useCallback((key: keyof AdminConfig, value: string | number | boolean) => {
    setAdmin((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleBridgeChange = useCallback((key: keyof BridgeConfig, value: boolean | BridgeRule[] | string[]) => {
    setBridge((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleFallbackChange = useCallback((key: keyof FallbackConfig, value: boolean | Record<string, string>) => {
    setFallback((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleTelemetryChange = useCallback((key: keyof TelemetryConfig, value: string | number) => {
    setTelemetry((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handlePricingChange = useCallback((key: keyof PricingConfig, value: string | number) => {
    setPricing((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleProviderFieldChange = useCallback((providerId: string, field: keyof ProviderConfig, value: string | number | boolean | Record<string, string>) => {
    setProviders((prev) =>
      prev.map((p) => (p.id === providerId ? { ...p, [field]: value } : p))
    )
  }, [])

  const handleAddBridgeRule = useCallback(() => {
    setBridge((prev) => ({ ...prev, rules: [...prev.rules, { from: '', to: '' }] }))
  }, [])

  const handleBridgeRuleChange = useCallback((index: number, field: 'from' | 'to', value: string) => {
    setBridge((prev) => ({
      ...prev,
      rules: prev.rules.map((r, i) => (i === index ? { ...r, [field]: value } : r)),
    }))
  }, [])

  const handleRemoveBridgeRule = useCallback((index: number) => {
    setBridge((prev) => ({ ...prev, rules: prev.rules.filter((_, i) => i !== index) }))
  }, [])

  const handleRemoveFallbackModel = useCallback((key: string) => {
    setFallback((prev) => {
      const newModels = { ...prev.models }
      delete newModels[key]
      return { ...prev, models: newModels }
    })
  }, [])

  const handleAddInterceptRule = useCallback(() => {
    setInterceptRules((prev) => [...prev, { name: '', enabled: true, status_codes: [], message_keywords: [], action: '' }])
  }, [])

  const handleInterceptRuleChange = useCallback((index: number, field: keyof InterceptRule, value: unknown) => {
    setInterceptRules((prev) =>
      prev.map((r, i) => (i === index ? { ...r, [field]: value } : r))
    )
  }, [])

  const handleRemoveInterceptRule = useCallback((index: number) => {
    setInterceptRules((prev) => prev.filter((_, i) => i !== index))
  }, [])

  const handleProviderToggle = useCallback((id: string) => {
    setProviders((prev) =>
      prev.map((p) => (p.id === id ? { ...p, enabled: !p.enabled } : p))
    )
  }, [])

  const handleProviderWeightChange = useCallback((id: string, weight: number) => {
    setProviders((prev) =>
      prev.map((p) => (p.id === id ? { ...p, weight } : p))
    )
  }, [])

  const handleAddModel = useCallback((providerId: string) => {
    setProviders((prev) =>
      prev.map((p) =>
        p.id === providerId
          ? { ...p, models: [...p.models, { public_model: '', upstream_model: '' }] }
          : p
      )
    )
  }, [])

  const handleModelChange = useCallback((providerId: string, index: number, field: 'public_model' | 'upstream_model', value: string) => {
    setProviders((prev) =>
      prev.map((p) =>
        p.id === providerId
          ? { ...p, models: p.models.map((m, i) => (i === index ? { ...m, [field]: value } : m)) }
          : p
      )
    )
  }, [])

  const handleRemoveModel = useCallback((providerId: string, index: number) => {
    setProviders((prev) =>
      prev.map((p) =>
        p.id === providerId
          ? { ...p, models: p.models.filter((_, i) => i !== index) }
          : p
      )
    )
  }, [])

  const activeProvider = useMemo(
    () => providers.find((p) => p.id === activeProviderId),
    [providers, activeProviderId]
  )

  const renderSubTab = () => {
    switch (subTab) {
      case 'current':
        return (
          <>
            {/* Current Revision */}
            <div class="config-section">
              <h3>{t('config.currentRevision')}</h3>
              {revision ? (
                <div class="config-card">
                  <div class="config-row">
                    <span class="config-label">{t('config.revisionId')}</span>
                    <span class="config-value code">{revision.id}</span>
                  </div>
                  <div class="config-row">
                    <span class="config-label">{t('config.createdAt')}</span>
                    <span class="config-value">{formatDate(revision.created_at)}</span>
                  </div>
                  <div class="config-row">
                    <span class="config-label">{t('config.createdBy')}</span>
                    <span class="config-value">{revision.created_by || 'system'}</span>
                  </div>
                  {revision.description && (
                    <div class="config-row">
                      <span class="config-label">{t('config.description')}</span>
                      <span class="config-value">{revision.description}</span>
                    </div>
                  )}
                  {revision.is_active && (
                    <div class="config-row">
                      <span class="config-label">{t('config.status')}</span>
                      <span class="config-value">
                        <span class="status-badge success">{t('config.active')}</span>
                      </span>
                    </div>
                  )}
                </div>
              ) : (
                <div class="empty-state-box">
                  <p class="empty-state-title">{t('config.noRevision')}</p>
                </div>
              )}
            </div>

            {/* Runtime Info */}
            {snapshotInfo && (
              <div class="config-section">
                <h3>{t('config.runtimeInfo')}</h3>
                <div class="config-card">
                  <div class="config-row">
                    <span class="config-label">{t('config.activeSnapshot')}</span>
                    <span class="config-value code">{snapshotInfo.active_snapshot_id || '-'}</span>
                  </div>
                  <div class="config-row">
                    <span class="config-label">{t('config.providerCount')}</span>
                    <span class="config-value">{snapshotInfo.provider_count ?? 0}</span>
                  </div>
                  <div class="config-row">
                    <span class="config-label">{t('overview.providersHealthy')}</span>
                    <span class="config-value">{snapshotInfo.enabled_provider_count ?? 0}</span>
                  </div>
                  <div class="config-row">
                    <span class="config-label">{t('overview.providersBlocked')}</span>
                    <span class="config-value">{snapshotInfo.unhealthy_provider_count ?? 0}</span>
                  </div>
                  <div class="config-row">
                    <span class="config-label">{t('overview.providersCooldown')}</span>
                    <span class="config-value">{snapshotInfo.cooldown_provider_count ?? 0}</span>
                  </div>
                </div>
              </div>
            )}

            {/* Policy */}
            {policy && (
              <div class="config-section">
                <h3>{t('config.policy')}</h3>
                <div class="config-card">
                  <div class="config-row">
                    <span class="config-label">{t('config.publishHistoryLimit')}</span>
                    <span class="config-value">{policy.publish_history_limit}</span>
                  </div>
                </div>
              </div>
            )}

            {/* Provider Health */}
            {providerStatusList.length > 0 && (
              <div class="config-section">
                <h3>{t('config.providerHealth')}</h3>
                <div class="config-table-wrapper">
                  <table class="config-table">
                    <thead>
                      <tr>
                        <th>{t('config.providerName')}</th>
                        <th>{t('config.healthStatus')}</th>
                        <th>{t('config.latency')}</th>
                        <th>{t('config.failures')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {providerStatusList.map((p) => (
                        <tr key={p.name}>
                          <td class="code">{p.name}</td>
                          <td>
                            <span class={`status-badge ${p.healthy ? 'success' : 'warning'}`}>
                              {p.status}
                            </span>
                          </td>
                          <td>{p.latency ? `${p.latency}ms` : '-'}</td>
                          <td>{p.failures}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </>
        )

      case 'editor':
        return (
          <ConfigYamlEditor
            jsonValue={jsonValue}
            onJsonChange={handleJsonChange}
            validation={validation}
            busy={busy}
            isUpdating={isUpdating}
            onValidate={handleValidate}
            onApplyChanges={handleApplyChanges}
          />
        )

      case 'visual':
        return (
          <div class="config-section visual-config">
            <div class="visual-config-grid">
              {/* Routing Strategy */}
              <div class="visual-config-card">
                <h4>{t('config.routing.title')}</h4>
                <div class="visual-form-group">
                  <label>{t('config.routing.strategy')}</label>
                  <select
                    value={routing.strategy}
                    onChange={(e) => handleRoutingChange('strategy', (e.currentTarget as HTMLSelectElement).value)}
                  >
                    <option value="health_weighted_rr">{t('config.routing.healthWeighted')}</option>
                    <option value="round_robin">{t('config.routing.roundRobin')}</option>
                    <option value="least_connections">{t('config.routing.leastConnections')}</option>
                    <option value="random">{t('config.routing.random')}</option>
                  </select>
                </div>
                <div class="visual-form-group">
                  <label>{t('config.routing.maxRetries')}</label>
                  <input
                    type="number"
                    min="0"
                    max="10"
                    value={routing.max_retries}
                    onChange={(e) => handleRoutingChange('max_retries', parseInt((e.currentTarget as HTMLInputElement).value) || 0)}
                  />
                </div>
                <div class="visual-form-group visual-form-row">
                  <label class="visual-checkbox-label">
                    <input
                      type="checkbox"
                      checked={routing.health_enabled}
                      onChange={(e) => handleRoutingChange('health_enabled', (e.currentTarget as HTMLInputElement).checked)}
                    />
                    <span>{t('config.routing.healthChecks')}</span>
                  </label>
                  <label class="visual-checkbox-label">
                    <input
                      type="checkbox"
                      checked={routing.sticky_sessions_enabled}
                      onChange={(e) => handleRoutingChange('sticky_sessions_enabled', (e.currentTarget as HTMLInputElement).checked)}
                    />
                    <span>{t('config.routing.stickySessions')}</span>
                  </label>
                </div>
              </div>

              {/* Providers */}
              <div class="visual-config-card">
                <h4>{t('config.providers.title')}</h4>

                {/* Provider Slider List */}
                {providers.length > 0 && (
                  <div class="provider-slider-panel">
                    <div class="provider-slider-list">
                      {providers.map((provider) => {
                          const health = providerHealthMap.get(provider.name)
                          const healthTone = health ? (health.healthy ? 'success' : 'error') : 'neutral'
                          const weightPct = maxWeight > 0 ? (provider.weight / maxWeight) * 100 : 0
                          return (
                        <div
                          key={provider.id}
                          class={`provider-slider-row ${activeProviderId === provider.id ? 'active' : ''} ${!provider.enabled ? 'disabled' : ''}`}
                        >
                          <button
                            type="button"
                            class="provider-slider-info"
                            onClick={() => setActiveProviderId(provider.id)}
                            title={provider.name}
                          >
                            <span class={`provider-slider-dot ${healthTone}`} />
                            <span class="provider-slider-name">{provider.name}</span>
                            <span class="provider-slider-models">{provider.models.length} {t('config.providers.models')}</span>
                            <span class="provider-slider-scale" style={{ width: `${Math.max(4, weightPct)}%` }} />
                          </button>
                          <div class="provider-slider-track">
                            <input
                              type="range"
                              min="0"
                              max="200"
                              value={provider.weight}
                              onChange={(e) => handleProviderWeightChange(provider.id, parseInt((e.currentTarget as HTMLInputElement).value))}
                            />
                          </div>
                          <span class="provider-slider-value">{provider.weight}</span>
                          <button
                            type="button"
                            class={`provider-slider-toggle ${provider.enabled ? 'on' : ''}`}
                            onClick={() => handleProviderToggle(provider.id)}
                            title={provider.enabled ? t('config.providers.disable') : t('config.providers.enable')}
                          >
                            {provider.enabled ? '✓' : '○'}
                          </button>
                        </div>
                        )})}
                    </div>
                  </div>
                )}
              </div>

              {/* Provider Details */}
              <div class="visual-config-card provider-details-card">
                <h4>{activeProvider ? `${activeProvider.name} ${t('config.providers.details')}` : t('config.providers.selectProvider')}</h4>
                {activeProvider ? (
                  <>
                    <div class="visual-form-group">
                      <label>{t('config.providers.baseUrl')}</label>
                      <input
                        type="text"
                        value={activeProvider.base_url}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'base_url', (e.currentTarget as HTMLInputElement).value)}
                      />
                    </div>
                    <div class="visual-form-group">
                      <label>{t('config.providers.anthropicBaseUrl')}</label>
                      <input
                        type="text"
                        value={activeProvider.anthropic_base_url}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'anthropic_base_url', (e.currentTarget as HTMLInputElement).value)}
                      />
                    </div>
                    <div class="visual-form-group">
                      <label>API Key</label>
                      <input
                        type="password"
                        value={activeProvider.api_key}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'api_key', (e.currentTarget as HTMLInputElement).value)}
                        placeholder="sk-..."
                      />
                    </div>
                    <div class="visual-form-group">
                      <label>{t('config.providers.providerClass')}</label>
                      <select
                        value={activeProvider.provider_class}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'provider_class', (e.currentTarget as HTMLSelectElement).value)}
                      >
                        <option value="">{t('config.providers.classDefault')}</option>
                        <option value="free">{t('config.providers.classFree')}</option>
                        <option value="quota_limited">{t('config.providers.classQuotaLimited')}</option>
                      </select>
                    </div>
                    <div class="visual-form-group">
                      <label>{t('config.providers.timeoutMs')}</label>
                      <input
                        type="number"
                        min="1000"
                        max="600000"
                        value={activeProvider.timeout_ms}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'timeout_ms', parseInt((e.currentTarget as HTMLInputElement).value) || 30000)}
                      />
                    </div>
                    <div class="visual-form-group">
                      <label>{t('config.providers.sameUpstreamRetries')}</label>
                      <input
                        type="number"
                        min="0"
                        max="10"
                        value={activeProvider.same_upstream_retries}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'same_upstream_retries', parseInt((e.currentTarget as HTMLInputElement).value) || 0)}
                      />
                    </div>
                    <div class="visual-form-group visual-form-row">
                      <label class="visual-checkbox-label">
                        <input
                          type="checkbox"
                          checked={activeProvider.rate_limit_enabled}
                          onChange={(e) => handleProviderFieldChange(activeProvider.id, 'rate_limit_enabled', (e.currentTarget as HTMLInputElement).checked)}
                        />
                        <span>{t('config.providers.outboundRateLimit')}</span>
                      </label>
                    </div>
                    <div class="visual-form-group">
                      <label>{t('config.providers.outboundRateLimitRps')}</label>
                      <input
                        type="number"
                        min="0"
                        step="0.000001"
                        value={activeProvider.rate_limit_rps}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'rate_limit_rps', parseFloat((e.currentTarget as HTMLInputElement).value) || 0)}
                      />
                    </div>
                    <div class="visual-form-group">
                      <label>{t('config.providers.outboundRateLimitBurst')}</label>
                      <input
                        type="number"
                        min="0"
                        value={activeProvider.rate_limit_burst}
                        onChange={(e) => handleProviderFieldChange(activeProvider.id, 'rate_limit_burst', parseInt((e.currentTarget as HTMLInputElement).value) || 0)}
                      />
                    </div>
                    <div class="model-mappings">
                      <div class="model-mappings-header">
                        <span>{t('config.providers.modelMappings')}</span>
                        <button type="button" class="btn-small" onClick={() => handleAddModel(activeProvider.id)}>
                          + {t('config.providers.addModel')}
                        </button>
                      </div>
                      {activeProvider.models.length === 0 ? (
                        <div class="no-models">{t('config.providers.noModels')}</div>
                      ) : (
                        <div class="model-list">
                          {activeProvider.models.map((model, index) => (
                            <div key={index} class="model-item">
                              <input
                                type="text"
                                placeholder={t('config.providers.publicModel')}
                                value={model.public_model}
                                onChange={(e) => handleModelChange(activeProvider.id, index, 'public_model', (e.currentTarget as HTMLInputElement).value)}
                              />
                              <span class="model-arrow">→</span>
                              <input
                                type="text"
                                placeholder={t('config.providers.upstreamModel')}
                                value={model.upstream_model}
                                onChange={(e) => handleModelChange(activeProvider.id, index, 'upstream_model', (e.currentTarget as HTMLInputElement).value)}
                              />
                              <button
                                type="button"
                                class="btn-icon btn-remove"
                                onClick={() => handleRemoveModel(activeProvider.id, index)}
                                aria-label={t('common.remove')}
                              >
                                <Icon name="close" size={14} />
                              </button>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </>
                ) : (
                  <div class="select-provider-hint">{t('config.providers.selectProviderHint')}</div>
                )}
              </div>

              {/* Server Config */}
              <div class="visual-config-card">
                <h4>{t('config.server.title')}</h4>
                <div class="visual-form-group">
                  <label>{t('config.server.listenAddress')}</label>
                  <input
                    type="text"
                    value={server.listen}
                    onChange={(e) => handleServerChange('listen', (e.currentTarget as HTMLInputElement).value)}
                    placeholder=":18080"
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.server.readTimeoutMs')}</label>
                  <input
                    type="number"
                    min="1000"
                    value={server.read_timeout_ms}
                    onChange={(e) => handleServerChange('read_timeout_ms', parseInt((e.currentTarget as HTMLInputElement).value) || 30000)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.server.writeTimeoutMs')}</label>
                  <input
                    type="number"
                    min="0"
                    value={server.write_timeout_ms}
                    onChange={(e) => handleServerChange('write_timeout_ms', parseInt((e.currentTarget as HTMLInputElement).value) || 0)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.server.idleTimeoutMs')}</label>
                  <input
                    type="number"
                    min="1000"
                    value={server.idle_timeout_ms}
                    onChange={(e) => handleServerChange('idle_timeout_ms', parseInt((e.currentTarget as HTMLInputElement).value) || 120000)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.server.maxBodyBytes')}</label>
                  <input
                    type="number"
                    min="1024"
                    value={server.max_body_bytes}
                    onChange={(e) => handleServerChange('max_body_bytes', parseInt((e.currentTarget as HTMLInputElement).value) || 104857600)}
                  />
                </div>
              </div>

              {/* Admin Config */}
              <div class="visual-config-card">
                <h4>{t('config.admin.title')}</h4>
                <div class="visual-form-group visual-form-row">
                  <label class="visual-checkbox-label">
                    <input
                      type="checkbox"
                      checked={admin.enabled}
                      onChange={(e) => handleAdminChange('enabled', (e.currentTarget as HTMLInputElement).checked)}
                    />
                    <span>{t('config.common.enabled')}</span>
                  </label>
                </div>
                <div class="visual-form-group">
                  <label>{t('config.admin.language')}</label>
                  <select
                    value={admin.language}
                    onChange={(e) => handleAdminChange('language', (e.currentTarget as HTMLSelectElement).value)}
                  >
                    <option value="zh">中文</option>
                    <option value="en">English</option>
                    <option value="ja">日本語</option>
                    <option value="ko">한국어</option>
                    <option value="es">Español</option>
                    <option value="fr">Français</option>
                    <option value="de">Deutsch</option>
                  </select>
                </div>
                <div class="visual-form-group">
                  <label>{t('config.admin.publishHistoryLimit')}</label>
                  <input
                    type="number"
                    min="10"
                    max="1000"
                    value={admin.publish_history_limit}
                    onChange={(e) => handleAdminChange('publish_history_limit', parseInt((e.currentTarget as HTMLInputElement).value) || 256)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.admin.rateLimitRps')}</label>
                  <input
                    type="number"
                    min="1"
                    value={admin.rate_limit_rps}
                    onChange={(e) => handleAdminChange('rate_limit_rps', parseInt((e.currentTarget as HTMLInputElement).value) || 10)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.admin.rateLimitBurst')}</label>
                  <input
                    type="number"
                    min="1"
                    value={admin.rate_limit_burst}
                    onChange={(e) => handleAdminChange('rate_limit_burst', parseInt((e.currentTarget as HTMLInputElement).value) || 20)}
                  />
                </div>
              </div>

              {/* Bridge Config */}
              <div class="visual-config-card">
                <h4>{t('config.bridge.title')}</h4>
                <div class="visual-form-group visual-form-row">
                  <label class="visual-checkbox-label">
                    <input
                      type="checkbox"
                      checked={bridge.enabled}
                      onChange={(e) => handleBridgeChange('enabled', (e.currentTarget as HTMLInputElement).checked)}
                    />
                    <span>{t('config.common.enabled')}</span>
                  </label>
                </div>
                <div class="model-mappings">
                  <div class="model-mappings-header">
                    <span>{t('config.bridge.rules')}</span>
                    <button type="button" class="btn-small" onClick={handleAddBridgeRule}>
                      + Add Rule
                    </button>
                  </div>
                  {bridge.rules.length === 0 ? (
                    <div class="no-models">No bridge rules configured</div>
                  ) : (
                    <div class="model-list">
                      {bridge.rules.map((rule, index) => (
                        <div key={index} class="model-item">
                          <input
                            type="text"
                            placeholder="From pattern (e.g., claude*)"
                            value={rule.from}
                            onChange={(e) => handleBridgeRuleChange(index, 'from', (e.currentTarget as HTMLInputElement).value)}
                          />
                          <span class="model-arrow">→</span>
                          <input
                            type="text"
                            placeholder={t('config.bridge.toModel')}
                            value={rule.to}
                            onChange={(e) => handleBridgeRuleChange(index, 'to', (e.currentTarget as HTMLInputElement).value)}
                          />
                          <button
                            type="button"
                            class="btn-icon btn-remove"
                            onClick={() => handleRemoveBridgeRule(index)}
                            aria-label={t('common.remove')}
                          >
                            <Icon name="close" size={14} />
                          </button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              {/* Fallback Config */}
              <div class="visual-config-card">
                <h4>{t('config.fallback.title')}</h4>
                <div class="visual-form-group visual-form-row">
                  <label class="visual-checkbox-label">
                    <input
                      type="checkbox"
                      checked={fallback.enabled}
                      onChange={(e) => handleFallbackChange('enabled', (e.currentTarget as HTMLInputElement).checked)}
                    />
                    <span>{t('config.common.enabled')}</span>
                  </label>
                  <label class="visual-checkbox-label">
                    <input
                      type="checkbox"
                      checked={fallback.detect_repetition}
                      onChange={(e) => handleFallbackChange('detect_repetition', (e.currentTarget as HTMLInputElement).checked)}
                    />
                    <span>{t('config.fallback.detectRepetition')}</span>
                  </label>
                </div>
                <div class="model-mappings">
                  <div class="model-mappings-header">
                    <span>{t('config.fallback.models')}</span>
                  </div>
                  {Object.keys(fallback.models).length === 0 ? (
                    <div class="no-models">No fallback models configured</div>
                  ) : (
                    <div class="model-list">
                      {Object.entries(fallback.models).map(([from, to]) => (
                        <div key={from} class="model-item">
                          <input type="text" value={from} readOnly />
                          <span class="model-arrow">→</span>
                          <input type="text" value={to} readOnly />
                          <button
                            type="button"
                            class="btn-icon btn-remove"
                            onClick={() => handleRemoveFallbackModel(from)}
                            aria-label={t('common.remove')}
                          >
                            <Icon name="close" size={14} />
                          </button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              {/* Telemetry Config */}
              <div class="visual-config-card">
                <h4>{t('config.telemetry.title')}</h4>
                <div class="visual-form-group">
                  <label>SQLite Path</label>
                  <input
                    type="text"
                    value={telemetry.sqlite_path}
                    onChange={(e) => handleTelemetryChange('sqlite_path', (e.currentTarget as HTMLInputElement).value)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.telemetry.retentionDays')}</label>
                  <input
                    type="number"
                    min="1"
                    value={telemetry.retention_days}
                    onChange={(e) => handleTelemetryChange('retention_days', parseInt((e.currentTarget as HTMLInputElement).value) || 3650)}
                  />
                </div>
              </div>

              {/* Pricing Config */}
              <div class="visual-config-card">
                <h4>{t('config.pricing.title')}</h4>
                <div class="visual-form-group">
                  <label>Cache Path</label>
                  <input
                    type="text"
                    value={pricing.cache_path}
                    onChange={(e) => handlePricingChange('cache_path', (e.currentTarget as HTMLInputElement).value)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.pricing.refreshIntervalHours')}</label>
                  <input
                    type="number"
                    min="1"
                    value={pricing.refresh_interval_hours}
                    onChange={(e) => handlePricingChange('refresh_interval_hours', parseInt((e.currentTarget as HTMLInputElement).value) || 12)}
                  />
                </div>
                <div class="visual-form-group">
                  <label>{t('config.pricing.requestTimeoutMs')}</label>
                  <input
                    type="number"
                    min="1000"
                    value={pricing.request_timeout_ms}
                    onChange={(e) => handlePricingChange('request_timeout_ms', parseInt((e.currentTarget as HTMLInputElement).value) || 30000)}
                  />
                </div>
              </div>

              {/* Intercept Rules */}
              <div class="visual-config-card">
                <h4>{t('config.intercept.title')}</h4>
                <div class="model-mappings">
                  <div class="model-mappings-header">
                    <span>{t('config.intercept.rules')}</span>
                    <button type="button" class="btn-small" onClick={handleAddInterceptRule}>
                      + Add Rule
                    </button>
                  </div>
                  {interceptRules.length === 0 ? (
                    <div class="no-models">No intercept rules configured</div>
                  ) : (
                    <div class="model-list">
                      {interceptRules.map((rule, index) => (
                        <div key={index} class="model-item" style={{ flexDirection: 'column', alignItems: 'stretch', gap: '8px' }}>
                          <div class="visual-form-row">
                            <label class="visual-checkbox-label">
                              <input
                                type="checkbox"
                                checked={rule.enabled}
                                onChange={(e) => handleInterceptRuleChange(index, 'enabled', (e.currentTarget as HTMLInputElement).checked)}
                              />
                              <span>{t('config.common.enabled')}</span>
                            </label>
                            <input
                              type="text"
                              placeholder={t('config.intercept.ruleName')}
                              value={rule.name}
                              onChange={(e) => handleInterceptRuleChange(index, 'name', (e.currentTarget as HTMLInputElement).value)}
                              style={{ flex: 1 }}
                            />
                            <button
                              type="button"
                              class="btn-icon btn-remove"
                              onClick={() => handleRemoveInterceptRule(index)}
                              aria-label={t('common.remove')}
                            >
                              <Icon name="close" size={14} />
                            </button>
                          </div>
                          <input
                            type="text"
                            placeholder="Status codes (comma separated)"
                            value={rule.status_codes.join(', ')}
                            onChange={(e) => handleInterceptRuleChange(index, 'status_codes', (e.currentTarget as HTMLInputElement).value.split(',').map(s => parseInt(s.trim())).filter(n => !isNaN(n)))}
                          />
                          <input
                            type="text"
                            placeholder="Message keywords (comma separated)"
                            value={rule.message_keywords.join(', ')}
                            onChange={(e) => handleInterceptRuleChange(index, 'message_keywords', (e.currentTarget as HTMLInputElement).value.split(',').map(s => s.trim()).filter(Boolean))}
                          />
                          <select
                            value={rule.action}
                            onChange={(e) => handleInterceptRuleChange(index, 'action', (e.currentTarget as HTMLSelectElement).value)}
                          >
                            <option value="">Select action</option>
                            <option value="fail">Fail</option>
                            <option value="retry">Retry</option>
                            <option value="ignore">Ignore</option>
                          </select>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </div>

            {/* Actions */}
            <div class="visual-config-actions">
              <button
                type="button"
                class="primary"
                disabled={busy || isUpdating || !visualConfigBase}
                onClick={handleVisualApplyChanges}
              >
                {isUpdating ? t('config.updating') || 'Updating...' : t('config.applyChanges')}
              </button>
              <button
                type="button"
                disabled={busy || validation.isValidating || !visualConfigBase}
                onClick={handleVisualValidate}
              >
                {validation.isValidating ? t('config.validating') || 'Validating...' : t('config.validate')}
              </button>
            </div>
            {!visualConfigBase && (
              <div class="validation-error visual-validation">
                <span class="error-icon">⚠️</span>
                <span>Visual editor requires the full raw YAML config to preserve secret fields.</span>
              </div>
            )}
            {/* Validation feedback */}
            {validation.error && (
              <div class="validation-error visual-validation">
                <span class="error-icon">⚠️</span>
                <span>{validation.error}</span>
              </div>
            )}
            {validation.result && (
              <div class={`validation-result visual-validation ${validation.result.valid ? 'success' : 'error'}`}>
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
        )

      case 'history':
        return (
          <ConfigHistory
            historyEntries={historyEntries}
            selectedVersion={selectedVersion}
            selectedEntry={selectedEntry}
            actionLabel={actionLabel}
            actionDisabled={actionDisabled}
            busy={busy}
            onVersionChange={onVersionChange}
            onApplySelection={handleApplySelection}
          />
        )
    }
  }

  return (
    <section class="panel">
      <div class="config-header">
        <h2>{t('config.title')}</h2>
        <div class="config-sub-tabs">
          {SUB_TABS.map((item) => (
            <button
              key={item.key}
              type="button"
              class={`config-sub-tab${subTab === item.key ? ' active' : ''}`}
              onClick={() => setSubTab(item.key)}
            >
              <Icon name={item.icon} class="sub-tab-icon" />
              <span>{t(`config.subTab.${item.key}`)}</span>
            </button>
          ))}
        </div>
      </div>

      {renderSubTab()}
    </section>
  )
}

export const ConfigTab = memo(ConfigTabComponent)
