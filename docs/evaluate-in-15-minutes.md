# Evaluate AI Model Gateway In 15 Minutes

Use this path when you want to decide quickly whether AI Model Gateway is worth a deeper trial for a self-hosted LLM gateway or internal model-routing project.

This is not a full production setup. It is a short evaluation sequence for the questions most operators ask first:

- Can the runtime start locally?
- Does the project solve a real operations problem instead of only proxying requests?
- Are fallback, config publish, telemetry, benchmarks, and rollback documented enough to inspect?
- Is the project healthy enough to spend more time on?

## 1. Check The Fit

Start with these three documents:

- [Use cases](use-cases.md) for the workflows the project is built around.
- [Self-hosted LLM gateway checklist](self-hosted-llm-gateway-checklist.md) for the buy/build/adopt questions.
- [LLM gateway comparison guide](llm-gateway-comparison.md) for adjacent gateway, observability, hosted-router, and API-gateway options.

If you mainly want a hosted model marketplace, the largest provider catalog, or standalone tracing on top of an existing proxy, this project is probably not the first tool to try.

## 2. Start The Runtime

For a source-based trial:

```bash
git clone https://github.com/SSC-STUDIO/Ai-Model-Gateway.git
cd Ai-Model-Gateway
go build -o ./dist/aigw ./cmd/aigw
go build -o ./dist/gatewayd ./cmd/gatewayd
go build -o ./dist/controld ./cmd/controld
go build -o ./dist/telemetryd ./cmd/telemetryd
cp configs/config.example.yaml configs/config.yaml
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control
ADMIN_BOOTSTRAP_TOKEN=change-me-32-characters-minimum \
COOKIE_SIGNING_KEY=change-me-32-characters-minimum \
ADMIN_TOKEN=change-me-admin-token \
VIEWER_TOKEN=change-me-viewer-token \
./dist/aigw supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir ./dist
```

Then open:

- Admin UI: `http://localhost:18080/admin`
- Gateway health: `http://localhost:18080/-/health`

For release archives or service deployment, use the [installation guide](installation.md) and [deployment guide](deployment.md).

## 3. Verify One Differentiating Behavior

Run the provider fallback demo:

```bash
go test ./examples/provider-fallback -run TestProviderFallbackDemo -v
```

The demo starts two fake OpenAI-compatible upstreams. The primary provider returns `429`, the gateway serves the request through the fallback provider, the forwarded model is rewritten to the fallback upstream model, and telemetry records `route_mode=model_fallback`.

This is a quick proof that the fallback path is not just a documentation claim.

## 4. Inspect The Operations Workflows

Read these in order:

1. [Config publish and rollback](config-publish-rollback.md)
2. [Provider fallback and health operations](provider-fallback-health.md)
3. [Troubleshooting](troubleshooting.md)
4. [Project roadmap](roadmap.md)

The important question is whether these workflows match your team's real incidents:

- bad provider credentials
- quota exhaustion or `429` bursts
- slow or failing upstreams
- config changes that need preview, audit, and rollback
- benchmark evidence before routing traffic to a model
- update workflows that need dry-run and rollback

## 5. Decide Whether To Go Deeper

Continue the trial if you need:

- one self-hosted gateway URL for application teams
- OpenAI, Anthropic, or Responses-compatible client entry points
- local ownership of provider keys, routing policy, telemetry, and audit history
- operations workflows for provider probes, config publish, diagnostics, replay, and rollback
- a compact Go runtime rather than a hosted broker

Pause the trial if your priority is:

- a hosted billing and model marketplace
- a very broad provider catalog above all else
- generic non-LLM API gateway policy
- an observability product that attaches to an existing gateway

## 6. Leave Useful Feedback

If the project is close but not enough, the most useful feedback is concrete:

- Which provider failure mode is missing?
- Which deployment path should be documented next?
- Which telemetry view would make the gateway easier to trust?
- Which config publish or rollback step is unclear?
- What benchmark signal would help with route promotion?

Use [Discussion #25](https://github.com/SSC-STUDIO/Ai-Model-Gateway/discussions/25) for broad feedback, or open an issue for a specific bug or feature request.
