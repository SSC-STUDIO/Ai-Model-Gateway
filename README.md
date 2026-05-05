[![CI](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-1.3.0-blue.svg)](VERSION)

# AI Model Gateway

仓库现在采用单一运维入口加三面内部运行时：

- `aigw`
  本地运维入口，负责 supervise、doctor/status/logs/backup、bundle verify、update/rollback

- `gatewayd`
  数据面，负责 `/-/health`、`/v1/models`、`/v1/chat/completions`
- `controld`
  控制面，负责 `/admin`、`/api/admin/*`、revision/publish/history
- `telemetryd`
  telemetry 面，负责事件落盘、投影、查询 RPC

`gateway` / `gateway.exe` 已经移除。生产运维默认只管理 `aigw supervise`，三个 daemon 保留为内部进程边界和高级调试入口。

## 当前运行模型

- 人类可编辑配置仍然是 `configs/config.yaml`
- `controld` 通过 `-authoring-config <path>` 读取 YAML，并把它编译/持久化为 revision
- daemon bootstrap JSON 是独立文件：`configs/gatewayd.json`、`configs/controld.json`、`configs/telemetryd.json`
- `gatewayd` 不读取 YAML，只执行 `controld` 发布的 snapshot
- `telemetryd` 仍是唯一 telemetry append/query owner
- `telemetryd` JSON 字段使用 `ingest_socket` / `query_socket`
- 推荐继续使用共享运行目录，例如 `.gateway-runtime/`，集中放置：
  - IPC socket / named pipe 名称
  - telemetry 数据目录
  - control 数据目录中的 `publisher-state.db`

本机开发注意：这台工作站的 live legacy monolith 可能已经占用 `127.0.0.1:18080`。除非明确要切换运行时，不要在 live 服务运行时再启动开发三面 runtime 抢占同一个端口。

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

正式发布包应生成并校验 manifest：

```bash
./dist/aigw bundle build -root . -out aigw-manifest.json
./dist/aigw bundle verify -root . -manifest aigw-manifest.json
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

2. 启动统一 supervisor。

Linux/macOS:

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control
./dist/aigw supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir ./dist
```

Windows PowerShell:

```powershell
.\dist\aigw.exe supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir .\dist
```

3. 验证：

```powershell
curl.exe http://127.0.0.1:18080/-/health
curl.exe http://127.0.0.1:18080/v1/models
curl.exe http://127.0.0.1:18081/admin
curl.exe http://127.0.0.1:18081/-/health
curl.exe -H "Authorization: Bearer $env:ADMIN_TOKEN" http://127.0.0.1:18081/api/admin/runtime/status
```

Windows 下推荐直接运行：

```powershell
.\scripts\verify-default-runtime.ps1
```

这个脚本会验证默认三面 runtime。部署脚本 `deploy/start.sh` 默认构建并启动 `aigw supervise`。

### 将本机 Codex / Claude Code / OpenClaw 指向网关

在已写好 `configs/gatewayd.json`（或已启动 `gatewayd`）的前提下，可用 `aigw clients` 生成环境变量片段，或一键写入本机工具配置（会先备份已有文件）：

```bash
# 仅打印 Bash / PowerShell 与各工具将改动的路径（不写盘）
./dist/aigw clients print -config-dir configs

# 写入 ~/.codex/config.toml、~/.claude/settings.json、~/.openclaw/openclaw.json
# 使用 -api-key 或环境变量 GATEWAY_CLIENT_API_KEY / GATEWAY_API_KEY / OPENAI_API_KEY
./dist/aigw clients apply -config-dir configs -api-key "<与 gateway 配置一致的 API key>"

# 仅改其中几个工具
./dist/aigw clients apply -tools codex,claude-code -gateway-url http://127.0.0.1:18080

# 预览 apply 行为
./dist/aigw clients apply -dry-run
```

说明：

- 数据面 OpenAI 兼容 Base URL 为 `http://<listen>/v1`（由 `gatewayd.json` 的 `listen` 或 `-gateway-url` 推导）。
- Claude Code 使用 `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` 指向同一网关（网关提供 Anthropic 兼容 `/v1/messages`）。
- OpenClaw 会合并 `models.providers["ai-model-gateway"]`（`api: "openai-completions"`）；未提供 key 时 `apiKey` 为 `${AI_MODEL_GATEWAY_API_KEY}`，请在环境中配置该变量。写入 `openclaw.json` 时会先做 HuJSON 规范化，**原文件中的注释可能被去掉**，请先备份或使用 `-dry-run`。
- `-openclaw-model` 默认 `gpt-4o`，需与网关中对外 `public_model` 一致；可用 `-openclaw-set-primary=false` 只增加 provider、不改默认主模型。
- 在 **WSL** 与 **Windows** 各自 home 下分别执行 `apply` 时，只会更新当前环境的用户目录。

## 管理界面截图

### Overview

![Admin overview](docs/screenshots/admin-overview.png)

### Config

![Admin config](docs/assets/admin-settings.png)

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
- `GET /api/admin/runtime/status`
- `POST /api/admin/runtime/preflight`
- `GET /api/admin/audit`
- `POST /api/admin/config/preview`
- `POST /api/admin/config/diff`
- `POST /api/admin/probe/provider`
- `POST /api/admin/probe/model`
- `GET|POST /api/admin/replay`
- `GET /api/admin/diagnostics`
- `GET /api/admin/secrets/status`
- `GET /metrics`
- `POST /api/admin/login`
- `POST /api/admin/logout`
- `GET /api/admin/session`

`/admin` 默认提供 overview、telemetry、pricing、logs、benchmark、config，并新增 Runtime、Audit、Probe、Diagnostics 运维入口。

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
./dist/gateway-cli -format json config show       # JSON 格式输出
./dist/gateway-cli config preview configs/config.yaml
./dist/gateway-cli config diff --file configs/config.yaml
./dist/gateway-cli runtime status
./dist/gateway-cli runtime preflight
./dist/gateway-cli audit 50
./dist/gateway-cli probe model gpt-4 openai-demo
./dist/gateway-cli replay list
./dist/gateway-cli diagnostics
./dist/gateway-cli secrets check

# Provider 管理
./dist/gateway-cli provider list                  # 列出所有 providers
./dist/gateway-cli provider test openai           # 测试 provider 连接

# 遥测查询
./dist/gateway-cli telemetry events               # 查询最近 24 小时事件
./dist/gateway-cli -format json telemetry events  # JSON 格式输出

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
- `-format text|json|csv` - 输出格式（默认：text；CSV 仅用于 benchmark telemetry 类命令）

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

- 仓库不再提供 `gateway validate`、`gateway health`、`gateway status`、`gateway install` 这类旧 launcher 命令。
- 生产服务默认只包装 `aigw supervise`。Linux 使用 `deploy/aigw.service` 或 `aigw service print` 生成的 unit；Windows 使用 NSSM / Windows Service Wrapper 包装 `aigw.exe supervise`。
- 不要在正常升级中单独替换 `gatewayd`、`controld` 或 `telemetryd`。发布包必须通过同一个 manifest 校验，混装版本会被 `aigw` 拒绝。
- 单独运行 daemon 只作为高级调试模式。

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
