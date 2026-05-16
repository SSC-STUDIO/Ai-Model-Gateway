[![CI](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-1.3.0-blue.svg)](VERSION)

# AI Model Gateway

AI 模型路由网关 — 统一管理多模型提供商的路由、遥测、限流、缓存、SSRF 防护和管理面板。

[变更日志](CHANGELOG.md) · [贡献指南](CONTRIBUTING.md) · [安全](SECURITY.md) · [文档](docs/)

---

## 功能特性

| 功能 | 说明 |
|------|------|
| **多协议支持** | OpenAI Chat Completions、Anthropic Messages、OpenAI Responses |
| **模型路由** | 通配符匹配、provider 级路由、策略 fallback |
| **速率限制** | 令牌桶算法、API Key / IP / 模型级限流 |
| **请求缓存** | 内存 LRU 缓存、可配置 TTL 和条目数 |
| **SSRF 防护** | DNS 固定、私有 IP 检测、可配置白名单 |
| **智能降级** | 主模型失败时自动 fallback 备选模型、循环检测 |
| **遥测** | 异步事件收集、成本聚合、投影查询 |
| **审计日志** | 控制面操作全量记录、可搜索过滤 |
| **管理面板** | Preact SPA：Overview / Monitoring / Benchmark / Ops / Config / Logs |
| **i18n 国际化** | 中文 / English / 日本語 / 한국어 / Español / Français / Deutsch |
| **配置热加载** | YAML authoring config → 编译 snapshot → 无中断发布 |
| **基准测试** | 多模型能力对比、5 种评分器（exact / judge / json / tool / stream） |

## 架构

仓库使用**三面内部架构**，统一运维入口 `aigw supervise`：

```text
                   ┌──────────────────────────────────────┐
                   │    Supervisor / systemd / k8s         │
                   └──────────────────┬───────────────────┘
                                      │
                               ┌──────▼──────┐
                               │   aigw      │
                               │  supervise  │
                               └──────┬──────┘
                                      │
                    ┌─────────────────▼──────┐  ┌──────────▼──────────┐
                    │   Data Plane           │  │   Control Plane     │
                    │   gatewayd (:18080)    │  │   controld (:18081) │
                    └──────────┬─────────────┘  └──────────┬──────────┘
                               │                           │
                               │                 ┌─────────▼──────────┐
                               └────────────────►│  Telemetry Plane   │
                                                  │  telemetryd (IPC)  │
                                                  └────────────────────┘
```

- **`aigw`** — 本地运维入口：supervise、doctor、bundle verify、版本升级/回滚
- **`gatewayd`** — 数据面：客户端推理流量、OpenAI/Anthropic API、健康检查（监听 `:18080`）
- **`controld`** — 控制面：管理面板 API、配置编译/发布/回滚、审计、探测、基准测试（监听 `:18081`）
- **`telemetryd`** — Telemetry 面：异步事件收集、投影聚合、查询（仅 IPC，不暴露 HTTP）

`gateway` / `gateway.exe` 旧 launcher 已移除。生产运维默认只管理 `aigw supervise`。

## 管理面板

启动后在 `http://localhost:18080/admin` 访问管理面板：

| 页面 | 功能 |
|------|------|
| **Overview** | 网关概览、健康状态、关键指标 |
| **Monitoring** | 流量监控、成本追踪、模型使用统计、价格查看 |
| **Benchmark** | 多模型能力对比、自动评分、模型上游/能力视图 |
| **Ops** | Runtime 状态、Provider 探测、审计日志、诊断、Replay |
| **Config** | YAML/JSON/图形化配置编辑、发布历史/版本对比 |
| **Logs** | 请求日志搜索、错误查看筛选、CSV 导出 |

## 测试

```bash
# 全部 Go 测试
go test ./... -count=1

# 特定包测试（含覆盖率）
go test ./internal/gateway/... -count=1 -cover

# 前端测试
cd web/admin && npm test

# 前端构建
cd web/admin && npm run build

# Playwright UI 审计
node output/playwright/ui_review_live.cjs
```

部署验证脚本：`scripts/sync-bundle-to-home-ai-gateway.sh`（构建 → manifest → 部署 → 验证完整流程）。

---

## 快速开始

### 构建

```bash
go build -o ./dist/aigw        ./cmd/aigw
go build -o ./dist/gatewayd    ./cmd/gatewayd
go build -o ./dist/controld    ./cmd/controld
go build -o ./dist/telemetryd  ./cmd/telemetryd
go build -o ./dist/gateway-cli ./cmd/gateway-cli
```

正式发布包应生成并校验 manifest：

```bash
./dist/aigw bundle build -root . -out aigw-manifest.json
./dist/aigw bundle verify -root . -manifest aigw-manifest.json
```

### 配置

1. 准备 authoring config：
```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
$env:ADMIN_BOOTSTRAP_TOKEN = "<32+ chars>"
$env:COOKIE_SIGNING_KEY = "<32+ chars>"
$env:ADMIN_TOKEN = "<admin token>"
$env:VIEWER_TOKEN = "<viewer token>"
```

2. 配置加载器会在校验前展开 `config.yaml` 里的 `$VAR` / `${VAR}`。

3. daemon bootstrap JSON 是独立文件：`configs/gatewayd.json`、`configs/controld.json`、`configs/telemetryd.json`

### 启动

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control
./dist/aigw supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir ./dist
```

### 验证

```bash
curl http://127.0.0.1:18080/-/health
curl http://127.0.0.1:18080/v1/models
curl http://127.0.0.1:18081/admin
```

**本机开发注意**：如果 live 服务已占用 `127.0.0.1:18080`，不要同时启动开发三面 runtime 抢占同一端口。

### 将本机 Codex / Claude Code / OpenClaw 指向网关

可用 `aigw clients` 生成环境变量片段或一键写入本机工具配置：

```bash
# 仅打印（不写盘）
./dist/aigw clients print -config-dir configs

# 写入 ~/.codex/config.toml、~/.claude/settings.json、~/.openclaw/openclaw.json
./dist/aigw clients apply -config-dir configs -api-key "<API key>"

# 预览 apply 行为
./dist/aigw clients apply -dry-run
```

## CLI 工具

`gateway-cli` 是独立的命令行管理工具。

### 命令示例

```bash
# 配置管理
./dist/gateway-cli config show                    # 显示当前配置
./dist/gateway-cli config preview configs/config.yaml
./dist/gateway-cli config diff --file configs/config.yaml

# Runtime
./dist/gateway-cli runtime status
./dist/gateway-cli runtime preflight

# 审计与诊断
./dist/gateway-cli audit 50                       # 最近 50 条审计记录
./dist/gateway-cli diagnostics
./dist/gateway-cli secrets check

# Provider 探测
./dist/gateway-cli probe model gpt-4 openai-demo
./dist/gateway-cli provider list
./dist/gateway-cli provider test openai

# 遥测查询
./dist/gateway-cli telemetry events

# 发布管理
./dist/gateway-cli publish history
./dist/gateway-cli publish rollback rev-001

# 其他
./dist/gateway-cli --help                         # 显示帮助
./dist/gateway-cli version                        # 显示版本
```

### 选项

- `-server url` — 控制面 URL（默认：http://127.0.0.1:18081）
- `-token token` — Admin token（或 `ADMIN_TOKEN` 环境变量）
- `-format text|json|csv` — 输出格式

## 端口与接口

数据面默认 `:18080`，控制面默认 `:18081`（数据面 + 1）。

完整路由列表见 [`docs/cli.md`](docs/cli.md)。

## 管理界面截图

### Overview

![Admin overview](docs/screenshots/admin-overview.png)

### Config

![Admin config](docs/assets/admin-settings.png)

## 仓库布局

| 路径 | 说明 |
|------|------|
| `cmd/aigw/` | 运维入口 |
| `cmd/gatewayd/` | 数据面 daemon |
| `cmd/controld/` | 控制面 daemon |
| `cmd/telemetryd/` | Telemetry daemon |
| `cmd/gateway-cli/` | 远程管理 CLI |
| `internal/control/` | 控制面 API、compiler、publisher |
| `internal/gateway/` | snapshot、API handler、telemetry client |
| `internal/telemetry/` | event log、projection、query |
| `internal/contracts/` | 跨面 RPC/transport 契约 |
| `internal/infra/` | 共享基础设施（auth、configloader、pricing） |
| `internal/proxy/` | SSRF 安全代理 |
| `web/admin/` | 管理面板 SPA（Preact + Vite） |
| `configs/` | 配置文件 |
| `docs/` | 文档 |
| `scripts/` | 部署/验证辅助脚本 |

## 运维约束

- 不在正常升级中单独替换 `gatewayd`、`controld` 或 `telemetryd`。发布包必须通过同一个 manifest 校验。
- 单独运行 daemon 仅用于高级调试。生产路径使用 `aigw supervise`。
- Linux 使用 `deploy/aigw.service` 或 `aigw service print` 生成的 unit；Windows 使用 NSSM 包装 `aigw.exe supervise`。

## 文档

- [架构设计](docs/architecture.md)
- [安装指南](docs/installation.md)
- [部署指南](docs/deployment.md)
- [CLI 指南](docs/cli.md)
- [故障排除](docs/troubleshooting.md)
- [API Messages 端点](docs/api-messages-endpoint.md)
- [国内模型集成](docs/chinese-models-integration.md)
- [变更日志](CHANGELOG.md)
- [贡献指南](CONTRIBUTING.md)
- [安全政策](SECURITY.md)

## License

MIT — 详见 [LICENSE](LICENSE)。
