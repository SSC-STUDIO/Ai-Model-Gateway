# AI Model Gateway Architecture

AI Model Gateway is a local, self-hosted LLM operations gateway. The runtime is organized as one operator entry point and three internal planes:

- `aigw`: local operations entry point and supervisor.
- `gatewayd`: data plane for model traffic.
- `controld`: control plane for Admin UI, Admin API, config lifecycle, probes, benchmarks, diagnostics, and updates.
- `telemetryd`: telemetry plane for event ingestion, projection, and read-side query data.

The default production shape is `aigw supervise`. Running individual daemons is useful for advanced debugging, but normal deployments should supervise the full bundle together.

## Runtime Topology

```text
external supervisor, service manager, container, or terminal
                         |
                         v
                  +--------------+
                  | aigw         |
                  | supervise    |
                  +------+-------+
                         |
         +---------------+---------------+
         |                               |
         v                               v
  +-------------+                 +-------------+
  | gatewayd    |<-- control RPC--| controld    |
  | data plane  |                 | control     |
  | :18080      |                 | plane :18081|
  +------+------+                 +------+------+
         |                               |
         | telemetry ingest RPC          | telemetry query RPC
         v                               v
  +---------------------------------------------+
  | telemetryd                                  |
  | event log, projection worker, query store   |
  | IPC only                                    |
  +---------------------------------------------+
```

The default bootstrap files wire those processes together:

| Daemon | Default listen or IPC | Main data directory |
| --- | --- | --- |
| `gatewayd` | HTTP `127.0.0.1:18080`, control IPC `.gateway-runtime/gateway-control.sock`, telemetry ingest IPC `.gateway-runtime/telemetry-ingest.sock` | `.gateway-runtime/gateway` |
| `controld` | HTTP `127.0.0.1:18081`, gateway IPC `.gateway-runtime/gateway-control.sock`, telemetry query IPC `.gateway-runtime/telemetry-query.sock` | `.gateway-runtime/control` |
| `telemetryd` | telemetry ingest IPC `.gateway-runtime/telemetry-ingest.sock`, telemetry query IPC `.gateway-runtime/telemetry-query.sock` | `.gateway-runtime/telemetry-migrated` |

On Linux and macOS, IPC uses Unix domain sockets. On Windows, the same transport abstraction uses named pipes.

## `aigw` Supervisor

`aigw` is the local operations command. It provides:

- `aigw supervise` to start `telemetryd`, `gatewayd`, and `controld`.
- `aigw doctor` for local config, manifest, and runtime checks.
- `aigw status` for gateway/control health probes.
- `aigw logs` for daemon log tailing.
- `aigw backup` for config and runtime-state backups.
- `aigw bundle build|verify` for release manifest workflows.
- `aigw update check|fetch|apply|rollback` for manifest-verified update flows.
- `aigw service print` for a systemd unit template.
- `aigw clients print|apply` for pointing local AI tools at the gateway.

Before supervision, `aigw` verifies that daemon binaries report the same product version. In strict manifest mode it also verifies the release manifest so deployments do not mix binaries from different bundles.

## Data Plane: `gatewayd`

`gatewayd` serves model traffic. It does not read `configs/config.yaml` directly. Instead, it executes compiled snapshots that `controld` applies over the gateway control RPC.

Public data-plane routes:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/-/health` | Data-plane health and optional provider detail |
| `GET` | `/v1/models` | OpenAI-compatible model list built from the active snapshot |
| `POST` | `/v1/chat/completions` | OpenAI Chat Completions-style traffic |
| `POST` | `/v1/messages` | Anthropic Messages-style traffic |
| `POST` | `/v1/responses` | OpenAI Responses-style traffic bridged through the runtime pipeline |

When `admin_proxy_url` is configured, `gatewayd` also proxies `/admin`, `/api/admin`, and admin static assets to `controld`. This preserves a single-port operator experience while keeping the control plane separate.

The data plane owns request execution concerns:

- model and provider selection from the active snapshot
- retries, fallback, and cooldown-aware routing
- request cache behavior
- provider health probes
- OpenAI, Anthropic Messages, and Responses-style bridge paths
- active request accounting
- telemetry event emission to `telemetryd`
- live pricing catalog refresh state

`gatewayd` is not a full clone of every OpenAI or Anthropic product API. Routes such as embeddings, images, audio, Assistants, Batch, and Realtime WebSocket are not data-plane promises unless implemented and documented separately.

## Control Plane: `controld`

`controld` owns the operator-facing API and the configuration lifecycle. It reads the authoring config, compiles it into runtime snapshots, publishes snapshots to `gatewayd`, stores revision history, records audit events, and exposes operational workflows to the Admin UI and CLI.

Common control-plane routes:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/-/health` | Control-plane health |
| `GET` | `/admin` | Admin UI shell |
| `GET` | `/api/admin/status` | Combined control, gateway, and telemetry status |
| `GET` | `/api/admin/runtime/status` | Runtime status with configured paths |
| `POST` | `/api/admin/runtime/preflight` | Gateway, snapshot, and telemetry preflight checks |
| `GET` | `/api/admin/config` | Current config view |
| `GET` | `/api/admin/config/history` | Config revision history |
| `POST` | `/api/admin/config/validate` | Validate draft config |
| `POST` | `/api/admin/config/preview` | Compile and summarize draft config |
| `POST` | `/api/admin/config/diff` | Diff revisions or draft config |
| `POST` | `/api/admin/config/update` | Store an updated config revision |
| `POST` | `/api/admin/config/reload` | Reload the authoring source and publish |
| `POST` | `/api/admin/config/publish` | Publish a selected revision |
| `POST` | `/api/admin/config/rollback` | Publish an older revision as rollback |
| `GET` | `/api/admin/overview` | Dashboard overview metrics from telemetry |
| `GET` | `/api/admin/telemetry` | Recent request events |
| `GET` | `/api/admin/timeseries` | Time-bucketed metrics |
| `GET` | `/api/admin/benchmark` | Model benchmark metrics |
| `GET` | `/api/admin/benchmark/runs` | Benchmark run list |
| `POST` | `/api/admin/benchmark/runs` | Start a benchmark run |
| `POST` | `/api/admin/probe/provider` | Diagnostic probe for one provider branch |
| `POST` | `/api/admin/probe/model` | Diagnostic probe for one model/provider path |
| `GET` | `/api/admin/audit` | Audit log tail |
| `GET` | `/api/admin/diagnostics` | Redacted diagnostics bundle |
| `GET` | `/api/admin/secrets/status` | Redacted secret presence checks |
| `GET` | `/api/admin/pricing/status` | Data-plane pricing catalog status |
| `POST` | `/api/admin/pricing/refresh` | Force a pricing refresh |
| `GET` | `/api/admin/update/status` | Local update status |
| `POST` | `/api/admin/update/check` | Check GitHub releases |
| `POST` | `/api/admin/update/fetch` | Download and verify a release bundle |
| `POST` | `/api/admin/update/apply` | Apply or dry-run a verified bundle |
| `POST` | `/api/admin/update/rollback` | Roll back the last local update |
| `GET` | `/metrics` | Small Prometheus-style control metrics surface |

Admin API requests use bearer-token or signed-cookie authentication when admin auth is configured. Viewer credentials are read-only. Browser write requests require same-origin validation; bearer-token automation can call write endpoints directly.

## Telemetry Plane: `telemetryd`

`telemetryd` receives events from `gatewayd`, persists an append-only event log, runs a projection worker, and serves read-side telemetry queries to `controld`.

Telemetry surfaces include:

- windowed request, success, failure, latency, and token metrics
- request event search and filters
- time-series buckets
- per-model and per-upstream distributions
- benchmark-specific synthetic traffic filters
- pricing economics summaries

The telemetry plane does not expose a user-facing HTTP server. `controld` is the query gateway for the Admin UI and CLI.

## Cross-Plane RPC

The internal RPC contracts are deliberately narrow:

| Direction | Contract | Main calls |
| --- | --- | --- |
| `controld -> gatewayd` | gateway control RPC | `ApplySnapshot`, `GetStatus`, `Drain`, `GetPricingStatus`, `RefreshPricing`, `RunBenchmarkCase` |
| `gatewayd -> telemetryd` | telemetry ingest RPC | `AppendBatch`, `Flush`, `Ping` |
| `controld -> telemetryd` | telemetry query RPC | `GetOverview`, `GetTelemetry`, `GetTimeSeries`, `GetModelBenchmark`, `Ping` |

`RunBenchmarkCase` uses the live gateway request pipeline with synthetic execution options. It supports `openai_chat_completions`, `anthropic_messages`, and `openai_responses` protocol values.

## Config Publish Model

The runtime separates authoring config from live execution:

```text
configs/config.yaml
        |
        v
controld revision state
        |
        v
compiled runtime snapshot
        |
        v
gatewayd ApplySnapshot
        |
        v
live data-plane routing
```

Key rules:

- `controld` is the owner of authoring config, revision state, publish records, rollback, and audit.
- `gatewayd` executes only the current compiled snapshot.
- `gatewayd` restores the last applied snapshot from its runtime data when available, but `controld` remains the source of revision truth and republishes after reconnects.
- rollback is implemented as a normal publish of an older revision, not as an out-of-band file copy.

See [Config Publish And Rollback](config-publish-rollback.md) for the operator workflow.

## State And Files

Typical local layout:

```text
configs/
  config.yaml
  gatewayd.json
  controld.json
  telemetryd.json

.gateway-runtime/
  gateway/
  control/
    audit.jsonl
    benchmark.db
    publisher-state.db
  telemetry-migrated/
    events.db
    query.db
  update/
  logs/
    gatewayd.log
    controld.log
    telemetryd.log
```

The exact telemetry directory name depends on the bootstrap config. The default repository config currently uses `.gateway-runtime/telemetry-migrated`.

## Startup Sequence

`aigw supervise` starts daemons in this order:

1. `telemetryd`
2. `gatewayd`
3. `controld`

`controld` connects to both internal RPC surfaces, restores or seeds publisher state, and publishes the current revision to `gatewayd`. If startup ordering or transient RPC timing leaves `gatewayd` without a snapshot, `controld` keeps trying to republish during the bootstrap window and after later reconnects.

## Operational Boundaries

- Ship `aigw`, `gatewayd`, `controld`, `telemetryd`, and `gateway-cli` as one manifest-verified bundle.
- Do not replace a single daemon binary during normal upgrades.
- Use `gateway-cli` or the Admin UI for config preview, diff, publish, rollback, probes, diagnostics, and benchmark workflows.
- Expose `gatewayd` to clients. Restrict `controld` to trusted operators.
- Treat `telemetryd` IPC paths and runtime databases as internal implementation details.

## Related Docs

- [Installation guide](installation.md)
- [Deployment guide](deployment.md)
- [Config publish and rollback](config-publish-rollback.md)
- [Provider fallback and health operations](provider-fallback-health.md)
- [Anthropic Messages endpoint](api-messages-endpoint.md)
- [CLI guide](cli.md)
- [Troubleshooting guide](troubleshooting.md)
