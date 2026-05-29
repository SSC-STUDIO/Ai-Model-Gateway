# Differentiation Notes

This document captures the public-market comparison used to position AI Model Gateway. It is intentionally practical: the goal is to decide what this project should emphasize, what it should avoid copying, and where the README/admin UI should make the value clear.

For a visitor-facing selection guide, see [LLM gateway comparison guide](llm-gateway-comparison.md).

## Research Snapshot

Checked with `gh` on 2026-05-20. Star counts and descriptions will change, but the market shape is useful:

| Repository | Stars checked | Public description signal |
| --- | ---: | --- |
| [BerriAI/litellm](https://github.com/BerriAI/litellm) | 47,566 | Python SDK and AI gateway/proxy for 100+ LLM APIs with cost tracking, guardrails, load balancing, and logging |
| [Portkey-AI/gateway](https://github.com/Portkey-AI/gateway) | 11,782 | Fast AI gateway with broad LLM routing and integrated guardrails |
| [Helicone/helicone](https://github.com/Helicone/helicone) | 5,692 | Open source LLM observability platform |
| [envoyproxy/ai-gateway](https://github.com/envoyproxy/ai-gateway) | 1,656 | Unified access to generative AI services built on Envoy Gateway |
| [Kong/kong](https://github.com/Kong/kong) | 43,416 | General API and AI gateway platform |
| [ferro-labs/ai-gateway](https://github.com/ferro-labs/ai-gateway) | 88 | Go-native AI gateway alternative with caching, guardrails, A/B testing, and cost controls |

## Similar Projects

The closest public projects and products are not weak. The useful differentiation comes from choosing a narrower operating model.

| Project | Public positioning | What users usually choose it for | Where AI Model Gateway should differ |
| --- | --- | --- | --- |
| [LiteLLM](https://github.com/BerriAI/litellm) | Python SDK and AI gateway/proxy for many LLM providers | Broad provider compatibility, OpenAI-compatible proxying, cost tracking, logging, budgets, guardrails, load balancing | Go-native self-hosted runtime, explicit data/control/telemetry planes, local bundle verification, admin-driven config publish and rollback |
| [Portkey AI Gateway](https://github.com/Portkey-AI/gateway) | Fast AI gateway with broad model routing and guardrails | Many-model access, gateway API, guardrails, production provider abstraction | Less SaaS-shaped, more operator-shaped: local runtime ownership, predictable deployment, provider probes, diagnostics, audit, and revision workflow |
| [Helicone](https://github.com/Helicone/helicone) | Open source LLM observability platform | Request monitoring, analytics, evaluation, experiments | Treat telemetry as an internal plane of the gateway rather than a separate observability product |
| [OpenRouter](https://openrouter.ai/) | Hosted router and unified API for many models | Quick hosted access to public models through one API/key | Keep keys, routing decisions, telemetry, and policy inside the user's own environment |
| [Envoy AI Gateway](https://github.com/envoyproxy/ai-gateway) | Unified access to generative AI services built on Envoy Gateway | Kubernetes-native gateway policy, Envoy ecosystem integration | Smaller LLM-specific binary set with built-in admin, config, telemetry, benchmarks, and local CLI |
| Kong AI Gateway ecosystem | API gateway plugins and enterprise gateway controls for AI traffic | Existing Kong deployments, enterprise API gateway policy | Avoid requiring a general API gateway stack for teams that mainly need an LLM operations gateway |

## Positioning

AI Model Gateway should be described as:

> A self-hosted LLM operations gateway for teams that need routing, config publishing, telemetry, benchmarks, and diagnostics without handing the control plane to a hosted broker.

This is more specific than "unified AI gateway" and avoids competing only on provider count.

## Differentiation Pillars

### 1. Local-first operations

The core product loop is not "sign up and call many models." It is:

1. Build a manifest-verified bundle.
2. Start `aigw supervise`.
3. Publish a compiled config snapshot.
4. Observe request health and cost.
5. Probe, diagnose, benchmark, and roll back from the same local control plane.

That makes the project more attractive to users who care about private keys, internal providers, regulated environments, or repeatable deployment.

### 2. Three-plane architecture

Many gateway projects expose a single proxy process. AI Model Gateway should keep highlighting its internal boundaries:

- `gatewayd` handles inference traffic.
- `controld` owns configuration, admin APIs, publish/rollback, probing, benchmark, and audit.
- `telemetryd` ingests events over IPC and keeps telemetry off the public HTTP surface.
- `aigw supervise` is the one operational entry point.

This is a concrete design difference, not just a marketing claim.

### 3. Config publish and rollback

The project should keep investing in the authoring config -> compiled snapshot -> publish ledger model. This is a strong differentiator against simple proxy configs because it gives operators a safer workflow:

- preview
- diff
- validate
- publish
- audit
- rollback

### 4. Admin UI as an operations console

The admin UI should avoid hero sections, decorative dashboards, and vague charts. It should always answer:

- Is the gateway healthy?
- Which providers are degraded?
- What changed recently?
- Which model/provider should receive traffic?
- Can I publish, roll back, probe, replay, or export from here?

The first screen should favor status, key metrics, and executable actions over marketing copy.

### 5. Benchmarks tied to promotion decisions

Benchmarking should not be a toy comparison page. The useful direction is to connect benchmark results to model readiness:

- capability matrix
- scoring mode transparency
- latency and cost alongside correctness
- promotion signals for routing policy
- exportable evidence for config changes

## What Not To Chase First

These areas are crowded and expensive to win on alone:

- Maximum provider count.
- Hosted marketplace UX.
- A complete generic API gateway replacement.
- A full standalone observability platform.
- Decorative dashboards that look impressive but do not support operations.

They can exist as integrations or future features, but they should not blur the core identity.

## README And UI Copy Rules

- Lead with "self-hosted", "operations gateway", "config publishing", "telemetry", and "benchmarks".
- Mention provider compatibility, but do not make provider count the headline.
- Show the admin console as a work surface, not as a marketing dashboard.
- Prefer exact workflow language: publish, rollback, probe, diagnose, replay, export.
- Keep screenshots current after UI polish work; stale screenshots weaken the professional positioning.

## Near-term Roadmap That Reinforces Differentiation

| Area | High-value next step |
| --- | --- |
| Config workflow | Add clearer publish readiness checks and rollback risk summaries |
| Provider health | Persist probe history and show recent degradation causes |
| Benchmark | Convert benchmark results into route promotion recommendations |
| Telemetry | Add cost anomaly and latency regression panels |
| Docs | Add deployment recipes for local, systemd, NSSM, Docker Compose, and Kubernetes |
| Security | Document key handling, SSRF policy, admin token model, and telemetry boundaries |
