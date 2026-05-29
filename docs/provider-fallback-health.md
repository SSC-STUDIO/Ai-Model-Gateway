# Provider Fallback And Health Operations

Use this guide when you are evaluating AI Model Gateway for upstream provider reliability. The goal is to show how the gateway detects degraded providers, routes around unhealthy upstreams, and gives operators enough evidence to decide whether to publish a config change or roll back.

For a small executable proof, run the [provider fallback demo](../examples/provider-fallback/). It starts two fake upstreams, forces the primary provider to return `429`, and verifies that the gateway serves the request through the fallback provider with `route_mode=model_fallback`.

AI Model Gateway treats provider reliability as part of the runtime lifecycle:

- `gatewayd` handles live inference traffic, provider routing, retries, health probes, and provider cooldown state.
- `controld` exposes runtime status, provider probes, config publishing, rollback, audit, and diagnostics through the Admin UI and CLI.
- `telemetryd` stores request events so provider failures can be investigated after the incident.

## Runtime Model

The default example config uses health-aware weighted routing:

```yaml
routing:
  strategy: health_weighted_rr
  max_retries: 2
  health:
    enabled: true
    interval_sec: 10
    timeout_ms: 2000
    path: /v1/models
  failure_policy:
    threshold: 20
    cooldown_sec: 60
    passthrough_after_sec: 600
    quota_recovery_interval_min: 0
    disable_cooldown: false
```

Providers that expose the same model can have different weights:

```yaml
providers:
  - name: station-a
    base_url: https://example-a.com
    api_key: "${STATION_A_API_KEY}"
    provider_class: free
    models:
      - gpt-4o-mini
    weight: 5
    enabled: true

  - name: station-b
    base_url: https://example-b.com
    api_key: "${STATION_B_API_KEY}"
    provider_class: quota_limited
    models:
      - gpt-4o-mini
    weight: 1
    enabled: true
```

At runtime, healthy providers stay in the primary candidate pool. Providers with repeated request or probe failures can enter cooldown. If every preferred path is degraded for long enough, the passthrough window gives operators a controlled way to keep traffic moving instead of permanently black-holing a route.

## What Counts As A Failure

Gateway provider state is updated from live request attempts and health probes. The data plane treats these as provider failures:

- network or forwarding errors
- HTTP `408`
- HTTP `429`
- HTTP `5xx`

Successful attempts reset consecutive failure state. A `429` also records quota-block timing so quota-limited providers can recover on a separate recovery interval when configured.

## Operator Checks

Start with the gateway and control-plane health endpoints:

```bash
curl http://127.0.0.1:18080/-/health
curl http://127.0.0.1:18081/-/health
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://127.0.0.1:18081/api/admin/runtime/status
```

Then use the CLI for structured status and provider checks:

```bash
./dist/gateway-cli runtime status
./dist/gateway-cli provider list
./dist/gateway-cli provider test openai
./dist/gateway-cli probe provider openai-demo gpt-4
./dist/gateway-cli probe model gpt-4 openai-demo
./dist/gateway-cli audit 50
./dist/gateway-cli telemetry events
```

Useful runtime signals include:

| Field | Why It Matters |
| --- | --- |
| `gateway_readiness` | Confirms whether the data plane is ready, starting, draining, or stopped |
| `active_snapshot_id` | Shows which compiled config snapshot is serving traffic |
| `provider_health_count` | Shows how many logical upstreams are represented in runtime health |
| `healthy_provider_count` | Shows how many logical upstreams are currently routable |
| `unhealthy_provider_count` | Shows how many logical upstreams are blocked or cooling down |
| `gateway.ProviderHealth` in raw runtime JSON | Includes provider IDs, upstream ID, last check, last success, consecutive failures, latency, and cooldown timing |

## Incident Workflow

1. Confirm the runtime state.

```bash
./dist/gateway-cli runtime status
curl http://127.0.0.1:18080/-/health
```

2. Identify the affected provider or model.

```bash
./dist/gateway-cli provider list
./dist/gateway-cli telemetry events
./dist/gateway-cli audit 50
```

3. Probe the failing route without relying on normal fallback.

```bash
./dist/gateway-cli probe provider <provider-id> <model>
./dist/gateway-cli probe model <model> <provider-id>
```

Diagnostic probes disable cache, fallback, and retries for that synthetic request so the result is useful for isolating one upstream branch.

4. Decide whether the fix is operational or configuration-based.

Operational fixes usually include restoring credentials, network egress, provider quota, or the provider service itself. Configuration fixes usually include reducing a provider weight, disabling a provider, changing retry behavior, or publishing a known-good revision.

5. Publish or roll back through the normal config lifecycle.

```bash
./dist/gateway-cli validate configs/config.yaml
./dist/gateway-cli config preview configs/config.yaml
./dist/gateway-cli config diff --file configs/config.yaml
./dist/gateway-cli reload
./dist/gateway-cli publish history
./dist/gateway-cli publish rollback <known-good-revision>
```

6. Verify recovery.

```bash
./dist/gateway-cli runtime status
./dist/gateway-cli provider list
./dist/gateway-cli audit 50
```

## Tuning Notes

- Use `health.enabled` when providers expose a low-cost health endpoint such as `/v1/models`.
- Keep `health.timeout_ms` short enough to avoid slow probes hiding provider degradation.
- Increase `failure_policy.threshold` if providers occasionally return transient errors that should not trigger cooldown.
- Decrease `failure_policy.cooldown_sec` if traffic should re-test a provider quickly after a short failure burst.
- Use `provider_class: quota_limited` for upstreams where quota behavior matters operationally.
- Use provider `rate_limit` to queue outbound dispatch for one logical upstream instead of returning gateway-side `429` responses.
- Keep `disable_cooldown: false` for normal production routing. The example config shows `disable_cooldown: true` only for clients that must never receive transient upstream errors and accept continuous retry behavior.

## Related Docs

- [Config publish and rollback](config-publish-rollback.md)
- [Provider fallback demo](../examples/provider-fallback/)
- [Troubleshooting](troubleshooting.md)
- [CLI guide](cli.md)
- [Deployment guide](deployment.md)
- [Use cases](use-cases.md)
