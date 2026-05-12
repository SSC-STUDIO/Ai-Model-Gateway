# Progress

## 2026-05-12

### Eventlog Test Coverage Improvement

- Fixed build failure in `internal/control/api/handlers_test.go` caused by unused `audit` import.
- Expanded `internal/telemetry/eventlog/log_test.go` from 8 test functions to 15 test functions.
- Added `TestSerializePayloadPricingFields` covering all 13 pricing fields in serialized payload (pricing_status, pricing_source_id, pricing_currency, pricing_fx_rate_to_usd, pricing_input_per_1m, pricing_cached_input_per_1m, pricing_output_per_1m, pricing_prompt_cost, pricing_completion_cost, pricing_total_cost, pricing_prompt_cost_usd, pricing_completion_cost_usd, pricing_total_cost_usd).
- Added `TestSerializePayloadBenchmarkFields` covering synthetic_kind, benchmark_run_id, benchmark_target_id, and benchmark_case_id serialization.
- Added `TestNewMkdirAllError` covering directory creation failure when a file blocks the path.
- Added `TestAppendClosedDB` covering Append error path when database is already closed.
- Added `TestAppendDuplicateAndAccept` covering mixed batch with both duplicate (dropped) and new (accepted) events.
- Added `TestAppendWithAllFields` covering end-to-end Append with full pricing and benchmark payload fields, verifying persisted JSON content.
- Added `TestAppendConcurrent` covering concurrent Append from 5 goroutines with mutex serialization.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42+ packages green. Coverage: 74.0% → 78.0%.

### Telemetry Test Coverage Improvement

- Expanded `internal/telemetry/pricing_test.go` with 25 new test functions targeting uncovered code paths.
- Added pricing runtime tests: `TestShouldRefresh` (5 sub-cases for zero/recent/old/zero-interval/defaults), `TestCloneSourceCatalogs` (nil/empty/non-empty mutation independence), `TestMergeSourceCatalogs` (normal/nil-base), `TestClonePricingSourceStates` (nil/empty/non-empty mutation independence), `TestClonePricingFXSnapshot` (empty/non-empty mutation independence), `TestLoadPricingFXCache` (empty-path error, valid round-trip, invalid JSON), `TestSavePricingFXCacheInvalidPath` (empty path no-op), `TestApplyFXToPrice` (USD/CNY/disabled/noRates), `TestApplyFXToCatalog` (empty/non-empty), `TestAnnotateSourceCatalog` (field verification), `TestHydrateSourceStates` (enabled/disabled sources, empty config), `TestClonePricingCatalogSnapshot` (deep mutation independence across Catalog/Sources/FX).
- Added pricing source/parser tests: `TestInferCurrency` (8 sub-cases for $, US$, ￥, ¥, CNY, 元, unknown with USD/CNY fallback), `TestExtractMoneyValues` (6 sub-cases for single/multiple/none/US$/￥), `TestParseModelAliasesFromCell` (single/empty/multi), `TestExtractPriceFromCells` (6 sub-cases: input+output, cache+output, empty, no money, unordered, three values), `TestParseGenericPricingTables` (simple table, empty body, no valid rows), `TestFetchPricingSourceCatalogUnsupportedVendor` (error path), `TestFetchSinglePageCatalogEmptyURL` (error path).
- Added mock-server tests: `TestFetchSinglePageCatalogWithMockServer` (httptest success path with parseGenericPricingTables, empty pricing rows error, 500 status error).
- Added FX XML parsing tests: `TestFetchPricingFXFromBytesSuccess` (valid ECB XML with USD/JPY/GBP/CNY rates, verifies BaseCurrency=EUR and correct USD conversion), `TestFetchPricingFXFromBytes` (invalid XML, missing USD rate).
- Added pricing page parser tests: `TestParseAPIPricingPage` (valid OpenAI pricing card, empty body, no price values), `TestParseGPT52PricingPage` (valid table with 2 models, empty body, no matching table).
- Refactored `fetchPricingFX` in `pricing_runtime.go` to extract `fetchPricingFXFromBytes` for testable FX XML parsing without HTTP/SSRF dependency.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42 packages green. Coverage: 64.6% → 79.9%.

### Rate Limiter Test Coverage Improvement

- Expanded `internal/gateway/ratelimit/limiter_test.go` from 10 test functions to 14 test functions.
- Added `TestAllow_CleanupStaleBuckets` covering periodic stale-bucket sweep: creates a stale bucket (10min old) and a fresh bucket, triggers the 256-call cleanup interval, and verifies the stale bucket is removed while the fresh one survives.
- Added `TestAllow_MaxBuckets_RejectsNewKeys` covering maxBuckets enforcement: sets `maxBuckets=2`, fills capacity, and verifies a third key is rejected while existing keys still work.
- Added `TestAllow_MaxBuckets_CleanupAllowsNewKeys` covering maxBuckets with stale cleanup: sets `maxBuckets=2` with one stale and one fresh bucket, verifies a new key is allowed after cleanup frees the stale entry.
- Added `TestAllow_TokenRefillCap` covering burst cap on token refill: uses one token, waits 1.1s (100 rps * 1.1s = 110 tokens >> burst of 5), and verifies tokens are capped to the burst value.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42 packages green. Coverage: 81.0% → 100.0%.

### Benchmarking Test Coverage Improvement

- Expanded `internal/control/benchmarking/suite_test.go` from 2 test functions to 37 test functions.
- Added `TestBuildRequestOpenAIProtocol` and `TestBuildRequestUnsupportedProtocol` covering OpenAI (default) and unsupported protocol error paths.
- Added `TestBuildToolRequestOpenAIProtocol` and `TestBuildToolRequestUnsupportedProtocol` covering OpenAI tool request and unsupported protocol paths.
- Added 5 `extractAssistantText` tests: OpenAI choices, Anthropic multi-content with join, Anthropic non-text skip fallback, OpenAI empty choices fallback, invalid JSON fallback.
- Added 6 `extractToolCall` tests: OpenAI match/mismatch/no tool_calls, Anthropic match/mismatch, plus invalid JSON for both protocols.
- Added `TestClampScore` covering all boundary values (-10, 0, 50, 100, 110).
- Added `TestExcerpt` covering short, long (truncation to 320), and whitespace trimming.
- Added `TestSuiteByName` covering empty (defaults to general_protocol_v1), valid, and unknown suite error.
- Added 3 `exactTextScorer` tests: match (score 100), mismatch (score 0), incomplete response.
- Added 4 `judgeScorer` tests: nil judge, empty response, judge error, successful scoring.
- Added 3 `jsonScorer` tests: valid full match, partial match (animal correct, count wrong), invalid JSON.
- Added 2 `toolCallScorer` tests: match and mismatch.
- Added 3 `streamScorer` tests: full score (data + done + tokens), Anthropic message_stop, no data.
- Added 4 `baseCaseResult` tests: nil response, explicit error, HTTP 429 status-only error, field copying verification.
- Expanded `internal/control/benchmarking/store_test.go` from 1 test function to 22 test functions.
- Added `TestListRunsEmpty` and `TestListRunsAfterStartRun` covering empty and populated ListRuns.
- Added `TestServiceListRunsNilService` and `TestServiceGetRunNilService` covering nil Service guards.
- Added 4 StartRun error path tests: nil service, no gateway runner, no config source, disabled benchmarking.
- Added 4 resolveJudgeProvider tests: nil config, preferred provider not found, model mismatch, fallback not found.
- Added `TestNormalizeProtocol` covering empty, auto, known protocols, and unknown passthrough.
- Added `TestUniqueStrings` covering deduplication and nil input.
- Added `TestWeightedAverage` and `TestWeightedAverageZeroWeight` covering normal and zero-total-weight.
- Added `TestMaxFloat` and `TestAbsFloat` for utility function coverage.
- Added 4 expandTargets tests: nil config, missing provider_id/public_model, provider not found, model not advertised.
- Added `TestValidateBaselineSelectionEmpty` and `TestParseBaselineRowsEmpty` for validation edge cases.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42 packages green. Coverage: 72.2% → 83.4%.

### i18n And Compiler Test Coverage Improvement

- Expanded `internal/i18n/bundle_test.go` from 6 test functions to 10 test functions.
- Added `TestSetLanguage` covering language change and T-key resolution after change.
- Added `TestMustLoadCatalog_Success` covering all 7 supported languages (zh, en, ja, ko, es, fr, de).
- Added `TestMustLoadCatalog_FallbackLang` covering unknown language normalization to zh.
- Added `TestLoadCatalog_InvalidLang` covering all normalizeLang variants (zh, zh-CN, zh-TW, en, en-US, en-GB, ja, ko, es, fr, de).
- i18n coverage: 79.5% → 92.3%.
- Expanded `internal/control/compiler/compiler_test.go` from 13 test functions to 25 test functions.
- Added `TestCloneStringMap` covering non-empty, empty, and nil map cloning with mutation independence.
- Added `TestCompileCompatPolicy` covering bridge rules with empty from/to skipping and empty bridge.
- Added `TestValidate_MissingProviderFields` covering 8 validation sub-cases (missing provider_id, base_url, model_table, snapshot_id, wrong schema version, missing ingress listen, empty providers, valid snapshot).
- Added `TestResolveQuotaRecoveryIntervalMin` covering 6 sub-cases (disable cooldown, zero/negative/default config, below minimum, exact minimum, valid value).
- Added `TestCompile_EmptyRevisionID` and `TestCompile_NilConfigFromSource` covering edge cases.
- Added `TestCompileFromConfig_WithPricingSources` covering pricing sources, manual prices, and FX config compilation.
- Added `TestCompileFromConfig_ClonePreservesOriginal*` covering clone mutation independence for pricing sources and fallback models.
- Added `TestCompileFromConfig_HeadersAllEmpty` and `TestCompileFromConfig_EmptyCredentials` covering edge cases.
- Added `TestCompileProvider_*` covering invalid URL scheme, empty name, and empty base_url errors.
- Compiler coverage: 79.1% → 97.1%.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42 packages green.

### Pricing Catalog Test Coverage Improvement

- Expanded `internal/infra/pricing/catalog_test.go` from 10 test functions to 17 test functions.
- Added tests for `RefreshNow` nil guard (returns nil for nil catalog) and normal invocation (returns without panic, snapshot remains valid).
- Added `TestFromLegacySnapshotWithFullData` covering complete snapshot mapping with Sources (enabled/disabled, all fields), SourceCatalogs (multi-source with model prices), and FX rates (multi-currency with mutation independence).
- Added `TestFromLegacySnapshotEmptyData` covering empty input producing non-nil empty catalog, zero-length sources, non-nil sourceCatalogs, and nil FX rates.
- Added `TestCloneRatesNonEmpty` covering correct value cloning and mutation independence between source and clone.
- Added `TestCloneRatesNil` and `TestCloneRatesEmpty` covering nil/empty map inputs returning nil.
- Added `TestSourceStateFields` and `TestFXSnapshotFields` for struct field coverage.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42 packages green. Coverage: 64.5% → 100.0%.

### Cache And WebSocket Test Coverage Improvement

- Expanded `internal/gateway/cache/cache_test.go` from 17 test functions to 33 test functions.
- Added tests for `Close` (idempotent double-close), `Stats` (hit/miss/eviction counters), `MaxBytes` accessor, byte-limit LRU eviction (`ByteLimitEviction`, `ZeroByteCacheEvictsEverything`), `sweepExpired` (removes expired entries, ignores fresh entries), `MakeKeyLegacy` equivalence with `MakeKey(empty ns)`, `MakeKey` with namespace differentiation, `makeKeyRaw` reference SHA comparison, byte accounting on overwrite (`ByteLimitAccounting`), delete-decrements-bytes, ensure-size evicts beyond byte limit, and sweep-on-get expiry removal.
- Fixed pre-existing concurrent websocket write panic in `TestForwardMessages_NormalFlow`: removed a test-side `echoConn.WriteMessage` call that raced with the `forwardMessages` goroutine writing to the same connection.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42+ packages green. Cache coverage: 59.8% → 86.1%.

### SSRF Proxy Test Coverage Improvement

- Expanded `internal/proxy/ssrf_test.go` from 6 test functions (covering only basic IsPrivateIP, ValidateURL, AllowedSchemes, BlockedRanges, and path traversal) to 24 test functions.
- Added tests for `AllowLocalhost` and `AllowPrivateIP` configuration options, confirming that AllowLocalhost only bypasses the hostname string check while DNS resolution still blocks private IPs unless AllowPrivateIP is also set.
- Added tests for `ResolveAndValidateHost` covering nil-return for no blocked ranges, public IP resolution, and unresolvable host errors.
- Added edge-case tests for `ValidateURL`: hex-encoded host (0x7f000001), path traversal variants, ws/wss scheme support, user-info-only and user:pass URL variants.
- Added tests for `NewSafeTransport` (nil and custom base transport), `NewSafeDialer` (TLS 1.2 minimum, NetDialContext injection), and `pinnedIPDialer` (direct private IP, localhost IP, private-resolved hostname, public host pass-through, invalid address, unresolvable host).
- Added end-to-end httptest TLS server test and transport-level private IP blocking test.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42 packages green. Coverage: 51.3% → 97.4%.

### Test HTTP Client Isolation Improvement

- Added exported `SetSharedHTTPClientForTesting` function in `internal/gateway/api/handler.go` to allow tests to swap the shared HTTP client and restore it via a cleanup function.
- Refactored `cmd/gatewayd/bridge_e2e_test.go` e2e tests to use `SetSharedHTTPClientForTesting(&http.Client{})` so each test gets an isolated HTTP client instead of sharing the production client.
- Refactored `internal/gateway/api/compat_test.go` tests to use the existing `swapSharedHTTPClient` helper consistently, and moved `allowLocalAnthropicTestUpstreams` calls before other test setup for correct initialization order.
- Validation: `go build ./...` passed; `go test ./... -count=1` passed with all 42 packages green.

## 2026-05-11

### Benchmark Segmented Navigation Refactor - Validation Complete

- Finalized the Benchmark segmented workspace refactor: Phase 3 (Validate) and Phase 4 (Summarize) completed.
- `npx tsc --noEmit` passed with no type errors.
- `npx vitest run` reported 13 files / 109 tests passed.
- `npm run build` produced `index-DenIUs7I.js`, `vendor-BYVBASC6.js`, and `index-B1k_hw-m.css` successfully.
- Code inspection confirmed each segment renders inside its own `WorkspaceBand` (upstream at line 338, capability at line 504), matching the Monitoring segmented pattern.
- No remaining risks or outstanding work for this task.

## 2026-05-10

### Benchmark Capability Quick Run And Chart Polish

- Started follow-up for missing `benchmark.verification.quickRun` translation, capability verification run UX, and chart style cleanup.
- Re-read required workstation context plus AI Model Gateway runtime notes; bootstrap status is OK and capsule status reports the latest gateway/admin fixes resolved.
- Confirmed the current hook's quick-run path posts `all_active: true`, but the manual capability verification form still defaults `allActive` to false and therefore looks blocked until a provider/model is entered.
- Confirmed `BenchmarkVerification.tsx` uses fallback expressions such as `t('benchmark.verification.quickRun') || 'Quick Run'`, but this i18n layer returns the key itself when missing, so missing locale keys render raw keys instead of fallback text.
- Confirmed `charts.css` still uses radial glow backgrounds for line/history/timeseries chart containers; these should be replaced with restrained panel/plot styling.
- Added missing `benchmark.verification.*` quick-run/read-only/polling locale keys across all loaded locale JSON files, with Chinese copy for the visible Chinese UI.
- Changed capability verification default `allActive` state to true so the manual run form starts ready to test every enabled upstream provider/model route.
- Added hook tests for default all-active behavior and quick-run POST body `{ all_active: true, suite: "general_protocol_v1" }`.
- Cleaned chart CSS to remove radial/glow backgrounds from chart containers and timeseries charts, reduce decorative shadows, remove negative letter spacing in chart text, and add a restrained plot backdrop.
- Validation passed: `npm --prefix web/admin test` reported 13 files / 109 tests passed; `npm --prefix web/admin run build` passed.
- Deployed with `bash scripts/sync-bundle-to-home-ai-gateway.sh`; live `ai-gateway.service` restarted at 2026-05-10 09:41:50 CST and health returned `status=healthy`, `version=1.3.0`.
- Verified live bundle with `/home/chenrunsen/ai-gateway/bin/aigw bundle verify -root /home/chenrunsen/ai-gateway -manifest /home/chenrunsen/ai-gateway/aigw-manifest.json`.
- Added and ran `output/playwright/benchmark_capability_check.cjs`; live `/admin/benchmark` Chinese capability page verified no raw `benchmark.verification.*` keys, default checked all-upstream checkbox, disabled provider/model inputs, visible quick-run button, and non-radial chart background.
- Ran live full UI audit `AIGW_ADMIN_BASE_URL=http://127.0.0.1:18080 node output/playwright/ui_review_live.cjs`; 44 views/subviews, 0 console errors, 0 page errors. Remaining categories match prior known audit noise.

## 2026-05-09

### WSL Codex Uses Gateway But Does Not Edit Code

- Started investigation of WSL Codex behavior where it uses the local AI gateway and reads code but does not modify code.
- Read required workstation context, bootstrap/runtime/known-issues references, AI-Model-Gateway runtime notes, and existing planning files.
- `bootstrap-status.ps1` reports all Windows/WSL Codex and Claude bootstrap files plus skill links are OK.
- `capsules-status.ps1` reports 63 capsules; relevant prior issues include WSL Codex model catalog path and earlier AI gateway Responses route/provider behavior.
- `codex --version` in WSL reports `codex-cli 0.128.0` at `/home/chenrunsen/.local/bin/codex`.
- First attempt to redact and print WSL Codex config failed because PowerShell parsed a complex sed regex; switching to simpler WSL/Python-based redaction.

## 2026-05-09

### Admin UI Icon And Logs Interaction Bugs

- Started a targeted follow-up for broken small-icon interactions, Logs page "load more", and 504/error display behavior.
- Re-read required workstation context, AI-Model-Gateway runtime notes, planning skill guidance, and previous plan/progress/findings.
- Initial code inspection found `LogsTab` expands rows only through whole-row clicks, has no explicit detail icon/button column, and computes `handleLoadMore` offset as `fetchedOffset + 500` even though initial telemetry length can differ from 500.
- `LogsTab` i18n contains `logs.loadMore`, but the current rendered bottom controls use paginator buttons only; need verify whether a load-more control was removed or is hidden by logic.
- Browser reproduction on `/admin/logs` confirmed the page had one 504 row, no explicit detail icon before the fix, and no visible load-more action in the 24h window.
- Added `total` to normalized telemetry data, made log load-more use the loaded request count as the next backend offset, and rendered a real load-more button with loaded/total status.
- Added an explicit log detail icon button column and converted 504 display to `HTTP 504`; the row no longer relies on whole-row click behavior.
- Added error-preview click behavior and full error wrapping in the details panel so the 504 `context canceled` message is readable.
- Added English/Chinese i18n for details, loaded count, no-more, clear search, and load-more failure states.
- Added `logsURL` and telemetry `total` coverage to `web/admin/src/utils/controlApi.test.ts`.
- Ran `npm --prefix web/admin run build`; frontend build passed with `index-CtPBQ75P.js` and `index-DjoTwoIr.css`.
- Ran `npm --prefix web/admin test`; 13 files / 107 tests passed.
- Ran `node output/playwright/logs_interaction_assert.cjs`; it verified `7d` logs load more requests `offset=500`, and a 504 row detail icon expands to show `HTTP 504` plus the full error.
- Ran `node output/playwright/ui_review.cjs`; 44 views/subviews passed with 0 console errors and 0 page errors.
- Deployed with `bash scripts/sync-bundle-to-home-ai-gateway.sh`; live `ai-gateway.service` restarted healthy.
- Ran `AIGW_ADMIN_BASE_URL=http://127.0.0.1:18080 node output/playwright/logs_interaction_assert.cjs`; live logs interaction passed with `offset=500`.
- Ran live full UI audit `node output/playwright/ui_review_live.cjs`; 44 views/subviews passed with 0 console errors and 0 page errors.

## 2026-05-08

### Admin UI Full Surface Review

- Started a new full admin UI review task covering every visible tab and nested view.
- Confirmed Playwright skill applies and `npx` is available in WSL.
- Existing worktree remains dirty with substantial admin UI changes; next steps are UI surface mapping and browser-based inspection.
- Mapped the main admin UI: Overview, Monitoring, Benchmark, Ops, Config, Logs. Nested views include Monitoring traffic/cost, Benchmark upstream/capability, Ops runtime/probe/audit/diagnostics, and Config current/JSON/visual/history.
- Ran `npm --prefix web/admin run build`; TypeScript and Vite build passed.
- Ran `npm --prefix web/admin test`; 13 files / 106 tests passed.
- Fixed the live deployment path so `scripts/sync-bundle-to-home-ai-gateway.sh` now builds the admin frontend, refreshes `internal/control/api/embedded_admin_dist`, stages `web/admin/dist` into `dist/web/admin/dist`, includes it in `aigw-manifest.json`, verifies the installed bundle after copy, and then restarts `ai-gateway.service`.
- Re-ran `npm --prefix web/admin run build`; Vite produced `index-CXC-kUrD.js`, `vendor-BYVBASC6.js`, and `index-BE52-JR1.css`.
- Re-ran `npm --prefix web/admin test`; 13 files / 106 tests passed.
- Ran `node output/playwright/ui_review.cjs`; 44 views/subviews passed with 0 console errors and 0 page errors. Remaining report items are expected business values, real audit/log errors, checkbox sizing, or wide tables inside scroll containers.
- Ran `bash scripts/sync-bundle-to-home-ai-gateway.sh`; rebuilt frontend and Go binaries, generated and verified manifest with `admin_dist_hash`, copied to `/home/chenrunsen/ai-gateway`, and restarted `ai-gateway.service`.
- Verified live `/home/chenrunsen/ai-gateway/web/admin/dist/index.html` references the same new assets as `web/admin/dist` and `internal/control/api/embedded_admin_dist`.
- Verified `http://127.0.0.1:18080/admin/` returns the new asset references and all three assets return HTTP 200.
- Verified `/home/chenrunsen/ai-gateway/bin/aigw bundle verify -root /home/chenrunsen/ai-gateway -manifest /home/chenrunsen/ai-gateway/aigw-manifest.json`, `systemctl --user is-active ai-gateway.service`, and `curl http://127.0.0.1:18080/-/health` all pass; health returned `status=healthy`, `version=1.3.0`.
- Ran live `node output/playwright/ui_review_live.cjs` against `http://127.0.0.1:18080`; 44 views/subviews passed with 0 console errors and 0 page errors.

### OpenAI Messages Deploy

- Read required workstation context and AI Model Gateway runtime notes.
- Confirmed existing local worktree has unrelated dirty admin UI changes and existing backend compatibility edits.
- Switched the active task plan to OpenAI-compatible `/v1/messages` bug fix plus live deployment.
- Read relevant capsules for previous Claude Messages to OpenAI bridge and live bundle sync/version pitfalls.
- Focused `/v1/messages` bridge tests passed before new edits, showing the existing backend compatibility changes cover the message conversion bug locally.
- `go test ./internal/gateway/api` initially failed at `TestHandleChatCompletionStreamInfiniteRetryStartsSSEBeforeUpstreamSuccess`: stream infinite retry returned 429 before opening SSE.
- Updated `internal/gateway/api/handler.go` so stream requests under infinite retry start an SSE keep-alive session after a retryable upstream failure and write the eventual successful stream through the already-started response.
- Re-ran the failing test, focused message bridge tests, and `go test ./internal/gateway/api -count=1`; all passed.
- Ran `go test ./...`; all Go packages passed.
- First deployment attempt with `scripts/sync-bundle-to-home-ai-gateway.sh` built and verified the dist bundle, then failed copying live binaries with `Text file busy` because `ai-gateway.service` was still running.
- Updated `scripts/sync-bundle-to-home-ai-gateway.sh` to stop `ai-gateway.service` before replacing binaries, use a trap to restart the service if copy fails, and start the service after copying.
- Re-ran the sync script successfully. Live `ai-gateway.service` restarted at 2026-05-08 21:25:50 CST with new PID 284932 and health `status=healthy`, `version=1.3.0`.
- Verified `/home/chenrunsen/ai-gateway/bin/{aigw,gatewayd,controld,telemetryd}` all report `1.3.0`, live manifest verifies, and systemd reports active/running.
- A live `/v1/messages` probe to `claude-opus-4-6[1m]` timed out at the client after 10 seconds while the gateway stayed healthy; gateway logs show the request reached the deployed data plane and was canceled by the short client window.

## 2026-05-07

- Read required `workstation-context` skill and key references needed for project work on this machine.
- Read `planning-with-files` skill and created local planning files because the referenced templates were not present.
- Confirmed the repo is already dirty and that several files in the planned scope have existing modifications.
- Located the relevant admin UI files and identified `MonitoringTab.tsx` plus shared `workspace-nav` styles as the concrete pattern to mirror in `BenchmarkTab.tsx`.
- Refactored `BenchmarkTab.tsx` into a real two-segment workspace: `modeUpstream` and `modeCapability`, each rendered inside its own `WorkspaceBand`.
- Kept model-detail content inside the upstream workspace instead of exposing a third segment, matching the requested split between “模型上游” and “模型能力”.
- Added small Benchmark-local toolbar/copy styles in `benchmark.css` and reused shared segmented-navigation styles from `layout.css`.
- Initial validation attempt via PowerShell failed because of UNC path execution constraints and incorrect `npx tsc` resolution; switching validation to WSL commands next.
2026-05-09 WSL Codex gateway edit fix: added Responses custom tool bridge coverage for apply_patch request conversion, response conversion, streaming events, and multi-turn tool history.
2026-05-09 refined Responses custom tool fix: custom tools are bridged as Chat function tools for chat-only providers and restored to Responses custom_tool_call events using request tool names.
2026-05-09 final custom-tool bridge adjustment: previous Responses custom_tool_call history is converted back into Chat function tool_calls so Codex multi-turn apply_patch conversations remain coherent.
- Ran `go test ./internal/gateway/api -count=1` and `go test ./... -count=1`; both passed.
- Deployed the final bundle with `bash scripts/sync-bundle-to-home-ai-gateway.sh`; live `ai-gateway.service` is healthy on 127.0.0.1:18080.
- Verified real WSL Codex edit smoke in `/tmp/codex-gateway-apply-patch-smoke2`: Codex used apply_patch and changed `probe.txt` from `before` to `after`.
- Created resolved workstation capsule `20260509-wsl-codex-gateway-dropped-apply-patch-custom-tool`.

- Continuing adjacent Responses bridge review for issues similar to dropped apply_patch custom tool; first candidate is Responses custom tool_choice passthrough into Chat providers.

- Confirmed and fixed adjacent Responses bridge downgrades: custom tool_choice conversion, instructions preservation, parallel_tool_calls passthrough, unsupported hosted tool rejection, output_text normalization, Chat array content conversion, and legacy function_call response/stream conversion.

- Validation after adjacent bridge fixes: go test ./internal/gateway/api -count=1 passed; go test ./... -count=1 passed.

- Deployed adjacent bridge fixes with scripts/sync-bundle-to-home-ai-gateway.sh. Live ai-gateway.service restarted healthy, bundle verified, /-/health returned healthy, and /v1/responses unsupported hosted-tool smoke returned HTTP 400 with a clear unsupported responses tool type error.

- Started admin UI functional optimization. Initial scope chosen: Logs page CSV export for current filters plus copy-detail action for expanded log rows.
- Implemented Logs CSV export, log-detail copy action, copy icon, English/Chinese i18n keys, responsive toolbar/detail styles, and expanded `output/playwright/logs_interaction_assert.cjs` to verify export/download and copied state.
- Ran `npm --prefix web/admin test`; 13 files / 107 tests passed.
- Ran `npm --prefix web/admin run build`; Vite build passed and produced `index-DRZg6hz3.js` plus `index-u9zW8nye.css`.
- Deployed with `bash scripts/sync-bundle-to-home-ai-gateway.sh`; live `ai-gateway.service` restarted at 2026-05-10 08:25:37 CST and `/ -/health` returned `status=healthy`, `version=1.3.0`.
- Verified live bundle with `/home/chenrunsen/ai-gateway/bin/aigw bundle verify -root /home/chenrunsen/ai-gateway -manifest /home/chenrunsen/ai-gateway/aigw-manifest.json`.
- Ran `AIGW_ADMIN_BASE_URL=http://127.0.0.1:18080 node output/playwright/logs_interaction_assert.cjs`; live check passed for load more `offset=500`, CSV download content, detail expansion, and copy-details state.
- Ran live full UI audit `AIGW_ADMIN_BASE_URL=http://127.0.0.1:18080 node output/playwright/ui_review_live.cjs`; 44 views/subviews, 0 console errors, 0 page errors.
- Fixed Ops diagnostics runtime/status field localization: `OpsTab` now resolves field labels through `ops.runtimeField.*`, translates common state values such as `connected` and `ready`, and renders a translated runtime status key/value grid before the raw diagnostics JSON.
- Added runtime/status i18n keys for current live fields and future fields including `provider_count`, `enabled_provider_count`, `router_strategy`, `health_enabled`, `sticky_sessions_enabled`, `bridge_enabled`, `gateway_readiness`, `active_snapshot_id`, provider health counters, telemetry counters, RPC contract version, and auto-remediation fields.
- Re-ran `npm --prefix web/admin test`; 13 files / 107 tests passed. Re-ran `npm --prefix web/admin run build`; Vite produced `index-C0iJ_Zc4.js`.
- Deployed the field localization bundle with `bash scripts/sync-bundle-to-home-ai-gateway.sh`; live `ai-gateway.service` restarted at 2026-05-10 09:06:07 CST and health returned `status=healthy`.
- Verified Chinese diagnostics labels in both `/admin/diagnostics` and `/admin/ops` embedded diagnostics using Playwright; no checked snake_case runtime/status labels remained visible in the page body.
- Re-verified live bundle with `/home/chenrunsen/ai-gateway/bin/aigw bundle verify -root /home/chenrunsen/ai-gateway -manifest /home/chenrunsen/ai-gateway/aigw-manifest.json`.
- Fixed Logs status badge vertical alignment by changing `.status-badge` to inline-flex centering and giving log status badges a 24px minimum height.
- Re-ran `npm --prefix web/admin run build` and `npm --prefix web/admin test`; build passed and 13 files / 107 tests passed.
- Deployed the badge alignment fix with `bash scripts/sync-bundle-to-home-ai-gateway.sh`; live `ai-gateway.service` restarted at 2026-05-10 09:26:01 CST and health returned `status=healthy`.
- Playwright geometry check on live `/admin/logs` measured `HTTP 504`, `HTTP 400`, and `HTTP 200` badges at 24px height with text-center offset about `-0.609px`; screenshot saved to `output/playwright/log-status-badge-centered.png`.
- Re-ran live Logs interaction script successfully and re-verified the live bundle.
