# Project Roadmap

This roadmap explains the current direction of AI Model Gateway. It is intentionally practical: it describes the product areas that would make the gateway more useful for real self-hosted LLM operations, without promising fixed dates.

## Current Focus

AI Model Gateway is focused on becoming a compact operations gateway for teams that want local control over LLM traffic:

- provider routing, fallback, retries, rate limits, and cache behavior
- config preview, diff, publish, audit, and rollback
- provider health, probes, cooldown state, diagnostics, and replay
- telemetry for traffic, latency, usage, cost, and failures
- benchmark workflows that help decide which model should receive traffic
- manifest-verified updates and rollback for the gateway runtime

Recent proof points:

- [Config publish and rollback](config-publish-rollback.md)
- [Provider fallback and health operations](provider-fallback-health.md)
- [Provider fallback demo](../examples/provider-fallback/)
- [LLM gateway comparison guide](llm-gateway-comparison.md)
- [Self-hosted LLM gateway checklist](self-hosted-llm-gateway-checklist.md)

## Near-term Priorities

| Area | Direction | Why it matters |
| --- | --- | --- |
| Provider health | Persist probe history and explain recent degradation causes | Operators need to know whether a route failed because of quota, timeout, upstream error, or local config |
| Fallback behavior | Make fallback decisions easier to inspect from logs, telemetry, and Admin UI | Teams need confidence that degraded providers are bypassed for the right reasons |
| Config publishing | Add clearer readiness checks and rollback risk summaries before publish | Config changes should be reviewable before they affect live traffic |
| Benchmarks | Turn benchmark results into route promotion recommendations | Benchmark data is more useful when it can guide routing decisions |
| Telemetry | Add latency regression and cost anomaly views | Operators need early signals before failures become incidents |
| Deployment docs | Add more production recipes for systemd, Windows services, Docker Compose, and Kubernetes | New users should be able to move from local trial to service deployment quickly |
| Security docs | Clarify key handling, SSRF policy, admin token model, and telemetry boundaries | Self-hosted operators need to understand the trust model before adoption |

## Good Contribution Areas

These are useful contributions that do not require redesigning the whole system:

- focused tests for OpenAI, Anthropic, and Responses protocol compatibility
- clearer Admin UI states for provider health, probes, and fallback decisions
- deployment recipes and smoke checks for common hosting environments
- documentation that turns an operational workflow into a reproducible runbook
- telemetry panels that answer a specific operator question
- safer validation for config fields that can break routing

Before opening a pull request, read [Contributing](../CONTRIBUTING.md) and run the local checks in [Local CI](ci-local.md).

## Not The Current Direction

The project is not trying to become:

- a hosted model marketplace
- a provider-count leaderboard
- a generic API gateway replacement
- a dashboard-only observability product
- a system that requires sending provider keys or telemetry to a third-party control plane

Those features can exist as integrations around the gateway, but they should not blur the core self-hosted operations focus.

## How To Influence The Roadmap

The most useful feedback is concrete and operational:

- Which provider failure modes do you hit most often?
- What rollback workflow would you trust in production?
- Which telemetry view would help you catch a real incident earlier?
- What benchmark signal would make you comfortable routing traffic to a model?
- What deployment path is missing from the docs?

Use the [maintainer discussion](https://github.com/SSC-STUDIO/Ai-Model-Gateway/discussions/25) for broad feedback, or open an issue when you have a specific bug or feature request.
