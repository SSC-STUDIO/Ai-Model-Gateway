# Task Plan — Revision 3: Revert Boundary Violations

## Goal
Remove two files from the diff that are outside the configured writable boundary (`internal/`, `configs/`, `docs/`, `ai/`, `scripts/`). The files `cmd/gatewayd/health_probe.go` and `cmd/gatewayd/main_test.go` were modified in a prior revision but violate the project boundary contract. Revert them to their origin state while preserving all other changes.

## Baseline
- Revision 2 submission included 14 modified files plus untracked `ai/` and `scripts/` content.
- Master review flagged `cmd/gatewayd/health_probe.go` and `cmd/gatewayd/main_test.go` as boundary violations.
- The revert restores both files to their committed state (matching `origin/main`).
- All other modified files remain within `internal/` and are preserved.

## Scope
- **Reverted files (2):**
  - `cmd/gatewayd/health_probe.go` — health probe auth-skip logic removed from diff
  - `cmd/gatewayd/main_test.go` — `TestRunHealthProbeOnceIgnoresAuthorizationOnlyProbeFailure` removed from diff
- **Preserved files (12, all within `internal/`):**
  - `internal/control/compiler/compiler.go`
  - `internal/core/config.go`
  - `internal/gateway/api/fallback.go`, `fallback_test.go`
  - `internal/gateway/api/forward.go`
  - `internal/gateway/api/handler.go`, `handler_test.go`
  - `internal/gateway/api/runtime_state.go`, `runtime_state_test.go`
  - `internal/gateway/api/streaming.go`, `streaming_test.go`
  - `internal/gateway/snapshot/snapshot.go`
- **Preserved untracked (within allowed boundaries):**
  - `ai/` — allowed per project boundaries
  - `scripts/verify-hermes.ps1` — allowed per mandatory rules

## Steps
1. Run `git checkout -- cmd/gatewayd/health_probe.go cmd/gatewayd/main_test.go` to restore both files.
2. Verify `git diff --name-only` no longer lists any `cmd/` files.
3. Run fast gate: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler`.
4. Run `go vet ./internal/gateway/api/...`.
5. Confirm the diff is within scope and differs from the previous submission.

## Verification
- Fast gate: `go test ./internal/gateway/api ./internal/core ./internal/control/compiler` — exit 0
- Vet: `go vet ./internal/gateway/api/...` — exit 0
- Diff check: `git diff --name-only` shows only `internal/` files
- Circuit breaker tests: `go test ./internal/gateway/api -run "TestCircuitBreaker" -v` — all 3 PASS

## Risks
- The health probe auth-skip logic (401/403 handling) is lost from this diff. It can be re-introduced in a future revision inside the allowed boundary if needed.
- The pre-existing `websocket.TestNewProxy` panic persists in broad gate (unrelated).

## Stop Conditions
- Do not proceed if `cmd/` files reappear in the diff.
- Do not proceed if fast gate fails.
- Do not proceed if the diff is identical to revision 2 submission.

## Evidence
- Revert confirmed: `git diff --name-only` shows zero `cmd/` files.
- Fast gate: exit 0 (verified after revert).
- Vet: exit 0.
- All 3 circuit breaker tests PASS.
- Diff now contains only `internal/` files plus allowed untracked `ai/` and `scripts/`.
