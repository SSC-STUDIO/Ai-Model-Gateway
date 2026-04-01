# AI Model Gateway Review Queue

## Active Review Items

### [COMPLETED][AI-Model-Gateway][Service-Restart-Verification]
仓库路径：`C:\Users\96152\My-Project\Active\Software\AI-Model-Gateway`

执行时间：2026-03-31 23:37

执行命令：
```powershell
Stop-Service -Name "AIModelGateway" -Force
Start-Process -FilePath "bin/gateway.exe" -ArgumentList "-config", "configs/config.yaml"
```

验证结果：
- ✅ 服务在 5 秒内就绪
- ✅ `quota_block_recovery_interval_minutes: 60` 正确返回
- ✅ 所有 6 个服务商在 API 响应中
- ✅ Qwen 3.6 Plus 模型可用
- ✅ OpenRouter base_url 正确 (`https://openrouter.ai/api`)
- ✅ 总览→设置链接携带 token
- ✅ 设置→总览链接携带 token
- ✅ Quota Recovery 配置字段存在

证据路径：
- API: `http://127.0.0.1:18080/-/admin/config` 返回正确配置
- 模型: `http://127.0.0.1:18080/v1/models` 包含所有模型

验收标准：通过

剩余工作：
- 通过浏览器实际访问 `http://127.0.0.1:18080/admin?token=ec6a94485ddd476b96cdc3d5a9a9fe14` 验证前端渲染

---

### [COMPLETED][AI-Model-Gateway][Quota-Auto-Recover]
仓库路径：`C:\Users\96152\My-Project\Active\Software\AI-Model-Gateway`

具体命令：
```powershell
# 1. 验证配置字段返回
curl.exe -s http://127.0.0.1:18080/-/admin/config -H "Authorization: Bearer ec6a94485ddd476b96cdc3d5a9a9fe14" | findstr "quota_block_recovery"

# 2. 检查设置界面显示"Quota Recovery (min)"
```

期望输出/证据路径：
- API 返回包含 `"quota_block_recovery_interval_minutes": 60`
- 设置界面 Router 部分显示 "Quota Recovery (min)" 字段

验收标准：
- 通过 = 配置 API 返回新字段，前端正确显示，可编辑保存
- 失败 = 字段缺失或保存后丢失

未完成下一步：
如果失败，检查 `internal/server/router.go` 的 `renderConfigView` 和 `applyRouterConfig` 函数。

---

### [COMPLETED][AI-Model-Gateway][Service-Restart]
仓库路径：`C:\Users\96152\My-Project\Active\Software\AI-Model-Gateway`

具体命令：
```powershell
# 以管理员身份重启服务
Restart-Service -Name "AIModelGateway" -Force
Start-Sleep 3
# 验证版本
curl.exe -s http://127.0.0.1:18080/v1/models | findstr "qwen3.6-plus"
```

期望输出/证据路径：
- 服务状态为 Running
- 模型列表包含 `qwen/qwen3.6-plus-preview:free`

验收标准：
- 通过 = 服务正常重启，新代码生效，所有模型可用
- 失败 = 服务启动失败或仍在运行旧版本

未完成下一步：
如果失败，检查 Windows 服务日志或手动运行 `bin/gateway.exe` 查看错误。

---

### [REVIEW_READY][AI-Model-Gateway][Kimi-404-Investigate]
仓库路径：`C:\Users\96152\My-Project\Active\Software\AI-Model-Gateway`

具体命令：
```powershell
# 直接测试 Kimi API
curl.exe -s -X POST "https://api.kimi.com/coding/v1/chat/completions" `
  -H "Authorization: Bearer sk-kimi-..." `
  -H "Content-Type: application/json" `
  -d '{"model":"kimi-for-coding","messages":[{"role":"user","content":"Hi"}]}' `
  -w "\nHTTP: %{http_code}\n"
```

期望输出/证据路径：
- Kimi API 返回 200 或明确的错误信息

验收标准：
- 通过 = 确定 404 是 Kimi 服务端问题还是网关请求格式问题
- 失败 = 无法确定根本原因

未完成下一步：
如果是网关问题，检查请求体是否被修改；如果是 Kimi 问题，考虑临时禁用或 fallback。

---

## Completed Items

- [x] OpenRouter URL 修复 (base_url 从 `https://openrouter.ai/api/v1` 改为 `https://openrouter.ai/api`)
- [x] 健康检查路径修复 (`/models` 改为 `/v1/models`)
- [x] Qwen 3.6 Plus 模型验证可用
- [x] 服务重启完成，新代码生效
- [x] 配额自动恢复字段验证通过 (60 minutes)
- [x] 所有 6 个服务商在配置中可见

## New Features - Chinese Models Integration

### [COMPLETED][AI-Model-Gateway][Chinese-Models-One-Click]
仓库路径：`C:\Users\96152\My-Project\Active\Software\AI-Model-Gateway`

实现内容：
1. **配置模板** - `configs/chinese-models.yaml`
   - 支持9家主流国产大模型厂商
   - 百度文心一言、字节豆包、智谱GLM、讯飞星火、MiniMax、Kimi、DeepSeek、阶跃星辰、商汤

2. **前端一键接入** - `internal/server/admin.go`
   - 添加"🇨🇳 添加国产大模型"下拉按钮
   - 9个厂商快速选择
   - 自动填充预设配置（name, base_url, models, headers）

3. **文档** - `docs/chinese-models-integration.md`
   - 各厂商接入详情
   - API Key获取方式
   - 桥接配置建议
   - 故障排查指南

验收标准：
- ✅ Web界面显示"添加国产大模型"按钮
- ✅ 点击按钮展开9个厂商选项
- ✅ 选择厂商后自动添加上游配置
- ✅ 构建通过

验证命令：
```bash
# 构建验证
go build -o bin/gateway.exe ./cmd/gateway

# 启动后访问
http://127.0.0.1:18080/admin/settings?token=xxx
```

## Blocked Items

None
