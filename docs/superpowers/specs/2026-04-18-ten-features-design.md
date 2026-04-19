# 10个实用新功能设计方案

## 1. 请求速率限制（Rate Limiting）

### 设计
- **位置**: `internal/gateway/ratelimit/`
- **算法**: 令牌桶（Token Bucket）
- **粒度**: 按 API key 限制
- **集成**: HTTP 中间件，在 handler 之前执行

### 配置
```go
type RateLimitConfig struct {
    Enabled           bool    `yaml:"enabled"`
    RequestsPerSecond float64 `yaml:"requests_per_second"`
    Burst             int     `yaml:"burst"`
}
```

### 实现要点
- 使用 `sync.RWMutex` 保护令牌桶状态
- 每个 API key 一个独立的令牌桶
- 超出限制返回 429 Too Many Requests
- 支持动态配置更新（通过 Snapshot）

---

## 2. 智能模型降级（Model Fallback）

### 设计
- **位置**: `internal/gateway/api/fallback.go`
- **策略**: 配置式降级
- **触发**: 主模型返回可重试错误时

### 配置
```go
type FallbackConfig struct {
    Enabled bool `yaml:"enabled"`
}

// ProviderSnapshot 扩展
type ProviderSnapshot struct {
    // ... 现有字段
    FallbackModels []string `yaml:"fallback_models"`
}
```

### 实现要点
- 在 `handleChatOrMessages` 中集成降级逻辑
- 主模型失败后，按 `FallbackModels` 顺序尝试
- 记录降级事件到遥测系统
- 支持最多 3 级降级

---

## 3. 请求缓存（Request Cache）

### 设计
- **位置**: `internal/gateway/cache/`
- **策略**: 请求体 SHA256 哈希作为 key
- **存储**: 内存 LRU 缓存
- **范围**: 仅缓存非流式响应

### 配置
```go
type CacheConfig struct {
    Enabled   bool `yaml:"enabled"`
    MaxSizeMB int  `yaml:"max_size_mb"`
    TTLSec    int  `yaml:"ttl_sec"`
}
```

### 实现要点
- 使用 `container/list` 实现 LRU
- 缓存 key: SHA256(请求体 + 模型名)
- 仅缓存 200 响应
- 支持 Cache-Control 头

---

## 4. 成本追踪仪表板（Cost Tracking）

### 设计
- **位置**: `internal/telemetry/query/cost.go`
- **集成**: 复用现有遥测系统
- **数据源**: `PricingCatalog` + 使用记录

### 实现要点
- 扩展 `telemetryd` 查询接口
- 按模型、provider、时间聚合成本
- 提供 REST API: `/api/admin/costs`
- 支持按时间段查询

---

## 5. 请求队列系统（Request Queue）

### 设计
- **位置**: `internal/gateway/queue/`
- **策略**: 三级优先级队列（高/中/低）
- **控制**: 信号量限制并发数

### 配置
```go
type QueueConfig struct {
    Enabled         int `yaml:"enabled"`
    MaxConcurrent   int `yaml:"max_concurrent"`
    HighPriorityPct int `yaml:"high_priority_pct"`
}
```

### 实现要点
- 三个 channel 分别对应不同优先级
- 调度器按优先级权重分配请求
- 支持队列长度监控
- 超时请求自动丢弃

---

## 6. API 密钥轮换（Key Rotation）

### 设计
- **位置**: `internal/gateway/api/key_rotation.go`
- **策略**: 轮询 + 故障转移
- **范围**: Provider 级别

### 配置
```go
type KeyRotationConfig struct {
    Enabled bool `yaml:"enabled"`
}

// APIKey 定义
type APIKey struct {
    Name      string `yaml:"name"`
    Value     string `yaml:"value"`
    Disabled  bool   `yaml:"disabled"`
    FailCount int    `yaml:"fail_count"`
}
```

### 实现要点
- 维护密钥状态（可用/故障）
- 轮询选择可用密钥
- 连续失败 3 次标记为故障
- 定期尝试恢复故障密钥

---

## 7. 响应压缩（Response Compression）

### 设计
- **位置**: `internal/infra/httpserver/compression.go`
- **算法**: Gzip + Brotli
- **集成**: HTTP 中间件

### 配置
```go
type CompressionConfig struct {
    Enabled  bool   `yaml:"enabled"`
    MinBytes int    `yaml:"min_bytes"`
    Level    string `yaml:"level"`
}
```

### 实现要点
- 根据 `Accept-Encoding` 自动选择算法
- 仅压缩大于 MinBytes 的响应
- 支持压缩级别配置
- 跳过已压缩的内容

---

## 8. WebSocket 支持（WebSocket Support）

### 设计
- **位置**: `internal/gateway/websocket/`
- **协议**: OpenAI Realtime API
- **集成**: 独立 WebSocket 处理器

### 实现要点
- 使用 `gorilla/websocket` 库
- 支持双向消息转发
- 复用 provider 选择逻辑
- 处理连接生命周期管理

---

## 9. 请求重放（Request Replay）

### 设计
- **位置**: `internal/control/api/replay.go`
- **范围**: 仅失败请求
- **存储**: 复用遥测系统 SQLite

### 实现要点
- 在 telemetryd 中存储失败请求详情
- 在 controld 提供重放 API
- 支持手动触发重放
- 记录重放结果

---

## 10. 健康检查增强（Enhanced Health Check）

### 设计
- **位置**: 扩展 `cmd/gatewayd/health_probe.go`
- **范围**: Provider 状态
- **端点**: `/-/health`

### 实现要点
- 返回每个 provider 的健康状态
- 包括响应时间、错误率
- 支持详细模式 `?detail=true`
- 复用现有健康探针逻辑

---

## 实施顺序

### Phase 1: 基础功能（P0）
1. 请求速率限制
2. 智能模型降级
3. 请求缓存

### Phase 2: 增强功能（P1）
4. 成本追踪仪表板
5. 请求队列系统
6. API 密钥轮换
7. 响应压缩

### Phase 3: 高级功能（P2）
8. WebSocket 支持
9. 请求重放
10. 健康检查增强
