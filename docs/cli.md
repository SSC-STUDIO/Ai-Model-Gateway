# AI Model Gateway Command-Line Surfaces

`gateway.exe` 的默认入口来自 `./cmd/gateway`，直接运行 v2 runtime。

## Default Runtime

- Build `gateway.exe` from `./cmd/gateway`.
- Default config path: `configs/config.yaml`.
- No subcommand: start the v2 runtime.
- Default health endpoint: `GET /-/health`.
- Default admin frontend: `GET /admin`.
- Default admin API: `/api/admin/v2/*`.

Usage:

```bash
gateway.exe [-config /path/to/config.yaml]
```

Examples:

```bash
# Start with the default config contract
gateway.exe

# Start with an explicit config
gateway.exe -config ./configs/config.yaml
```

## CLI Commands

`cmd/gateway` 保留这些兼容命令：

- `gateway.exe validate`
- `gateway.exe health`
- `gateway.exe install`
- `gateway.exe service-start`
- `gateway.exe service-stop`
- `gateway.exe service-status`
- `gateway.exe uninstall`

也就是说，用户侧入口名不变，runtime 已经是 v2。

## Config Migration

Build `config-convert.exe` from `./cmd/config-convert` when you need to migrate an existing v1-style config into the v2 schema.

```bash
config-convert.exe -in ./configs/config.yaml -out ./configs/config.v2.yaml
config-convert.exe -in ./configs/config.yaml -out ./configs/config.v2.yaml -listen :18081
```

The converter is for config migration only. It does not start the server.

## Deterministic Verification

Use the verification script to build the default runtime, launch it with a minimal `config.yaml`, and verify health plus admin reachability:

```powershell
.\scripts\verify-default-runtime.ps1
```

The script verifies:

- `go build` succeeds for `./cmd/gateway`
- `GET /-/health` returns `200`
- `GET /v1/models` returns the smoke model
- `GET /api/admin/v2/overview` succeeds with a Bearer admin token
- stderr contains a `[v2]` runtime marker

If you prefer to run the steps manually:

```powershell
go build -o .\dist\gateway.exe .\cmd\gateway
.\scripts\verify-default-runtime.ps1 -SkipBuild
```

## Admin Paths

Use these paths with the replacement runtime:

- Frontend: `http://127.0.0.1:18080/admin`
- Login API: `POST /api/admin/v2/auth/login`
- Logout API: `POST /api/admin/v2/auth/logout`
- Overview API: `GET /api/admin/v2/overview`
- Telemetry API: `GET /api/admin/v2/data`
- Timeseries API: `GET /api/admin/v2/timeseries`
- Config API: `GET|PUT /api/admin/v2/config`
- Config export API: `GET /api/admin/v2/config/export`
- Config history API: `GET /api/admin/v2/config/history`
- Config diff API: `GET /api/admin/v2/config/history/{version_id}/diff`
- Config rollback API: `POST /api/admin/v2/config/rollback`
- Upstream probe API: `POST /api/admin/v2/upstreams/test`
