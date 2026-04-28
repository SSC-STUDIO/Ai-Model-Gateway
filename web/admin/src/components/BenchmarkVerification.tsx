import { useI18n } from '../i18n'
import { formatUsd } from '../utils/formatting'
import { Icon } from './Icon'
import {
  caseStatus,
  identityModeLabel,
  statusBadgeClass,
  statusTone,
  useBenchmarkVerification,
} from '../hooks/useBenchmarkVerification'

interface BenchmarkVerificationProps {
  canWrite: boolean
  onRunStarted?: () => void
  onUnauthorized?: () => void
}

function formatStatusTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return value
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(parsed)
}

export function BenchmarkVerification({ canWrite, onRunStarted, onUnauthorized }: BenchmarkVerificationProps) {
  const { t } = useI18n()
  const verification = useBenchmarkVerification({ onUnauthorized })
  const routeReady = verification.allActive || Boolean(verification.providerInput.trim() && verification.publicModelInput.trim())
  const suiteReady = Boolean(verification.suiteInput.trim())
  const runDisabledReason = !canWrite
    ? t('benchmark.verification.runDisabledNoWrite')
    : verification.loadingLists
      ? t('benchmark.verification.loadingLists')
    : verification.startingRun
      ? t('benchmark.verification.runDisabledBusy')
      : !suiteReady
        ? t('benchmark.verification.runDisabledSuite')
        : !routeReady
          ? t('benchmark.verification.runDisabledRoute')
          : ''
  const importDisabledReason = !canWrite
    ? t('benchmark.verification.importDisabledNoWrite')
    : verification.importingBaseline
      ? t('benchmark.verification.importDisabledBusy')
      : !verification.baselineFile
        ? t('benchmark.verification.importDisabledFile')
        : ''
  const hasSelectedRunSettled = !verification.loadingLists && !verification.loadingRunDetail
  const hasSelectedRunTargets = (verification.selectedRun?.targets?.length ?? 0) > 0
  const handleStartRun = async () => {
    const started = await verification.startVerification()
    if (started) {
      onRunStarted?.()
    }
  }

  return (
    <div class="panel-subsection benchmark-verification">
      <div class="benchmark-verification-header">
        <div class="benchmark-verification-heading">
          <div class="benchmark-verification-title">
            <span class="benchmark-verification-icon"><Icon name="benchmark" size={18} /></span>
            <h3>{t('benchmark.verification.title')}</h3>
          </div>
          <p class="benchmark-verification-copy">{t('benchmark.verification.subtitle')}</p>
        </div>
        <div class="benchmark-verification-meta" aria-label={t('benchmark.verification.summary')}>
          <span class="status-badge neutral">{t('benchmark.verification.runs')}: {verification.runs.length}</span>
          <span class="status-badge neutral">{t('benchmark.verification.baselines')}: {verification.baselines.length}</span>
          {verification.selectedRun && <span class={statusBadgeClass(verification.selectedRun.status)}>{verification.selectedRun.status}</span>}
        </div>
      </div>

      <div class="verification-controls-grid">
        <section class="verification-surface verification-run-setup" aria-labelledby="verification-run-setup-title">
          <div class="verification-surface-header">
            <div>
              <h4 id="verification-run-setup-title">{t('benchmark.verification.runSetup')}</h4>
              <p>{t('benchmark.verification.runSetupHint')}</p>
            </div>
          </div>
          <div class="verification-form-grid">
            <label class="verification-field verification-field-check verification-field-wide">
              <input type="checkbox" checked={verification.allActive} onChange={(e) => verification.setAllActive((e.currentTarget as HTMLInputElement).checked)} />
              <span>{t('benchmark.verification.allActiveRoutes')}</span>
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.provider')}</span>
              <input
                type="text"
                value={verification.providerInput}
                onInput={(e) => verification.setProviderInput((e.currentTarget as HTMLInputElement).value)}
                placeholder="provider-id"
                disabled={verification.allActive}
              />
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.publicModel')}</span>
              <input
                type="text"
                value={verification.publicModelInput}
                onInput={(e) => verification.setPublicModelInput((e.currentTarget as HTMLInputElement).value)}
                placeholder="gpt-4o"
                disabled={verification.allActive}
              />
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.protocol')}</span>
              <select value={verification.protocolInput} onChange={(e) => verification.setProtocolInput((e.currentTarget as HTMLSelectElement).value as 'auto' | 'openai_chat_completions' | 'anthropic_messages')}>
                <option value="auto">auto</option>
                <option value="openai_chat_completions">openai_chat_completions</option>
                <option value="anthropic_messages">anthropic_messages</option>
              </select>
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.suite')}</span>
              <input type="text" value={verification.suiteInput} onInput={(e) => verification.setSuiteInput((e.currentTarget as HTMLInputElement).value)} placeholder="general_protocol_v1" required />
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.publicBaseline')}</span>
              <select value={verification.publicSnapshotID} onChange={(e) => verification.setPublicSnapshotID((e.currentTarget as HTMLSelectElement).value)}>
                <option value="">{t('benchmark.verification.none')}</option>
                {verification.baselines.filter((item) => item.kind === 'public_standard').map((item) => (
                  <option key={item.snapshot_id} value={item.snapshot_id}>{item.snapshot_id}</option>
                ))}
              </select>
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.vendorBaseline')}</span>
              <select value={verification.vendorSnapshotID} onChange={(e) => verification.setVendorSnapshotID((e.currentTarget as HTMLSelectElement).value)}>
                <option value="">{t('benchmark.verification.none')}</option>
                {verification.baselines.filter((item) => item.kind === 'vendor_claim').map((item) => (
                  <option key={item.snapshot_id} value={item.snapshot_id}>{item.snapshot_id}</option>
                ))}
              </select>
            </label>
          </div>
          <div class="verification-actions">
            <button
              class="primary verification-primary-action"
              type="button"
              onClick={() => void handleStartRun()}
              disabled={Boolean(runDisabledReason)}
              title={runDisabledReason || undefined}
            >
              <Icon name="benchmark" size={16} />
              <span>{verification.startingRun ? t('benchmark.verification.running') : t('benchmark.verification.run')}</span>
            </button>
            <button class="secondary" type="button" onClick={() => void verification.loadVerification()} disabled={verification.loadingLists}>
              <Icon name="history" size={16} />
              <span>{verification.loadingLists ? t('benchmark.verification.loadingLists') : t('benchmark.verification.refresh')}</span>
            </button>
            {runDisabledReason && <p class="verification-action-reason">{runDisabledReason}</p>}
          </div>
        </section>

        <section class="verification-surface verification-import" aria-labelledby="verification-import-title">
          <div class="verification-surface-header">
            <div>
              <h4 id="verification-import-title">{t('benchmark.verification.baselineImport')}</h4>
              <p>{t('benchmark.verification.baselineImportHint')}</p>
            </div>
          </div>
          <div class="verification-form-grid verification-form-grid-compact">
            <label class="verification-field">
              <span>{t('benchmark.verification.baselineKind')}</span>
              <select value={verification.baselineKind} onChange={(e) => verification.setBaselineKind((e.currentTarget as HTMLSelectElement).value as 'public_standard' | 'vendor_claim')}>
                <option value="public_standard">public_standard</option>
                <option value="vendor_claim">vendor_claim</option>
              </select>
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.sourceName')}</span>
              <input type="text" value={verification.baselineSourceName} onInput={(e) => verification.setBaselineSourceName((e.currentTarget as HTMLInputElement).value)} placeholder="April 2026 baseline" />
            </label>
            <label class="verification-field">
              <span>{t('benchmark.verification.baselineFile')}</span>
              <span class="verification-file-control">
                <Icon name="file" size={16} />
                <span>{verification.baselineFile?.name ?? t('benchmark.verification.chooseBaselineFile')}</span>
                <input
                  key={verification.baselineFileInputKey}
                  type="file"
                  accept=".json,.csv"
                  onChange={(e) => verification.setBaselineFile((e.currentTarget as HTMLInputElement).files?.[0] ?? null)}
                />
              </span>
            </label>
          </div>
          <div class="verification-actions">
            <button type="button" onClick={() => void verification.importBaseline()} disabled={Boolean(importDisabledReason)} title={importDisabledReason || undefined}>
              <Icon name="file" size={16} />
              <span>{verification.importingBaseline ? t('benchmark.verification.importingBaseline') : t('benchmark.verification.importBaseline')}</span>
            </button>
            {importDisabledReason && <p class="verification-action-reason">{importDisabledReason}</p>}
          </div>
        </section>
      </div>

      {verification.verificationError && <p class="status-error verification-error">{verification.verificationError}</p>}

      <div class="verification-results-grid">
        <aside class="verification-surface verification-sidebar" aria-label={t('benchmark.verification.runs')}>
          <div class="verification-surface-header compact">
            <div>
              <h4>{t('benchmark.verification.runs')}</h4>
              <p>{verification.loadingLists ? t('benchmark.verification.loadingLists') : verification.runs.length === 0 ? t('benchmark.verification.noRuns') : `${verification.runs.length} ${t('benchmark.verification.availableRuns')}`}</p>
            </div>
          </div>
          <label class="verification-field verification-field-stack">
            <span>{t('benchmark.verification.selectRun')}</span>
            <select value={verification.selectedRun?.run_id ?? ''} onChange={(e) => void verification.selectRun((e.currentTarget as HTMLSelectElement).value)} disabled={verification.loadingLists || verification.loadingRunDetail}>
              <option value="">{t('benchmark.verification.selectRunPlaceholder')}</option>
              {verification.runs.map((run) => (
                <option key={run.run_id} value={run.run_id}>
                  {run.run_id} · {run.status} · {run.suite_version}
                </option>
              ))}
            </select>
          </label>

          <div class="verification-section-heading">
            <h5>{t('benchmark.verification.baselineSnapshots')}</h5>
            <span>{verification.baselines.length}</span>
          </div>
          {verification.loadingLists ? (
            <p class="verification-muted">{t('benchmark.verification.loadingBaselines')}</p>
          ) : verification.baselines.length === 0 ? (
            <p class="verification-muted">{t('benchmark.verification.noBaselines')}</p>
          ) : (
            <>
              <div class="table-wrap verification-table verification-table-compact">
                <table>
                  <thead>
                    <tr>
                      <th>{t('benchmark.verification.snapshot')}</th>
                      <th>{t('benchmark.verification.kind')}</th>
                      <th>{t('benchmark.verification.rows')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {verification.baselines.map((item) => (
                      <tr key={item.snapshot_id}>
                        <td>{item.snapshot_id}</td>
                        <td>{item.kind}</td>
                        <td>{item.row_count}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div class="verification-baseline-list">
                {verification.baselines.map((item) => (
                  <div class="verification-baseline-card" key={item.snapshot_id}>
                    <strong>{item.snapshot_id}</strong>
                    <span>{item.kind}</span>
                    <span>{item.row_count} {t('benchmark.verification.rows')}</span>
                  </div>
                ))}
              </div>
            </>
          )}
        </aside>

        <section class="verification-surface verification-report" aria-labelledby="verification-report-title">
          <div class="verification-report-header">
            <div>
              <h4 id="verification-report-title">{t('benchmark.verification.latestReport')}</h4>
              {verification.selectedRun ? (
                <p class="verification-run-id">{verification.selectedRun.run_id}</p>
              ) : verification.loadingLists || verification.loadingRunDetail ? (
                <p class="verification-muted">{t('benchmark.verification.loadingReport')}</p>
              ) : (
                <p class="verification-muted">{t('benchmark.verification.noRunSelected')}</p>
              )}
            </div>
            {verification.selectedRun && (
              <div class="verification-report-badges">
                <span class={statusBadgeClass(verification.selectedRun.status)}>{verification.selectedRun.status}</span>
                <span class="status-badge neutral">{verification.selectedRunCompletedTargets}/{verification.selectedRunTargetCount} {t('benchmark.verification.targets')}</span>
              </div>
            )}
          </div>

          {(verification.loadingLists || verification.loadingRunDetail) && (
            <div class="verification-empty-panel">
              <Icon name="chart" size={28} />
              <p>{t('benchmark.verification.loadingReport')}</p>
            </div>
          )}

          {hasSelectedRunSettled && !verification.selectedRun && (
            <div class="verification-empty-panel">
              <Icon name="chart" size={28} />
              <p>{t('benchmark.verification.noRunSelected')}</p>
            </div>
          )}

          {hasSelectedRunSettled && verification.selectedRun && (
            <>
              <div class="verification-summary-strip">
                <div>
                  <span>{t('benchmark.verification.status')}</span>
                  <strong class={`verification-status-text ${statusTone(verification.selectedRun.status)}`}>{verification.selectedRun.status}</strong>
                </div>
                <div>
                  <span>{t('benchmark.verification.suite')}</span>
                  <strong>{verification.selectedRun.suite_version}</strong>
                </div>
                <div>
                  <span>{t('benchmark.verification.protocol')}</span>
                  <strong>{verification.selectedRun.protocol}</strong>
                </div>
                <div>
                  <span>{t('benchmark.verification.started')}</span>
                  <strong>{formatStatusTime(verification.selectedRun.started_at)}</strong>
                </div>
              </div>

              {verification.selectedRun.targets.length > 1 && (
                <label class="verification-field verification-target-select">
                  <span>{t('benchmark.verification.target')}</span>
                  <select value={verification.selectedTarget?.target_id ?? ''} onChange={(e) => verification.setSelectedTargetID((e.currentTarget as HTMLSelectElement).value)}>
                    {verification.selectedRun.targets.map((target) => (
                      <option key={target.target_id} value={target.target_id}>
                        {target.provider_id} · {target.public_model} · {target.verdict ?? target.status}
                      </option>
                    ))}
                  </select>
                </label>
              )}

              <div class="verification-report-section">
                <div class="verification-section-heading">
                  <h5>{t('benchmark.verification.targetResults')}</h5>
                  <span>{verification.selectedRun.targets.length}</span>
                </div>
                {hasSelectedRunTargets ? (
                  <div class="table-wrap verification-table">
                  <table>
                    <thead>
                      <tr>
                        <th>{t('benchmark.verification.provider')}</th>
                        <th>{t('benchmark.verification.model')}</th>
                        <th>{t('benchmark.verification.verdict')}</th>
                        <th>{t('benchmark.verification.publicGap')}</th>
                        <th>{t('benchmark.verification.vendorGap')}</th>
                        <th>{t('benchmark.verification.suspicion')}</th>
                        <th>{t('benchmark.verification.completion')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {verification.selectedRun.targets.map((target) => (
                        <tr key={target.target_id}>
                          <td>{target.provider_id}</td>
                          <td>{target.public_model}</td>
                          <td><span class={statusBadgeClass(target.verdict ?? target.status)}>{target.verdict ?? target.status}</span></td>
                          <td>{(target.public_gap ?? 0).toFixed(1)}</td>
                          <td>{(target.vendor_gap ?? 0).toFixed(1)}</td>
                          <td>{(target.suspicion_score ?? 0).toFixed(1)}</td>
                          <td>{(target.completion_rate ?? 0).toFixed(0)}%</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  </div>
                ) : (
                  <div class="verification-empty-panel verification-empty-panel-compact">
                    <Icon name="chart" size={24} />
                    <p>{t('benchmark.verification.noTargets')}</p>
                  </div>
                )}
              </div>

              {verification.selectedTarget && (
                <div class="verification-target-detail">
                  <div class="verification-score-strip">
                    <div>
                      <span>{t('benchmark.verification.verdict')}</span>
                      <strong class={`verification-status-text ${statusTone(verification.selectedTarget.verdict ?? verification.selectedTarget.status)}`}>{verification.selectedTarget.verdict ?? verification.selectedTarget.status}</strong>
                    </div>
                    <div>
                      <span>{t('benchmark.verification.completion')}</span>
                      <strong>{(verification.selectedTarget.completion_rate ?? 0).toFixed(0)}%</strong>
                    </div>
                    <div>
                      <span>{t('benchmark.verification.suspicion')}</span>
                      <strong>{(verification.selectedTarget.suspicion_score ?? 0).toFixed(1)}</strong>
                    </div>
                    <div>
                      <span>{t('benchmark.verification.estimatedCost')}</span>
                      <strong>{formatUsd(verification.selectedTarget.estimated_cost_usd ?? 0)}</strong>
                    </div>
                  </div>

                  <div class="verification-reasons">
                    <span>{t('benchmark.verification.reasons')}</span>
                    <div class="verification-reason-list">
                      {verification.selectedTargetReasons.length === 0 ? (
                        <span class="verification-muted">{t('benchmark.verification.none')}</span>
                      ) : (
                        verification.selectedTargetReasons.map((reason) => <span key={reason}>{reason}</span>)
                      )}
                    </div>
                  </div>

                  <div class="verification-report-section">
                    <div class="verification-section-heading">
                      <h5>{t('benchmark.verification.cases')}</h5>
                      <span>{verification.selectedTargetCases.length}</span>
                    </div>
                    {verification.selectedTargetCases.length === 0 ? (
                      <div class="verification-empty-panel verification-empty-panel-compact">
                        <Icon name="chart" size={24} />
                        <p>{t('benchmark.verification.noCases')}</p>
                      </div>
                    ) : (
                      <div class="table-wrap verification-table">
                      <table>
                        <thead>
                          <tr>
                            <th>{t('benchmark.verification.case')}</th>
                            <th>{t('benchmark.verification.dimension')}</th>
                            <th>{t('benchmark.verification.score')}</th>
                            <th>{t('benchmark.verification.status')}</th>
                            <th>{t('benchmark.verification.route')}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {verification.selectedTargetCases.map((item) => (
                            <tr key={item.case_id}>
                              <td>{item.case_id}</td>
                              <td>{item.dimension}</td>
                              <td>{(item.score ?? 0).toFixed(1)}</td>
                              <td><span class={statusBadgeClass(caseStatus(item))}>{caseStatus(item)}</span></td>
                              <td>{item.route_mode ?? '-'}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                      </div>
                    )}
                  </div>

                  <div class="verification-report-section">
                    <div class="verification-section-heading">
                      <h5>{t('benchmark.verification.rawTelemetry')}</h5>
                      {!verification.telemetryLoading && !verification.selectedRunTelemetryError && (
                        <span>{t('benchmark.verification.identity')}: {identityModeLabel(verification.selectedRunTelemetryIdentityMode)}</span>
                      )}
                    </div>
                    {verification.selectedRunTelemetryError && <p class="status-error">{verification.selectedRunTelemetryError}</p>}
                    {verification.telemetryLoading && <p class="verification-muted">{t('benchmark.verification.loadingTelemetry')}</p>}
                    {!verification.telemetryLoading && !verification.selectedRunTelemetryError && verification.selectedTelemetryRequests.length === 0 && (
                      <p class="verification-muted">{t('benchmark.verification.noTelemetry')}</p>
                    )}
                    {!verification.telemetryLoading && verification.selectedTelemetryRequests.length > 0 && (
                      <div class="table-wrap verification-table">
                        <table>
                          <thead>
                            <tr>
                              <th>{t('benchmark.verification.time')}</th>
                              <th>{t('benchmark.verification.case')}</th>
                              <th>{t('benchmark.verification.status')}</th>
                              <th>{t('benchmark.verification.latency')}</th>
                              <th>{t('benchmark.verification.tokens')}</th>
                              <th>{t('benchmark.verification.cost')}</th>
                              <th>{t('benchmark.verification.route')}</th>
                              <th>{t('benchmark.verification.error')}</th>
                            </tr>
                          </thead>
                          <tbody>
                            {verification.selectedTelemetryRequests.map((item) => (
                              <tr key={`${item.RequestID ?? item.request_id ?? 'req'}-${item.BenchmarkCaseID ?? item.benchmark_case_id ?? 'case'}`}>
                                <td>{(item.Timestamp ?? item.time ?? '').replace('T', ' ').replace('Z', '')}</td>
                                <td>{item.BenchmarkCaseID ?? item.benchmark_case_id ?? '-'}</td>
                                <td>{item.StatusCode ?? item.status ?? 0}</td>
                                <td>{((item.LatencyMs ?? item.latency_ms) ?? 0).toFixed(0)}ms</td>
                                <td>{(((item.InputTokens ?? item.input_tokens) ?? 0) + ((item.OutputTokens ?? item.output_tokens) ?? 0)).toLocaleString()}</td>
                                <td>{formatUsd((item.TotalCostUSD ?? item.total_cost_usd) ?? 0)}</td>
                                <td>{item.RouteMode ?? item.route_mode ?? '-'}</td>
                                <td>{item.Error ?? item.error ?? '-'}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </section>
      </div>
    </div>
  )
}
