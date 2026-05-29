# AI Model Gateway Use Cases

AI Model Gateway is built for teams that need an internal LLM operations gateway, not a hosted model marketplace. These use cases describe where the project is a practical fit and what to evaluate first.

## 1. Internal LLM Gateway For Product Teams

Use AI Model Gateway when several applications need one controlled entry point for OpenAI-compatible, Anthropic-compatible, or Responses-compatible traffic.

What it gives you:

- one local gateway URL for application teams
- provider routing and fallback behind that URL
- request logs, latency, cost, model usage, and provider health
- admin-side config publishing, audit, and rollback

Evaluate first:

- whether your current clients can point at an OpenAI-compatible gateway URL
- whether the built-in routing rules match your provider/model naming strategy
- which telemetry fields you need for internal reporting

## 2. Safer Config Publishing For LLM Routing

Use AI Model Gateway when provider routing changes should go through a controlled rollout path instead of editing a live proxy config file.

What it gives you:

- authoring config separate from compiled runtime snapshots
- preview, diff, publish history, audit, and rollback
- provider probes and diagnostics before or after changes

Evaluate first:

- how your team reviews routing and credential changes today
- which rollback evidence operators need during incidents
- whether publish history should become part of your change-management process

## 3. Provider Health And Fallback Operations

Use AI Model Gateway when you need to see which providers are degraded and keep applications running through fallback routes.

What it gives you:

- provider-level routing and fallback policy
- provider probes from the operations surface
- request-level status and error visibility
- logs and replay workflows for investigation

Evaluate first:

- common provider failure modes in your environment
- expected timeout and retry behavior for streaming and non-streaming traffic
- whether fallback should optimize for reliability, latency, cost, or model quality

## 4. LLM Cost, Latency, And Usage Visibility

Use AI Model Gateway when teams need cost and latency visibility near the gateway, not only inside application logs.

What it gives you:

- async telemetry ingestion
- traffic, latency, cost, model usage, and provider health views
- CSV export and request search from the admin UI
- a separate telemetry plane that keeps event ingestion off the public HTTP surface

Evaluate first:

- which cost catalog and currency assumptions your team needs
- how long request logs should be retained
- whether exported telemetry fits your downstream reporting workflow

## 5. Model Benchmarking Before Route Promotion

Use AI Model Gateway when a new model or provider should be tested before receiving production traffic.

What it gives you:

- benchmark suites for comparing model behavior
- scoring modes for exact, judge, JSON, tool, and stream behavior
- latency and cost context next to capability results
- benchmark evidence that can support routing decisions

Evaluate first:

- which prompts represent your production workload
- what score threshold is required before route promotion
- whether benchmark results should be exported for review

## 6. Local Update And Rollback Workflow

Use AI Model Gateway when you want upgrade operations to be visible and reversible from the same tool surface.

What it gives you:

- manifest-verified bundles
- CLI and admin update workflows
- dry-run apply and local rollback support
- release bundle checks in CI

Evaluate first:

- whether your deployment path uses release archives, Docker images, or service managers
- how operators verify a bundle before applying it
- how quickly rollback must happen during incidents

## When Not To Use It

AI Model Gateway is probably not the right first choice if you mainly need:

- the largest possible provider catalog
- a hosted broker that manages accounts and billing for you
- a full generic API gateway platform
- a standalone observability product unrelated to routing

For those cases, compare dedicated hosted routers, broad LLM gateway products, general API gateways, or observability platforms before adopting this project.

## Next Steps

- Start with the [README quick trial](../README.md#try-it-quickly).
- Choose a path from the [setup table](../README.md#choose-a-setup-path).
- Review [config publish and rollback](config-publish-rollback.md) if you are evaluating operational rollout safety.
- Read the [differentiation notes](differentiation.md) if you are comparing alternatives.
- Open a question in [Discussion #25](https://github.com/SSC-STUDIO/Ai-Model-Gateway/discussions/25) if a use case is missing.
