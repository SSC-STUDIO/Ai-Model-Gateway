# v2 完善 & v1 彻底移除 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix remaining v2 bugs, clean up all v1 remnants (configs, docs, binaries, logs), update .gitignore, ensure all tests pass.

**Architecture:** v2 已经完整就位 — `internal/app`, `internal/core`, `internal/infra/*`, `internal/adminapi` 等。主要工作是修复 2 个 bug 和清理 v1 残留物。注意：v1 网关正在运行，`configs/config.yaml` 和 `configs/config.manual.yaml` 不能动。

**Tech Stack:** Go 1.25, chi router, SQLite telemetry

**⚠️ 安全约束:** `configs/config.yaml` 和 `configs/config.manual.yaml` 是运行中的 v1 网关配置，绝对不能修改或删除。

---

### Task 1: Fix matchGlob exact-match bug

**Files:**
- Modify: `internal/app/resolver.go:73-102`
- Test: `internal/app/resolver_test.go`

- [ ] **Step 1: Run existing tests to confirm failures**

Run: `go test ./internal/app/ -run "TestResolver_Resolve_BridgeRewrite|TestResolver_Resolve_ExcludeUserAgent" -v`
Expected: FAIL — `matchGlob` returns `false` for exact (non-glob) patterns.

- [ ] **Step 2: Fix matchGlob function**

Replace `internal/app/resolver.go` lines 73-102 with:

```go
// matchGlob performs a case-insensitive glob match, consistent with v1 logic.
func matchGlob(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	// Fast path: no glob metacharacters — do exact match.
	if !strings.ContainsAny(pattern, "*?[]") {
		return strings.EqualFold(pattern, value)
	}
	if ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(value)); err == nil && ok {
		return true
	}
	return false
}
```

- [ ] **Step 3: Run resolver tests to verify fix**

Run: `go test ./internal/app/ -run "TestResolver" -v`
Expected: All 5 resolver tests PASS.

- [ ] **Step 4: Run full app tests to verify cascade fix**

Run: `go test ./internal/app/ -v -count=1`
Expected: Previously failing handler/pipeline tests now PASS because matchGlob works correctly.

- [ ] **Step 5: Commit**

```bash
git add internal/app/resolver.go
git commit -m "fix(resolver): matchGlob returns false for exact patterns instead of comparing"
```

---

### Task 2: Fix configloader test referencing wrong example config filename

**Files:**
- Modify: `internal/infra/configloader/loader_test.go:187`

- [ ] **Step 1: Run test to confirm failure**

Run: `go test ./internal/infra/configloader/ -run "TestLoadFromFile_ExampleConfigUsesCutoverSafePaths" -v`
Expected: FAIL — `config.v2.example.yaml` not found.

- [ ] **Step 2: Fix filename in test**

Change line 187 from:
```go
configPath := filepath.Join("..", "..", "..", "configs", "config.v2.example.yaml")
```
To:
```go
configPath := filepath.Join("..", "..", "..", "configs", "config.example.yaml")
```

- [ ] **Step 3: Run test to verify fix**

Run: `go test ./internal/infra/configloader/ -run "TestLoadFromFile_ExampleConfigUsesCutoverSafePaths" -v`
Expected: PASS

- [ ] **Step 4: Run all configloader tests**

Run: `go test ./internal/infra/configloader/ -v -count=1`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/infra/configloader/loader_test.go
git commit -m "fix(configloader): test references config.example.yaml not config.v2.example.yaml"
```

---

### Task 3: Remove v1 config remnants

**⚠️ DO NOT touch `configs/config.yaml` or `configs/config.manual.yaml` — the v1 gateway is using them.**

**Files:**
- Delete: `configs/config.v1-converted.yaml`
- Delete: `configs/config.v1.yaml.bak`
- Delete: `configs/config.v2.yaml` (redundant with config.example.yaml)
- Delete: `configs/config.yaml.bak`

- [ ] **Step 1: Remove v1 config backups and redundant files**

```bash
rm -f configs/config.v1-converted.yaml
rm -f configs/config.v1.yaml.bak
rm -f configs/config.v2.yaml
rm -f configs/config.yaml.bak
```

- [ ] **Step 2: Verify config.yaml is untouched**

```bash
md5sum configs/config.yaml
```

Record the hash — it MUST not change.

- [ ] **Step 3: Verify build still works**

Run: `go build ./...`
Expected: Success, no errors.

- [ ] **Step 4: Commit**

```bash
git add -u configs/
git commit -m "chore: remove v1 config backups and redundant config files"
```

---

### Task 4: Remove v1 documentation and historical reports

**Files:**
- Delete: `docs/v1-v2-feature-comparison.md`
- Delete: `docs/v2-cli-implementation.md`
- Delete: `RELEASE_NOTES_v1.0.0.md`
- Delete: `SECURITY_FIXES_BATCH4.md`
- Delete: `SECURITY_FIXES_BATCH4_PART2.md`
- Delete: `UI_UX_IMPROVEMENTS.md`
- Delete: `OPTIMIZATION_REPORT.md`
- Delete: `OPTIMIZATION_SUMMARY.md`

- [ ] **Step 1: Remove v1 migration docs**

```bash
rm -f docs/v1-v2-feature-comparison.md
rm -f docs/v2-cli-implementation.md
```

- [ ] **Step 2: Remove historical reports no longer relevant**

```bash
rm -f RELEASE_NOTES_v1.0.0.md
rm -f SECURITY_FIXES_BATCH4.md
rm -f SECURITY_FIXES_BATCH4_PART2.md
rm -f UI_UX_IMPROVEMENTS.md
rm -f OPTIMIZATION_REPORT.md
rm -f OPTIMIZATION_SUMMARY.md
```

- [ ] **Step 3: Commit**

```bash
git add -u .
git commit -m "chore: remove v1 migration docs and historical reports"
```

---

### Task 5: Remove temporary files, logs, and binary artifacts

**Files:**
- Delete: `v2-gateway-monitor.log`
- Delete: `cookies.txt`
- Delete: `gateway_manual.exe`
- Delete: `manual-gateway.log`
- Delete: `embedded_admin_dist/` (root level duplicate; canonical copy is in `internal/adminapi/embedded_admin_dist/`)
- Delete: `internal/app/pipeline.go.bak`

- [ ] **Step 1: Remove temporary/log files**

```bash
rm -f v2-gateway-monitor.log cookies.txt manual-gateway.log
rm -f gateway_manual.exe
rm -f internal/app/pipeline.go.bak
rm -rf embedded_admin_dist/
```

- [ ] **Step 2: Commit**

```bash
git add -u .
git commit -m "chore: remove temporary files, logs, and duplicate embedded dist"
```

---

### Task 6: Update .gitignore for v2

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Replace .gitignore with comprehensive v2 version**

```gitignore
# Binaries
bin/
*.exe
*.exe~

# Config (active config + backups are local-only)
configs/config.yaml
configs/config.yaml.bak
configs/config.manual.yaml
configs/.config.yaml.history/
configs/data/

# Database
*.db
*.db-shm
*.db-wal

# Logs
*.log
logs/

# Test artifacts
coverage
*.test

# OS
Thumbs.db
.DS_Store

# Frontend
web/admin/node_modules/
web/admin/dist/

# Root-level embedded dist (auto-generated)
/embedded_admin_dist/

# Temp files
cookies.txt
findings.md
progress.md
task_plan.md
nul
```

- [ ] **Step 2: Verify no important files are excluded**

Run: `git status` — confirm no needed files are now being ignored.

- [ ] **Step 3: Commit**

```bash
git add .gitignore
git commit -m "chore: update .gitignore for v2 architecture"
```

---

### Task 7: Stage and commit the v1 package deletions

The git status shows 75 deleted files (old v1 packages: `internal/cli/` old files, `internal/config/`, `internal/router/`, `internal/server/`). These are already deleted in the working directory but not yet staged.

**Files:**
- Stage deletions: `internal/cli/` (old files), `internal/config/`, `internal/proxy/handler.go`, `internal/proxy/handler_test.go`, `internal/proxy/ssrf_test.go`, `internal/router/`, `internal/server/`
- Stage additions: All new untracked v2 files in `internal/app/`, `internal/adminapi/`, `internal/cli/` (new i18n), `internal/core/`, `internal/infra/`, `internal/runtime/`, `cmd/gateway/main_test.go`
- Delete: `configs/config.test.json` (evaluate if used by tests — if not, remove)

- [ ] **Step 1: Stage all v1 deletions**

```bash
git add -u internal/cli/ internal/config/ internal/proxy/ internal/router/ internal/server/
```

- [ ] **Step 2: Stage all new v2 files**

```bash
git add internal/adminapi/ internal/app/ internal/core/ internal/infra/ internal/runtime/ internal/cli/
git add cmd/gateway/main_test.go
```

- [ ] **Step 3: Stage remaining modified files**

```bash
git add .github/workflows/ci.yml .github/workflows/release.yml
git add CLAUDE.md README.md
git add configs/config.example.yaml
git add docs/chinese-models-integration.md docs/cli.md docs/installation.md
git add internal/app/run.go
```

- [ ] **Step 4: Verify build and full test suite**

```bash
go build ./...
go test ./... -count=1
```
Expected: Build succeeds, ALL tests pass.

- [ ] **Step 5: Commit the v1→v2 migration**

```bash
git commit -m "feat: complete v2 architecture, remove all v1 packages

Remove v1 packages: internal/config, internal/router, internal/server,
old internal/cli, old internal/proxy handlers.

Add v2 packages: internal/app (gateway handler, pipeline, resolver,
selector, compat layers), internal/adminapi (admin REST + frontend),
internal/core (domain types), internal/infra/* (auth, configloader,
configstate, httpserver, pricing, runtime, telemetrydb),
internal/cli (i18n), internal/i18n, internal/runtime (service mgmt).

All tests passing. v1 gateway config untouched."
```

---

### Task 8: Clean up worktrees and final verification

- [ ] **Step 1: Remove old agent worktrees**

```bash
rm -rf .claude/worktrees/agent-a833c185/
rm -rf .claude/worktrees/agent-a9505dc9/
rm -rf .claude/worktrees/agent-adcbe108/
```

- [ ] **Step 2: Full verification**

```bash
go build ./...
go test ./... -count=1
go vet ./...
```
Expected: All pass, zero warnings.

- [ ] **Step 3: Verify config.yaml is still intact for running v1 gateway**

```bash
cat configs/config.yaml | head -5
```
Confirm the file exists and is readable.

- [ ] **Step 4: Review git log**

```bash
git log --oneline -10
```
Confirm clean commit history.
