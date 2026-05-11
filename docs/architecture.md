# AI Model Gateway 架构设计文档

## 概述

AI Model Gateway 当前采用单入口运维、三面内部架构：

- 运维入口：`aigw`
- 数据面：`gatewayd`
- 控制面：`controld`
- Telemetry 面：`telemetryd`

`gateway` launcher 已移除。外部 supervisor、service manager 或容器编排默认只启动 `aigw supervise`，由它按 `telemetryd -> gatewayd -> controld` 拉起内部 daemon。

## 架构图

```text
            ┌──────────────────────────────────────┐
            │ 外部运维层 / Supervisor / Orchestrator │
            │ systemd / NSSM / Compose / k8s 等     │
            └───────────────────┬──────────────────┘
                                │
                         ┌──────▼──────┐
                         │    aigw     │
                         │  supervise  │
                         └──────┬──────┘
                                │
              ┌────────────▼──┐  ┌───────▼──────┐
              │   数据面       │  │   控制面      │
              │   gatewayd    │  │   controld   │
              │   :18080      │  │   :18081     │
              └───────┬───────┘  └───────┬──────┘
                      │                  │
                      │                  │
                      │        ┌─────────▼─────────┐
                      │        │  Telemetry 面      │
                      └───────►│  telemetryd        │
                               │  IPC only          │
                               └────────────────────┘
```

## 三面职责

### 数据面 `gatewayd`

- 处理客户端推理流量
- 暴露：
  - `GET /-/health`
  - `GET /v1/models`
  - `POST /v1/chat/completions`（OpenAI Chat Completions 形态）
  - `POST /v1/messages`（Anthropic Messages 形态）
  - `POST /v1/responses`（OpenAI Responses 形态；对上游以 Chat Completions 桥接，见 `internal/gateway/api/compat.go`）
- **未实现**的 OpenAI 系子 API（若客户端按「全量 OpenAI」集成会 404），例如：`/v1/embeddings`、图像/音频、Assistants、Batch 等；也不包含 Realtime WebSocket 代理（`cmd/gatewayd` 未挂载 `internal/gateway/websocket`）。
- 仅执行来自 `controld` 的已编译 snapshot
- 不直接读取 `config.yaml`
- 向 `telemetryd` 异步写入事件

#### RunBenchmarkCase 协议字符串（control ↔ gatewayd RPC）

合成基准通过 RPC 走与 HTTP 相同的入口，支持的 `protocol` 值为：`openai_chat_completions`、`anthropic_messages`，以及 `openai_responses`（与常量 `BenchmarkProtocolOpenAIResponses` 对应，定义见 `internal/core/config.go`）。

### 控制面 `controld`

- 持有 authoring config、revision history、publish ledger
- 提供：
  - `GET /admin`
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
- 通过 `-authoring-config` 读取 YAML
- 编译 config 并通过 RPC 发布 snapshot 到 `gatewayd`

### Telemetry 面 `telemetryd`

- 接收 `gatewayd` 上报的 telemetry 事件
- 维护 event log、projection、query store
- 通过 query RPC 为 `controld` 提供 overview / telemetry / timeseries / benchmark
- 不暴露用户 HTTP 端口

## 面间通信

平台相关 IPC：

- Linux/macOS：Unix domain socket
- Windows：Named pipe

契约：

```text
controld ──ApplySnapshot/GetStatus/Drain──► gatewayd
gatewayd ──AppendBatch/Flush/Ping──────────► telemetryd
controld ──GetOverview/GetTelemetry/
            GetTimeSeries/GetModelBenchmark ─► telemetryd
```

## 数据与状态

- authoring config：`configs/config.yaml`
- 控制面状态：`publisher-state.db`
- telemetry 状态：event log + query store
- 建议共享运行目录：`.gateway-runtime/`

推荐目录：

```text
.gateway-runtime/
├── telemetry/
├── gateway/
└── control/
```

## 关键原则

1. `controld` 是唯一配置 owner。
2. `gatewayd` 只执行已编译 snapshot。
3. 所有配置文件通过 `config.yaml` 管理，不暴露敏感信息。
4. 遥测面专注于数据采集和分析，不处理配置变更。
3. `telemetryd` 是唯一 telemetry owner。
4. `aigw` 是默认生命周期、日志、bundle 和升级入口。
5. 单独替换任意 daemon 不是正常升级路径，manifest 校验应拒绝混装运行。
