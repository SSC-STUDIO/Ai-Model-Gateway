# Task Plan — Revision 4: Add RuntimeState.TryRecoverAPIKeys Test Coverage

## Goal
Add focused tests for `RuntimeState.TryRecoverAPIKeys`, which is currently at 0% coverage. This function orchestrates API key recovery across all providers at the state level, bridging `RuntimeState` and `KeyRotator`. It is part of the key resilience story: after a provider's API keys are exhausted (e.g., 403/quota), the system must recover them after a cooldown period. Without integration-level tests, regressions in the state-to-rotator delegation path would go undetected.

## Baseline
- Commit: 2a6f2e6 (published revision 3)
- Coverage: `internal/gateway/api` at 71.1%, `TryRecoverAPIKeys` at 0%
- `KeyRotator.TryRecover` already has unit tests in `key_rotation_test.go` (lines 249-300)
- `RuntimeState.TryRecoverAPIKeys` has zero tests in `runtime_state_test.go`

## Scope
- Target file: `internal/gateway/api/runtime_state_test.go` (append new tests)
- Target function: `RuntimeState.TryRecoverAPIKeys` (lines 315-336 of runtime_state.go)
- No source code changes — only test additions
- All tests within `internal/` boundary

## Steps
1. Create the task plan (this file)
2. Append 3 focused tests to `runtime_state_test.go`:
   - `TestTryRecoverAPIKeysNilReceiver`: nil RuntimeState returns false
   - `TestTryRecoverAPIKeysNilSnapshot`: nil snapshot returns false
   - `TestTryRecoverAPIKeysRecoversExhaustedKeys`: full path — ApplySnapshot with API keys, exhaust a key via ReportFailure, advance time, call TryRecoverAPIKeys, verify key is recovered
3. Run fast gate: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler`
4. Run focused: `go test ./internal/gateway/api -run "TestTryRecoverAPIKeys" -v`
5. Run vet: `go vet ./internal/gateway/api/...`
6. Verify coverage improvement for TryRecoverAPIKeys
7. Inspect diff, ensure no unintended changes

## Verification (actual results)
- Fast gate (`go test ./internal/gateway/api ./internal/core ./internal/control/compiler`): exit 0, all PASS
- Focused (`go test -run "TestTryRecoverAPIKeys" -v`): 3/3 PASS
- Vet (`go vet ./internal/gateway/api/...`): exit 0, clean
- Coverage: `TryRecoverAPIKeys` 0% → 93.3%, overall 71.1% → 71.6%

### Key finding during implementation
`KeyRotator.TryRecover` (key_rotation.go:149) uses `time.Now()` directly instead of the injected clock (`state.now`). This means `RuntimeState.TryRecoverAPIKeys` cannot be tested via clock injection alone — the test must backdate `lastFail` via direct struct manipulation, matching the pattern in `TestKeyRotatorTryRecover`.

## Risks
- Minimal: only test additions, no source changes
- Risk of false positive if test doesn't exercise the actual code path
- Mitigation: verified coverage delta (0% → 93.3%)

## Stop Conditions
- Do not proceed if fast gate fails
- Do not proceed if tests don't pass
- Do not modify any production source code

## Evidence
- 3 new test functions: `TestTryRecoverAPIKeysNilReceiver`, `TestTryRecoverAPIKeysNilSnapshot`, `TestTryRecoverAPIKeysRecoversExhaustedKeys`
- All existing tests continue to pass (fast gate exit 0)
- Coverage delta: `TryRecoverAPIKeys` 0% → 93.3%
- Diff: 1 file, 100 insertions, 0 deletions, within `internal/` boundary
