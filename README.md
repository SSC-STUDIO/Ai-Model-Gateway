[![CI](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-1.1.0-blue.svg)](VERSION)

# AI Model Gateway

仓库现在只保留三面运行时：

- `gatewayd`
  数据面，负责 `/-/health`、`/v1/models`、`/v1/chat/completions`
- `controld`
  控制面，负责 `/admin`、`/api/admin/*`、revision/publish/history
- `telemetryd`
  telemetry 面，负责事件落盘、投影、查询 RPC

`gateway` / `gateway.exe` 已经移除。仓库不再提供单一 launcher 或单一 Windows 服务包装器，运维必须直接管理三个 daemon。

## 当前运行模型

- 人类可编辑配置仍然是 `configs/config.yaml`
- `controld` 通过 `-authoring-config <path>` 读取 YAML，并把它编译/持久化为 revision
- `gatewayd` 不读取 YAML，只执行 `controld` 发布的 snapshot
- `telemetryd` 仍是唯一 telemetry append/query owner
- 推荐继续使用共享运行目录，例如 `.gateway-runtime/`，集中放置：
  - IPC socket / named pipe 名称
  - telemetry 数据目录
  - control 数据目录中的 `publisher-state.db`

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

## 快速开始

1. 准备 authoring config：

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
$env:ADMIN_BOOTSTRAP_TOKEN = "<32+ chars>"
$env:COOKIE_SIGNING_KEY = "<32+ chars>"
$env:ADMIN_TOKEN = "<admin token>"
$env:VIEWER_TOKEN = "<viewer token>"
```

配置加载器会在校验前展开 `config.yaml` 里的 `$VAR` / `${VAR}`。

2. 启动三个 daemon。

Linux/macOS:

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control

./dist/telemetryd \
  -ingest ./.gateway-runtime/telemetry-ingest.sock \
  -query ./.gateway-runtime/telemetry-query.sock \
  -data-dir ./.gateway-runtime/telemetry &

./dist/gatewayd \
  -listen 127.0.0.1:18080 \
  -control ./.gateway-runtime/gateway-control.sock \
  -telemetry ./.gateway-runtime/telemetry-ingest.sock \
  -data-dir ./.gateway-runtime/gateway &

./dist/controld \
  -listen 127.0.0.1:18081 \
  -gateway ./.gateway-runtime/gateway-control.sock \
  -telemetry ./.gateway-runtime/telemetry-query.sock \
  -data-dir ./.gateway-runtime/control \
  -authoring-config ./configs/config.yaml &
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

3. 验证：

```powershell
curl.exe http://127.0.0.1:18080/-/health
curl.exe http://127.0.0.1:18080/v1/models
curl.exe http://127.0.0.1:18081/admin
curl.exe http://127.0.0.1:18081/api/admin/status
```

Windows 下推荐直接运行：

```powershell
.\scripts\verify-default-runtime.ps1
```

这个脚本现在会直接构建并启动 `telemetryd`、`gatewayd`、`controld` 三个进程。

## 端口与接口

给定 `server.listen` 为数据面监听地址：

- 数据面:
  - `GET /-/health`
  - `GET /v1/models`
  - `POST /v1/chat/completions`
- 控制面:
  - 默认约定为数据面端口 + 1
  - 常见本地组合是 `127.0.0.1:18080` / `127.0.0.1:18081`

当前控制面公开的 HTTP 路由包括：

- `GET /-/health`
- `GET /admin`
- `GET /admin/login`
- `GET /admin/logout`
- `GET /api/admin/overview`
- `GET /api/admin/config`
- `GET /api/admin/config/history`
- `POST /api/admin/config/publish`
- `POST /api/admin/config/rollback`
- `GET /api/admin/telemetry`
- `GET /api/admin/timeseries`
- `GET /api/admin/benchmark`
- `GET /api/admin/status`
- `POST /api/admin/login`
- `POST /api/admin/logout`
- `GET /api/admin/session`

`/admin` 当前正式支持的页面范围是 overview、telemetry、timeseries、history、benchmark。旧单体里的完整 config 编辑、logs、probe、audit、diff 不再属于默认交付面。

## CLI 工具

`gateway-cli` 是独立的命令行管理工具：

### 构建

```bash
go build -o ./dist/gateway-cli ./cmd/gateway-cli
```

### 命令

```bash
# 配置管理
./dist/gateway-cli config show                    # 显示当前配置
./dist/gateway-cli config show -format json       # JSON 格式输出

# Provider 管理
./dist/gateway-cli provider list                  # 列出所有 providers
./dist/gateway-cli provider test openai           # 测试 provider 连接

# 遥测查询
./dist/gateway-cli telemetry events               # 查询最近 24 小时事件
./dist/gateway-cli telemetry events -format json  # JSON 格式输出

# 发布管理
./dist/gateway-cli publish history                # 查看配置发布历史
./dist/gateway-cli publish rollback rev-001       # 回滚配置

# 测试
./dist/gateway-cli test convert                   # 测试协议转换

# 其他
./dist/gateway-cli version                        # 显示版本
./dist/gateway-cli --help                         # 显示帮助
```

### 选项

- `-server url` - 控制面 URL（默认：http://127.0.0.1:18081）
- `-token token` - Admin token（或设置 ADMIN_TOKEN 环境变量）
- `-format text|json` - 输出格式（默认：text）

### 环境变量

```bash
export ADMIN_TOKEN="your-admin-token"
./dist/gateway-cli config show
```

## 协议转换

网关支持 OpenAI 和 Claude 协议的双向转换：

- OpenAI 客户端可以调用 Claude 模型
- Claude 客户端可以调用 OpenAI 模型
- 自动模型名称映射（gpt-4 ↔ claude-3-opus 等）
- 支持流式 SSE 转换
- 工具调用自动转换

测试协议转换：

```bash
./dist/gateway-cli test convert
```

## 运维约束

- 仓库不再提供 `gateway validate`、`gateway health`、`gateway status`、`gateway install` 这类单一入口命令。
- 仓库不再提供单一 Windows 服务 `AIModelGateway`。
- 请使用你自己的 supervisor / service manager：
  - systemd
  - NSSM / Windows Service Wrapper
  - Docker Compose / Kubernetes
  - 自定义守护脚本

## 仓库布局

- `cmd/gatewayd`
  数据面 daemon
- `cmd/controld`
  控制面 daemon
- `cmd/telemetryd`
  telemetry daemon
- `internal/control`
  控制面 API、compiler、publisher
- `internal/gateway`
  snapshot、API、telemetry client
- `internal/telemetry`
  event log、projection、query
- `internal/contracts`
  plane 间 RPC/transport contract
- `web/admin`
  控制面 SPA

## 文档

- Architecture: [docs/architecture.md](docs/architecture.md)
- Daemon CLI: [docs/cli.md](docs/cli.md)
- Installation: [docs/installation.md](docs/installation.md)
- Deployment: [docs/deployment.md](docs/deployment.md)
- Troubleshooting: [docs/troubleshooting.md](docs/troubleshooting.md)

## License

MIT
