# 安装指南

本仓库当前交付一个本地运维入口和三个内部 daemon：

- `aigw`
- `gatewayd`
- `controld`
- `telemetryd`

不再提供旧的 `gateway` / `gateway.exe` launcher。默认启动方式是 `aigw supervise`。

本机开发注意：如果 live legacy monolith 已经监听 `127.0.0.1:18080`，不要同时启动开发三面 runtime 绑定同一端口。先停掉其中一个，或改用测试端口。

## 构建

Windows:

```powershell
go build -o .\dist\aigw.exe .\cmd\aigw
go build -o .\dist\gatewayd.exe .\cmd\gatewayd
go build -o .\dist\controld.exe .\cmd\controld
go build -o .\dist\telemetryd.exe .\cmd\telemetryd
go build -o .\dist\gateway-cli.exe .\cmd\gateway-cli
```

Linux/macOS:

```bash
go build -o ./dist/aigw ./cmd/aigw
go build -o ./dist/gatewayd ./cmd/gatewayd
go build -o ./dist/controld ./cmd/controld
go build -o ./dist/telemetryd ./cmd/telemetryd
go build -o ./dist/gateway-cli ./cmd/gateway-cli
```

发布/部署前生成 manifest：

```bash
./dist/aigw bundle build -root . -out aigw-manifest.json
./dist/aigw bundle verify -root . -manifest aigw-manifest.json
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

三个 daemon 的 `-config` 参数读取各自的 bootstrap JSON：

- `configs/gatewayd.json`
- `configs/controld.json`
- `configs/telemetryd.json`

这些 JSON 只放监听地址、IPC socket/named pipe、数据目录等启动参数。不要把 `configs/config.yaml` 传给 daemon 的 `-config`。

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
./dist/aigw supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir ./dist
```

Windows PowerShell:

```powershell
$runtimeRoot = Join-Path $PWD ".gateway-runtime"
New-Item -ItemType Directory -Force -Path `
  (Join-Path $runtimeRoot "telemetry"), `
  (Join-Path $runtimeRoot "gateway"), `
  (Join-Path $runtimeRoot "control") | Out-Null

.\dist\aigw.exe supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir .\dist
```

## 验证

```powershell
curl.exe http://127.0.0.1:18080/-/health
curl.exe http://127.0.0.1:18080/v1/models
curl.exe http://127.0.0.1:18081/admin
curl.exe http://127.0.0.1:18081/-/health
curl.exe -H "Authorization: Bearer $env:ADMIN_TOKEN" http://127.0.0.1:18081/api/admin/runtime/status
curl.exe -H "Authorization: Bearer $env:ADMIN_TOKEN" http://127.0.0.1:18081/api/admin/config/history
```

Windows 下可以直接运行：

```powershell
.\scripts\verify-default-runtime.ps1
```

## 说明

- `publisher-state.db` 位于 `controld` 的 `-data-dir`
- 当 `publisher-state.db` 不存在时，`controld` 会从 `-authoring-config` 种出初始 revision
- 当 `publisher-state.db` 已存在时，`controld` 会优先恢复已有 revision/history
- 生产服务默认只包装 `aigw supervise`。单独管理 daemon 仅作为高级调试模式。
