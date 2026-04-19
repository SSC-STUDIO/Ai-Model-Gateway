# 部署指南

部署目标现在是三进程 bundle，而不是单一 `gateway` 入口。

## 产物

发布包只包含：

- `gatewayd`
- `controld`
- `telemetryd`

## 构建

Linux:

```bash
go build -trimpath -ldflags="-s -w" -o dist/gatewayd   ./cmd/gatewayd
go build -trimpath -ldflags="-s -w" -o dist/controld   ./cmd/controld
go build -trimpath -ldflags="-s -w" -o dist/telemetryd ./cmd/telemetryd
```

Windows:

```powershell
go build -trimpath -ldflags="-s -w" -o dist\gatewayd.exe   .\cmd\gatewayd
go build -trimpath -ldflags="-s -w" -o dist\controld.exe   .\cmd\controld
go build -trimpath -ldflags="-s -w" -o dist\telemetryd.exe .\cmd\telemetryd
```

## 部署约定

推荐使用一个共享运行目录：

```text
.gateway-runtime/
├── telemetry/
├── gateway/
└── control/
```

推荐约定：

- `gatewayd` 数据面监听 `127.0.0.1:18080`
- `controld` 控制面监听 `127.0.0.1:18081`
- `controld -authoring-config` 指向你的 `config.yaml`
- 三个 daemon 都由 systemd、NSSM、容器编排或其他 supervisor 分别管理

## Linux 示例

```bash
mkdir -p /opt/ai-model-gateway/.gateway-runtime/{telemetry,gateway,control}

/opt/ai-model-gateway/dist/telemetryd \
  -ingest /opt/ai-model-gateway/.gateway-runtime/telemetry-ingest.sock \
  -query /opt/ai-model-gateway/.gateway-runtime/telemetry-query.sock \
  -data-dir /opt/ai-model-gateway/.gateway-runtime/telemetry

/opt/ai-model-gateway/dist/gatewayd \
  -listen 127.0.0.1:18080 \
  -control /opt/ai-model-gateway/.gateway-runtime/gateway-control.sock \
  -telemetry /opt/ai-model-gateway/.gateway-runtime/telemetry-ingest.sock \
  -data-dir /opt/ai-model-gateway/.gateway-runtime/gateway

/opt/ai-model-gateway/dist/controld \
  -listen 127.0.0.1:18081 \
  -gateway /opt/ai-model-gateway/.gateway-runtime/gateway-control.sock \
  -telemetry /opt/ai-model-gateway/.gateway-runtime/telemetry-query.sock \
  -data-dir /opt/ai-model-gateway/.gateway-runtime/control \
  -authoring-config /opt/ai-model-gateway/configs/config.yaml
```

## Windows 示例

```powershell
$root = "C:\AI-Model-Gateway"
$runtimeRoot = Join-Path $root ".gateway-runtime"

New-Item -ItemType Directory -Force -Path `
  (Join-Path $runtimeRoot "telemetry"), `
  (Join-Path $runtimeRoot "gateway"), `
  (Join-Path $runtimeRoot "control") | Out-Null

$gatewayPipe = "aigw-gateway-control-prod"
$ingestPipe = "aigw-telemetry-ingest-prod"
$queryPipe = "aigw-telemetry-query-prod"

& "$root\dist\telemetryd.exe" `
  -ingest $ingestPipe `
  -query $queryPipe `
  -data-dir (Join-Path $runtimeRoot "telemetry")

& "$root\dist\gatewayd.exe" `
  -listen 127.0.0.1:18080 `
  -control $gatewayPipe `
  -telemetry $ingestPipe `
  -data-dir (Join-Path $runtimeRoot "gateway")

& "$root\dist\controld.exe" `
  -listen 127.0.0.1:18081 `
  -gateway $gatewayPipe `
  -telemetry $queryPipe `
  -data-dir (Join-Path $runtimeRoot "control") `
  -authoring-config "$root\configs\config.yaml"
```

## 验证

```bash
curl http://127.0.0.1:18080/-/health
curl http://127.0.0.1:18080/v1/models
curl http://127.0.0.1:18081/admin
curl http://127.0.0.1:18081/api/admin/status
```

## 说明

- `gatewayd` 的 `-listen` 必须与你的目标数据面地址一致
- `controld` 仍然是 revision/publish/history 的唯一 owner
- 仓库不再提供 `gateway install` / `service-start` / `service-stop` / `service-status`
