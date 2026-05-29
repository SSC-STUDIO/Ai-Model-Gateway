# 100-Star Campaign Plan

This plan is for legitimate growth of `SSC-STUDIO/Ai-Model-Gateway` from the current baseline toward 100 GitHub stars. It focuses on clarity, useful technical distribution, and repeatable follow-up.

## Current Baseline

- Repository: https://github.com/SSC-STUDIO/Ai-Model-Gateway
- Verified on 2026-05-29: 2 GitHub stars, 0 open issues, 0 open pull requests.
- Target: 100 GitHub stars.
- Gap: 98 stars.

## Positioning

AI Model Gateway is a self-hosted LLM operations gateway for teams that want routing, fallback, telemetry, benchmarking, config publishing, diagnostics, updates, and rollback inside their own environment.

Primary message:

> A self-hosted LLM operations gateway for teams that want local control over provider keys, routing policy, telemetry, config changes, benchmarks, updates, and rollback.

Avoid positioning it as:

- a hosted model marketplace
- a generic API gateway replacement
- a dashboard-only observability product
- a provider-count competition

## Target Audiences

| Audience | Why They Care | Best Hook |
| --- | --- | --- |
| Self-hosted AI operators | They want local control over keys, telemetry, and config rollout | "Run an LLM gateway without handing the control plane to a hosted broker." |
| Platform engineers | They need repeatable deploy, rollback, audit, and diagnostics | "Config publish, audit, provider probes, and rollback are built into the runtime." |
| Go developers | They value compact native binaries and clear architecture | "A Go-native three-plane gateway: data, control, telemetry." |
| LLM app teams | They need fallback, cost visibility, logs, and benchmarks | "Route across providers and compare model behavior before promoting traffic." |
| Open source maintainers | They evaluate project quality quickly | "CI is green, releases are packaged, docs and screenshots are current." |

## Conversion Checklist

Before each promotion push, verify:

- `README.md` starts with the self-hosted operations-gateway value proposition.
- GitHub topics cover discoverable keywords: `llm`, `ai`, `gateway`, `openai`, `anthropic`, `observability`, `self-hosted`, `proxy`, `golang`, `benchmark`.
- Releases include installable assets, checksums, and concise notes.
- Screenshots show the actual admin UI, not abstract marketing art.
- Open issues and PRs are triaged so visitors see an actively maintained repository.

## Primary Channels

| Channel | Action | Goal |
| --- | --- | --- |
| GitHub README and releases | Keep landing page, screenshots, release notes, and topics sharp | Convert GitHub visitors into stars |
| Hacker News | Submit "Show HN" with a short technical post | Reach operators and engineers |
| Reddit | Post to relevant communities with feedback framing | Reach self-hosted, Go, and LLM practitioners |
| LinkedIn | Publish an engineering-oriented release/update post | Reach platform and engineering leads |
| X/Twitter | Post a concise launch thread with screenshots | Reach AI infra builders |
| V2EX / Zhihu / Juejin / Bilibili | Use Chinese-language technical posts and videos | Reach Chinese developer communities |
| GitHub Discussions in related projects | Only comment when directly relevant and helpful | Earn attention without spam |

## Copy Blocks

### Short English

AI Model Gateway is a self-hosted LLM operations gateway.

It combines OpenAI/Anthropic/Responses-compatible entry points with provider routing, fallback, config publishing, telemetry, benchmarks, diagnostics, updates, and rollback in one local Go runtime.

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

### Technical English

I built AI Model Gateway for teams that want to run LLM traffic through their own control plane instead of handing routing policy, provider keys, telemetry, and operational decisions to a hosted broker.

The runtime is split into data, control, and telemetry planes. The admin console supports provider health, request logs, cost/latency telemetry, config publish/rollback, benchmark runs, diagnostics, replay, and verified update workflows.

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

### Show HN

Title:

Show HN: AI Model Gateway, a self-hosted LLM operations gateway

Post:

I built AI Model Gateway, a self-hosted LLM gateway focused on day-2 operations rather than hosted model brokerage.

It combines OpenAI/Anthropic/Responses-compatible entry points with provider routing, fallback, config publishing, telemetry, benchmarks, diagnostics, audit logs, and an Admin UI. The runtime is split into data, control, and telemetry planes, supervised through a compact Go entry point.

The latest releases also add verified update and rollback workflows through the CLI and Admin UI.

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

### Reddit

Title:

I built a self-hosted LLM operations gateway with routing, telemetry, benchmarks, and rollback

Post:

I have been working on AI Model Gateway, a self-hosted LLM gateway written in Go.

The focus is operational control:

- OpenAI Chat Completions, Anthropic Messages, and OpenAI Responses compatibility
- provider routing, fallback, rate limiting, and request cache
- local config publishing with preview, diff, audit, and rollback
- telemetry for traffic, latency, cost, model usage, and provider health
- benchmark workspace for comparing model behavior
- Admin UI for diagnostics, probes, logs, replay, and updates
- manifest-verified bundles and in-product update/rollback flow

Repo: https://github.com/SSC-STUDIO/Ai-Model-Gateway

I would appreciate feedback from people running self-hosted or team-internal LLM infrastructure.

## 30-Day Execution Cadence

| Day | Action | Expected Result |
| --- | --- | --- |
| 1 | Verify PRs/issues are clear, README is current, release is healthy | Strong first impression |
| 1 | Post to one broad technical channel and one focused community | First discovery wave |
| 2 | Reply to comments with technical detail and invite issues/PRs | Improve trust and engagement |
| 3 | Share screenshots and a short update thread | Visual proof |
| 5 | Publish a short "how config publish and rollback work" post | Deeper technical credibility |
| 7 | Publish a "self-hosted LLM gateway comparison" post | Search/discovery traffic |
| 10 | Share benchmark/admin UI workflow clip | Product clarity |
| 14 | Review stars, traffic signals, and feedback issues | Adjust message |
| 21 | Release a small polish update based on feedback | Show momentum |
| 30 | Re-check star count and repeat best-performing channel | Continue toward 100 |

## Operating Rules

- Do not buy, trade, or automate fake stars.
- Do not mass-DM or spam unrelated communities.
- Do not post identical copy across many communities on the same day.
- Prefer feedback-oriented posts over "please star" posts.
- Respond to technical questions with concrete implementation details and links.
- Convert useful feedback into GitHub issues so the repository activity remains visible and actionable.

## Progress Log

| Date | Stars | Open PRs | Open Issues | Action |
| --- | ---: | ---: | ---: | --- |
| 2026-05-29 | 2 | 0 | 0 | Cleared Dependabot PR backlog and created this campaign plan. |
| 2026-05-29 | 2 | 0 | 0 | Added README setup links, use cases, and self-hosted gateway evaluation checklist to improve visitor conversion. |
| 2026-05-29 | 2 | 0 | 0 | Added `docs/README.md` so the README Docs link opens a navigable documentation index instead of a plain directory listing. |
| 2026-05-29 | 2 | 0 | 0 | Added `CODE_OF_CONDUCT.md` to close a visible GitHub community-profile trust gap. |
