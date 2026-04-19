# AI Model Gateway 架构设计文档

## 概述

AI Model Gateway 当前采用纯三面架构：

- 数据面：`gatewayd`
- 控制面：`controld`
- Telemetry 面：`telemetryd`

`gateway` launcher 已移除。三个 daemon 直接由外部 supervisor、service manager 或容器编排负责启动和拉起。

## 架构图

```text
            ┌──────────────────────────────────────┐
            │ 外部运维层 / Supervisor / Orchestrator │
            │ systemd / NSSM / Compose / k8s 等     │
            └──────────────┬──────────────┬────────┘
                           │              │
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
  - `POST /v1/chat/completions`
- 只执行来自 `controld` 的已编译 snapshot
- 不直接读取 `config.yaml`
- 向 `telemetryd` 异步写入事件

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
3. `telemetryd` 是唯一 telemetry owner。
4. 三个 daemon 不再依赖单一 launcher。
5. 运维层需要自己管理 daemon 生命周期、日志和重启策略。
