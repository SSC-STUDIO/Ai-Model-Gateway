# AI Model Gateway - 协议转换与 CLI 增强

**日期**: 2026-04-18
**状态**: 设计完成，待实现
**方案**: 并行开发（方案 B）

## 概述

为 AI Model Gateway 添加双向协议转换能力（OpenAI ↔ Claude）和完整的 CLI 管理功能，采用多 agent 并行开发策略。

## 目标

### 核心目标

1. **协议转换**: 实现 OpenAI 和 Claude 协议的双向转换
   - OpenAI 客户端可以无缝调用 Claude 模型
   - Claude 客户端可以无缝调用 OpenAI 模型

2. **CLI 增强**: 提供完整的命令行管理能力
   - 配置管理（provider、model 配置）
   - 遥测查询（telemetry、metrics）
   - 配置发布与回滚
   - 测试与调试功能

### 非目标

- 不改变现有三面架构
- 不添加新的 daemon 进程
- 不修改现有 API 的向后兼容性

## 架构设计

### 整体架构

```
并行开发轨道：

轨道 1: 协议转换层
├─ internal/gateway/converter/
│  ├─ openai_to_claude.go    # OpenAI → Claude 转换
│  ├─ claude_to_openai.go    # Claude → OpenAI 转换
│  ├─ types.go                # 转换类型定义
│  └─ converter_test.go       # 单元测试
└─ 集成点: gatewayd handler

轨道 2: CLI 功能
├─ cmd/gateway/commands/
│  ├─ config.go              # 配置管理命令
│  ├─ telemetry.go           # 遥测查询命令
│  ├─ publish.go             # 发布/回滚命令
│  └─ test.go                # 测试/调试命令
├─ internal/cli/
│  └─ client.go              # 控制面 API 客户端
└─ 集成点: controld API
```

### 模块职责

#### 协议转换层 (internal/gateway/converter)

**职责**:
- OpenAI 请求格式 → Claude 请求格式
- Claude 响应格式 → OpenAI 响应格式
- Claude 请求格式 → OpenAI 请求格式
- OpenAI 响应格式 → Claude 响应格式
- 流式 SSE 转换
- 错误映射

**关键类型**:

```go
// 转换器接口
type RequestConverter interface {
    ConvertOpenAIToClaude(req *OpenAIRequest) (*ClaudeRequest, error)
    ConvertClaudeToOpenAI(req *ClaudeRequest) (*OpenAIRequest, error)
}

type ResponseConverter interface {
    ConvertClaudeToOpenAI(resp *ClaudeResponse) (*OpenAIResponse, error)
    ConvertOpenAIToClaude(resp *OpenAIResponse) (*ClaudeResponse, error)
}

// 转换配置
type ConverterConfig struct {
    EnableOpenAIToClaude bool
    EnableClaudeToOpenAI bool
    StreamBufferSize      int
}
```

**集成方式**:
- 在 `internal/gateway/api/handler.go` 中作为中间件或路由组
- 根据 provider 类型自动选择转换器
- 保持现有请求处理流程不变

#### CLI 功能 (cmd/gateway/commands)

**职责**:
- 提供用户友好的命令行界面
- 与控制面 API 交互
- 格式化输出结果
- 支持交互式和脚本式两种模式

**命令结构**:

```
gateway config
  ├─ show              # 显示当前配置
  ├─ edit              # 编辑配置（打开编辑器）
  ├─ validate          # 验证配置文件
  └─ set <key> <value> # 设置配置项

gateway provider
  ├─ list              # 列出所有 provider
  ├─ add <name>        # 添加 provider
  ├─ remove <name>     # 删除 provider
  ├─ test <name>       # 测试 provider 连接
  └─ status <name>     # 查看 provider 状态

gateway telemetry
  ├─ events            # 查询事件日志
  ├─ metrics           # 查询指标数据
  ├─ timeseries        # 查询时序数据
  └─ benchmark         # 查询基准测试

gateway publish
  ├─ apply <file>      # 应用新配置
  ├─ history           # 查看发布历史
  └─ rollback [rev]    # 回滚到指定版本

gateway test
  ├─ request <model>   # 测试模型请求
  ├─ route <model>     # 测试路由策略
  └─ convert <dir>     # 测试协议转换
```

**API 客户端**:

```go
// internal/cli/client.go
type ControlPlaneClient struct {
    baseURL    string
    token      string
    httpClient *http.Client
}

func (c *ControlPlaneClient) GetConfig() (*Config, error)
func (c *ControlPlaneClient) PublishConfig(config *Config) error
func (c *ControlPlaneClient) GetTelemetry(query *TelemetryQuery) (*TelemetryResult, error)
func (c *ControlPlaneClient) GetStatus() (*SystemStatus, error)
```

## 数据流

### 协议转换流程

```
OpenAI 客户端请求
    ↓
gatewayd handler 检测请求格式
    ↓
converter.OpenAIToClaude()
    ↓
转发到 Claude Provider
    ↓
converter.ClaudeToOpenAI()
    ↓
返回 OpenAI 格式响应
```

### CLI 命令流程

```
用户执行 CLI 命令
    ↓
解析命令和参数
    ↓
cli.ControlPlaneClient 调用控制面 API
    ↓
controld 处理请求
    ↓
格式化并输出结果
```

## 测试策略

### 单元测试

**协议转换层**:
- 每个转换函数的单元测试
- 边界条件测试（空值、无效输入）
- 格式兼容性测试
- 流式转换测试

**CLI 层**:
- 命令解析测试
- 参数验证测试
- 输出格式化测试
- API 客户端 mock 测试

### 集成测试

- 协议转换端到端测试
- CLI 与控制面集成测试
- 多 provider 路由测试

### E2E 测试

- 完整请求流程测试（OpenAI 客户端 → Claude provider）
- CLI 完整工作流测试
- 错误处理和恢复测试

## 实现计划

### 阶段 1: 接口契约定义（同步）

**时间**: 0.5 天

**任务**:
1. 定义协议转换接口（`internal/gateway/converter/types.go`）
2. 定义 CLI 客户端接口（`internal/cli/client.go`）
3. 编写接口文档和示例

**交付物**:
- 接口定义文件
- 接口使用文档

### 阶段 2: 并行开发（异步）

**时间**: 2-3 天

**轨道 1: 协议转换层**
- Agent 1 负责实现
- 独立开发，使用 mock provider 测试
- 不依赖轨道 2

**轨道 2: CLI 功能**
- Agent 2 负责实现
- 基于现有控制面 API 开发
- 不依赖轨道 1

**交付物**:
- 轨道 1: 完整的协议转换实现和单元测试
- 轨道 2: 完整的 CLI 实现和单元测试

### 阶段 3: 集成与测试（同步）

**时间**: 1 天

**任务**:
1. 在 gatewayd handler 中集成协议转换层
2. CLI 与控制面集成测试
3. 编写集成测试和 E2E 测试
4. 性能测试和优化

**交付物**:
- 集成后的 gatewayd
- 完整的测试套件
- 性能测试报告

### 阶段 4: 文档与发布（同步）

**时间**: 0.5 天

**任务**:
1. 更新 README.md
2. 更新 docs/architecture.md
3. 编写 CLI 使用文档
4. 编写协议转换使用指南
5. 更新 CHANGELOG.md

**交付物**:
- 完整的文档
- CHANGELOG 更新

## 技术细节

### 协议转换映射

#### OpenAI → Claude 请求映射

| OpenAI 字段 | Claude 字段 | 转换逻辑 |
|------------|------------|---------|
| messages | messages | 角色映射: system→system, user→user, assistant→assistant |
| model | model | 模型名称映射 |
| temperature | temperature | 直接传递 |
| max_tokens | max_tokens | 直接传递 |
| stream | stream | 直接传递 |
| tools | tools | 工具定义转换 |
| functions | - | 映射到 tools |

#### Claude → OpenAI 响应映射

| Claude 字段 | OpenAI 字段 | 转换逻辑 |
|------------|------------|---------|
| content | choices[0].message.content | 文本内容 |
| role | choices[0].message.role | 角色映射 |
| stop_reason | choices[0].finish_reason | 停止原因映射 |
| usage | usage | Token 使用量 |
| model | model | 模型名称 |

### CLI 输出格式

**表格格式**（默认）:
```
PROVIDER      STATUS    HEALTH    LATENCY    MODELS
openai        active    healthy   45ms       5
anthropic     active    healthy   120ms      3
azure         inactive  -         -          -
```

**JSON 格式**（`-o json`）:
```json
{
  "providers": [
    {
      "name": "openai",
      "status": "active",
      "health": "healthy",
      "latency_ms": 45,
      "models": 5
    }
  ]
}
```

## 风险与缓解

### 风险 1: 协议转换不完整

**风险**: OpenAI 和 Claude 协议存在不兼容的特性

**缓解**:
- 明确支持的特性范围
- 对不支持的特性返回明确的错误信息
- 提供配置选项控制转换行为

### 风险 2: 并行开发集成冲突

**风险**: 两个轨道的代码在集成时产生冲突

**缓解**:
- 提前定义清晰的接口契约
- 使用接口隔离开发依赖
- 频繁的代码同步和冲突检测
- 集成前进行代码审查

### 风险 3: 性能影响

**风险**: 协议转换增加请求延迟

**缓解**:
- 使用对象池减少内存分配
- 优化转换逻辑，避免不必要的复制
- 性能基准测试
- 提供配置选项禁用转换

## 成功标准

### 功能标准

- [ ] OpenAI 客户端可以成功调用 Claude 模型
- [ ] Claude 客户端可以成功调用 OpenAI 模型
- [ ] CLI 所有命令正常工作
- [ ] 流式请求正确转换
- [ ] 错误正确映射和返回

### 质量标准

- [ ] 单元测试覆盖率 ≥ 80%
- [ ] 集成测试覆盖主要场景
- [ ] E2E 测试通过
- [ ] 无性能回归（延迟增加 < 10ms）
- [ ] 代码通过 golangci-lint 检查

### 文档标准

- [ ] README 更新完整
- [ ] CLI 使用文档完整
- [ ] 协议转换使用指南完整
- [ ] CHANGELOG 更新

## 依赖关系

### 外部依赖

- 无新增外部依赖
- 使用现有的 HTTP 客户端
- 使用现有的 JSON 序列化库

### 内部依赖

**轨道 1 依赖**:
- `internal/core/types.go`（现有）
- `internal/gateway/api/handler.go`（现有，集成点）

**轨道 2 依赖**:
- `internal/control/api/routes.go`（现有）
- `internal/cli/i18n.go`（现有）

## 后续扩展

### 可能的扩展方向

1. **更多协议支持**: 添加对其他 AI 模型协议的支持（如 Google PaLM、AWS Bedrock）
2. **协议自动检测**: 自动检测请求格式，无需手动配置
3. **转换规则配置**: 允许用户自定义转换规则
4. **CLI 交互模式**: 提供 REPL 交互式 CLI
5. **CLI 自动补全**: 生成 shell 自动补全脚本

### 不在当前范围

- 图形化管理界面
- 多租户支持
- 分布式部署
