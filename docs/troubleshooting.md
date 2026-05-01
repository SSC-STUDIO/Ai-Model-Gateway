# 故障排查指南

当前运行模型默认是 `aigw supervise`，内部仍包含三个 daemon。排障时先检查 `aigw`，再分别检查：

- `aigw`
- `gatewayd`
- `controld`
- `telemetryd`

本机特别注意：如果 `127.0.0.1:18080` 已经由 live legacy monolith 服务占用，开发三面 runtime 的 `gatewayd` 不会正常绑定该端口。先确认当前服务归属，再决定停止 live 服务、改端口，或只做离线构建测试。

## 1. 数据面健康检查失败

症状：

```bash
curl http://127.0.0.1:18080/-/health
```

返回非 200 或持续 `starting`。

排查：

```bash
# Linux
ps aux | grep gatewayd
ps aux | grep aigw
ss -tlnp | grep 18080

# Windows
tasklist | findstr gatewayd
netstat -ano | findstr 18080
```

常见原因：

- `gatewayd -listen` 与预期端口不一致
- live legacy monolith 已占用 `127.0.0.1:18080`
- `controld` 没有成功连接 `gatewayd`
- 还没有 active snapshot 被发布到 `gatewayd`
- `aigw supervise` 因 manifest 或 daemon 版本混装拒绝启动

## 2. 控制面无法访问

症状：

```bash
curl http://127.0.0.1:18081/-/health
curl http://127.0.0.1:18081/admin
```

无法访问或返回错误。

排查：

```bash
# Linux
ps aux | grep controld
ss -tlnp | grep 18081

# Windows
tasklist | findstr controld
netstat -ano | findstr 18081
```

还需要检查：

- `-authoring-config` 是否指向正确的 `config.yaml`
- `controld -gateway` / `-telemetry` 是否和另外两个 daemon 使用同一组 IPC 名称

## 3. Telemetry 数据缺失

症状：

- `/admin` 中 overview / telemetry / timeseries / benchmark 没有数据
- `/api/admin/runtime/status` 中 `telemetry_status` 不是 `connected`

排查：

```bash
# Linux
ps aux | grep telemetryd

# Windows
tasklist | findstr telemetryd
```

检查：

- `telemetryd -ingest` 与 `gatewayd -telemetry` 是否一致
- `telemetryd -query` 与 `controld -telemetry` 是否一致
- telemetry `-data-dir` 是否可写

## 4. 配置修改没有生效

症状：

- 编辑了 `config.yaml`
- 重启后 `/api/admin/config/history` 看不到新的 revision

原因：

- `controld` 有 `publisher-state.db` 时会优先恢复持久化 revision/history
- 它不会因为 YAML 改了就自动重种一条新 revision

处理方式：

- 使用 Admin API 的 publish/rollback 流程管理 revision
- 或者在你明确要重新 seed 的前提下，备份并删除控制面数据目录里的 `publisher-state.db`，然后重启 `controld`

## 5. Provider 连接失败

症状：

- 请求返回 502/503
- `/api/admin/status` 中 provider health 异常

排查：

```bash
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://127.0.0.1:18081/api/admin/status
```

检查：

- `config.yaml` 中 `base_url` 和 `api_key` 是否正确
- provider 是否可达
- 配额是否耗尽

## 6. 日志与运行目录

`aigw supervise` 会把内部 daemon 日志统一写到运行目录：

如果你采用共享运行目录，至少应检查：

```text
.gateway-runtime/
├── telemetry/
├── gateway/
├── control/
└── logs/
```

重点文件：

- `control/publisher-state.db`
- telemetry 数据目录中的 SQLite 文件
- `logs/gatewayd.log`、`logs/controld.log`、`logs/telemetryd.log`
- `aigw` 自身输出通常在 systemd journal、Windows service wrapper 日志或 `deploy/start.sh` 的 `logs/aigw.log`

## 7. Windows 服务

仓库默认提供一个 `aigw.service`，只包装 `aigw supervise`。Windows 请使用：

- NSSM
- 自定义 Windows Service Wrapper
- Task Scheduler
- 容器/虚拟机编排

包装 `aigw.exe supervise`。分别管理 `gatewayd.exe`、`controld.exe`、`telemetryd.exe` 只建议用于高级调试。

## 获取帮助

提交问题时请附上：

- `aigw supervise` 的启动命令
- `aigw logs` 输出或 `.gateway-runtime/logs/` 内容
- `config.yaml`（隐藏敏感信息）
- `publisher-state.db` 是否存在
- 使用的 socket / named pipe 名称
