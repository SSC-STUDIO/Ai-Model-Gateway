# Kimi Upstream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让当前 `AI-Model-Gateway` 运行实例接通 Kimi 官方上游，并补齐示例配置和 README 文档。

**Architecture:** 复用项目现有 upstream 配置结构和重启脚本，不新增协议层实现。运行实例以 `configs/config.yaml` 为准完成启动与冒烟验证，仓库文档以 `configs/config.example.yaml` 和 `README.md` 为主要更新面。

**Tech Stack:** Go, PowerShell, YAML, Markdown

---

### Task 1: Verify runtime inputs

**Files:**
- Modify: `configs/config.yaml`
- Read: `scripts/rebuild-and-restart.ps1`
- Read: `scripts/start-gateway.ps1`

- [ ] **Step 1: Inspect the active Kimi upstream block**

Run: `Get-Content .\configs\config.yaml`
Expected: find a `kimi-official` upstream block with `models: [kimi-k2.5]`

- [ ] **Step 2: Inspect the restart path**

Run: `Get-Content .\scripts\rebuild-and-restart.ps1`
Expected: confirm the script targets `configs/config.yaml` and `bin\gateway.exe`

- [ ] **Step 3: Apply the minimal config correction if needed**

```yaml
- name: kimi-official
  base_url: https://api.moonshot.cn
  anthropic_base_url: https://api.moonshot.cn/anthropic
  api_key: sk-REDACTED
  provider_class: quota_limited
  models:
    - kimi-k2.5
  weight: 1
  timeout_ms: 180000
  same_upstream_retries: 1
  enabled: true
  headers: {}
```

- [ ] **Step 4: Re-read only the touched region**

Run: `Get-Content .\configs\config.yaml`
Expected: Kimi upstream remains present and syntactically aligned with adjacent upstream entries

### Task 2: Bring up the runtime instance

**Files:**
- Use: `scripts/rebuild-and-restart.ps1`
- Observe: `bin/gateway.exe`

- [ ] **Step 1: Run the project restart entrypoint**

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\rebuild-and-restart.ps1`
Expected: gateway rebuilds, restarts, and reports healthy status

- [ ] **Step 2: Verify listener presence**

Run: `Get-NetTCPConnection -LocalPort 18080 -State Listen`
Expected: one listening socket bound to port `18080`

- [ ] **Step 3: Verify health endpoint**

Run: `Invoke-RestMethod http://127.0.0.1:18080/-/health`
Expected: healthy response from the restarted instance

### Task 3: Verify Kimi model routing

**Files:**
- No file edits

- [ ] **Step 1: Query models**

Run: `Invoke-RestMethod http://127.0.0.1:18080/v1/models -Headers @{ Authorization = 'Bearer <gateway-token>' }`
Expected: `data[].id` includes `kimi-k2.5`

- [ ] **Step 2: Run a Kimi responses smoke**

```powershell
$headers = @{
  Authorization = "Bearer <gateway-token>"
  "Content-Type" = "application/json"
}
$body = @{
  model = "kimi-k2.5"
  input = "Reply with exactly kimi-ok."
} | ConvertTo-Json
Invoke-RestMethod http://127.0.0.1:18080/v1/responses -Method Post -Headers $headers -Body $body
```

Expected: successful response whose text output is `kimi-ok.`

- [ ] **Step 3: Record any runtime failure mode before changing docs**

Run: inspect command output and logs
Expected: either a passing smoke or a concrete upstream/runtime error to fix first

### Task 4: Update example config and README

**Files:**
- Modify: `configs/config.example.yaml`
- Modify: `README.md`

- [ ] **Step 1: Add a redacted Kimi upstream example**

```yaml
- name: kimi-official
  base_url: https://api.moonshot.cn
  anthropic_base_url: https://api.moonshot.cn/anthropic
  api_key: sk-your-kimi-key
  provider_class: quota_limited
  models:
    - kimi-k2.5
  weight: 1
  timeout_ms: 180000
  same_upstream_retries: 1
  enabled: true
  headers: {}
```

- [ ] **Step 2: Document runtime validation**

```markdown
### Kimi upstream

Add a `kimi-official` upstream to `configs/config.yaml`, restart the gateway, then verify:

- `GET /v1/models` includes `kimi-k2.5`
- `POST /v1/responses` with `model: "kimi-k2.5"` succeeds
```

- [ ] **Step 3: Document `kimi-cli` gateway usage**

```markdown
`kimi-cli` can use the gateway through an `openai_responses` provider whose `base_url` points to `http://127.0.0.1:18080/v1`.
```

- [ ] **Step 4: Re-read the changed sections**

Run: `Get-Content .\README.md` and `Get-Content .\configs\config.example.yaml`
Expected: examples are redacted, aligned with actual runtime shape, and do not leak live credentials

### Task 5: Final verification

**Files:**
- No file edits

- [ ] **Step 1: Run project tests**

Run: `go test ./...`
Expected: exit code `0`

- [ ] **Step 2: Re-run runtime smoke after docs/config edits**

Run: repeat `/-/health`, `/v1/models`, and `/v1/responses`
Expected: health OK, `kimi-k2.5` present, Kimi smoke still succeeds

- [ ] **Step 3: Summarize exact outcomes**

Run: collect the final command results
Expected: report real verification evidence, not assumptions
