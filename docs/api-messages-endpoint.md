# `/v1/messages` Anthropic Messages API 端点

## 概述

AI Model Gateway 支持 Anthropic Messages API 格式的请求，允许与原生 Anthropic API 服务商集成。

## 端点

```
POST /v1/messages
```

## 功能

- 支持 Anthropic Messages API 格式
- 自动添加 `anthropic-version: 2023-06-01` header
- 支持流式和非流式响应
- 完全兼容 Anthropic SDK

## 配置

在 `config.yaml` 中配置支持 Anthropic API 的服务商：

```yaml
providers:
  - name: anthropic-provider
    base_url: https://api.anthropic.com
    anthropic_base_url: https://api.anthropic.com  # Anthropic Messages API 端点
    api_key: 'your-api-key'
    models:
      - claude-sonnet-4-6
      - claude-opus-4-7
```

### 配置说明

- `base_url`: OpenAI Chat Completions 格式的 base URL（可选）
- `anthropic_base_url`: Anthropic Messages API 的 base URL
- `api_key`: API 密钥

当设置了 `anthropic_base_url` 时：
- `/v1/messages` 请求会使用此 URL
- 认证使用 `x-api-key` header 而非 `Authorization: Bearer`
- 自动添加 `anthropic-version` header

## 请求示例

### 非流式请求

```bash
curl -X POST http://localhost:18080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-api-key" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ],
    "max_tokens": 1024
  }'
```

### 流式请求

```bash
curl -X POST http://localhost:18080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-api-key" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ],
    "max_tokens": 1024,
    "stream": true
  }'
```

## 与 OpenAI Chat Completions 的区别

| 特性 | `/v1/chat/completions` | `/v1/messages` |
|------|------------------------|----------------|
| API 格式 | OpenAI | Anthropic |
| 认证 Header | `Authorization: Bearer` | `x-api-key` |
| 版本 Header | 无 | `anthropic-version: 2023-06-01` |
| 流式字段 | `stream: true/false` | `stream: true/false` |

## 路由逻辑

1. Gateway 接收 `/v1/messages` 请求
2. 查找配置了 `anthropic_base_url` 的 provider
3. 使用 `anthropic_base_url` + `/v1/messages` 作为上游 URL
4. 添加 `x-api-key` 和 `anthropic-version` headers
5. 透传请求到上游服务

## 错误处理

- 如果 provider 未配置 `anthropic_base_url`，请求会回退到 `base_url`
- URL 验证失败会返回配置错误
- 上游错误会透传给客户端

## 兼容性

- 完全兼容 Anthropic Python SDK
- 完全兼容 Anthropic TypeScript SDK
- 支持所有 Anthropic 模型特性（工具调用、视觉等）
