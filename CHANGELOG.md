# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, with a lightweight structure suitable for a small operational project.

## [Unreleased]

## [1.4.2] - 2026-05-21

### Added

- **In-product update workflow**: Added release check, verified bundle download, dry-run apply, local apply, and rollback surfaces through `aigw update` and the Admin Ops workspace.

### Fixed

- Fixed unresolved Chinese placeholders in the Admin Ops update workspace and added locale regression coverage so placeholder text cannot ship unnoticed.
- Synced embedded Admin UI assets with the rebuilt frontend bundle.

## [1.4.0] — 2026-05-20

### Added

- **Professional admin UI release**: Reworked the admin console toward an operations-tool style with quieter loading states, clearer navigation, tighter workspace grouping, and less decorative motion.
- **Monitoring / Ops / Benchmark workspace consolidation**: Folded legacy pricing, telemetry, audit, probe, and diagnostics routes into clearer Monitoring and Ops workspaces while keeping old links compatible.
- **Admin UI test coverage**: Added focused Vitest coverage for icons, toast behavior, URL canonicalization, config editor handling, formatting helpers, control API normalization, and workspace navigation behavior.
- **Release automation upgrades**: Expanded tag builds to Linux amd64, Linux arm64, Windows amd64, and macOS arm64 bundles with manifest verification, SHA-256 checksums, and GHCR image publishing.
- **Project positioning docs**: Added a public differentiation guide comparing LiteLLM, Portkey, Helicone, OpenRouter, Envoy AI Gateway, Kong, and related Go-native gateways.
- **Updated README screenshots**: Replaced stale admin screenshots with current Overview, Monitoring, Ops mobile, and Benchmark mobile images.
- **`aigw clients print|apply`**：从 `configs/gatewayd.json`（或 `-gateway-url`）解析数据面地址，打印或写入 Codex（`openai_base_url`）、Claude Code（`~/.claude/settings.json` 中 `ANTHROPIC_*`）、OpenClaw（`~/.openclaw/openclaw.json` 中自定义 OpenAI 兼容 provider），便于本机工具一键指向本网关。

### Security

- Hardened WebSocket DNS pinning and origin validation paths.

### Changed

- Repositioned the project as a self-hosted LLM operations gateway rather than a generic model marketplace or decorative dashboard.
- Rewrote the README into a cleaner GitHub landing page with architecture, quick start, CLI examples, operations constraints, and current screenshots.
- Removed generated runtime/test artifacts from tracking and cleaned up invalid Windows-path repository entries.
- Improved repository contributor metadata.

### Fixed

- Fixed admin frontend build/test wiring so `.test.tsx` component tests are included in Vitest and the standard live sync path works again.
- Fixed release manifest tests on Windows.
- Fixed grouped provider health by upstream URL.
- Preserved Responses API cached token usage during compatibility conversion.
- Fixed benchmark status logging, retry loop behavior, and Responses API request conversion.
- Removed duplicate `handler.go` declarations that conflicted with the newer forwarding, streaming, routing, and compatibility modules.
- Added safety coverage around logger concurrency, response forwarding, streaming, retry loop behavior, and Anthropic header forwarding.

## [1.3.0] — 2026-05-01

### Security

- Removed `/admin?token=...` URL-token login and require JSON login via `POST /api/admin/login` to avoid token leakage through browser history and proxy logs.
- Added same-origin checks for cookie-auth write requests while keeping Bearer-token automation flows supported.
- Removed demo credentials from `configs/config.yaml` and replaced them with environment-variable placeholders.

### Fixed

- Fixed admin UI static asset routing for `/icon.svg`, `/favicon.svg`, and `/manifest.json`.
- Fixed SSRF checker duplicate declarations.
- Fixed cache tests that referenced an undefined `entrySize` variable.
- Added daemon JSON config files for service startup.

## [1.2.0] — 2026-04-19

### Added

- **请求速率限制**: 基于令牌桶算法的 API key/IP/模型级限流
  - 可配置的 RPS (每秒请求数) 和突发容量
  - 自动清理过期桶防止内存泄漏
  - 测试覆盖率：100%

- **智能模型降级**: 主模型失败自动切换备用模型
  - 配置式降级链 (FallbackModels)
  - 循环检测防止无限降级
  - 自动遥测事件记录

- **请求缓存**: 内存 LRU 缓存减少上游调用
  - 请求体哈希作为缓存键
  - 可配置的最大条目数和 TTL
  - 缓存命中率统计

- **成本追踪仪表板**: 增强的成本聚合查询
  - 按模型、提供商、时间范围聚合
  - 支持成本预测和预算警告
  - 集成 PricingCatalog 定价数据

- **请求队列系统**: 三级优先级请求调度
  - 高/中/低优先级通道
  - 并发限制和信号量控制
  - 队列状态实时监控

- **API 密钥轮换**: 多密钥支持和故障转移
  - 轮询选择下一个可用密钥
  - 失败计数和自动禁用
  - 冷却期后密钥恢复

- **响应压缩**: Gzip/Brotli 中间件
  - 自动内容编码协商
  - 可配置的压缩级别
  - 响应大小阈值控制

- **WebSocket 支持**: `/v1/realtime` 实时端点
  - 全双工 WebSocket 代理
  - gorilla/websocket 实现
  - 连接池和心跳管理

- **请求重放**: 失败请求查询和手动重放
  - 按时间范围查询失败请求
  - 重放请求到网关
  - 管理员 API 端点

- **健康检查增强**: 扩展的健康状态报告
  - Provider 可用性状态
  - 依赖服务健康检查
  - 详细状态响应

- **协议转换层**: OpenAI ↔ Claude 双向协议转换
  - OpenAI 请求 → Claude 请求转换
  - Claude 响应 → OpenAI 响应转换
  - Claude 请求 → OpenAI 请求转换
  - OpenAI 响应 → Claude 响应转换
  - 流式 SSE 事件转换
  - 工具调用映射（tools、functions）
  - 模型名称映射（gpt-4 ↔ claude-3-opus 等）
  - 测试覆盖率：85.6%

- **独立 CLI 工具**: `gateway-cli` 命令行管理工具
  - `gateway-cli config show` - 显示当前配置
  - `gateway-cli provider list` - 列出所有 providers
  - `gateway-cli provider test <name>` - 测试 provider 连接
  - `gateway-cli telemetry events` - 查询事件日志
  - `gateway-cli publish history` - 查看配置发布历史
  - `gateway-cli publish rollback <revision>` - 回滚配置
  - `gateway-cli test convert` - 测试协议转换
  - `gateway-cli version` - 显示版本
  - 支持 text 和 JSON 输出格式
  - 测试覆盖率：80%+

- **控制面 API 客户端**: 完整的 HTTP 客户端实现
  - 11 个 API 方法覆盖所有控制面端点
  - 20+ 类型定义
  - Bearer token 认证
  - 上下文支持和超时处理

### Changed

- CLI 现在支持 `-format text|json` 输出格式化
- 增强错误消息，提供更好的上下文信息

### Technical Details

- 新增 `internal/gateway/converter` 包用于协议转换
- 增强 `internal/cli/client.go` 完整 API 客户端
- 所有新代码遵循 Go 最佳实践
- 完整测试套件，包含竞态检测

## [1.1.1] — 2026-04-17

### Changed

- **Admin UI 质感全面升级**：骨架屏加载动画、精美空状态、Tab 切换过渡、卡片交错进入动画
- **微交互增强**：按钮按下缩放、Tab 悬停下划线光效、SSE 状态脉冲呼吸灯、Metric 数值 hover 光晕
- **全局滚动条美化**：自定义滚动条，亮色/暗色主题自适应
- **焦点环增强**：输入框聚焦外发光，提升可访问性感知
- **玻璃拟态细节**：Panel 顶部光泽线、Topbar 渐变边框、卡片 hover 边框发光
- **Switch 开关组件**：全新精致的 toggle switch，替换所有原生 checkbox
- **全局 Toast 通知系统**：右上角堆叠式通知，支持 4 种类型，带图标和滑入动画
- **数值更新闪光**：Overview/Telemetry metric 数值变化时触发高亮闪光
- **折线图发光滤镜**：SVG filter 让折线带柔和光晕
- **背景浮动光斑**：3 个缓慢漂移的径向渐变光球
- **按钮 Loading Spinner**：busy 状态显示旋转动画替代文字变化
- **表单输入框图标**：ProbeTab 关键输入框添加前缀图标（🏷️🔗🔒🤖等）
- **复制到剪贴板**：Probe 结果区和 Settings JSON 编辑器添加一键复制按钮
- **diff 行交错进入动画**：历史 diff 每行依次淡入
- **表格行 hover 微移**：悬停时轻微右移增强交互感

### Fixed

- 暗色主题下空状态图标和背景渐变的适配

## [1.1.0] — 2026-04-07

### Added

- **Multi-Token RBAC**: 支持多令牌认证与基于角色的访问控制
- **审计日志**: 完整的管理操作审计追踪
- **实时 SSE 监控**: 通过 Server-Sent Events 实时推送运行状态变更
- **速率限制**: 可配置的请求速率限制策略
- **响应式 Admin UI**: 全面适配桌面与移动端，支持触控操作
- **Admin UI 多语言**: 支持英文 (en) 与中文 (zh) 界面切换
- **Admin UI 主题**: 支持亮色/暗色主题切换，跟随系统偏好
- **Favicon 与 PWA**: 添加 favicon、manifest、tab icons
- **后端 i18n**: 后端国际化包，支持 7 种语言
- **CLI i18n**: 命令行界面国际化，支持 7 种语言

### Changed

- **v2 架构重构**: 清晰分层 — core（领域）、app（应用）、infra（基础设施）
- **简化运行时**: 移除 v1 兼容层，直接加载 v2 配置
- **前端构建优化**: Vite build target 升级到 es2020，vendor chunk 分离（preact），显式 CSS 压缩
- **性能优化**: 实现 buffer pool、优化 telemetry store、集成 json-iterator、调优 HTTP 连接池
- **项目布局**: `internal/` 重组为 core/app/infra/adminapi 模块化结构，移除旧版 cli/server/v2 目录
- Bumped VERSION from 1.0.0 to 1.1.0

### Fixed

- Admin UI 在小屏设备上的布局溢出问题
- 主题切换时的闪烁问题（通过 inline script 预加载主题）

## [1.0.0] — 2026-04-01

### Added

- **CLI Support**: Comprehensive command-line interface for managing the gateway
  - `gateway start` - Start the gateway server
  - `gateway validate` - Validate configuration without starting
  - `gateway health` - Check gateway health status
  - `gateway install` / `uninstall` - Windows service management
  - `gateway service-start` / `service-stop` / `service-status` - Service control
  - `gateway version` - Display version information
- **Test Coverage Improvements**: Achieved 80%+ coverage for critical packages
  - `internal/cli`: Comprehensive test suite with mocks
  - `internal/config`: Extended config validation and save tests
  - `internal/router`: Health, manager, and strategy tests
- **Windows Service Management**: Native Windows service integration via CLI commands
- **Configuration Management**: Runtime config reload and export via CLI

### Changed

- **Architecture Refactoring**: Cleaner separation between CLI, app, and service layers
  - `internal/cli` - Command handling and execution
  - `internal/app` - Application lifecycle management  
  - `internal/service` - Windows service integration
- **Entry Point**: `cmd/gateway/main.go` now supports both legacy mode and new CLI mode
- Bumped VERSION from 0.2.3 to 1.0.0

### Documentation

- Added comprehensive CLI documentation in [docs/cli.md](docs/cli.md)
- Added installation guide in [docs/installation.md](docs/installation.md)
- Updated README.md with CLI examples, installation instructions, and test coverage info

## [0.2.3] — 2026-03-30

### Added

- Config and telemetry regression coverage for atomic config saves and pricing catalog hot-reload cache updates.

### Changed

- Anthropic/OpenAI compatibility bridges now preserve tool calls, tool results, multimodal image content, and stream model rewrites across chat, messages, and responses flows.

### Fixed

- Pricing catalog config reloads now refresh the in-memory pricing cache immediately after config watcher updates.
- Router strategy normalization and validation now reject unsupported strategy values while still defaulting empty values consistently.
- Config saves now write atomically via temp-file rename to avoid partial writes during updates.
- Admin config view/apply now round-trips `admin.enabled`, and admin language validation errors now list the full supported set (`zh`, `en`, `ja`, `ko`, `es`, `fr`, `de`), with targeted regression coverage for both behaviors.

## [0.2.1] — 2026-03-22

### Added

- Settings page section nav links (Health, Bridge, Router, Intercepts, Providers, History) added to topbar, matching overview page navigation style.
- Config package test coverage: `config_test.go` with tests for Normalize defaults, NormalizeAdminLanguage, RewriteModel, Upstream.IsEnabled, NormalizeUpstreamClass.
- SQL identifier validation (`validSQLIdentifier` regex guard) in telemetry store before any `fmt.Sprintf` with table/column names.
- `rows.Err()` checks after all 7 `for rows.Next()` loops in telemetry store.

### Changed

- Settings page nav redesigned: jumpbar changed from 3x2 grid cards to horizontal pill tabs matching topbar style.
- Settings rail (history panel) now scrollable with `max-height: calc(100vh - 100px)` to prevent exceeding left column height.
- Settings shell `align-items: stretch` for provider card bottom alignment.
- Frontend performance: `setHTML()` helper skips unchanged DOM updates (~22 elements per 5s cycle).
- Frontend performance: unified polling — `loadCharts()` piggybacks on every 3rd `load()` cycle instead of independent `setInterval`.
- Frontend performance: forced reflows replaced with double-`requestAnimationFrame`.
- Frontend performance: sparkline IDs stabilized (deterministic `sp-m-{idx}` instead of `Math.random()`).
- Upstream disable poll interval: 100ms → 1s (10x reduction in goroutine overhead).
- Frontend style consistency: unified green RGB values, normalized chip padding (two tiers), semantic state alphas (border 0.30/bg 0.08), background alpha 0.04, hover patterns with translateY(-1px), transition timings, media query ordering.
- Chart danger color `#f87171` → `#ff7f6e` (matches `--danger` CSS variable).
- Validation-warning amber corrected from `rgba(240,179,90)` to `rgba(241,184,102)`.
- 9px font-size normalized to 10px.
- Removed 3 no-op compact-card CSS overrides and unused `.hero-side` CSS/HTML/Go code.
- Bumped VERSION from 0.2.0 to 0.2.1.

### Fixed

- Security: auth token comparison uses `crypto/subtle.ConstantTimeCompare` (prevents timing attacks on all 3 auth paths).
- Security: admin config PUT endpoint capped at 10MB via `http.MaxBytesReader`.
- Config watcher: fsnotify errors and reload failures now logged (were silently discarded).
- Config `Normalize()` no longer forces `admin.enabled=true` — respects user setting.
- Telemetry `flushWriter`: `select` with 5s timeout prevents deadlock when channel full.
- Router `PickSticky`: refactored to `defer m.mu.Unlock()` via helper, eliminating fragile manual unlock across return paths.
- Admin data endpoint: `sync.Cond.Wait()` now has 3s deadline, serves stale data on timeout instead of blocking forever.
- Server shutdown errors now logged instead of discarded.
- `.detail-toggle` added to focus-visible selector list.

## [0.2.0] — 2026-03-22

### Added

- Anthropic Messages API support: `/v1/messages` and `/v1/messages/count_tokens` proxy endpoints with native `x-api-key` auth, sticky routing, and stream completion detection.
- Full Chinese/English/Japanese/Korean/Spanish/French/German i18n for admin dashboard with runtime language selector.
- Sparkline trend charts inside performance metric cards (RPM, latency, tokens).
- Success rate donut chart in hero priority grid.
- Model token distribution horizontal bar chart in economics section.
- Upstream token distribution horizontal bar chart in usage section.
- Cache hit rate trend line added to latency chart as 3rd series.
- Request table row coloring: red for 5xx, amber for 4xx, green left border for 2xx.
- Collapsible detail tables (model economics, upstream usage, cache ranking, requests) with smooth expand/collapse animation.
- Page load card entrance animations with staggered fadeSlideUp.
- Hero title shimmer gradient effect.
- Section expand/collapse sectionReveal animation.
- Chart SVG fade-in on render.
- Data refresh value-refresh opacity animation on metrics.
- Hero priority glow pulse on healthy cards.
- Print stylesheet for white-background printing.
- Firefox scrollbar support (`scrollbar-width`/`scrollbar-color`).
- `prefers-reduced-motion` media query disables all animations.
- Focus-visible outline rings on all interactive elements.
- Aria labels on nav, asides, inputs, selects.
- Aria-expanded on section/provider toggle buttons.
- Chart SVG `role=img` and `aria-hidden=true`.

### Changed

- Admin overview layout: performance/runtime 8/4→7/5 split, usage/cache 8/4→7/5 split.
- Card padding 12px→14px for primary cards, compact-card top padding aligned to 14px.
- Chart height 190→210px, compact chart 160→210px, y-axis padding 52→44px.
- Error feed max-height raised from 300px to 500px cap.
- Responsive breakpoints: merged 920/900px into single 920px, settings rail 1380→1100px.
- Settings page redesigned: hero hidden, nav changed to horizontal pill tabs, controls split into Filters + Actions panels, rail narrowed 320→260px.
- Settings jumpbar: card-style links replaced with compact horizontal pills matching topnav style.
- Removed redundant surface strips from economics and usage sections (replaced by horizontal bar charts).
- Upstream tile and error item border-radius unified to 16px.
- Table-shell border-radius 16→14px, table padding improved with first/last-child insets.
- Section-head align-items changed from end to flex-start.
- Policy-card and mode-preset transitions normalized to 160ms.
- Topnav/jumpbar transitions extended with box-shadow for smooth active state.
- Success/failure chart colors: failure changed from orange to red (#f87171).
- Stacked bar chart `drawStackedBarChart` accepts optional `customColors` parameter.
- `table()` helper accepts optional `rowClasses` parameter for row-level styling.
- Telemetry store: in-memory counters, async write batching, snapshot caching with 2s TTL.
- Router manager: shared health HTTP client, cursor-based weighted round-robin, sticky session pruning.
- Admin data endpoint: sync.Cond single-flight thundering herd protection.
- Bumped VERSION from 0.1.2 to 0.2.0.

### Fixed

- Anthropic Messages API token usage: compute `total_tokens` from `input_tokens + output_tokens` when absent, read `cache_read_input_tokens`, parse `message.usage` from streaming `message_start` events.
- SSE usage extraction: accumulate max values across all events instead of returning first match (fixes Anthropic streaming output_tokens=0).
- XSS: escape `pricing.source_url` in href, escape `pricing.input_per_1m_usd`/`output_per_1m_usd` in priceLine and economics table.
- Format string injection: `fmt.Errorf(reason)` → `fmt.Errorf("%s", reason)` at 8 call sites.
- Client `X-Api-Key` header no longer forwarded to non-Anthropic upstreams.
- Request body size limited to 100MB via `io.LimitReader`.
- API key inputs changed from `type=text` to `type=password`.
- Provider-summary-strip CSS: added 8th column for Probe (was 7 cols, 8 items rendered).
- Table-cache min-width reduced from 580px to 420px (fits span-5 container).
- Inline styles replaced with CSS classes (`config-field-wide`, `diff-preview-card`).
- Sparkline flicker: re-render immediately after `load()` updates metrics innerHTML.
- Card bottom alignment: `align-items: stretch` with flex content fill.
- Config-card-head sticky top: 10→86px (clears topbar properly).
- Config-section scroll-margin-top: 18→88px.
- Jumpbar link height consistency: `white-space: nowrap` on strong text.
- `markInvalid` now sets `aria-expanded=true` when auto-expanding collapsed cards.
- `load()` wrapped in try/catch, null guard on `data.telemetry`.
- Degraded/warn upstream tiles preserve status color on hover.
- Duplicate `loaded` key removed from Japanese i18n locale.
- Removed dead code: 4 JS functions, 3 CSS rules, 30 Go template replacer entries (~100 lines net reduction).

## [0.1.2] — 2026-03-15

### Added

- OpenAI-compatible gateway endpoints for chat, responses, embeddings, files, audio, images, models, health, and admin APIs.
- Admin dashboard with throughput, latency, token usage, success/failure, cache hit, upstream health, pricing, and recent request/error views.
- Runtime config editing for health checks, bridge rules, retry policy, response intercepts, upstream providers, config export, history, diff preview, and rollback.
- SQLite-backed telemetry persistence and pricing cache persistence.
- Claude compatibility fallback from `Responses API` to `chat/completions` when the upstream does not implement responses natively.
- Sticky session routing for Responses API to improve prompt cache hit rate.
- Public documentation assets: project icon, admin screenshot, rewritten README, LICENSE, and CI workflow.

### Changed

- Admin UI layout was tightened for higher information density and better readability.
- Runtime settings were moved off the overview page to a dedicated `/admin/settings` view.
- Pricing aggregation now merges duplicate display-model rows and preserves bridge-pricing attribution.
- Hot paths were optimized with shared HTTP transport reuse, cached admin data payloads, and SQLite WAL/busy-timeout tuning.

### Fixed

- `/v1/responses/compact` route handling now avoids misrouting into `responses/{response_id}` semantics.
- SSE completion detection now distinguishes between Responses streams and Chat Completions streams.
- Claude requests sent through `Responses API` now work against chat-only upstreams via compatibility conversion.
- Prompt cache accounting is surfaced in pricing and admin aggregations.
