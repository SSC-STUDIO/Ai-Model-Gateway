# Task Plan

## Current Task - Eventlog Test Coverage Improvement

### Goal

Improve test coverage for `internal/telemetry/eventlog` (74.0% → 78.0%) by covering serializePayload with pricing/benchmark fields, closed-DB Append error path, directory creation error, concurrent Append, and duplicate+accept mixed batches.

### Current Phase

Complete - coverage improved from 74.0% to 78.0%; all 42+ Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | Identified gaps in New (MkdirAll error), Append (closed DB error), serializePayload (pricing/benchmark fields). |
| Complete | 2. Write eventlog tests | Added 6 test functions: TestSerializePayloadPricingFields, TestSerializePayloadBenchmarkFields, TestNewMkdirAllError, TestAppendClosedDB, TestAppendDuplicateAndAccept, TestAppendWithAllFields, TestAppendConcurrent. |
| Complete | 3. Validate | `go build ./...` and `go test ./... -count=1` passed (42+ packages). Coverage: 74.0% → 78.0%. |

### Constraints

- Use WSL ext4 for all commands.

---

## Current Task - Telemetry Test Coverage Improvement

### Goal

Improve test coverage for `internal/telemetry` (64.6% → 79.9%) by covering pricing runtime helpers (shouldRefresh, cloneSourceCatalogs, mergeSourceCatalogs, clonePricingSourceStates, clonePricingFXSnapshot, annotateSourceCatalog, applyFXToPrice, applyFXToCatalog, hydrateSourceStates, loadPricingFXCache, savePricingFXCache), pricing source parsers (parseGenericPricingTables, extractPriceFromCells, inferCurrency, extractMoneyValues, parseModelAliasesFromCell, fetchSinglePageCatalog mock), pricing page parsers (parseAPIPricingPage, parseGPT52PricingPage), and FX XML parsing (fetchPricingFXFromBytes success/error paths).

### Current Phase

Complete - coverage improved from 64.6% to 79.9%; all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | Identified gaps in pricing_runtime (shouldRefresh, clone*, FX cache, applyFX*), pricing_sources (inferCurrency, extractMoneyValues, parseModelAliases, parseGenericPricingTables, fetchSinglePageCatalog), pricing_catalog (parseAPIPricingPage, parseGPT52PricingPage), and FX XML parsing. |
| Complete | 2. Write pricing runtime tests | Added 14 test functions: shouldRefresh, cloneSourceCatalogs, mergeSourceCatalogs, clonePricingSourceStates, clonePricingFXSnapshot, loadPricingFXCache (round-trip + error), savePricingFXCache, applyFXToPrice (USD/CNY/disabled/noRates), applyFXToCatalog, annotateSourceCatalog, hydrateSourceStates, clonePricingCatalogSnapshot. |
| Complete | 3. Write pricing source/parser tests | Added 11 test functions: inferCurrency, extractMoneyValues, parseModelAliasesFromCell, extractPriceFromCells, parseGenericPricingTables, fetchPricingSourceCatalogUnsupportedVendor, fetchSinglePageCatalog (emptyURL + mock server success/empty/error), fetchPricingFXFromBytes (success + invalid XML + missing USD), fetchPricingFXFromBytesSuccess, parseAPIPricingPage, parseGPT52PricingPage. |
| Complete | 4. Refactor fetchPricingFX | Extracted fetchPricingFXFromBytes from fetchPricingFX for testable FX XML parsing without HTTP dependency. |
| Complete | 5. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). Coverage: 64.6% → 79.9%. |

### Constraints

- Use WSL ext4 for all commands.

---

## Current Task - Benchmarking Test Coverage Improvement

### Goal

Improve test coverage for `internal/control/benchmarking` (72.2% → 83.4%) by covering all scorer functions (exactTextScorer, judgeScorer, jsonScorer, toolCallScorer, streamScorer), extractAssistantText/extractToolCall edge cases, baseCaseResult paths, clampScore, excerpt, suiteByName, buildRequest/buildToolRequest protocol variants, ListRuns, Service nil-guard error paths, resolveJudgeProvider branches, expandTargets validation, and normalizeProtocol.

### Current Phase

Complete - benchmarking coverage 72.2% → 83.4%; all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | Identified gaps in scorers, extractAssistantText/extractToolCall, baseCaseResult, ListRuns, Service nil guards, resolveJudgeProvider, expandTargets, normalizeProtocol, clampScore, excerpt. |
| Complete | 2. Write suite scorer tests | Added 35 test functions: extractAssistantText (OpenAI, Anthropic, skips non-text, empty choices, invalid JSON), extractToolCall (OpenAI match/mismatch/no calls, Anthropic match/mismatch/invalid JSON, OpenAI invalid JSON), clampScore, excerpt, suiteByName, all 5 scorers with match/mismatch/incomplete/nil-judge/empty-response/judge-error paths, baseCaseResult (nil, error, HTTP error status, field copying), buildRequest/buildToolRequest (OpenAI, Anthropic, unsupported protocol). |
| Complete | 3. Write store and service tests | Added 20 test functions: ListRuns empty/after-run, Service nil-guard (ListRuns, GetRun, StartRun), StartRun error paths (no gateway, no config, disabled), resolveJudgeProvider (nil config, preferred not found, model mismatch, fallback not found), normalizeProtocol, uniqueStrings, weightedAverage/zero-weight, maxFloat, absFloat, expandTargets (nil config, missing fields, provider not found, model not advertised), validateBaselineSelection empty, parseBaselineRows empty. |
| Complete | 4. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). Coverage: 72.2% → 83.4%. |

### Constraints

- Worktree is already dirty with prior staged changes; do not revert them.
- Use WSL ext4 for all commands.

---

## Current Task - Release And Audit Test Coverage Improvement

### Goal

Improve test coverage for `internal/release` (72.6% → 87.8%) and `internal/control/audit` (65.3% → 92.1%) by covering SaveManifest, LoadManifest, BuildManifest edge cases, HashFile/HashDir error paths, verifyBinary paths, and full audit store CRUD/redaction coverage.

### Current Phase

Complete - release coverage 72.6% → 87.8%; audit coverage 65.3% → 92.1%; all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | release: SaveManifest (0%), LoadManifest (0%), BuildManifest defaults/edge cases, HashFile error (0%), HashDir error (0%), verifyBinary path error. audit: Path (0%), NewStore empty path, Record nil store/cancelled ctx/auto ID, List nil store/empty file/action filter/since filter/limit. |
| Complete | 2. Write release tests | Added 14 test functions: SaveAndLoadManifestRoundTrip, SaveManifestEmptyPath, LoadManifestInvalidJSON, LoadManifestNonExistent, BuildManifestDefaultsAndEdgeCases (4 sub-tests), HashFileError, HashDirWithSubdirectories, HashDirError, VerifyManifestPlatformMismatch, VerifyManifestMissingDaemon, VerifyManifestBinaryHashMismatch, VerifyManifestEmptySchemaVersion, VerifyManifestEmptyProductVersion, VerifyManifestAdminDistHashMismatch. |
| Complete | 3. Write audit tests | Expanded from 1 test to 15 test functions: NewStoreEmptyPath, NewStoreCreatesParentDir, PathNilStore, RecordNilStore, RecordCancelledContext, RecordGeneratesIDAndTime, RecordTrimsFields, ListNilStore, ListCancelledContext, ListNonExistentFile, ListActionFilter, ListSinceFilter, ListLimitEnforcement, ListDefaultLimit, ListMaxLimit, ListReverseChronological, RedactMapNilAndEmpty, RedactMapSensitiveKeys, RedactMapStringMap, RedactMapSliceValues. |
| Complete | 4. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). release: 72.6% → 87.8%; audit: 65.3% → 92.1%. |

### Constraints

- Worktree is already dirty with prior staged changes; do not revert them.
- Use WSL ext4 for all commands.

---

## Current Task - i18n And Compiler Test Coverage Improvement

### Goal

Improve test coverage for `internal/i18n` (79.5% → 92.3%) and `internal/control/compiler` (79.1% → 97.1%) packages.

### Current Phase

Complete - i18n coverage 79.5% → 92.3%; compiler coverage 79.1% → 97.1%; all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | i18n: SetLanguage (0%), MustLoadCatalog (0%), LoadCatalog error path (20% uncovered). compiler: cloneStringMap (0%), compileCompatPolicy (37.5%), Validate (62.5%), resolveQuotaRecoveryIntervalMin (62.5%). |
| Complete | 2. Write i18n tests | Added 4 test functions: SetLanguage, MustLoadCatalog success/fallback, LoadCatalog all langs. |
| Complete | 3. Write compiler tests | Added 12 test functions: cloneStringMap, compileCompatPolicy, Validate (8 sub-cases), resolveQuotaRecoveryIntervalMin (6 sub-cases), empty revision ID, nil config, pricing sources, clone mutation, headers all empty, empty credentials, provider errors. |
| Complete | 4. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). i18n: 79.5% → 92.3%; compiler: 79.1% → 97.1%. |

### Constraints

- Worktree is already dirty with prior staged changes; do not revert them.
- Use WSL ext4 for all commands.

## Current Task - Pricing Catalog Test Coverage Improvement

### Goal

Improve test coverage for the `internal/infra/pricing` package (64.5% → 100%) by covering RefreshNow nil guard and normal paths, fromLegacySnapshot with full data (sources, sourceCatalogs, FX rates), cloneRates empty/nil/non-empty paths, and struct field verification.

### Current Phase

Complete - coverage improved from 64.5% to 100.0%; all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | Identified gaps in RefreshNow, fromLegacySnapshot with full data, cloneRates non-empty path, SourceState/FXSnapshot field coverage. |
| Complete | 2. Write pricing tests | Added 7 new test functions covering RefreshNow nil guard, RefreshNow normal invocation, fromLegacySnapshot with complete sources/FX/sourceCatalogs data, fromLegacySnapshot with empty data, cloneRates non-empty/nil/empty, SourceState fields, FXSnapshot fields. |
| Complete | 3. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). Coverage: 64.5% → 100.0%. |

### Constraints

- Worktree is already dirty with prior staged changes; do not revert them.
- Use WSL ext4 for all commands.

## Current Task - Cache And WebSocket Test Coverage Improvement

### Goal

Improve test coverage for the `internal/gateway/cache` package (59.8% → 86%+) by covering Close, Stats, MaxBytes, byte-limit eviction, sweepExpired, MakeKeyLegacy, and makeKeyRaw paths. Also fix a pre-existing concurrent websocket write race in `TestForwardMessages_NormalFlow`.

### Current Phase

Complete - cache coverage 59.8% → 86.1%; websocket concurrent-write panic fixed; all 42+ Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | Identified gaps in Close, Stats, MaxBytes, sweepExpired, ensureSize, MakeKeyLegacy, makeKeyRaw, byte-limit eviction, and byte accounting. |
| Complete | 2. Write cache tests | Added 16 new test functions covering Close idempotency, Stats hit/miss/eviction counters, MaxBytes accessor, byte-limit LRU eviction, sweepExpired, MakeKeyLegacy equivalence, MakeKey with namespace, makeKeyRaw, byte accounting on overwrite, delete-decrements-bytes, zero-byte-cache eviction, and sweep-on-get expiry removal. |
| Complete | 3. Fix websocket concurrent-write panic | Removed concurrent `echoConn.WriteMessage` in `TestForwardMessages_NormalFlow` that raced with `forwardMessages` goroutine writing to the same connection. |
| Complete | 4. Validate | `go build ./...` and `go test ./... -count=1` passed (42+ packages). Cache coverage: 59.8% → 86.1%. |

### Constraints

- Worktree is already dirty with prior staged changes; do not revert them.
- Use WSL ext4 for all commands.

## Current Task - Test HTTP Client Isolation Improvement

### Goal

Improve test isolation by ensuring each test case uses its own HTTP client instead of sharing the production `sharedHTTPClient`, preventing cross-test interference in bridge and e2e tests.

### Current Phase

Complete - exported `SetSharedHTTPClientForTesting`, refactored e2e and compat tests, all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Add exported test helper | `SetSharedHTTPClientForTesting` in `handler.go` swaps `sharedHTTPClient` and returns a restore function. |
| Complete | 2. Refactor e2e tests | `cmd/gatewayd/bridge_e2e_test.go` uses the new helper for per-test HTTP client isolation. |
| Complete | 3. Refactor compat tests | `internal/gateway/api/compat_test.go` uses `swapSharedHTTPClient` consistently and fixes init ordering. |
| Complete | 4. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). |

### Constraints

- Do not revert unrelated backend or frontend changes already in the worktree.
- Use WSL ext4 for all commands.

## Current Task - WSL Codex Uses Gateway But Does Not Edit Code

### Goal

Diagnose and fix why WSL Codex routed through the local AI gateway can read repository files but does not perform code edits.

### Current Phase

Complete - Responses custom apply_patch bridge and adjacent silent-downgrade paths fixed, deployed, and verified.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Inspect configuration and known issues | Bootstrap status is OK; WSL Codex is `codex-cli 0.128.0`; config is not read-only and points at the local Responses gateway. |
| Complete | 2. Reproduce with a minimal WSL Codex edit task | Initial shell-write smoke passed; first apply_patch smoke showed the model tried to run `apply_patch` as a shell command. |
| Complete | 3. Identify gateway or Codex config root cause | Responses custom `apply_patch` tools were not preserved/restored correctly across the Responses-to-Chat bridge for chat-only upstreams. |
| Complete | 4. Patch/configure the smallest fix | Updated `internal/gateway/api/compat.go` to bridge custom tools as Chat function tools and restore Responses custom tool calls/events. |
| Complete | 5. Validate and record capsule | `go test ./internal/gateway/api -count=1`, `go test ./... -count=1`, live deploy, health probe, real WSL Codex apply_patch smoke, and capsule completed. |
| Complete | 6. Sweep adjacent Responses bridge downgrades | Fixed forced/object `tool_choice`, top-level `instructions`, `parallel_tool_calls`, unsupported hosted-tool errors, `output_text` history, Chat array text output, and legacy `function_call` response/stream conversion. |

### Constraints

- Do not print API keys or bearer tokens from Codex or gateway config.
- The repo is already dirty with unrelated frontend and backend changes.
- Prefer WSL ext4 commands for Codex behavior checks.
- Use the live gateway facts from workstation context before restarting or changing runtime.

## Current Task - Admin UI Icon And Logs Interaction Bugs

### Goal

Fix broken small-icon interactions, starting with the Logs page expand/detail control, non-working load-more behavior, and incorrect 504/error presentation. Validate with browser interaction tests and redeploy the live admin UI.

### Current Phase

Complete - logs icon and load-more controls fixed and deployed.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Reproduce logs interaction bugs | Browser reproduction confirmed no explicit detail icon, dead load-more path, and ambiguous 504 display. |
| Complete | 2. Patch logs controls and data handling | Added explicit detail button, real load-more control, backend `total` normalization, and HTTP 504 display. |
| Complete | 3. Sweep similar icon controls | Checked visible icon buttons on Logs, Benchmark, Ops, and Config; active icon controls expose labels/titles. |
| Complete | 4. Validate locally | Build, 107 frontend tests, targeted Logs Playwright check, and 44-view UI audit passed. |
| Complete | 5. Deploy live | Rebuilt/synced bundle and verified live `/admin/logs` plus full live UI audit. |

## Current Task - Admin UI Full Surface Review And Fixes

### Goal

Review every admin UI screen and sub-screen, identify visible/runtime UI defects, fix confirmed problems without reverting existing local work, and validate with browser screenshots plus frontend build/tests.

### Current Phase

Complete - UI review fixes deployed live.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Map admin UI surface | App shell has Overview, Monitoring, Benchmark, Ops, Config, Logs; Monitoring/Benchmark/Ops/Config have nested views. |
| Complete | 2. Run frontend validation | `npm --prefix web/admin run build` and `npm --prefix web/admin test` pass. |
| Complete | 3. Browser review all views | Playwright covered 44 desktop/mobile views and subviews. |
| Complete | 4. Fix confirmed defects | Shared layout, table, segmented control, pricing-source, chart, benchmark, and ops CSS fixes are in place. |
| Complete | 5. Re-validate | Frontend build, 106 tests, local 44-view Playwright audit, and live 44-view Playwright audit pass. |
| Complete | 6. Deploy if needed | Live bundle replaced under `/home/chenrunsen/ai-gateway` with matching `admin_dist_hash` and healthy service. |

### Constraints

- The worktree is already dirty with substantial admin UI changes from prior work.
- Do not revert unrelated backend or UI changes.
- Use WSL ext4 for commands.
- Prefer existing design system styles and avoid new decorative patterns.

### Coverage Checklist

| Status | View |
| --- | --- |
| Complete | Login/auth state if shown |
| Complete | Overview/dashboard |
| Complete | Monitoring and nested telemetry/pricing views |
| Complete | Benchmark and nested upstream/capability views |
| Complete | Config editor and sub-sections |
| Complete | Logs |
| Complete | Any error/loading/empty states reachable locally |

---

## Current Task - OpenAI Messages Bug Fix And Live Deploy

### Goal

Fix the OpenAI-compatible `/v1/messages` bridge bug, replace the live gateway bundle under `/home/chenrunsen/ai-gateway`, restart `ai-gateway.service`, and verify the deployed endpoint.

### Current Phase

Phase 5 - Complete. All deployed behavior verified.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Diagnose OpenAI messages bug | Existing `/v1/messages` bridge changes pass focused tests; API package exposed a stream infinite-retry regression. |
| Complete | 2. Implement targeted backend fix | Added stream infinite-retry SSE keep-alive path in `handler.go`. |
| Complete | 3. Validate locally | Focused `/v1/messages` tests, full `internal/gateway/api`, and `go test ./...` pass. |
| Complete | 4. Replace and deploy live bundle | Updated sync script to stop service before replacing binaries; deployed and restarted live service. |
| Complete | 5. Verify deployed behavior | Health (HTTP 200, 1ms), service running (PID 4591, active since 2026-05-11 21:05 CST), and live `/v1/messages` returns proper Anthropic Messages response (HTTP 200, 33s) on 2026-05-11. |

### Constraints

- The repository is already dirty, including backend compatibility files and unrelated admin UI work.
- Do not revert unrelated user changes.
- The live runtime is `/home/chenrunsen/ai-gateway` served by `ai-gateway.service`.
- Strict manifest mode requires rebuilding/replacing the bundle consistently rather than copying a single binary.

### Validation Targets

1. `go test ./internal/gateway/api`
2. Relevant full build or `go test ./...` if practical
3. `bash scripts/sync-bundle-to-home-ai-gateway.sh`
4. `systemctl --user restart ai-gateway.service`
5. `curl http://127.0.0.1:18080/-/health`

---

## Goal

Refactor the admin Benchmark tab into a segmented navigation workspace like Monitoring, splitting the current combined page into independent "model upstream" and "model capability" views while preserving existing behavior and minimizing interference with ongoing local changes.

## Current Phase

Complete - segmented workspace refactor validated and summarized.

## Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Bootstrap + inspect current repo state | Read workstation context, planning workflow, and target files. |
| Complete | 2. Implement Benchmark segmented navigation refactor | Updated Benchmark segmented layout and reused shared `WorkspaceBand`. |
| Complete | 3. Validate | `npx tsc --noEmit` passed; `npx vitest run` reported 13 files / 109 tests passed; `npm run build` passed. |
| Complete | 4. Summarize | BenchmarkTab refactored into two-segment workspace (upstream/capability) matching Monitoring pattern; no remaining risks. |

## Scope

- `web/admin/src/components/WorkspaceBand.tsx`
- `web/admin/src/components/tabs/MonitoringTab.tsx`
- `web/admin/src/components/tabs/BenchmarkTab.tsx`
- `web/admin/src/theme/benchmark.css`
- `web/admin/src/i18n/locales/en.json`
- `web/admin/src/i18n/locales/zh.json`

## Constraints

- Repository is already dirty, including several target files.
- Do not revert or overwrite unrelated user changes.
- Prefer smallest change that aligns Benchmark with the existing Monitoring segmented pattern.

## Validation Targets

1. `npx tsc --noEmit`
2. `npx vitest run`
3. `npm run build`
4. Manual sanity check by code inspection that each segment renders inside its own `WorkspaceBand`

## Errors Encountered

| Error | Attempt | Resolution |
| --- | --- | --- |
| `Get-Content /mnt/c/.../SKILL.md` failed in PowerShell path resolution | 1 | Switched to Windows path `C:\Users\96152\...` |
| Several workstation reference reads timed out | 1 | Used partial outputs that returned enough context for this task |
| Planning skill template paths referenced in `SKILL.md` were absent | 1 | Created local planning files manually |
| `npx tsc --noEmit` from PowerShell under `\\wsl.localhost\...` failed with UNC-path fallback and wrong `tsc` resolution | 1 | Switch validation commands to WSL/bash execution |

## Current Task - Admin UI Functional Optimization

### Goal

Improve practical admin UI workflows without large visual redesign. Initial scope: Logs page export and copy-detail actions for faster operations/debugging.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Inspect UI structure | Logs page already has search, pagination, load more, detail expansion. |
| Complete | 2. Implement feature optimizations | Added CSV export for current filtered logs and copy details for expanded rows. |
| Complete | 3. Validate | Admin tests/build passed, targeted live Logs Playwright check passed, and full live UI audit had 0 console/page errors. |
| Complete | 4. Deploy if validation passes | Synced bundle to `/home/chenrunsen/ai-gateway`; live service restarted healthy on 2026-05-10 08:25 CST. |

### Constraints

- Worktree already has unrelated backend/admin UI changes; do not revert them.
- Keep changes scoped and compatible with existing design system.
- Use WSL ext4 commands.

### Outcome

- Logs toolbar now exports the current filtered result set as CSV.
- Expanded log details now include a copy-details action with clipboard fallback.
- Live admin UI at `http://127.0.0.1:18080/admin` has the new bundle deployed and verified.

## Current Task - Benchmark Capability Quick Run And Chart Polish

### Goal

Fix missing Benchmark capability translations, make model capability verification default to automatically test all enabled upstream provider/model routes, and clean up chart styling for a more readable operational UI.

### Current Phase

Complete - translations, all-upstream capability verification default, chart polish, live deploy, and browser checks are done.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Inspect current implementation | Confirmed `benchmark.verification.quickRun` is missing from locale files and the hook already supports `all_active` quick runs. |
| Complete | 2. Patch translations and default run behavior | Added missing locale keys and defaulted manual capability verification to all enabled upstream routes. |
| Complete | 3. Optimize chart styling | Removed radial/glow backgrounds and tightened chart summaries, plot areas, labels, and hover states. |
| Complete | 4. Validate and deploy | Frontend tests/build passed, live bundle synced, health/bundle verify passed, and Playwright verified the capability page. |

### Constraints

- Worktree is already dirty with unrelated backend/admin UI changes; do not revert them.
- Use WSL ext4 execution for npm/build/deploy commands.
- Live runtime remains `/home/chenrunsen/ai-gateway` served by `ai-gateway.service`.

---

## Current Task - SSRF Proxy Test Coverage Improvement

### Goal

Improve test coverage for the security-critical `internal/proxy` package (SSRF protection) from 51.3% to 85%+, covering configuration options, DNS validation, safe transport/dialer construction, and pinned IP dialing paths.

### Current Phase

Complete - coverage improved from 51.3% to 97.4%; all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | Identified gaps in AllowLocalhost/AllowPrivateIP config, ResolveAndValidateHost, hex-encoded host, pinnedIPDialer, NewSafeTransport, NewSafeDialer. |
| Complete | 2. Write SSRF config option tests | AllowLocalhost bypasses hostname check only (DNS still blocks private); AllowPrivateIP removes all blocked ranges; combined flags needed for full localhost access. |
| Complete | 3. Write ResolveAndValidateHost tests | Covered nil-return for no blocked ranges, public IP resolution, and unresolvable host error. |
| Complete | 4. Write ValidateURL edge cases | Hex-encoded host rejection, path traversal variants, ws/wss scheme support, user info variants. |
| Complete | 5. Write transport/dialer tests | NewSafeTransport (nil/custom base), NewSafeDialer TLS config, pinnedIPDialer (private IP, localhost IP, private-resolved localhost, public host, invalid address, unresolvable host), end-to-end httptest TLS server, transport blocks private IP. |
| Complete | 6. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). Coverage: 51.3% → 97.4%. |

### Constraints

- Worktree is already dirty with prior staged changes; do not revert them.
- Use WSL ext4 for all commands.

---

## Current Task - Rate Limiter Test Coverage Improvement

### Goal

Improve test coverage for the `internal/gateway/ratelimit` package (81.0% → 100%) by covering cleanupStaleBucketsLocked periodic sweep, maxBuckets enforcement (reject new keys when at capacity, allow after stale cleanup), and token refill cap (burst ceiling).

### Current Phase

Complete - coverage improved from 81.0% to 100.0%; all 42 Go packages pass.

### Phases

| Status | Phase | Notes |
| --- | --- | --- |
| Complete | 1. Analyze uncovered code paths | Identified gaps in cleanupStaleBucketsLocked (0% coverage), maxBuckets new-key rejection and cleanup recovery, and token refill burst cap. |
| Complete | 2. Write ratelimit tests | Added 4 test functions: TestAllow_CleanupStaleBuckets (periodic sweep removes stale bucket after 256 calls), TestAllow_MaxBuckets_RejectsNewKeys (third key rejected at capacity), TestAllow_MaxBuckets_CleanupAllowsNewKeys (stale bucket cleanup frees slot for new key), TestAllow_TokenRefillCap (elapsed-time refill capped to burst). |
| Complete | 3. Validate | `go build ./...` and `go test ./... -count=1` passed (42 packages). Coverage: 81.0% → 100.0%. |

### Constraints

- Use WSL ext4 for all commands.
