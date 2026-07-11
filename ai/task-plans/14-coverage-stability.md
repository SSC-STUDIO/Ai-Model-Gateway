# Task Plan — Revision 14: TryRecoverAPIKeys Coverage Stability Verification

## Goal
Verify that the TryRecoverAPIKeys test coverage fix remains stable through revision 14, all gates pass, and no regression occurs. Create the revision-14-matched task plan with actual evidence to satisfy the task plan gate.

## Baseline
- HEAD: commit 2a6f2e6 on branch main (published revision 3)
- Staged changes: 26 files, ~2205 insertions total
- Key source change: internal/gateway/api/runtime_state_test.go (+100 lines, 6 test functions)
- Pre-fix coverage: TryRecoverAPIKeys at 0% (5 uncovered branches in tryRecoverAPIKeys)
- Post-fix coverage: TryRecoverAPIKeys at 93.3% (1 nil-guard branch untestable without refactoring)
- Pre-existing baseline failure: internal/gateway/websocket.TestNewProxy panic (unrelated to current work)

## Scope
Source code changes (unchanged since revision 4):
- internal/gateway/api/runtime_state_test.go — 3 TryRecoverAPIKeys tests + 3 circuit breaker tests (+100 lines)

Task plan files staged:
- ai/task-plans/3-revert-boundary-violations.md — rev3 boundary fix
- ai/task-plans/4-key-recovery-coverage.md — rev4 coverage implementation
- ai/task-plans/5-try-recover-keys-coverage.md — rev5 coverage verification
- ai/task-plans/7-try-recover-keys-coverage.md — rev7 gate fix
- ai/task-plans/11-coverage-stability.md — rev11 stability check
- ai/task-plans/12-coverage-stability.md — rev12 stability check
- ai/task-plans/14-coverage-stability.md — this plan (revision-matched)

Orchestrator-owned files (staged, not modified by this task):
- ai/AGENT_BOUNDARIES.md, ai/GOAL_CONTRACT.md, ai/VERIFICATION.md, ai/PROJECT_PROFILE.md
- ai/KNOWLEDGE_BASE.md, ai/AUTONOMOUS_MAINTENANCE_AND_EVOLUTION_WORKFLOW.md
- ai/CANONICAL_TESTING_AND_VERIFICATION_SUITE.md, ai/UNIVERSAL_AI_MASTER_PROMPT.md
- ai/PROJECT_GOAL_PROMPT.md, ai/LIVE_DEBUGGING_AND_OCR_UI_INSPECTOR_PROMPT.md
- ai/agent_optimization_guide.md, ai/ai-model-gateway-src-goal.md
- ai/all_repo_bug_hunter_prompt.md, ai/all_repo_ui_ux_inspector_prompt.md
- ai/claude_code_goal_prompts.md, ai/codex_opencode_bug_reporter_prompt.md
- ai/opencode_multi_agent_bug_reporter_prompt.md, ai/plugin_ui_and_engineering_governance.md
- scripts/verify-hermes.ps1

## Steps
1. Create ai/task-plans/14-coverage-stability.md with all 9 required headings and actual evidence from this run
2. Stage the plan: git add ai/task-plans/14-coverage-stability.md
3. Run focused gate: go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys|TestCircuitBreaker" -v
4. Run fast gate: go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1
5. Run vet: go vet ./internal/gateway/api/...
6. Compute diff hash: git diff --cached --no-color | sha256sum
7. Verify plan meets gate: >= 800 bytes, all 9 headings, actual evidence embedded

## Verification
Commands executed during this revision:
- Focused gate: go test ./internal/gateway/api -count=1 -run "TestTryRecoverAPIKeys|TestCircuitBreaker" -v
  Result: 6/6 PASS, exit 0
- Fast gate: go test ./internal/gateway/api ./internal/core ./internal/control/compiler -count=1
  Result: exit 0, all packages OK (api 0.740s, core 1.411s, compiler 1.306s)
- Vet: go vet ./internal/gateway/api/...
  Result: exit 0, clean
- Diff hash: git diff --cached --no-color | sha256sum

## Risks
1. No new source code changes — this revision is a stability verification only
2. TestNewProxy panic is pre-existing and unrelated to TryRecoverAPIKeys or circuit breaker coverage
3. Coverage at 93.3% not 100% — nil-guard branch requires interface refactoring to test (accepted, documented)
4. Diff contains only task-plans and orchestrator-owned ai/ files — no new runtime code

## Stop Conditions
- Stop if any go test command returns non-zero exit code
- Stop if plan file is missing required headings or under 800 bytes
- Stop if diff contains files outside allowed boundaries
- Do not commit or push — await master approval for controlled publication

## Evidence
### Test Results (revision 14, collected this run)
Focused gate:
  TestCircuitBreakerBlocksAfterThreshold — PASS (0.00s)
  TestCircuitBreakerPassthroughAllowed — PASS (0.00s)
  TestCircuitBreakerQuotaCooldownRecovery — PASS (0.00s)
  TestTryRecoverAPIKeysNilReceiver — PASS (0.00s)
  TestTryRecoverAPIKeysNilSnapshot — PASS (0.00s)
  TestTryRecoverAPIKeysRecoversExhaustedKeys — PASS (0.00s)
  Total: 6/6 PASS, exit 0

Fast gate:
  internal/gateway/api — OK (0.740s)
  internal/core — OK (1.411s)
  internal/control/compiler — OK (1.306s)
  Total: exit 0

Vet: clean, exit 0

### Coverage (stable since rev 4)
  TryRecoverAPIKeys: 0% -> 93.3%
  internal/gateway/api package: ~71% -> ~73%

### Stability History
  Rev 4: fix introduced, all gates pass
  Rev 5-7: task plan gate iterations, code confirmed stable
  Rev 8-9: master declared "complete" (6 consecutive stable runs)
  Rev 10-12: NO_NEW_EDIT_NEEDED, all gates pass
  Rev 13: master declared "complete" (4th consecutive stable verification)
  Rev 14: this verification — all gates pass, no regression
