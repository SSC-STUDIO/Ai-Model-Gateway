# Task Plan - AI-Model-Gateway

## 上一轮状态
第16轮（Admin UI UX Enhancement + 全面验证）已完成：
- 状态徽章无障碍图标（::before 伪元素）✓
- 对比度增强（warning/error 更亮颜色 + 更高不透明度）✓
- 刷新间隔显示（Auto-refreshing (30s)）✓
- 全面验证 + 线上部署 ✓

## 本轮任务 - 全景回归验证 + 前端交互视觉审计

### Goal

对 AI-Model-Gateway 进行全景式回归验证，覆盖 Go 后端、Preact 前端、线上服务。执行实际 Playwright 浏览器交互和视觉检查，发现并修复影响使用体验的问题。

### 选择理由

1. 上轮（第16轮）验证距今已有时日，需要确认无回归
2. 本仓库有 web 前端（admin UI），按 Goal 0 要求需做实际交互验证和视觉检查
3. 当前已有 audit-output/ 和 dist-live/ 等目录残留，需要清理状态
4. 需要确认前端测试（当前 109 tests）和新构建无问题

### 验证方案

1. Go 构建 + 测试: `go build ./...` → `go test ./... -count=1`
2. 前端构建 + 测试: `npm --prefix web/admin run build` → `npm --prefix web/admin test`
3. Playwright UI 交互验证: 启动线上服务，浏览器截图关键页面
4. Codex 视觉检查: 使用 sonnet-4-6 检查截图

### 视觉验证点

- Login 页面
- Overview 仪表盘
- Monitoring → Telemetry / Pricing 子视图
- Ops → Audit / Diagnostics 子视图
- Config 编辑器
- Logs 页面

### Agent Team 分工

本任务涉及多个独立验证维度，可以并行执行：
- Agent A: Go 构建/测试
- Agent B: 前端构建/测试 + Playwright UI 审计
- Agent C: 线上服务检查 + 截图 + Codex 视觉验证

### 第17轮状态

| Status | Phase |
| --- | --- |
| Completed | 1. 创建任务计划 + 读取项目状态 |
| Completed | 2. Go 构建/测试基线 — 42+ 包全部通过 |
| Completed | 3. 前端构建/测试 — 13 文件 / 109 测试全部通过；修复 vitest pool 超时 |
| Completed | 4. Playwright UI 审计 + 截图 — 11 视图, 0 console errors, 0 page errors |
| Completed | 5. 视觉检查 — 关键页面排版正常，无遮挡/错位/不可读文字 |
| Completed | 6. 无需修复——零回归 |
| Completed | 7. 更新 planning 文件 + git commit |

---

## 本轮任务（第18轮） - 前端测试覆盖补充 + 全量回归验证

### Goal

补充 admin 前端关键组件的单元测试，提升前端测试覆盖率；同时运行全量回归验证确保无退化。当前前端 30+ 组件中仅 13 个有测试文件，主要缺覆盖的是核心 UI 组件（登录、图表、错误边界、Toast 通知等）和业务 tab 组件。

### 选择理由

1. 前端测试覆盖缺口大：30+ 组件仅 13 个有测试，核心组件（LoginScreen、ErrorBoundary、Toast、Icon）完全无测试
2. 用户体验直接影响的组件（如 ErrorBoundary 错误处理、Toast 反馈、LoginScreen 认证流）若出现回归，用户会直接遇到崩溃或无反馈
3. 前端 tab 组件（OverviewTab、LogsTab 等）缺少测试意味着业务逻辑回归只能靠 Playwright 手动发现
4. 按 shared_prompt Goal 6 "improve repository construction quality" 和 Goal 0 "fewer errors"，测试覆盖是减少用户可见错误的最直接手段

### 验证方案

1. Go 构建 + 测试: `go build ./...` → `go test ./... -count=1`
2. 前端测试: `npx vitest run` — 确认 vitest config 修复有效 + 新增测试通过
3. 前端构建: `npm run build` — 确认无构建回归
4. Playwright 交互验证: 线上服务关键页面截图检查
5. 线上服务: `curl http://127.0.0.1:18080/-/health`

### 状态

| Status | Phase |
| --- | --- |
| In Progress | 1. 更新任务计划 + 读取项目状态 |
| Pending | 2. Go 构建/测试基线 |
| Pending | 3. 前端测试基线（验证 vitest config 修复） |
| Pending | 4. 前端关键组件测试补充 |
| Pending | 5. 前端构建 + 全量测试回归 |
| Pending | 6. Playwright 交互验证 + 视觉检查 |
| Pending | 7. 更新 planning 文件 + git commit |

---

## 2026-05-17 Hotfix - Provider Outbound Rate Limit UI Clarity

### Goal

Make the admin visual config editor show and persist provider-level outbound rate limits, so operators can set upstream RPM limits without confusing them with admin/client ingress rate limits.

### Scope

1. Add provider outbound rate limit state to `configEditor.ts`.
2. Render provider outbound rate limit controls in the visual config editor.
3. Add focused frontend unit coverage for hydrate/build round trips.
4. Validate with frontend tests and a targeted Go/runtime smoke where useful.

### Status

| Status | Phase |
| --- | --- |
| Completed | 1. Confirm live runtime, active DeepSeek upstream, and outbound-only limiter behavior |
| In Progress | 2. Add provider outbound rate limit fields to admin visual editor |
| Pending | 3. Run focused frontend tests |
| Pending | 4. Record result and leave live service stable |

---

## 2026-05-17 Hotfix - Preserve GPT Cached Token Usage

### Goal

Prevent OpenAI/GPT prompt-cache usage from being lost when Chat Completions upstream responses are bridged back to the Responses API. The gateway must preserve upstream cache accounting so Codex/OpenAI-compatible clients can see `usage.input_tokens_details.cached_tokens`.

### Scope

1. Preserve `prompt_tokens_details.cached_tokens` and `input_tokens_details.cached_tokens` in the shared OpenAI usage payload.
2. Emit cached input token details in non-streaming Chat-to-Responses conversions.
3. Emit cached input token details in streaming `response.completed` events.
4. Keep adjacent OpenAI-to-Anthropic streaming usage extraction aligned with both cached-token field shapes.

### Status

| Status | Phase |
| --- | --- |
| Completed | 1. Confirmed cache loss root cause in Chat-to-Responses usage conversion |
| Completed | 2. Implemented shared cached-token extraction and Responses usage builder |
| Completed | 3. Added non-streaming and streaming regression coverage |
| Completed | 4. Ran `go test ./internal/gateway/api -count=1` successfully |
| Pending | 5. Decide whether to deploy the broader dirty worktree to the live gateway bundle |

---

## 2026-05-19 Hotfix - Repair Admin Frontend Test Integration And Restore Standard Live Sync

### Goal

Repair the broken `web/admin` test integration that was blocking `npm run build` and therefore blocking the standard `scripts/sync-bundle-to-home-ai-gateway.sh` live deployment path.

### Scope

1. Fix the newly added component tests so they use the repo's existing `vitest + @testing-library/preact` stack.
2. Include `.test.tsx` files in Vitest so component tests are actually executed instead of only breaking TypeScript builds.
3. Re-run admin frontend build and test validation.
4. Re-run the standard live sync script and verify `ai-gateway.service` stays healthy under strict manifest mode.

### Status

| Status | Phase |
| --- | --- |
| Completed | 1. Identified broken half-migrated test setup (`preact-testing-library`, Jest globals, `.tsx` tests excluded from Vitest) |
| Completed | 2. Rewrote component tests to the existing Vitest + `@testing-library/preact` style |
| Completed | 3. Added `toast.close` locale strings and updated Vitest include/worker settings |
| Completed | 4. Verified `npm run build` and `npm test` (`17` files / `116` tests) |
| Completed | 5. Verified `bash scripts/sync-bundle-to-home-ai-gateway.sh` and healthy live `ai-gateway.service` |
