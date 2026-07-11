# Task Plan — Revision 7: RuntimeState.TryRecoverAPIKeys Coverage Gate

## Goal
Address the master review feedback "Detailed task plan gate did not pass" for cycle 4's TryRecoverAPIKeys integration tests. The implementation (3 test functions, 100 insertions in runtime_state_test.go) is correct and all tests pass. The task plan gate failure is because the plan file must match the current revision number (7), not a prior revision (5).

## Baseline
- Published commit: 2a6f2e6 (revision 3, pushed to origin/main)
- Working tree: 1 file modified (internal/gateway/api/runtime_state_test.go), 100 insertions, 0 deletions
- Pre-existing coverage: internal/gateway/api at 71.1%, TryRecoverAPIKeys at 0%
- Pre-existing test infrastructure: KeyRotator.TryRecover has unit tests (key_rotation_test.go:249-300) but RuntimeState.TryRecoverAPIKeys has zero tests
- Pre-existing invariant: after a provider's API keys are exhausted via ReportFailure, the system must recover them after a cooldown so routing resumes

## Scope
- Target file: internal/gateway/api/runtime_state_test.go — 3 test functions appended (100 lines)
- No production source code changes
- All changes within internal/ boundary
- Files explicitly NOT changed: all cmd/, deploy/, dist-runtime/, configs/provider-secrets.env.example

## Steps
1. Inspect RuntimeState.TryRecoverAPIKeys source (runtime_state.go:315-336) and KeyRotator.TryRecover (key_rotation.go:134-159) to understand the delegation path
2. Identify that KeyRotator.TryRecover uses time.Now() directly (not injected clock), meaning tests must backdate lastFail via direct struct manipulation
3. Append TestTryRecoverAPIKeysNilReceiver — proves nil *RuntimeState returns false without panic
4. Append TestTryRecoverAPIKeysNilSnapshot — proves nil snapshot returns false without panic
5. Append TestTryRecoverAPIKeysRecoversExhaustedKeys — full integration path: ApplySnapshot with multi-key provider, ReportFailure to exhaust key-a, verify only key-b routable, call TryRecoverAPIKeys(snap, 0) (no recovery, cooldown=0), backdate lastFail to 2 minutes ago, call TryRecoverAPIKeys(snap, time.Minute), verify both keys routable
6. Run focused gate: go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys" -v
7. Run fast gate: go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1
8. Run vet: go vet ./internal/gateway/api/...
9. Verify coverage: TryRecoverAPIKeys coverage above 0%
10. Inspect final diff, confirm only test file modified, no production code changes

## Verification (actual results)
- Focused gate (go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys" -v): 3/3 PASS, exit 0
  - TestTryRecoverAPIKeysNilReceiver: PASS
  - TestTryRecoverAPIKeysNilSnapshot: PASS
  - TestTryRecoverAPIKeysRecoversExhaustedKeys: PASS
- Fast gate (go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1): exit 0, all packages PASS
- Vet (go vet ./internal/gateway/api/...): exit 0, clean
- Coverage (go test -coverprofile): TryRecoverAPIKeys 0% to 93.3%, overall 71.1% to 71.6%
- Diff: 1 file changed, 100 insertions, 0 deletions

### Key implementation finding
KeyRotator.TryRecover (key_rotation.go:149) calls time.Now() directly instead of accepting the injected clock (state.now). This means TryRecoverAPIKeys cannot be tested via clock advancement alone. The test must directly manipulate kr.keys[i].lastFail via the struct field to simulate passage of time, matching the established pattern in TestKeyRotatorTryRecover.

## Risks
- Minimal: only test additions, zero production code changes
- Risk: test could exercise wrong code path — mitigated by coverage delta proof (0% to 93.3%)
- Risk: backdating lastFail could mask a clock bug in production — noted as known gap; KeyRotator.TryRecover should accept an injectable clock in a future revision

## Stop Conditions
- Do not proceed if any test fails
- Do not proceed if fast gate exits non-zero
- Do not modify production source code
- Do not touch files outside internal/ boundary

## Evidence
- 3 new test functions in internal/gateway/api/runtime_state_test.go
- All 3 new tests PASS; all existing tests continue to PASS
- Coverage improvement: TryRecoverAPIKeys 0% to 93.3%
- Diff: 1 file, 100 insertions, within internal/ boundary
- No production code modified
- Task plan: ai/task-plans/7-try-recover-keys-coverage.md (this file), revision-matched
