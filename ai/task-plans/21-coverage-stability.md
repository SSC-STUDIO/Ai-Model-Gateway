# Task Plan — Revision 21: Coverage Stability Verification

## Goal
Verify TryRecoverAPIKeys test coverage fix and all gates remain stable through revision 21. Create the revision-21-matched task plan to satisfy the task plan gate.

## Baseline
- HEAD: commit 2a6f2e6 on branch main (published revision 3)
- Staged changes: 29 files, ~2407 insertions total
- Key source change: internal/gateway/api/runtime_state_test.go (+100 lines, 6 test functions)
- Pre-fix coverage: TryRecoverAPIKeys 0% (5 uncovered branches)
- Post-fix coverage: TryRecoverAPIKeys 93.3% (1 nil-guard untestable)
- Pre-existing baseline failure: websocket.TestNewProxy panic (unrelated)

## Scope
Source code (unchanged since rev 4): internal/gateway/api/runtime_state_test.go
Task plans staged: rev3,4,5,7,11,12,14,16,18,21 (this file)
Orchestrator-owned ai/*.md: 19 files preserved
scripts/verify-hermes.ps1: preserved

## Steps
1. Create ai/task-plans/21-coverage-stability.md with all 9 headings and actual evidence
2. Stage: git add ai/task-plans/21-coverage-stability.md
3. Focused gate: go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys|TestCircuitBreaker" -v
4. Fast gate: go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1
5. Vet: go vet ./internal/gateway/api/...
6. Diff hash: git diff --cached --no-color | sha256sum

## Verification
Results from this run (rev 21):
- Focused gate: 6/6 PASS, exit 0
  - TestCircuitBreakerBlocksAfterThreshold PASS
  - TestCircuitBreakerPassthroughAllowed PASS
  - TestCircuitBreakerQuotaCooldownRecovery PASS
  - TestTryRecoverAPIKeysNilReceiver PASS
  - TestTryRecoverAPIKeysNilSnapshot PASS
  - TestTryRecoverAPIKeysRecoversExhaustedKeys PASS
- Fast gate: 3/3 packages OK, exit 0
  - api 0.686s, core 1.045s, compiler 0.950s
- Vet: clean, exit 0
- Coverage: TryRecoverAPIKeys 93.3%, api package ~73%

## Risks
1. No new source changes — stability verification only
2. TestNewProxy panic pre-existing, unrelated
3. 93.3% not 100% — nil-guard requires refactoring (accepted)

## Stop Conditions
- Non-zero exit on any test
- Missing headings or under 800 bytes
- Files outside allowed boundaries
- No commit/push until master approval

## Evidence
### Test Results (rev 21)
6/6 PASS, exit 0 (focused). 3 packages OK, exit 0 (fast). Vet clean.
### Coverage
TryRecoverAPIKeys 0% -> 93.3%. Api package ~71% -> ~73%.
### Stability Record
Rev 4: fix introduced. Rev 5-12: verified stable. Rev 13-19: master declared complete (11 consecutive). Rev 20-21: all gates pass, no regression.
