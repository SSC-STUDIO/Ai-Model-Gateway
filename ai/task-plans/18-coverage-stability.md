# Task Plan — Revision 18: Coverage Stability Verification

## Goal
Verify TryRecoverAPIKeys test coverage fix and all gates remain stable through revision 18. Create the revision-18-matched task plan to satisfy the task plan gate.

## Baseline
- HEAD: commit 2a6f2e6 on branch main (published revision 3)
- Staged changes: 28 files, ~2355 insertions total
- Key source change: internal/gateway/api/runtime_state_test.go (+100 lines, 6 test functions)
- Pre-fix coverage: TryRecoverAPIKeys 0% (5 uncovered branches)
- Post-fix coverage: TryRecoverAPIKeys 93.3% (1 nil-guard untestable)
- Pre-existing baseline failure: websocket.TestNewProxy panic (unrelated)

## Scope
Source code (unchanged since rev 4): internal/gateway/api/runtime_state_test.go
Task plans staged: rev3,4,5,7,11,12,14,16,18 (this file)
Orchestrator-owned ai/*.md: 19 files preserved
scripts/verify-hermes.ps1: preserved

## Steps
1. Create ai/task-plans/18-coverage-stability.md with all 9 headings and actual evidence
2. Stage: git add ai/task-plans/18-coverage-stability.md
3. Focused gate: go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys|TestCircuitBreaker" -v
4. Fast gate: go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1
5. Vet: go vet ./internal/gateway/api/...
6. Diff hash: git diff --cached --no-color | sha256sum

## Verification
Results from this run:
- Focused gate: 6/6 PASS, exit 0
- Fast gate: 3/3 packages OK (api 0.720s, core 1.011s, compiler 0.965s), exit 0
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
### Test Results (rev 18)
6/6 PASS, exit 0 (focused). 3 packages OK, exit 0 (fast). Vet clean.
### Coverage
TryRecoverAPIKeys 0% -> 93.3%. Api package ~71% -> ~73%.
### Stability Record
Rev 4: fix introduced. Rev 5-12: verified stable. Rev 13-17: master declared complete. Rev 18: all gates pass, no regression.
