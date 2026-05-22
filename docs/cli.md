# CLI Guide

AI Model Gateway has two supported operator-facing CLIs:

- `aigw`
  本机运维入口。负责统一启动、健康检查、日志、备份、bundle 校验、升级和回滚。
- `gateway-cli`
  远程/admin CLI。通过 `controld` 的 Admin API 做配置、审计、探测、replay、runtime 查询等操作。

`gatewayd`、`controld`、`telemetryd` 仍然存在，但只作为 `aigw supervise` 管理的内部 daemon。直接运行 daemon 只建议用于高级调试，不是正常部署、升级或回滚路径。

## 总原则

- 默认生产入口是 `aigw supervise`，service manager 只包装这一条命令。
- 三个 daemon 必须来自同一个 bundle、同一个 manifest、同一个产品版本。
- `aigw supervise` 启动顺序固定为 `telemetryd -> gatewayd -> controld`。
- `-config` 仍表示 daemon 自己的 bootstrap JSON，不是 `config.yaml`。
- `controld -authoring-config` 才是人类可编辑 YAML 的入口。
- `gatewayd` 不读取 YAML，只执行 `controld` 发布的 snapshot。
- 默认 bootstrap JSON 是 `configs/gatewayd.json`、`configs/controld.json`、`configs/telemetryd.json`。
- `telemetryd` bootstrap JSON 字段是 `ingest_socket` / `query_socket`，不是 `ingest` / `query`。
- 推荐把 IPC 名称、数据目录、备份和日志都放进同一个运行目录，例如 `.gateway-runtime/`。

## aigw

### version

```bash
./dist/aigw version
```

打印本地运维入口版本和平台。

### supervise

```bash
./dist/aigw supervise \
  -runtime-root .gateway-runtime \
  -config-dir configs \
  -bin-dir ./dist \
  -manifest aigw-manifest.json
```

常用 flags：

- `-runtime-root`
  运行目录，默认 `.gateway-runtime`
- `-config-dir`
  daemon bootstrap JSON 目录，默认 `configs`
- `-bin-dir`
  `gatewayd`、`controld`、`telemetryd` 所在目录
- `-manifest`
  bundle manifest 路径，默认 `aigw-manifest.json`
- `-strict-manifest`
  manifest 缺失时也失败
- `-startup-timeout`
  启动健康检查超时，默认 `30s`

`supervise` 会在启动前做两类拒绝检查：

- 校验 manifest 中的 daemon hash、产品版本、RPC contract、snapshot schema 和平台。
- 执行每个 daemon 的 `-version`，发现任意 daemon 与 `aigw` 版本不同就拒绝启动。

日志写入：

```text
.gateway-runtime/logs/telemetryd.log
.gateway-runtime/logs/gatewayd.log
.gateway-runtime/logs/controld.log
```

### doctor

```bash
./dist/aigw doctor -runtime-root .gateway-runtime -config-dir configs -manifest aigw-manifest.json
./dist/aigw doctor -format json
```

检查本地 bootstrap JSON、manifest 和运行目录 socket 状态。适合在启动或升级前做本机预检。

### status

```bash
./dist/aigw status
./dist/aigw status -control-url http://127.0.0.1:18081 -gateway-url http://127.0.0.1:18080 -token "$ADMIN_TOKEN" -format json
```

查询本机 control plane runtime 状态和 gateway health。`-token` 默认读取 `ADMIN_TOKEN`，用于访问启用了认证的 runtime status API。

### logs

```bash
./dist/aigw logs
./dist/aigw logs -n 200 gatewayd
./dist/aigw logs -runtime-root .gateway-runtime telemetryd controld
```

读取统一日志目录下的 daemon 日志。未指定 daemon 时默认输出 `telemetryd`、`gatewayd`、`controld`。

### backup

```bash
./dist/aigw backup -runtime-root .gateway-runtime -config-dir configs
./dist/aigw backup -out .gateway-runtime/backups/manual-001
```

备份配置、control/gateway/telemetry 状态、迁移后的 telemetry 数据目录和 manifest。未指定 `-out` 时写入 `.gateway-runtime/backups/<utc timestamp>`。

### bundle

```bash
./dist/aigw bundle build -root . -out aigw-manifest.json
./dist/aigw bundle verify -root . -manifest aigw-manifest.json
./dist/aigw bundle verify -format json
```

manifest 包含产品版本、git commit、构建时间、平台、binary hash、admin dist hash、snapshot schema、RPC contract、迁移要求和默认配置路径。

### update

```bash
./dist/aigw update apply -bundle /path/to/bundle -install-dir /opt/ai-model-gateway
./dist/aigw update apply -bundle /path/to/bundle -install-dir /opt/ai-model-gateway -dry-run
./dist/aigw update rollback -install-dir /opt/ai-model-gateway
```

`update apply` 会先校验目标 bundle manifest，再备份当前 payload，然后复制新 payload。复制失败会尝试恢复刚创建的备份。危险主机操作仍应只在本机执行。

### service

```bash
./dist/aigw service print
```

打印默认 systemd unit 模板。Linux 默认只安装一个 `aigw.service`；Windows 请用 NSSM 或 Windows Service Wrapper 包装 `aigw.exe supervise`。

## gateway-cli

`gateway-cli` 通过 `controld` Admin API 工作，默认连接 `http://127.0.0.1:18081`。

通用选项：

- `-server url`
  控制面 URL，默认 `http://127.0.0.1:18081`
- `-token token`
  Admin token，默认读取 `ADMIN_TOKEN`
- `-format text|json|csv`
  输出格式。默认 text，`json` 输出结构化 JSON；CSV 仅支持 benchmark telemetry、telemetry-summary、target-summary。其他命令传入无效格式会返回错误。

常用命令：

```bash
./dist/gateway-cli health
./dist/gateway-cli status
./dist/gateway-cli validate configs/config.yaml
./dist/gateway-cli reload

./dist/gateway-cli config show
./dist/gateway-cli config preview configs/config.yaml
./dist/gateway-cli config diff --file configs/config.yaml
./dist/gateway-cli config diff --from rev-001 --to rev-002

./dist/gateway-cli runtime status
./dist/gateway-cli runtime preflight
./dist/gateway-cli audit 100
./dist/gateway-cli probe provider openai-demo gpt-4
./dist/gateway-cli probe model gpt-4 openai-demo
./dist/gateway-cli replay list
./dist/gateway-cli replay <request-id>
./dist/gateway-cli diagnostics
./dist/gateway-cli secrets check

./dist/gateway-cli provider list
./dist/gateway-cli provider test openai
./dist/gateway-cli telemetry events
./dist/gateway-cli publish history
./dist/gateway-cli publish rollback rev-001
./dist/gateway-cli benchmark baselines
./dist/gateway-cli benchmark run --all-active --public-snapshot <id>
./dist/gateway-cli test convert
./dist/gateway-cli version
```

权限约定：

- viewer token 只应访问只读状态、审计、日志、diagnostics 等查询能力。
- admin token 才能执行配置发布、rollback、probe、replay 和 runtime preflight。
- API、CLI、UI、日志都不得输出明文密钥。

## Admin API

当前主要 admin/ops API：

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

## Daemon CLI

直接运行 daemon 只用于高级调试。正常部署、服务管理、升级和回滚请使用 `aigw`。

### gatewayd

```text
gatewayd -listen <addr> -control <socket-or-pipe> -telemetry <socket-or-pipe> -data-dir <dir>
```

可用 flags：

- `-config`
  读取 `gatewayd` bootstrap JSON
- `-listen`
  数据面 HTTP 监听地址
- `-control`
  控制面 RPC socket / named pipe
- `-telemetry`
  telemetry ingest socket / named pipe
- `-data-dir`
  数据面运行数据目录
- `-version`
  打印版本

### controld

```text
controld -listen <addr> -gateway <socket-or-pipe> -telemetry <socket-or-pipe> -data-dir <dir> -authoring-config <config.yaml>
```

可用 flags：

- `-config`
  读取 `controld` bootstrap JSON
- `-listen`
  控制面 HTTP 监听地址
- `-gateway`
  `gatewayd` 控制 RPC socket / named pipe
- `-telemetry`
  `telemetryd` query RPC socket / named pipe
- `-data-dir`
  控制面数据目录，包含 `publisher-state.db`
- `-authoring-config`
  人类可编辑 YAML 配置文件路径
- `-version`
  打印版本

当 `publisher-state.db` 不存在时，`controld` 会从 `-authoring-config` 种出初始 revision。当 `publisher-state.db` 已存在时，`controld` 会优先恢复持久化 revision/history。

### telemetryd

```text
telemetryd -ingest <socket-or-pipe> -query <socket-or-pipe> -data-dir <dir>
```

可用 flags：

- `-config`
  读取 `telemetryd` bootstrap JSON
- `-ingest`
  接收 `gatewayd` 事件写入的 ingest socket / named pipe
- `-query`
  接收 `controld` 查询请求的 query socket / named pipe
- `-data-dir`
  telemetry 数据目录
- `-version`
  打印版本

## 高级调试启动

只在需要绕过 `aigw supervise` 做问题定位时使用。

Linux/macOS:

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control

./dist/telemetryd -config configs/telemetryd.json
./dist/gatewayd -config configs/gatewayd.json
./dist/controld -config configs/controld.json
```

Windows:

```powershell
.\dist\telemetryd.exe -config .\configs\telemetryd.json
.\dist\gatewayd.exe -config .\configs\gatewayd.json
.\dist\controld.exe -config .\configs\controld.json
```

## 健康检查

- 数据面:
  - `http://127.0.0.1:18080/-/health`
  - `http://127.0.0.1:18080/v1/models`
- 控制面:
  - `http://127.0.0.1:18081/-/health`
  - `http://127.0.0.1:18081/admin`
  - `http://127.0.0.1:18081/api/admin/runtime/status`（需要 admin/viewer token）
- Prometheus:
  - `http://127.0.0.1:18081/metrics`

## 已移除接口

下列旧 `gateway` 单一入口命令已经移除：

- `gateway validate`
- `gateway health`
- `gateway status`
- `gateway install`
- `gateway uninstall`
- `gateway service-start`
- `gateway service-stop`
- `gateway service-status`
- `gateway version`
