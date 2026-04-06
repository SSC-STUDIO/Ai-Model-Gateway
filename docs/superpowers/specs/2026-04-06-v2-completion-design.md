# AI Model Gateway v2 功能完善设计文档

**日期**: 2026-04-06  
**版本**: 1.0  
**作者**: Agent Team  
**状态**: 待实现

---

## 1. 项目概述

### 1.1 背景

当前 AI Model Gateway 代码库已采用 v2 架构实现，具备完整的 7 阶段 Pipeline、Admin API/前端、遥测系统。经 5 人 Agent Team 探索确认：

- **v1 功能已基本补齐**：未发现遗留 v1 代码需要迁移
- **主要差距**：后端 API/CLI 多语言支持、性能优化

### 1.2 目标

1. **多语言支持**：后端 API 错误消息、CLI 输出支持 7 种语言（zh/en/ja/ko/es/fr/de）
2. **性能优化**：P99 < 100ms、吞吐量 > 1000 RPS、内存 < 200MB（空闲）/ < 500MB（高负载）

### 1.3 方案选择

采用**并行推进方案**：3 个子系统同时开发，通过清晰的接口边界避免冲突。

---

## 2. 架构设计

### 2.1 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                     v2 Gateway                           │
├─────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Backend     │  │  CLI         │  │  Performance │  │
│  │  i18n        │  │  i18n        │  │  Optimization│  │
│  │  (2 agents)  │  │  (1 agent)   │  │  (2 agents)  │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### 2.2 子系统划分

| 子系统 | Agent 数量 | 负责人 | 代码范围 | 依赖 |
|--------|-----------|--------|----------|------|
| Backend i18n | 2 | team-i18n-backend | `internal/i18n/`, `internal/core/errors.go`, `internal/adminapi/routes.go` | `golang.org/x/text` |
| CLI i18n | 1 | team-i18n-cli | `cmd/gateway/main.go`, `internal/cli/i18n.go` | 无 |
| Performance | 2 | team-perf-mem, team-perf-latency | `internal/app/transport.go`, `internal/infra/telemetrydb/`, JSON 解析 | `json-iterator/go` |

---

## 3. 子系统 1：Backend i18n

### 3.1 目标

将所有后端 API 返回的错误消息翻译为 7 种语言，根据 `admin.language` 配置自动切换。

### 3.2 组件设计

#### 3.2.1 `internal/i18n/` 新包

```go
// bundle.go
package i18n

type Bundle struct {
    printer *message.Printer
    lang    string
}

func New(lang string) *Bundle
func (b *Bundle) T(key string, args ...interface{}) string
func (b *Bundle) SetLanguage(lang string)
```

#### 3.2.2 翻译文件结构

```
internal/i18n/locales/
├── en.json      # 英文（默认 fallback）
├── zh.json      # 中文
├── ja.json      # 日语
├── ko.json      # 韩语
├── es.json      # 西班牙语
├── fr.json      # 法语
└── de.json      # 德语
```

**JSON 格式示例**（zh.json）：
```json
{
  "errors": {
    "no_provider": "没有可用的模型提供商",
    "model_not_found": "模型未找到",
    "upstream_timeout": "上游请求超时",
    "retry_exhausted": "所有重试尝试已耗尽",
    "request_too_large": "请求体过大",
    "unauthorized": "未授权",
    "forbidden": "禁止访问",
    "invalid_token": "无效令牌",
    "invalid_request_body": "无效请求体",
    "config_export_unavailable": "配置导出不可用"
  }
}
```

#### 3.2.3 错误消息键常量

```go
// internal/i18n/keys.go
package i18n

const (
    ErrNoProvider           = "errors.no_provider"
    ErrModelNotFound        = "errors.model_not_found"
    ErrUpstreamTimeout      = "errors.upstream_timeout"
    ErrRetryExhausted       = "errors.retry_exhausted"
    ErrRequestTooLarge      = "errors.request_too_large"
    ErrUnauthorized         = "errors.unauthorized"
    ErrForbidden            = "errors.forbidden"
    ErrInvalidToken         = "errors.invalid_token"
    ErrInvalidRequestBody   = "errors.invalid_request_body"
    ErrConfigExportUnavailable = "errors.config_export_unavailable"
    ErrConfigSaveUnavailable   = "errors.config_save_unavailable"
    ErrInvalidConfigPayload    = "errors.invalid_config_payload"
    ErrConfigHistoryUnavailable = "errors.config_history_unavailable"
    ErrConfigDiffUnavailable    = "errors.config_diff_unavailable"
    ErrConfigRollbackUnavailable = "errors.config_rollback_unavailable"
    ErrInvalidRollbackPayload    = "errors.invalid_rollback_payload"
    ErrInvalidUpstreamProbePayload = "errors.invalid_upstream_probe_payload"
)
```

### 3.3 数据流

```
HTTP Request → Handler → Error Occurs → i18n.T(key, args...) →
    ↓
Lookup Printer(admin.language) → Return Translated Message → JSON Response
```

### 3.4 修改范围

| 文件 | 修改内容 |
|------|----------|
| `internal/core/errors.go` | 将 sentinel errors 改为使用 i18n 包装 |
| `internal/adminapi/routes.go` | 所有 `writeJSON(w, status, map[string]string{"error": ...})` 改为使用 i18n |
| `internal/app/gateway_handler.go` | 错误响应使用 i18n |
| `internal/infra/auth/cookie.go` | 认证错误使用 i18n |

### 3.5 接口集成

Admin API handlers 通过 `Context` 或闭包访问 i18n Bundle：

```go
// internal/adminapi/routes.go
func (s *Server) handleLogin() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // ...
        if err != nil {
            s.i18n.T(ErrInvalidToken)  // 使用 server 的 i18n bundle
        }
    }
}
```

---

## 4. 子系统 2：CLI i18n

### 4.1 目标

将所有 CLI 输出（错误消息、帮助信息、状态提示）翻译为 7 种语言。

### 4.2 组件设计

#### 4.2.1 `internal/cli/i18n.go`

CLI 专用的简化 i18n 实现（不需要 HTTP 请求的动态语言切换）：

```go
package cli

var currentLang = "zh"

func SetLanguage(lang string)
func T(key string, args ...interface{}) string
```

#### 4.2.2 翻译文件

```
internal/cli/locales/
├── en.json
├── zh.json
└── ... (其他 5 种语言)
```

**JSON 格式示例**（zh.json）：
```json
{
  "cli": {
    "gateway_failed": "网关启动失败: {{.Error}}",
    "install_failed": "安装失败: {{.Error}}",
    "uninstall_failed": "卸载失败: {{.Error}}",
    "start_failed": "启动失败: {{.Error}}",
    "stop_failed": "停止失败: {{.Error}}",
    "status_failed": "状态检查失败: {{.Error}}",
    "unknown_command": "未知命令: {{.Command}}",
    "config_not_found": "配置文件未找到: {{.Path}}",
    "config_invalid": "配置验证失败: {{.Error}}",
    "config_valid": "✓ 配置有效",
    "health_check_failed": "健康检查失败: {{.Error}}",
    "service_installed": "✓ 服务安装成功",
    "service_uninstalled": "✓ 服务卸载成功",
    "service_started": "✓ 服务启动成功",
    "service_stopped": "✓ 服务停止成功"
  },
  "usage": {
    "title": "用法: gateway [命令] [选项]",
    "commands": {
      "start": "启动网关服务器",
      "validate": "验证配置文件",
      "health": "检查网关健康状态",
      "install": "安装为 Windows 服务",
      "uninstall": "卸载 Windows 服务"
    },
    "options": {
      "config": "配置文件路径"
    }
  }
}
```

### 4.3 修改范围

| 文件 | 修改内容 |
|------|----------|
| `cmd/gateway/main.go` | 所有 `fmt.Fprintf` 和 `printUsage()` 改为使用 `cli.T()` |
| `internal/runtime/service_windows.go` | Windows 服务相关输出使用 i18n |

### 4.4 语言选择逻辑

1. 优先使用 `-lang` 命令行参数
2. 其次使用系统环境变量 `LANG`
3. 默认使用 `zh`

---

## 5. 子系统 3：Performance Optimization

### 5.1 目标

- P99 延迟 < 100ms（网关自身处理，不含上游响应）
- 吞吐量 > 1000 RPS（单实例）
- 内存占用 < 200MB（空闲）/ < 500MB（高负载）

### 5.2 Agent 分工

#### Agent 1：内存优化（team-perf-mem）

**优化点 1：Request Body Buffer Pool**

```go
// internal/app/buffer_pool.go
var bodyBufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 32*1024) // 32KB
    },
}

func GetBodyBuffer() []byte
func PutBodyBuffer(buf []byte)
```

**优化点 2：Response Body Pool**

```go
// internal/app/transport.go
var responseBufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}
```

**优化点 3：SQLite 批处理优化**

当前：`batch size 64, flush interval 200ms`

优化后：`batch size 256, flush interval 100ms`

**优化点 4：Prepared Statement 缓存**

```go
// internal/infra/telemetrydb/store.go
type Store struct {
    // ...
    stmtCache map[string]*sql.Stmt
}
```

#### Agent 2：延迟优化（team-perf-latency）

**优化点 1：JSON 解析器替换**

从 `encoding/json` 迁移到 `json-iterator`：

```go
import jsoniter "github.com/json-iterator/go"

var json = jsoniter.ConfigCompatibleWithStandardLibrary
```

涉及文件：
- `internal/app/pipeline.go`
- `internal/app/gateway_handler.go`
- `internal/adminapi/routes.go`

**优化点 2：Header 处理零拷贝**

避免不必要的字符串转换：

```go
// 优化前
contentType := strings.ToLower(r.Header.Get("Content-Type"))

// 优化后
contentType := r.Header.Get("Content-Type")
if len(contentType) > 0 && contentType[0] == 'a' { // 直接比较
```

**优化点 3：连接池调优**

```go
// internal/app/transport.go
transport := &http.Transport{
    MaxIdleConns:        500,  // 默认 100
    MaxIdleConnsPerHost: 100,  // 默认 10
    MaxConnsPerHost:     200,  // 新增
    IdleConnTimeout:     90 * time.Second,
    // ...
}
```

### 5.3 基准测试

每个优化点需要配套的 benchmark：

```go
// internal/app/benchmark_test.go
func BenchmarkPipelineJSONParsing(b *testing.B)
func BenchmarkTransportRequest(b *testing.B)
func BenchmarkTelemetryBatchWrite(b *testing.B)
```

---

## 6. 协调与冲突避免

### 6.1 共享文件修改策略

| 共享文件 | 修改策略 |
|----------|----------|
| `go.mod` / `go.sum` | 各 agent 记录依赖需求，由主协调人统一添加 |
| `internal/core/errors.go` | Backend i18n agent 统一修改，其他 agent 只读 |
| `cmd/gateway/main.go` | CLI i18n agent 修改，其他 agent 避免修改 |

### 6.2 代码审查流程

1. 每个子系统完成后提交 PR
2. 其他子系统 Agent 审查接口兼容性
3. 主协调人合并前统一运行测试

### 6.3 测试策略

| 子系统 | 测试类型 | 验证内容 |
|--------|----------|----------|
| Backend i18n | 单元测试 | 每种语言的翻译输出正确 |
| CLI i18n | 集成测试 | CLI 输出语言切换正常 |
| Performance | Benchmark | 优化前后的延迟/内存对比 |

---

## 7. 风险与缓解

| 风险 | 可能性 | 影响 | 缓解措施 |
|------|--------|------|----------|
| go.mod 冲突 | 高 | 中 | 统一由主协调人管理依赖 |
| i18n key 命名不一致 | 中 | 低 | 统一使用 `errors.xxx` / `cli.xxx` 前缀 |
| 性能优化引入 bug | 中 | 高 | 每个优化点必须配套基准测试和回归测试 |
| 翻译质量不高 | 低 | 低 | 使用常见翻译，后续迭代改进 |

---

## 8. 验收标准

### 8.1 功能验收

- [ ] Admin API 所有错误消息返回中文（配置 `language: zh`）
- [ ] CLI 帮助信息支持 `-lang` 参数切换
- [ ] 7 种语言翻译文件完整

### 8.2 性能验收

- [ ] Benchmark 显示 JSON 解析性能提升 > 30%
- [ ] 内存分配次数减少 > 20%（通过 `benchcmp` 验证）
- [ ] 1000 RPS 压测 5 分钟，P99 < 100ms，无内存泄漏

---

## 9. 附录

### 9.1 依赖列表

```
golang.org/x/text v0.21.0
github.com/json-iterator/go v1.1.12
```

### 9.2 参考文档

- `README.md` - 项目概述
- `docs/v1-v2-feature-comparison.md` - v1/v2 功能对比
- `configs/config.example.yaml` - v2 配置结构

---

**文档结束**
