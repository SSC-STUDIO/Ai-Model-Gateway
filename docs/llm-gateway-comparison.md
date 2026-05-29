# LLM Gateway Comparison Guide

This guide helps operators compare AI Model Gateway with adjacent open source LLM gateway, observability, hosted-routing, and general API gateway options.

The goal is not to declare a universal winner. Most projects below are strong in their chosen scope. The useful question is which operating model fits your team.

Checked with GitHub metadata on 2026-05-29:

| Project | Stars | Public positioning signal |
| --- | ---: | --- |
| [LiteLLM](https://github.com/BerriAI/litellm) | 48,664 | Python SDK and proxy server for 100+ LLM APIs with cost tracking, guardrails, load balancing, and logging |
| [Portkey AI Gateway](https://github.com/Portkey-AI/gateway) | 11,892 | Fast AI gateway with integrated guardrails, broad model routing, and one API |
| [Helicone](https://github.com/Helicone/helicone) | 5,752 | Open source LLM observability for monitoring, evaluation, and experiments |
| [Envoy AI Gateway](https://github.com/envoyproxy/ai-gateway) | 1,698 | Unified access to generative AI services built on Envoy Gateway |
| [Kong](https://github.com/Kong/kong) | 43,472 | General API and AI gateway |
| [ferro-labs/ai-gateway](https://github.com/ferro-labs/ai-gateway) | 105 | Go-native AI gateway alternative with caching, guardrails, A/B testing, and cost controls |

## Quick Decision Table

| If you mainly need... | Start by evaluating... | Why |
| --- | --- | --- |
| Maximum provider/model coverage | LiteLLM or Portkey | Their public positioning emphasizes broad provider access and unified APIs. |
| LLM observability, tracing, evaluations, and experiments | Helicone | It is built as an observability platform first. |
| Hosted model access without operating a gateway | Hosted routers such as OpenRouter-style products | The operating burden is lower, but keys, routing policy, and telemetry move outside your environment. |
| Kubernetes-native gateway policy and Envoy ecosystem integration | Envoy AI Gateway or Kong ecosystem tools | They fit teams already standardized on gateway infrastructure. |
| A compact self-hosted LLM operations runtime | AI Model Gateway | It combines routing, config publishing, telemetry, benchmarks, diagnostics, updates, and rollback in one local runtime. |

## Where AI Model Gateway Fits

AI Model Gateway is optimized for teams that want the LLM gateway to be an operations tool, not only a request proxy.

It is a good fit when you need:

- local control over provider keys, routing policy, telemetry, and config history
- OpenAI, Anthropic, and Responses-compatible entry points behind one local gateway
- preview, diff, publish, audit, and rollback for routing config changes
- provider probes, diagnostics, replay, request logs, and admin-side investigation tools
- benchmark evidence before routing traffic to a model
- manifest-verified bundles, update checks, dry-run apply, and rollback

## Feature Emphasis

| Area | AI Model Gateway emphasis | Broad gateway emphasis | Observability platform emphasis | General API gateway emphasis |
| --- | --- | --- | --- | --- |
| Provider access | Enough compatibility for operational control | Maximum provider and model catalog | Usually integrates with an existing gateway or SDK path | Plugin or policy-driven access |
| Runtime ownership | Self-hosted local runtime | Often proxy-first or SaaS-adjacent | Usually separate telemetry service | Infrastructure gateway stack |
| Config workflow | Authoring config -> compiled snapshot -> publish ledger -> rollback | Varies by product | Usually not the central workflow | Gateway policy and route config |
| Telemetry | Built into the gateway lifecycle | Often cost/logging-focused | Primary product surface | Depends on plugins and integrations |
| Admin surface | Operations console for publish, probe, diagnose, benchmark, update, rollback | Routing/control dashboard | Analytics and evaluation dashboard | API operations and traffic policy |
| Update model | Manifest-verified bundle, dry-run, apply, rollback | Product-specific | Product-specific | Platform-specific |

## Choose AI Model Gateway When

Choose AI Model Gateway when these statements describe your team:

- You want application teams to use one internal gateway URL.
- You do not want provider keys, telemetry, and routing policy managed by a hosted broker.
- You need a safer config publishing workflow than editing a live proxy file.
- You want provider health, request logs, telemetry, benchmarks, diagnostics, and updates in one admin surface.
- You are comfortable operating a small Go runtime instead of adopting a broader platform stack.

## Choose Something Else When

Choose a different project first when:

- provider count is the deciding requirement
- hosted account/billing aggregation is the main value
- the team already runs Kong, Envoy, or another gateway platform and mainly needs plugins
- the only missing piece is observability or evaluation on top of an existing proxy
- a full AI application UI is the target rather than gateway operations

## Questions To Ask Before Migrating

Before replacing an existing gateway or proxy, answer:

- Which client API shapes must remain compatible?
- Which routing, fallback, retry, and timeout behavior is already depended on?
- Which telemetry fields must be preserved for reporting or billing?
- How are config changes reviewed today?
- How quickly must operators roll back a bad route or bad release?
- Which model benchmarks would be trusted enough to influence production traffic?

## Related Docs

- [Use cases](use-cases.md)
- [Self-hosted LLM gateway checklist](self-hosted-llm-gateway-checklist.md)
- [Differentiation notes](differentiation.md)
- [Architecture](architecture.md)
- [Deployment guide](deployment.md)
