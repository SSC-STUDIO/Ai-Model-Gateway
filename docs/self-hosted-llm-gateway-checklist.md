# Self-Hosted LLM Gateway Evaluation Checklist

Use this checklist when deciding whether to run a self-hosted LLM gateway, a hosted model router, a general API gateway, or a standalone observability product.

AI Model Gateway is designed for the self-hosted operations path: one local runtime for routing, config publishing, telemetry, benchmarks, diagnostics, updates, and rollback.

## 1. Control Plane Ownership

Ask:

- Where do provider keys live?
- Who can change routing policy?
- Can the team review config changes before they reach production traffic?
- Is there an audit trail for publish, rollback, probe, and update actions?

AI Model Gateway fit:

- Provider keys and routing policy stay in your environment.
- The control plane owns preview, diff, publish history, audit, and rollback.
- The data, control, and telemetry planes are separate internal processes supervised by `aigw`.

## 2. Client Compatibility

Ask:

- Which client APIs do applications already use?
- Can application teams point at one local gateway URL?
- Do you need OpenAI Chat Completions, Anthropic Messages, OpenAI Responses, or a mix?
- What behavior must be preserved for streaming, tool calls, and usage accounting?

AI Model Gateway fit:

- It exposes OpenAI-compatible entry points and bridges OpenAI, Anthropic, and Responses-style workflows.
- It is useful when teams want client applications to keep familiar API shapes while routing decisions move into the gateway.

## 3. Routing And Fallback

Ask:

- What should happen when a provider is slow, rate-limited, or unavailable?
- Should fallback optimize for reliability, latency, cost, or model quality?
- Can operators see why traffic moved from one provider to another?
- Do you need provider-level probes before promoting traffic?

AI Model Gateway fit:

- It supports provider routing, fallback policy, provider probes, model checks, and request logs.
- The admin surface is built around operational actions such as probe, diagnose, replay, publish, and rollback.

## 4. Config Change Safety

Ask:

- Is today’s gateway config edited live?
- Can operators preview and diff changes before publishing?
- Can the team roll back to a known revision during an incident?
- Does the change path produce evidence for reviews and postmortems?

AI Model Gateway fit:

- The config workflow separates authoring YAML from compiled runtime snapshots.
- Operators can preview, diff, validate, publish, audit, and roll back changes.
- Publish history turns routing changes into a visible operations record.

## 5. Telemetry And Cost Visibility

Ask:

- Where do request count, latency, cost, model usage, and provider health data go?
- Can operators investigate errors without joining multiple log systems first?
- Do you need CSV export or downstream reporting?
- Is telemetry allowed to leave your environment?

AI Model Gateway fit:

- It includes async telemetry ingestion and admin views for traffic, latency, cost, model usage, provider health, request logs, and exports.
- The telemetry plane is internal, so event ingestion is not exposed as a public model endpoint.

## 6. Benchmarking Before Promotion

Ask:

- How do you decide whether a model is ready for production routing?
- Do you compare correctness, latency, streaming behavior, tool behavior, and cost together?
- Can benchmark evidence be shared during config review?

AI Model Gateway fit:

- It includes benchmark suites and scoring modes for exact, judge, JSON, tool, and stream behavior.
- Benchmark results can support route promotion decisions instead of living in a separate experiment notebook.

## 7. Deployment And Upgrade Model

Ask:

- Is the gateway a single service, a bundle of daemons, or a cluster-managed stack?
- How are release assets verified?
- Can operators dry-run and roll back updates?
- Does CI build the same artifacts that production will run?

AI Model Gateway fit:

- It ships a manifest-verified runtime bundle with `aigw`, `gatewayd`, `controld`, `telemetryd`, and the admin UI.
- Update workflows can check releases, fetch platform bundles, verify manifests, dry-run apply, apply locally, and roll back.

## 8. When To Choose Something Else

Choose a hosted router first if you mainly need fast access to a very large public model catalog and do not want to operate infrastructure.

Choose a general API gateway first if your main problem is broad API traffic management, not LLM-specific routing, telemetry, benchmarks, and config rollout.

Choose a standalone observability platform first if you already have a gateway and only need analytics, tracing, or evaluations.

Choose AI Model Gateway when local control, routing operations, publish safety, telemetry, benchmarks, diagnostics, updates, and rollback need to live together.

## Related Docs

- [Use cases](use-cases.md)
- [Differentiation notes](differentiation.md)
- [Installation guide](installation.md)
- [Deployment guide](deployment.md)
- [Troubleshooting](troubleshooting.md)
