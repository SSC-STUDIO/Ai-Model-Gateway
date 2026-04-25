# Daemon CLI Guide

仓库已经移除 `gateway` / `gateway.exe`。当前只有三个正式 CLI：

- `gatewayd`
- `controld`
- `telemetryd`

## 总原则

- `-config` 仍表示 daemon 自己的 bootstrap JSON，不是 `config.yaml`
- `controld -authoring-config` 才是人类可编辑 YAML 的入口
- `gatewayd` 不读取 YAML
- 仓库内默认 bootstrap JSON 分别是 `configs/gatewayd.json`、`configs/controld.json`、`configs/telemetryd.json`
- `telemetryd` bootstrap JSON 字段是 `ingest_socket` / `query_socket`，不是 `ingest` / `query`
- 本地默认 HTTP 端口是 `gatewayd` `127.0.0.1:18080`、`controld` `127.0.0.1:18081`
- 推荐把 IPC 名称、数据目录和日志路径都放进同一个运行目录，例如 `.gateway-runtime/`

## gatewayd

```text
gatewayd -listen <addr> -control <socket-or-pipe> -telemetry <socket-or-pipe> -data-dir <dir>
```

可用 flags：

- `-config`
  读取 bootstrap JSON
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

## controld

```text
controld -listen <addr> -gateway <socket-or-pipe> -telemetry <socket-or-pipe> -data-dir <dir> -authoring-config <config.yaml>
```

可用 flags：

- `-config`
  读取 bootstrap JSON
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

说明：

- 当 `publisher-state.db` 不存在时，`controld` 会从 `-authoring-config` 种出初始 revision
- 当 `publisher-state.db` 已存在时，`controld` 会优先恢复持久化 revision/history，而不是重新读取 YAML 生成新 revision

## telemetryd

```text
telemetryd -ingest <socket-or-pipe> -query <socket-or-pipe> -data-dir <dir>
```

可用 flags：

- `-config`
  读取 bootstrap JSON
- `-ingest`
  接收 `gatewayd` 事件写入的 ingest socket / named pipe
- `-query`
  接收 `controld` 查询请求的 query socket / named pipe
- `-data-dir`
  telemetry 数据目录
- `-version`
  打印版本

## 推荐本地启动

Linux/macOS:

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control

./dist/telemetryd \
  -ingest ./.gateway-runtime/telemetry-ingest.sock \
  -query ./.gateway-runtime/telemetry-query.sock \
  -data-dir ./.gateway-runtime/telemetry

./dist/gatewayd \
  -listen 127.0.0.1:18080 \
  -control ./.gateway-runtime/gateway-control.sock \
  -telemetry ./.gateway-runtime/telemetry-ingest.sock \
  -data-dir ./.gateway-runtime/gateway

./dist/controld \
  -listen 127.0.0.1:18081 \
  -gateway ./.gateway-runtime/gateway-control.sock \
  -telemetry ./.gateway-runtime/telemetry-query.sock \
  -data-dir ./.gateway-runtime/control \
  -authoring-config ./configs/config.yaml
```

Windows:

```powershell
$gatewayPipe = "aigw-gateway-control-local"
$ingestPipe = "aigw-telemetry-ingest-local"
$queryPipe = "aigw-telemetry-query-local"

.\dist\telemetryd.exe -ingest $ingestPipe -query $queryPipe -data-dir .\.gateway-runtime\telemetry
.\dist\gatewayd.exe -listen 127.0.0.1:18080 -control $gatewayPipe -telemetry $ingestPipe -data-dir .\.gateway-runtime\gateway
.\dist\controld.exe -listen 127.0.0.1:18081 -gateway $gatewayPipe -telemetry $queryPipe -data-dir .\.gateway-runtime\control -authoring-config .\configs\config.yaml
```

## 健康检查

- 数据面:
  - `http://127.0.0.1:18080/-/health`
  - `http://127.0.0.1:18080/v1/models`
- 控制面:
  - `http://127.0.0.1:18081/-/health`
  - `http://127.0.0.1:18081/admin`
  - `http://127.0.0.1:18081/api/admin/status`

## 已移除接口

下列 `gateway` 单一入口命令已经移除：

- `gateway validate`
- `gateway health`
- `gateway status`
- `gateway install`
- `gateway uninstall`
- `gateway service-start`
- `gateway service-stop`
- `gateway service-status`
- `gateway version`
