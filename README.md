# AI Model Gateway

[![CI](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/SSC-STUDIO/ai-model-gateway/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

![AI Model Gateway icon](docs/assets/icon.svg)

本项目是一个本地运行的 OpenAI-compatible 网关，目标是把多家上游服务、热加载配置、自动切换、可视化运维、成本估算和缓存命中分析收敛到一个统一入口。

它适合这些场景：

- 用统一的 `base_url` / `api_key` 接入多家 OpenAI 兼容中转站
- 按模型白名单把请求路由到不同上游
- 在上游不稳定时自动重试、切换、冷却降级
- 观察请求量、延迟、失败轨迹、缓存命中和估算成本
- 运行时直接在管理页维护 bridge / retry / upstream / config history
- 兼容 Claude 等非原生 OpenAI 模型，通过网关做协议适配

## Highlights

- OpenAI 兼容接口：`chat/completions`、`responses`、`embeddings`、`files`、`audio`、`images`、`models` 等
- 多上游容灾：按健康状态、权重和失败窗口自动切换
- 热加载：修改 YAML 后自动生效
- 会话粘性：`responses` 会优先回到同一上游，提高 prompt cache 命中率
- 定价系统：本地缓存 OpenAI 官方价格页，桥接请求也能按请求模型计价
- 持久化 telemetry：请求、错误、token、缓存命中、成本都落 SQLite
- 管理页：图表、表格、成本、缓存趋势、配置编辑、配置历史、diff 预览
- Claude 兼容：当 Claude 上游不支持 `Responses API` 时，自动回退到 `chat/completions` 并封装回 `response` 对象

## Screenshot

### Admin overview

![Admin overview](docs/screenshots/admin-overview.png)

## Repo metadata

- License: [MIT](LICENSE)
- Change log: [CHANGELOG.md](CHANGELOG.md)
- CI: [GitHub Actions workflow](.github/workflows/ci.yml)

## Architecture

### Request flow

1. 客户端把请求发到本地网关。
2. 网关读取请求中的 `model`，应用 bridge 规则和模型白名单。
3. 路由层根据健康状态、权重、失败计数和 sticky session 选择上游。
4. 代理层转发请求，必要时做 SSE/Responses/Claude 兼容转换。
5. telemetry 记录请求轨迹、usage、缓存命中、错误和估算成本。
6. 管理页从 `/-/admin/data` 与 `/-/admin/timeseries` 读取聚合数据展示。

### Project layout

- `cmd/gateway`
  Gateway 入口。
- `internal/config`
  YAML 配置结构、默认值、校验、加载器。
- `internal/router`
  上游选择、失败冷却、sticky session。
- `internal/proxy`
  OpenAI-compatible 转发、重试、Claude/Responses 兼容。
- `internal/server`
  HTTP 路由、管理页、管理接口。
- `internal/telemetry`
  SQLite telemetry、时间序列、pricing 聚合。
- `internal/state`
  运行时配置原子存储。
- `configs`
  配置示例。
- `scripts`
  常用启动、服务安装、压测脚本。
- `docs`
  图标与 README 截图素材。

## Supported endpoints

当前已覆盖以下接口：

- `POST /v1/chat/completions`
- `POST /v1/completions`
- `POST /v1/embeddings`
- `POST /v1/responses`
- `POST /v1/responses/compact`
- `GET /v1/responses/{response_id}`
- `DELETE /v1/responses/{response_id}`
- `POST /v1/moderations`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `POST /v1/images/variations`
- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`
- `POST /v1/audio/translations`
- `GET /v1/files`
- `POST /v1/files`
- `GET /v1/files/{file_id}`
- `DELETE /v1/files/{file_id}`
- `GET /v1/files/{file_id}/content`
- `GET /v1/models`
- `GET /-/health`
- `GET /admin`
- `GET /admin/settings`
- `GET /-/admin/data`
- `GET /-/admin/timeseries`
- `GET|PUT /-/admin/config`
- `GET /-/admin/config/export`
- `GET /-/admin/config/history`
- `GET /-/admin/config/history/{version_id}/diff`
- `POST /-/admin/config/rollback`

## Quick start

### Requirements

- Go 1.26+
- Windows PowerShell（脚本示例默认按 PowerShell 写）

### 1. Prepare config

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
```

按需填写：

- `upstreams[].base_url`
- `upstreams[].api_key`
- `upstreams[].models`
- `admin.auth_token`
- `telemetry.sqlite_path`
- `pricing.cache_path`

### 2. Run the gateway

```powershell
go run .\cmd\gateway -config .\configs\config.yaml
```

默认监听地址由配置里的 `listen` 决定，常见本地地址例如：

```text
http://127.0.0.1:18080
```

### 3. Open admin pages

概览页：

```text
http://127.0.0.1:18080/admin?token=YOUR_ADMIN_TOKEN
```

设置页：

```text
http://127.0.0.1:18080/admin/settings?token=YOUR_ADMIN_TOKEN
```

## Configuration guide

### Upstreams

每个上游都可以独立配置：

- `name`
- `base_url`
- `api_key`
- `models`
- `weight`
- `timeout_ms`
- `same_upstream_retries`
- `enabled`
- `headers`

这样可以把不同模型或不同供应商拆成多个路由条目。

### Routing and retry

`router` 控制：

- `strategy`
- `max_retries`
- `retry_backoff_ms`
- `retry_backoff_max_ms`
- `failure_threshold`
- `cooldown_sec`
- `failure_passthrough_after_sec`
- `sticky_sessions.enabled`
- `sticky_sessions.ttl_sec`

### Health checks

`health` 控制主动探活：

- `enabled`
- `interval_sec`
- `timeout_ms`
- `path`

### Model bridge

bridge 可以把请求模型重写到另一个模型，例如把部分流量临时桥接到更稳定的模型。

```yaml
bridge:
  enabled: true
  exclude_user_agents:
    - "*Codex Desktop*"
  rules:
    - from: gpt-5.2
      to: gpt-5.4
```

说明：

- `from` 支持通配
- `exclude_user_agents` 支持通配
- 命中 bridge 后，定价仍优先按原始 `requested_model` 归因
- 对 Codex / IDE 内部请求，建议按 UA 做排除，避免误改写系统请求

### Retry and intercept

`proxy.retry` 和 `proxy.intercepts` 支持把原来写死在代码里的失败判定变成配置项。

常见用途：

- 对 `408` / `429` / `5xx` 自动重试
- 对上游错误正文中的关键字触发 retry
- 对某些路径或状态码提前判定为 fail / retry

## Admin UI

### Overview page

概览页聚焦读数和决策，不直接展示配置编辑器，适合日常巡检：

- Success rate / total cost / 1m RPM / 1m TPM
- Live Performance
- Throughput / Latency / Token / Success-Failure 图表
- Model Economics
- Cost Snapshot
- Upstream Health / Usage
- Cache Trends / Cache Hit Ranking
- Recent Errors / Recent Requests

### Settings page

设置页单独拆到 `/admin/settings`，避免把大块配置表单直接塞进首页。

设置页支持：

- health check 编辑
- model bridge 编辑
- retry policy 编辑
- response intercept 编辑
- upstream provider 编辑
- 配置导出
- 历史版本浏览
- 差异预览与回滚

## Pricing and cache accounting

本项目的计价与缓存统计做了几件事：

- OpenAI 官方价格抓取结果会缓存到本地 JSON
- 抓取失败时，回退到缓存和内置价格种子
- 桥接流量优先按 `requested_model` 定价，而不是只看实际转发模型
- telemetry 会分别记录 `prompt_tokens`、`cached_prompt_tokens`、`completion_tokens`
- 管理页会展示模型级、上游级、窗口级的 cache hit

这能更准确地回答两个问题：

- 现在的钱到底花在哪个模型上
- 成本高是因为 token 真的多，还是因为缓存命中被路由打散了

## Claude compatibility

一些 Claude 上游只支持 `chat/completions` 或 `messages`，不支持 OpenAI `Responses API`。

网关内置了一层兼容逻辑：

- 客户端仍然可以打 `/v1/responses`
- 当 Claude 上游返回 `not implemented` / `unsupported` 时
- 网关自动回退到 `chat/completions`
- 再把结果包装回 `response` 对象或简化的 SSE 事件

这让使用 OpenAI 风格 SDK 的客户端也能接 Claude。

## Persistence

- telemetry 使用 SQLite 持久化
- pricing cache 使用本地 JSON
- 配置保存时会自动保留 `.bak` 和历史版本目录

因此：

- 网关重启后管理页统计不会立刻归零
- 配置误改后可以从历史版本回滚
- 定价页不会因为官方页面短时不可达而完全失效

## Scripts

常用脚本：

```powershell
.\scripts\start-gateway.ps1
.\scripts\rebuild-and-restart.ps1
.\scripts\install-service.ps1
.\scripts\uninstall-service.ps1
.\scripts\invoke-responses-burst.ps1 -Concurrency 20 -RequestsPerWorker 1 -LaunchIntervalMs 200
```

如果你要把它装成 Windows 服务，请以管理员 PowerShell 运行安装/卸载脚本。

## Development

### Format and test

```powershell
gofmt -w .\cmd\gateway\main.go .\internal\**\*.go
go test ./...
```

### Useful checks

```powershell
curl.exe http://127.0.0.1:18080/-/health
curl.exe http://127.0.0.1:18080/v1/models
```

## Public repo notes

公开发布时，建议不要提交这些内容：

- 实际生产 `config.yaml`
- API keys
- SQLite telemetry 数据文件
- pricing cache 数据文件
- 本机日志
- AI 过程文件 / 临时文件

本仓库已经通过 `.gitignore` 排除了这些本机状态文件。

## License

当前仓库未附带许可证；如果你要公开分发，建议补一个明确的 LICENSE 文件。
