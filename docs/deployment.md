# 部署指南

部署目标现在是 `aigw` 单入口 bundle。`gatewayd`、`controld`、`telemetryd` 仍是内部进程边界，但生命周期由 `aigw supervise` 统一管理。

## 产物

发布包至少包含同一个 manifest 管理的五个可执行文件：

- `aigw`
- `gatewayd`
- `controld`
- `telemetryd`
- `gateway-cli`
- `aigw-manifest.json`
- `configs/`、`deploy/` 和可选的 `web/admin/dist/`

## 构建

Linux:

```bash
mkdir -p dist/bin
go build -trimpath -ldflags="-s -w" -o dist/bin/aigw        ./cmd/aigw
go build -trimpath -ldflags="-s -w" -o dist/bin/gatewayd    ./cmd/gatewayd
go build -trimpath -ldflags="-s -w" -o dist/bin/controld    ./cmd/controld
go build -trimpath -ldflags="-s -w" -o dist/bin/telemetryd  ./cmd/telemetryd
go build -trimpath -ldflags="-s -w" -o dist/bin/gateway-cli ./cmd/gateway-cli
dist/bin/aigw bundle build -root dist -out dist/aigw-manifest.json
dist/bin/aigw bundle verify -root dist -manifest dist/aigw-manifest.json
```

Windows:

```powershell
New-Item -ItemType Directory -Force -Path dist\bin | Out-Null
go build -trimpath -ldflags="-s -w" -o dist\bin\aigw.exe        .\cmd\aigw
go build -trimpath -ldflags="-s -w" -o dist\bin\gatewayd.exe    .\cmd\gatewayd
go build -trimpath -ldflags="-s -w" -o dist\bin\controld.exe    .\cmd\controld
go build -trimpath -ldflags="-s -w" -o dist\bin\telemetryd.exe  .\cmd\telemetryd
go build -trimpath -ldflags="-s -w" -o dist\bin\gateway-cli.exe .\cmd\gateway-cli
.\dist\bin\aigw.exe bundle build -root dist -out dist\aigw-manifest.json
.\dist\bin\aigw.exe bundle verify -root dist -manifest dist\aigw-manifest.json
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
- 默认只安装/管理一个 `aigw.service`，旧的 daemon unit 仅用于高级调试

## Linux 示例

```bash
mkdir -p /opt/ai-model-gateway/.gateway-runtime/{telemetry,gateway,control}

/opt/ai-model-gateway/bin/aigw supervise \
  -runtime-root /opt/ai-model-gateway/.gateway-runtime \
  -config-dir /opt/ai-model-gateway/configs \
  -bin-dir /opt/ai-model-gateway/bin \
  -manifest /opt/ai-model-gateway/aigw-manifest.json
```

## Windows 示例

```powershell
$root = "C:\AI-Model-Gateway"
$runtimeRoot = Join-Path $root ".gateway-runtime"

New-Item -ItemType Directory -Force -Path `
  (Join-Path $runtimeRoot "telemetry"), `
  (Join-Path $runtimeRoot "gateway"), `
  (Join-Path $runtimeRoot "control") | Out-Null

& "$root\bin\aigw.exe" supervise `
  -runtime-root $runtimeRoot `
  -config-dir "$root\configs" `
  -bin-dir "$root\bin" `
  -manifest "$root\aigw-manifest.json"
```

## 验证

```bash
curl http://127.0.0.1:18080/-/health
curl http://127.0.0.1:18080/v1/models
curl http://127.0.0.1:18081/admin
curl http://127.0.0.1:18081/-/health
curl -H "Authorization: Bearer $ADMIN_TOKEN" http://127.0.0.1:18081/api/admin/runtime/status
```

## 说明

- `gatewayd` 的 `-listen` 必须与你的目标数据面地址一致
- `controld` 仍然是 revision/publish/history 的唯一 owner
- 仓库不再提供 `gateway install` / `service-start` / `service-stop` / `service-status`
- 单独替换某个 daemon 后，`aigw supervise` 会通过 manifest/hash 和版本检查拒绝混装运行
- Docker/Compose 使用 `configs/docker/*.json`，其中 HTTP listener 绑定 `0.0.0.0` 以匹配端口发布；本机直接运行仍使用 `configs/*.json`。
- 本机开发不要和 live legacy monolith 同时抢占 `127.0.0.1:18080`。
