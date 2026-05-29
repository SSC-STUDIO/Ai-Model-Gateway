# AI Model Gateway Promotion Kit

Use this document as copy-ready launch material for GitHub, Chinese developer communities, and English developer communities.

Current links:

- GitHub: `https://github.com/SSC-STUDIO/Ai-Model-Gateway`
- Latest release: `https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/tag/v1.4.4`
- Docs: `https://github.com/SSC-STUDIO/Ai-Model-Gateway/tree/main/docs`
- Installation guide: `docs/installation.md`
- Deployment guide: `docs/deployment.md`
- Troubleshooting guide: `docs/troubleshooting.md`
- Config publish and rollback: `docs/config-publish-rollback.md`
- Provider fallback and health operations: `docs/provider-fallback-health.md`
- Self-hosted LLM gateway checklist: `docs/self-hosted-llm-gateway-checklist.md`
- LLM gateway comparison guide: `docs/llm-gateway-comparison.md`
- Maintainer discussion: `https://github.com/SSC-STUDIO/Ai-Model-Gateway/discussions/25`
- 100-star campaign plan: `docs/100-star-campaign.md`

Recommended images:

- `docs/assets/admin-overview.png`
- `docs/assets/admin-monitoring.png`
- `docs/assets/admin-ops-mobile.png`
- `docs/assets/admin-benchmark-mobile.png`

Generated videos:

- `output/promotion/ai-model-gateway-upstream-zh-16x9.mp4`
- `output/promotion/ai-model-gateway-upstream-zh-9x16.mp4`
- `output/promotion/ai-model-gateway-upstream-en-16x9.mp4`
- `output/promotion/ai-model-gateway-bilibili-1080p.mp4`
- `output/promotion/ai-model-gateway-bilibili-cover.jpg`

Published v1.4.1 release video URLs:

- Chinese 16:9: `https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/download/v1.4.1/ai-model-gateway-upstream-zh-16x9.mp4`
- Chinese 9:16: `https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/download/v1.4.1/ai-model-gateway-upstream-zh-9x16.mp4`
- English 16:9: `https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/download/v1.4.1/ai-model-gateway-upstream-en-16x9.mp4`

Publication log, 2026-05-20:

- GitHub Release: published and attached the three promo videos at `https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/tag/v1.4.1`
- Zhihu: published at `https://zhuanlan.zhihu.com/p/2040580684746581877`
- Juejin: not published because the editor returned `must bind phone`.
- X, Reddit, V2EX, Hacker News: not published because the current network timed out.
- LinkedIn: not published because browser navigation returned an HTTP response failure.
- Bilibili: published at `https://www.bilibili.com/video/BV134Lb6GEbV/` on 2026-05-21. The upload manager showed the item under `已通过`.

Publication log, 2026-05-21:

- Bilibili: published the optimized 1080p Chinese video at `https://www.bilibili.com/video/BV134Lb6GEbV/`.
- Bilibili assets: `output/promotion/ai-model-gateway-bilibili-1080p.mp4`, `output/promotion/ai-model-gateway-bilibili-cover.jpg`.
- Bilibili title: `上游大模型不稳定，业务怎么扛住？AI Model Gateway 开源实战`.
- Bilibili category: `人工智能`.
- Bilibili tags: `AI`, `大模型`, `LLM`, `开源项目`, `运维`, `网关`.
- Bilibili disclosure: selected `含AI生成内容` and stated that the narration uses AI voice in the description.

Publication log, 2026-05-29:

- GitHub Release `v1.4.4`: updated notes with direct links to README, installation, deployment, troubleshooting, use cases, self-hosted gateway checklist, comparison guide, docs index, and Discussion #25.
- GitHub Discussion #25: added a maintenance update linking the high-intent install/deploy/troubleshooting/comparison docs and confirming PRs/issues remain clean.

## Core Message

AI Model Gateway is a self-hosted LLM operations gateway. It is built for teams that want model routing, config publishing, telemetry, benchmarks, diagnostics, and update/rollback workflows in one local runtime.

It is not a hosted model marketplace and not just a pretty dashboard. The product direction is local control: keep provider keys, routing policy, telemetry, and operational decisions inside your own environment.

## Short Chinese Pitch

我做了一个自托管 LLM 运维网关：AI Model Gateway。

它不是模型市场，也不是只看图表的 dashboard，而是偏生产运维工具：

- OpenAI / Anthropic / Responses API 兼容入口
- 多 provider 路由、fallback、限流、缓存
- 配置发布、diff、审计、回滚
- 请求日志、成本、延迟和 provider 健康监控
- 内置模型 Benchmark，用结果辅助路由决策
- Admin UI 里支持检查更新、下载验证 bundle、dry-run、应用和回滚

核心目标是：把 LLM 网关从“能转发请求”推进到“能稳定运维”。

GitHub：https://github.com/SSC-STUDIO/Ai-Model-Gateway

## GitHub Release Notes

### AI Model Gateway v1.4.4

This release focuses on making AI Model Gateway feel more like a practical operations tool for self-hosted LLM infrastructure.

Highlights:

- Professional Admin UI polish: quieter loading states, clearer workspaces, reduced visual noise, and more operations-focused layout.
- Stronger Operations workspace: runtime status, provider probes, audit, diagnostics, replay, and now update workflows live in one place.
- In-product update workflow: check the latest release, download a verified platform bundle, dry-run apply, apply locally, and roll back from Admin or CLI.
- Model benchmarking improvements: compare upstream behavior with exact, judge, JSON, tool, and stream scoring modes.
- Safer local lifecycle: manifest-verified bundles, config publish history, rollback, and audit logging.

AI Model Gateway is for teams that want local control over provider keys, routing policy, telemetry, and day-2 operations instead of handing everything to a hosted broker.

Try it: https://github.com/SSC-STUDIO/Ai-Model-Gateway

Start links:

- Installation: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/installation.md
- Deployment: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/deployment.md
- Troubleshooting: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/troubleshooting.md
- Config publish and rollback: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/config-publish-rollback.md
- Provider fallback and health: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/provider-fallback-health.md
- Comparison guide: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/llm-gateway-comparison.md

## X / Twitter

### English

AI Model Gateway v1.4.4 is out.

A self-hosted LLM operations gateway for teams that want routing, telemetry, benchmarks, config publishing, diagnostics, and update/rollback workflows in one local runtime.

Not a hosted model marketplace. Not just a dashboard.

https://github.com/SSC-STUDIO/Ai-Model-Gateway

### Provider Fallback / Health Thread

Upstream LLM providers fail in boring but expensive ways: 429s, timeouts, 5xx bursts, quota exhaustion, and slow model endpoints.

I added a focused operations guide for AI Model Gateway covering health-aware weighted routing, provider probes, cooldown state, runtime status, config publish, and rollback.

It is written for teams evaluating self-hosted LLM routing where provider keys, policy, telemetry, and incident response stay inside their own environment.

Guide: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/provider-fallback-health.md
Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

### Chinese

AI Model Gateway v1.4.4 更新了。

一个自托管 LLM 运维网关：路由、监控、Benchmark、配置发布、诊断、更新和回滚都在本地控制面完成。

不是模型市场，也不是装饰型 dashboard，目标是把 LLM 网关做成能长期运维的工具。

https://github.com/SSC-STUDIO/Ai-Model-Gateway

### Provider Fallback / Health 中文短帖

上游大模型 provider 不稳定时，团队真正需要看的不是“有没有转发出去”，而是：

- 哪个 provider 在失败或冷却
- 429、408、5xx、超时怎么影响路由
- probe 能不能绕过正常 fallback 单独验证一条上游
- 配置变更如何 preview / diff / publish / rollback

我给 AI Model Gateway 补了一份 provider fallback 和 health 运维指南，面向自托管 LLM 网关评估场景。

指南：https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/provider-fallback-health.md
项目：https://github.com/SSC-STUDIO/Ai-Model-Gateway

## Hacker News

Title:

Show HN: AI Model Gateway, a self-hosted LLM operations gateway

Post:

I built AI Model Gateway, a self-hosted LLM gateway focused on day-2 operations rather than hosted model brokerage.

The project combines OpenAI/Anthropic/Responses-compatible entry points with provider routing, config publishing, telemetry, benchmarks, diagnostics, audit logs, and an Admin UI. The runtime is split into data, control, and telemetry planes, all supervised through a compact Go entry point.

The latest work adds a safer in-product update workflow: check GitHub releases, download the matching platform bundle, verify it, dry-run apply, apply locally, and roll back.

I am trying to make it useful for teams that want to keep provider keys, routing policy, telemetry, and operational controls inside their own environment.

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

## Reddit

Suggested subreddits: `r/selfhosted`, `r/LocalLLaMA`, `r/golang`, `r/opensource`, `r/LLMDevs`.

Title:

I built a self-hosted LLM operations gateway with routing, telemetry, benchmarks, and rollback

Post:

I have been working on AI Model Gateway, a self-hosted LLM gateway written in Go.

The focus is not provider count or marketplace UX. It is more operational:

- OpenAI Chat Completions, Anthropic Messages, and OpenAI Responses compatibility
- provider routing, fallback, rate limiting, and request cache
- local config publishing with preview, diff, audit, and rollback
- telemetry for traffic, latency, cost, model usage, and provider health
- built-in benchmark workspace for comparing model behavior
- Admin UI for diagnostics, probes, logs, replay, and updates
- manifest-verified bundles and in-product update/rollback flow

The use case is running an internal LLM gateway where keys, telemetry, and routing policy stay under your control.

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

I would appreciate feedback from people running self-hosted or team-internal LLM infrastructure.

Start here if you want to evaluate it:

- Install: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/installation.md
- Deploy: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/deployment.md
- Config publish and rollback: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/config-publish-rollback.md
- Provider fallback and health: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/provider-fallback-health.md
- Compare options: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/llm-gateway-comparison.md

## V2EX

标题：

分享一个自托管 LLM 运维网关：AI Model Gateway

正文：

最近在打磨一个项目：AI Model Gateway。

它的定位不是“再做一个模型市场”，也不是“只展示漂亮图表的面板”，而是一个可以长期运维的自托管 LLM 网关。

目前主要能力：

- OpenAI Chat Completions / Anthropic Messages / OpenAI Responses 兼容入口
- 多 provider 路由、fallback、限流、请求缓存
- 配置发布流程：预览、diff、validate、publish、audit、rollback
- Telemetry：请求量、延迟、成本、模型使用、provider 健康
- Logs：请求搜索、错误过滤、CSV 导出
- Benchmark：按 exact/judge/JSON/tool/stream 等方式比较模型表现
- Ops：provider probe、diagnostics、audit、replay
- 新增内置更新：检查 release、下载验证 bundle、dry-run、应用、回滚

我想解决的问题是：很多 LLM proxy 能把请求转出去，但真正上线后需要的是 provider 健康、配置变更记录、回滚、审计、成本和 benchmark 这些运维闭环。

GitHub：https://github.com/SSC-STUDIO/Ai-Model-Gateway

欢迎大家提建议，尤其是已经在自托管 LLM gateway / proxy 的场景里踩过坑的朋友。

快速看项目：

- 安装：https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/installation.md
- 部署：https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/deployment.md
- 排障：https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/troubleshooting.md
- 配置发布与回滚：https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/config-publish-rollback.md
- Provider fallback 与健康检查：https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/provider-fallback-health.md
- 对比：https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/llm-gateway-comparison.md

## 知乎 / 掘金 Long Post

标题：

我为什么做一个自托管 LLM 运维网关，而不是另一个模型聚合平台

正文：

过去一年，LLM API 的接入方式越来越多：OpenAI 兼容接口、Claude Messages、Responses API、各种国内外 provider、企业内网模型，以及越来越多的代理和聚合产品。

但真正落到团队内部使用时，问题往往不是“能不能调用模型”，而是：

- provider 挂了之后怎么切？
- 配置改坏了怎么回滚？
- 哪一次发布引入了问题？
- 请求量、成本、延迟怎么追踪？
- 某个模型到底适不适合进入路由？
- 网关本身怎么升级，升级失败怎么恢复？

所以我做了 AI Model Gateway。它的目标不是模型市场，而是一个自托管 LLM operations gateway。

核心设计有几个取舍：

第一，控制面留在本地。provider key、路由策略、telemetry、审计和配置发布记录不需要交给 hosted broker。

第二，运行时拆成 data/control/telemetry 三个平面。`gatewayd` 负责推理流量，`controld` 负责配置、Admin API、审计、诊断和 benchmark，`telemetryd` 负责事件摄取和聚合。

第三，配置不是直接改完就生效，而是走 authoring config -> compiled snapshot -> publish ledger 的流程。这样可以做 preview、diff、validate、publish、audit 和 rollback。

第四，Admin UI 不是营销 dashboard，而是运维控制台。首屏关注健康状态、关键指标和可执行动作：probe、diagnose、replay、publish、rollback、export。

第五，Benchmark 不只做展示，而是为了辅助路由决策。它支持 exact、judge、JSON、tool、stream 等评分方式，把正确性、延迟和成本放在一起看。

v1.4.x 这一轮又补了一条运维闭环：内置更新能力。现在可以从 Admin 或 CLI 检查 GitHub release，下载当前平台的 bundle，验证 manifest，dry-run apply，应用更新，并保留本地回滚路径。

如果你在维护团队内部的 LLM proxy/gateway，希望把它从“能转发请求”变成“能稳定运维”，欢迎试试：

https://github.com/SSC-STUDIO/Ai-Model-Gateway

## LinkedIn

I have been working on AI Model Gateway, a self-hosted LLM operations gateway.

The project is built for teams that want to keep model routing, provider keys, telemetry, config publishing, benchmarks, diagnostics, and update/rollback workflows inside their own environment.

The latest release improves the Admin UI and adds an in-product update workflow:

- check the latest GitHub release
- download the matching platform bundle
- verify the bundle before applying
- dry-run updates
- apply locally
- roll back the last update

The goal is to move beyond "LLM proxy that forwards requests" toward an operational control plane for production LLM usage.

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

Evaluation links:

- Installation: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/installation.md
- Deployment: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/deployment.md
- Troubleshooting: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/troubleshooting.md
- Config publish and rollback: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/config-publish-rollback.md
- Comparison guide: https://github.com/SSC-STUDIO/Ai-Model-Gateway/blob/main/docs/llm-gateway-comparison.md

## Product Hunt

Name:

AI Model Gateway

Tagline:

Self-hosted LLM operations gateway for routing, telemetry, benchmarks, and rollback.

Description:

AI Model Gateway is a compact self-hosted LLM gateway for teams that want local control over provider routing, keys, telemetry, config publishing, diagnostics, benchmarking, and update/rollback workflows. It supports OpenAI, Anthropic, and Responses-compatible entry points, with an Admin UI built as an operations console rather than a marketing dashboard.

## Bilibili / Short Video Script

开场：

“很多 LLM proxy 只能解决一个问题：把请求转出去。但真正上线以后，你还要知道 provider 健不健康、成本多少、配置谁改了、改坏了怎么回滚、哪个模型适合进路由。”

中段：

“这是 AI Model Gateway，一个自托管 LLM 运维网关。它支持 OpenAI、Anthropic、Responses 兼容入口，有多 provider 路由、限流、缓存、请求日志、成本和延迟监控，还有内置 Benchmark。”

演示顺序：

1. 打开 Overview，看网关健康状态。
2. 进入 Monitoring，看请求量、延迟和成本。
3. 进入 Benchmark，对比模型输出质量。
4. 进入 Ops，看 provider probe、diagnostics、audit。
5. 进入 Updates，演示检查更新、下载验证、dry-run、回滚入口。

结尾：

“如果你想把团队内部 LLM 网关从‘能用’推进到‘能运维’，可以看看这个项目。GitHub 链接放在简介。”

## Pinned GitHub Issue / Discussion

Title:

What should AI Model Gateway improve next for real-world LLM operations?

Body:

AI Model Gateway is currently focused on self-hosted LLM operations: routing, config publishing, telemetry, benchmarks, diagnostics, audit, and local update/rollback workflows.

I would like feedback from people running internal LLM gateways or proxy layers:

- What provider failure modes do you hit most often?
- What config rollback workflow do you need?
- Which telemetry views are missing?
- What benchmark signals would you trust before routing traffic to a model?
- How should update/restart workflows behave in production?

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

## Posting Checklist

- Use the Overview screenshot for broad posts.
- Use Monitoring when talking about telemetry/cost.
- Use Benchmark when talking about model comparison.
- Use Ops mobile when talking about operations and diagnostics.
- Do not claim hosted marketplace behavior.
- Emphasize local control, config publishing, telemetry, benchmark, diagnostics, update, and rollback.
- Ask for specific operational feedback instead of generic stars.
