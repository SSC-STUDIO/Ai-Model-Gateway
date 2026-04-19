# 安装指南

本仓库当前只交付三个 daemon：

- `gatewayd`
- `controld`
- `telemetryd`

不再提供 `gateway` / `gateway.exe` launcher。

## 构建

Windows:

```powershell
go build -o .\dist\gatewayd.exe .\cmd\gatewayd
go build -o .\dist\controld.exe .\cmd\controld
go build -o .\dist\telemetryd.exe .\cmd\telemetryd
```

Linux/macOS:

```bash
go build -o ./dist/gatewayd ./cmd/gatewayd
go build -o ./dist/controld ./cmd/controld
go build -o ./dist/telemetryd ./cmd/telemetryd
```

## 配置

仍然使用 `configs/config.yaml` 作为 operator authoring config：

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
$env:ADMIN_BOOTSTRAP_TOKEN = "<32+ chars>"
$env:COOKIE_SIGNING_KEY = "<32+ chars>"
$env:ADMIN_TOKEN = "<admin token>"
$env:VIEWER_TOKEN = "<viewer token>"
```

`controld` 会通过 `-authoring-config` 读取这个 YAML。`gatewayd` 和 `telemetryd` 不会直接读取它。

## 本地运行目录建议

推荐为三个 daemon 准备同一个共享运行目录，例如：

```text
.gateway-runtime/
├── telemetry/
├── gateway/
└── control/
```

Linux/macOS 下把 Unix socket 放到这个目录中；Windows 下使用显式 named pipe 名称，并把数据目录放到这个目录中。

## 启动

Linux/macOS:

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control

./dist/telemetryd \
  -ingest ./.gateway-runtime/telemetry-ingest.sock \
  -query ./.gateway-runtime/telemetry-query.sock \
  -data-dir ./.gateway-runtime/telemetry

./dist/gatewayd \
  -listen 127.0.0.1:18080 \
  -control ./.gateway-runtime/gateway-control.sock \
  -telemetry ./.gateway-runtime/telemetry-ingest.sock \
  -data-dir ./.gateway-runtime/gateway

./dist/controld \
  -listen 127.0.0.1:18081 \
  -gateway ./.gateway-runtime/gateway-control.sock \
  -telemetry ./.gateway-runtime/telemetry-query.sock \
  -data-dir ./.gateway-runtime/control \
  -authoring-config ./configs/config.yaml
```

Windows PowerShell:

```powershell
$runtimeRoot = Join-Path $PWD ".gateway-runtime"
New-Item -ItemType Directory -Force -Path `
  (Join-Path $runtimeRoot "telemetry"), `
  (Join-Path $runtimeRoot "gateway"), `
  (Join-Path $runtimeRoot "control") | Out-Null

$gatewayPipe = "aigw-gateway-control-local"
$ingestPipe = "aigw-telemetry-ingest-local"
$queryPipe = "aigw-telemetry-query-local"

Start-Process .\dist\telemetryd.exe -ArgumentList @(
  "-ingest", $ingestPipe,
  "-query", $queryPipe,
  "-data-dir", (Join-Path $runtimeRoot "telemetry")
)

Start-Process .\dist\gatewayd.exe -ArgumentList @(
  "-listen", "127.0.0.1:18080",
  "-control", $gatewayPipe,
  "-telemetry", $ingestPipe,
  "-data-dir", (Join-Path $runtimeRoot "gateway")
)

Start-Process .\dist\controld.exe -ArgumentList @(
  "-listen", "127.0.0.1:18081",
  "-gateway", $gatewayPipe,
  "-telemetry", $queryPipe,
  "-data-dir", (Join-Path $runtimeRoot "control"),
  "-authoring-config", ".\configs\config.yaml"
)
```

## 验证

```powershell
curl.exe http://127.0.0.1:18080/-/health
curl.exe http://127.0.0.1:18080/v1/models
curl.exe http://127.0.0.1:18081/admin
curl.exe http://127.0.0.1:18081/api/admin/status
curl.exe http://127.0.0.1:18081/api/admin/config/history
```

Windows 下可以直接运行：

```powershell
.\scripts\verify-default-runtime.ps1
```

## 说明

- `publisher-state.db` 位于 `controld` 的 `-data-dir`
- 当 `publisher-state.db` 不存在时，`controld` 会从 `-authoring-config` 种出初始 revision
- 当 `publisher-state.db` 已存在时，`controld` 会优先恢复已有 revision/history
- 仓库不再提供单一 Windows 服务。请用你自己的 supervisor 管理三个 daemon
