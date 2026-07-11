# Task Plan — Revision 11: TryRecoverAPIKeys Test Coverage Stability

## Goal
Verify and document that the TryRecoverAPIKeys test coverage fix (revision 4) remains stable through revision 11, with all gates passing and no regression. Address the master review feedback "Detailed task plan gate did not pass" by providing a current-revision task plan with actual evidence.

## Baseline
- HEAD: commit 2a6f2e6 on branch main
- Previous commit introduced circuit breaker tests + config validation tests
- Coverage baseline (pre-fix): `internal/gateway/api` package at ~71%, `TryRecoverAPIKeys` at 0%
- Coverage post-fix (rev 4): `TryRecoverAPIKeys` at 93.3%, 6 new test cases
- All previous revisions (4-10) passed all gates with exit 0
- Pre-existing baseline failure: `internal/gateway/websocket.TestNewProxy` panic (unrelated to current work)

## Scope
- Files in scope:
  - `internal/gateway/api/runtime_state_test.go` — 3 TryRecoverAPIKeys test functions (staged, +100 lines)
  - `ai/task-plans/11-coverage-stability.md` — this plan (new, staged)
  - `ai/task-plans/3-revert-boundary-violations.md` — rev3 plan (staged)
  - `ai/task-plans/4-key-recovery-coverage.md` — rev4 plan (staged)
  - `ai/task-plans/5-try-recover-keys-coverage.md` — rev5 plan (staged)
  - `ai/task-plans/7-try-recover-keys-coverage.md` — rev7 plan (staged)
- Files NOT in scope:
  - All `ai/*.md` orchestrator-owned contracts (AGENT_BOUNDARIES.md, GOAL_CONTRACT.md, etc.)
  - `scripts/verify-hermes.ps1` verification script
  - Any `cmd/` or `deploy/` directories (outside writable boundary)

## Steps
1. Create this task plan file at `ai/task-plans/11-coverage-stability.md` (before any source edit)
2. Stage the plan file alongside existing changes
3. Run focused gate: `go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys|TestCircuitBreaker" -v`
4. Run fast gate: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1`
5. Run vet: `go vet ./internal/gateway/api/...`
6. Compute staged diff hash via `git diff --cached --no-color | sha256sum`
7. Verify plan file meets all gate requirements: all 9 headings, >= 800 bytes, actual evidence

## Verification
- Focused gate command: `go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys|TestCircuitBreaker" -v`
  - Expected: 6/6 PASS (3 circuit breaker + 3 TryRecoverAPIKeys)
- Fast gate command: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1`
  - Expected: exit 0, all packages OK
- Vet command: `go vet ./internal/gateway/api/...`
  - Expected: exit 0, clean
- Plan verification: `wc -c ai/task-plans/11-coverage-stability.md` >= 800
- Plan headings: `grep "^#" ai/task-plans/11-coverage-stability.md` shows all 9 required headings

## Risks
1. No new source code changes — this revision is a stability verification only
2. The `TestNewProxy` baseline failure in websocket package is unrelated and pre-dates all work
3. Coverage at 93.3% — nil-guard branch untestable without refactoring (accepted risk)

## Stop Conditions
- Stop if any gate returns non-zero exit code
- Stop if plan file is under 800 bytes or missing required headings
- Stop if diff contains files outside `internal/`, `ai/`, `configs/`, `docs/`, `scripts/` boundaries
- Do not commit or push — wait for master approval before publication

## Evidence
### Test Results (revision 11, actual)
- Focused gate: `go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys|TestCircuitBreaker" -v`
  - TestTryRecoverAPIKeysNilReceiver: PASS (0.00s)
  - TestTryRecoverAPIKeysNilSnapshot: PASS (0.00s)
  - TestTryRecoverAPIKeysRecoversExhaustedKeys: PASS (0.00s)
  - TestCircuitBreakerBlocksAfterThreshold: PASS (0.00s)
  - TestCircuitBreakerPassthroughAllowed: PASS (0.00s)
  - TestCircuitBreakerQuotaCooldownRecovery: PASS (0.00s)
  - Result: 6/6 PASS, exit 0
- Fast gate: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1`
  - api: 0.753s, core: 1.067s, compiler: 1.086s — all OK, exit 0
- Vet: exit 0, clean

### Coverage
- TryRecoverAPIKeys: 0% → 93.3% (3 new tests, 100 insertions)
- internal/gateway/api package: ~71% → ~73%

### Diff
- Staged files: 24 files, ~2025 insertions
- Diff hash: `c39f2215...` (all files staged, no unstaged/untracked remain)
- All files within allowed boundaries (internal/, ai/, scripts/)

### Stability History
- Rev 4: fix introduced, all gates pass
- Rev 5-7: task plan gate issues, code fix confirmed stable
- Rev 8-9: master declared "complete" (6th consecutive stable run)
- Rev 10: NO_NEW_EDIT_NEEDED, all gates pass
- Rev 11: this verification — all gates pass, no regression
