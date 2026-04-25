import { useCallback, useEffect, useMemo, useRef, useState } from 'preact/hooks'
import type {
  DataResponse,
  RequestEntry,
  VerificationBaselineSnapshot,
  VerificationRunCase,
  VerificationRunDetail,
  VerificationRunSummary,
  VerificationRunTarget,
} from '../types'
import { fetchJSON } from '../utils/fetch'
import { normalizeTelemetryResponse } from '../utils/controlApi'

export type SelectedTelemetryIdentityMode = 'exact' | 'legacy' | 'none'

export function statusTone(value: string | null | undefined): 'success' | 'warning' | 'error' | 'neutral' {
  const normalized = (value ?? '').toLowerCase()
  if (['pass', 'passed', 'success', 'succeeded', 'complete', 'completed', 'ok', 'ready', 'normal'].includes(normalized)) {
    return 'success'
  }
  if (['running', 'pending', 'queued', 'incomplete', 'partial', 'cancelled', 'canceled', 'suspect', 'suspicious'].includes(normalized)) {
    return 'warning'
  }
  if (['fail', 'failed', 'error', 'blocked', 'rejected', 'highly_suspect', 'mismatch'].includes(normalized)) {
    return 'error'
  }
  return 'neutral'
}

export function statusBadgeClass(value: string | null | undefined): string {
  return `status-badge ${statusTone(value)}`
}

export function caseStatus(item: VerificationRunCase): string {
  if (!item.completed) return 'incomplete'
  return item.success ? 'pass' : 'fail'
}

export function identityModeLabel(mode: SelectedTelemetryIdentityMode): string {
  if (mode === 'exact') return 'exact benchmark_target_id'
  if (mode === 'legacy') return 'legacy heuristic fallback'
  return 'no matched telemetry'
}

function requestBenchmarkTargetID(entry: RequestEntry): string {
  return (entry.BenchmarkTargetID ?? entry.benchmark_target_id ?? '').trim()
}

function requestRequestedModel(entry: RequestEntry): string {
  return (entry.RequestedModel ?? entry.requested_model ?? '').trim()
}

function requestEffectiveModel(entry: RequestEntry): string {
  return (entry.EffectiveModel ?? entry.effective_model ?? entry.Model ?? entry.model ?? '').trim()
}

function requestProvider(entry: RequestEntry): string {
  return (entry.Upstream ?? entry.upstream ?? '').trim()
}

function matchesLegacyVerificationTarget(target: VerificationRunTarget, entry: RequestEntry): boolean {
  const provider = requestProvider(entry)
  if (!provider || provider !== target.provider_id) {
    return false
  }

  const requestedModel = requestRequestedModel(entry)
  const effectiveModel = requestEffectiveModel(entry)
  const targetEffectiveModel = (target.effective_model ?? '').trim()

  if (targetEffectiveModel) {
    if (effectiveModel && effectiveModel !== targetEffectiveModel) {
      return false
    }
    return effectiveModel === targetEffectiveModel || requestedModel === target.public_model
  }

  return requestedModel === target.public_model || effectiveModel === target.public_model
}

function filterLegacyTelemetryForTarget(data: DataResponse | null, target: VerificationRunTarget): DataResponse | null {
  if (!data?.requests?.length) return data
  const requests = data.requests.filter((entry) => requestBenchmarkTargetID(entry) === '' && matchesLegacyVerificationTarget(target, entry))
  if (requests.length === 0) {
    return {
      ...data,
      requests: [],
      errors: [],
    }
  }
  return {
    ...data,
    requests,
    errors: (data.errors ?? []).filter((entry) => {
      const model = (entry.Model ?? entry.model ?? '').trim()
      const provider = (entry.Upstream ?? entry.upstream ?? '').trim()
      if (provider !== '' && provider !== target.provider_id) {
        return false
      }
      const targetEffectiveModel = (target.effective_model ?? '').trim()
      if (!model) {
        return true
      }
      if (targetEffectiveModel && model !== targetEffectiveModel && model !== target.public_model) {
        return false
      }
      return model === targetEffectiveModel || model === target.public_model
    }),
  }
}

interface UseBenchmarkVerificationOptions {
  onUnauthorized?: () => void
}

export function useBenchmarkVerification({ onUnauthorized }: UseBenchmarkVerificationOptions) {
  const [providerInput, setProviderInput] = useState('')
  const [publicModelInput, setPublicModelInput] = useState('')
  const [protocolInput, setProtocolInput] = useState<'auto' | 'openai_chat_completions' | 'anthropic_messages'>('auto')
  const [suiteInput, setSuiteInput] = useState('general_protocol_v1')
  const [allActive, setAllActive] = useState(false)
  const [publicSnapshotID, setPublicSnapshotID] = useState('')
  const [vendorSnapshotID, setVendorSnapshotID] = useState('')
  const [baselineKind, setBaselineKind] = useState<'public_standard' | 'vendor_claim'>('public_standard')
  const [baselineSourceName, setBaselineSourceName] = useState('')
  const [baselineFile, setBaselineFile] = useState<File | null>(null)
  const [baselines, setBaselines] = useState<VerificationBaselineSnapshot[]>([])
  const [runs, setRuns] = useState<VerificationRunSummary[]>([])
  const [selectedRun, setSelectedRun] = useState<VerificationRunDetail | null>(null)
  const [selectedTargetID, setSelectedTargetID] = useState('')
  const [selectedRunTelemetry, setSelectedRunTelemetry] = useState<DataResponse | null>(null)
  const [selectedRunTelemetryIdentityMode, setSelectedRunTelemetryIdentityMode] = useState<SelectedTelemetryIdentityMode>('none')
  const [telemetryLoading, setTelemetryLoading] = useState(false)
  const [selectedRunTelemetryError, setSelectedRunTelemetryError] = useState('')
  const [loadingLists, setLoadingLists] = useState(true)
  const [loadingRunDetail, setLoadingRunDetail] = useState(false)
  const [startingRun, setStartingRun] = useState(false)
  const [importingBaseline, setImportingBaseline] = useState(false)
  const [verificationError, setVerificationError] = useState('')
  const [baselineFileInputKey, setBaselineFileInputKey] = useState(0)
  const selectedRunIDRef = useRef('')
  const baselineDefaultsInitializedRef = useRef(false)

  const selectedTarget = useMemo<VerificationRunTarget | null>(() => {
    if (!selectedRun?.targets?.length) return null
    return selectedRun.targets.find((target) => target.target_id === selectedTargetID) ?? selectedRun.targets[0]
  }, [selectedRun, selectedTargetID])

  const loadRunDetail = useCallback(
    async (runID: string) => {
      if (!runID) {
        selectedRunIDRef.current = ''
        setSelectedRun(null)
        setSelectedTargetID('')
        setSelectedRunTelemetry(null)
        setSelectedRunTelemetryError('')
        return
      }
      setLoadingRunDetail(true)
      try {
        const payload = await fetchJSON<VerificationRunDetail>(`/api/admin/benchmark/runs/${encodeURIComponent(runID)}`, { onUnauthorized })
        selectedRunIDRef.current = payload.run_id
        setSelectedRun(payload)
        setSelectedTargetID(payload.targets?.[0]?.target_id ?? '')
      } finally {
        setLoadingRunDetail(false)
      }
    },
    [onUnauthorized]
  )

  const loadVerification = useCallback(async (preferredRunIDOverride?: string) => {
    setLoadingLists(true)
    setVerificationError('')
    try {
      const [baselinePayload, runPayload] = await Promise.all([
        fetchJSON<{ snapshots: VerificationBaselineSnapshot[] }>('/api/admin/benchmark/baselines', { onUnauthorized }),
        fetchJSON<{ runs: VerificationRunSummary[] }>('/api/admin/benchmark/runs?limit=20', { onUnauthorized }),
      ])
      const nextBaselines = baselinePayload.snapshots ?? []
      const nextRuns = runPayload.runs ?? []
      setBaselines(nextBaselines)
      setRuns(nextRuns)

      const publicBaseline = nextBaselines.find((item) => item.kind === 'public_standard')
      const vendorBaseline = nextBaselines.find((item) => item.kind === 'vendor_claim')
      if (!baselineDefaultsInitializedRef.current) {
        if (publicBaseline?.snapshot_id) {
          setPublicSnapshotID((current) => current || publicBaseline.snapshot_id)
        }
        if (vendorBaseline?.snapshot_id) {
          setVendorSnapshotID((current) => current || vendorBaseline.snapshot_id)
        }
        baselineDefaultsInitializedRef.current = true
      }
      const preferredRunID = preferredRunIDOverride || selectedRunIDRef.current || nextRuns[0]?.run_id
      if (preferredRunID) {
        await loadRunDetail(preferredRunID)
      } else {
        selectedRunIDRef.current = ''
        setSelectedRun(null)
        setSelectedTargetID('')
      }
    } catch (error) {
      setVerificationError(error instanceof Error ? error.message : 'Failed to load verification data')
    } finally {
      setLoadingLists(false)
    }
  }, [loadRunDetail, onUnauthorized])

  useEffect(() => {
    void loadVerification()
  }, [loadVerification])

  const selectRun = useCallback(
    async (runID: string) => {
      setVerificationError('')
      try {
        await loadRunDetail(runID)
      } catch (error) {
        setVerificationError(error instanceof Error ? error.message : 'Failed to load verification run')
      }
    },
    [loadRunDetail]
  )

  const startVerification = useCallback(async () => {
    setStartingRun(true)
    setVerificationError('')
    try {
      const payload = await fetchJSON<VerificationRunDetail>('/api/admin/benchmark/runs', {
        method: 'POST',
        onUnauthorized,
        body: JSON.stringify({
          provider_id: allActive ? '' : providerInput.trim(),
          public_model: allActive ? '' : publicModelInput.trim(),
          protocol: protocolInput === 'auto' ? '' : protocolInput,
          all_active: allActive,
          suite: suiteInput.trim(),
          public_snapshot_id: publicSnapshotID.trim(),
          vendor_snapshot_id: vendorSnapshotID.trim(),
        }),
      })
      selectedRunIDRef.current = payload.run_id
      setSelectedRun(payload)
      setSelectedTargetID(payload.targets?.[0]?.target_id ?? '')
      await loadVerification(payload.run_id)
    } catch (error) {
      setVerificationError(error instanceof Error ? error.message : 'Failed to start verification run')
    } finally {
      setStartingRun(false)
    }
  }, [allActive, loadVerification, onUnauthorized, protocolInput, providerInput, publicModelInput, publicSnapshotID, suiteInput, vendorSnapshotID])

  const importBaseline = useCallback(async () => {
    if (!baselineFile) {
      setVerificationError('Select a baseline file first')
      return
    }
    setImportingBaseline(true)
    setVerificationError('')
    try {
      const contents = await baselineFile.text()
      await fetchJSON('/api/admin/benchmark/baselines/import', {
        method: 'POST',
        onUnauthorized,
        body: JSON.stringify({
          kind: baselineKind,
          source_name: baselineSourceName.trim() || baselineFile.name,
          file_name: baselineFile.name,
          contents,
        }),
      })
      setBaselineFile(null)
      setBaselineFileInputKey((value) => value + 1)
      await loadVerification()
    } catch (error) {
      setVerificationError(error instanceof Error ? error.message : 'Failed to import baseline')
    } finally {
      setImportingBaseline(false)
    }
  }, [baselineFile, baselineKind, baselineSourceName, loadVerification, onUnauthorized])

  useEffect(() => {
    const runID = selectedRun?.run_id?.trim()
    if (!runID || !selectedTarget) {
      setSelectedRunTelemetry(null)
      setSelectedRunTelemetryIdentityMode('none')
      setSelectedRunTelemetryError('')
      setTelemetryLoading(false)
      return
    }

    let cancelled = false
    setSelectedRunTelemetry(null)
    setSelectedRunTelemetryIdentityMode('none')
    setTelemetryLoading(true)
    setSelectedRunTelemetryError('')

    const loadTargetTelemetry = async () => {
      const exactParams = new URLSearchParams()
      exactParams.set('limit', '200')
      exactParams.set('target_id', selectedTarget.target_id)

      const exactPayload = await fetchJSON<unknown>(
        `/api/admin/benchmark/runs/${encodeURIComponent(runID)}/telemetry?${exactParams.toString()}`,
        { onUnauthorized }
      )
      const exactTelemetry = normalizeTelemetryResponse(exactPayload)
      if ((exactTelemetry?.requests?.length ?? 0) > 0) {
        return { telemetry: exactTelemetry, identityMode: 'exact' as const }
      }

      const legacyParams = new URLSearchParams()
      legacyParams.set('limit', '200')
      legacyParams.set('providers', selectedTarget.provider_id)
      legacyParams.set('models', selectedTarget.public_model)
      const legacyPayload = await fetchJSON<unknown>(
        `/api/admin/benchmark/runs/${encodeURIComponent(runID)}/telemetry?${legacyParams.toString()}`,
        { onUnauthorized }
      )
      const legacyTelemetry = filterLegacyTelemetryForTarget(normalizeTelemetryResponse(legacyPayload), selectedTarget)
      if ((legacyTelemetry?.requests?.length ?? 0) > 0) {
        return { telemetry: legacyTelemetry, identityMode: 'legacy' as const }
      }

      return { telemetry: exactTelemetry ?? legacyTelemetry, identityMode: 'none' as const }
    }

    void loadTargetTelemetry()
      .then(({ telemetry, identityMode }) => {
        if (cancelled) return
        setSelectedRunTelemetry(telemetry)
        setSelectedRunTelemetryIdentityMode(identityMode)
      })
      .catch((error) => {
        if (cancelled) return
        setSelectedRunTelemetry(null)
        setSelectedRunTelemetryIdentityMode('none')
        setSelectedRunTelemetryError(error instanceof Error ? error.message : 'Failed to load benchmark telemetry')
      })
      .finally(() => {
        if (!cancelled) {
          setTelemetryLoading(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [onUnauthorized, selectedRun?.run_id, selectedTarget])

  const selectedTargetCases = selectedTarget?.cases ?? []
  const selectedTargetReasons = selectedTarget?.reason_codes ?? []
  const selectedTelemetryRequests = selectedRunTelemetry?.requests ?? []
  const selectedRunTargetCount = selectedRun?.target_count ?? selectedRun?.targets?.length ?? 0
  const selectedRunCompletedTargets = selectedRun?.completed_targets ?? (selectedRun?.targets ?? []).filter((target) => statusTone(target.status) === 'success').length
  const verificationLoading = loadingLists || loadingRunDetail || startingRun || importingBaseline

  return {
    allActive,
    baselineFile,
    baselineFileInputKey,
    baselineKind,
    baselineSourceName,
    baselines,
    importingBaseline,
    importBaseline,
    loadingLists,
    loadingRunDetail,
    loadVerification,
    protocolInput,
    providerInput,
    publicModelInput,
    publicSnapshotID,
    runs,
    selectRun,
    selectedRun,
    selectedRunCompletedTargets,
    selectedRunTargetCount,
    selectedRunTelemetryError,
    selectedRunTelemetryIdentityMode,
    selectedRunTelemetryLoading: telemetryLoading,
    startingRun,
    selectedTarget,
    selectedTargetCases,
    selectedTargetID,
    selectedTargetReasons,
    selectedTelemetryRequests,
    setAllActive,
    setBaselineFile,
    setBaselineKind,
    setBaselineSourceName,
    setProtocolInput,
    setProviderInput,
    setPublicModelInput,
    setPublicSnapshotID,
    setSelectedTargetID,
    setSuiteInput,
    setVendorSnapshotID,
    startVerification,
    suiteInput,
    telemetryLoading,
    vendorSnapshotID,
    verificationError,
    verificationLoading,
  }
}
