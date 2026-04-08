# Installation Guide

This guide documents the default runtime: `gateway.exe` comes from `./cmd/gateway` and runs the v2 runtime directly.

## Runtime Defaults

- Default runtime binary: `gateway.exe` built from `./cmd/gateway`
- Default config file: `configs/config.yaml`
- Default health endpoint: `GET /-/health`
- Default admin frontend: `GET /admin`
- Default admin API: `/api/admin/v2/*`
- Migration helper: `config-convert.exe` built from `./cmd/config-convert`

## System Requirements

- Windows 10/11, Linux, or macOS
- Go 1.26+ if building from source
- Network access to your upstream model providers

## Install From Source

```powershell
git clone https://github.com/SSC-STUDIO/ai-model-gateway.git
cd ai-model-gateway

go build -o .\gateway.exe .\cmd\gateway
go build -o .\config-convert.exe .\cmd\config-convert
```

On non-Windows platforms, omit the `.exe` suffix.

## Create a Config

### Option 1: Keep the stable `config.yaml` contract

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
```

`gateway.exe` 会在启动时自动识别 `config.yaml` 的 v1 结构，并转换到稳定的 managed sidecar v2 配置后运行。之后 admin 的 save/history/rollback 也会持久化到这个 sidecar。

### Option 2: Migrate explicitly to `config.v2.yaml`

```powershell
.\config-convert.exe -in .\configs\config.yaml -out .\configs\config.v2.yaml
```

### Option 3: Start from a minimal v2 config

Create `configs/config.v2.yaml`:

```yaml
server:
  listen: :18080

admin:
  enabled: true
  bootstrap_token: "0123456789abcdef0123456789abcdef"
  cookie_signing_key: "abcdef0123456789abcdef0123456789"
  language: zh

routing:
  strategy: health_weighted_rr
  health:
    enabled: true
    interval_sec: 10
    timeout_ms: 2000
    path: /v1/models

providers:
  - name: example-provider
    base_url: https://api.openai.com/v1
    api_key: sk-your-api-key
    provider_class: quota_limited
    models:
      - gpt-4o-mini
    enabled: true

telemetry:
  sqlite_path: data/telemetry.db

pricing:
  cache_path: data/pricing-cache.json

compat:
  bridge:
    enabled: false
    exclude_user_agents: []
    rules: []
  fallback:
    enabled: false
    detect_repetition: false
    models: {}
```

Notes:

- `admin.bootstrap_token` must be at least 32 characters when admin is enabled.
- `admin.cookie_signing_key` must be at least 32 characters when admin is enabled.
- The v2 runtime requires at least one configured provider.

## Run the Default Runtime

```powershell
.\gateway.exe -config .\configs\config.yaml
```

For development without building:

```powershell
go run .\cmd\gateway -config .\configs\config.yaml
```

## Verify the Default Path

### Recommended

```powershell
.\scripts\verify-default-runtime.ps1
```

### Manual checks

Start the runtime, then verify:

```powershell
curl.exe http://127.0.0.1:18080/-/health
curl.exe http://127.0.0.1:18080/v1/models
curl.exe http://127.0.0.1:18080/admin
curl.exe -H "Authorization: Bearer 0123456789abcdef0123456789abcdef" http://127.0.0.1:18080/api/admin/v2/overview
```

Expected results:

- `/-/health` returns `200`
- `/v1/models` returns the configured smoke model
- `/admin` returns HTML
- `/api/admin/v2/overview` returns JSON when called with a valid Bearer bootstrap token

## Admin Access

Open the admin frontend in your browser:

```text
http://127.0.0.1:18080/admin
```

The v2 frontend presents a single app shell with tabs for overview, telemetry, timeseries, config, history, and upstream probe.

For browser login, use the bootstrap token in the `/admin` login form.

For automation, prefer Bearer authentication against `/api/admin/v2/*`.

## Packaging and Release Expectations

The default cutover artifacts should include:

- `gateway.exe` / `gateway` built from `./cmd/gateway`
- `config-convert.exe` / `config-convert` built from `./cmd/config-convert`

Those are the binaries referenced by the repository workflows and verification guidance.

## Compatibility Notes

The same `./cmd/gateway` binary still exposes:

- `validate`
- `health`
- Windows service install/start/stop/status commands

So from the operator point of view, the binary name and CLI surface stay stable while the runtime core has already moved to v2.
