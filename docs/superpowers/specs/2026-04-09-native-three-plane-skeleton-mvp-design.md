# AI Model Gateway Native Three-Plane Skeleton MVP Design

**Date**: 2026-04-09  
**Status**: Draft for review  
**Supersedes as target architecture**: [2026-04-06-v2-completion-design.md](C:/Users/96152/My-Project/Active/Software/AI-Model-Gateway/docs/superpowers/specs/2026-04-06-v2-completion-design.md)

---

## 1. Overview

### 1.1 Goal

Rebuild the system as a native three-plane architecture with hard process boundaries:

- `gatewayd`: pure data plane
- `controld`: pure control plane
- `telemetryd`: pure telemetry plane

The target is not an incremental cleanup of the current monolithic runtime. The current single-process runtime, admin sidecar, config watcher, runtime hook injection, and local telemetry/query coupling are migration sources only, not target architecture.

### 1.2 Continuity Rules

The only continuity preserved from the current system is:

- legacy YAML can be imported into the new control configuration model
- legacy SQLite telemetry can be imported into the new telemetry event/query model

No other internal compatibility layer is preserved as a design goal.

### 1.3 Scope Of This Spec

This spec defines the first skeletal MVP of the native architecture.

It does:

- define the three daemons and their boundaries
- define the native control config model
- define the immutable gateway runtime snapshot model
- define the local binary IPC contracts
- define the telemetry event and projection model
- define explicit legacy migration tools
- define repository reorganization and staged cutover

It does not:

- preserve the current admin/gateway/runtime internal layering
- preserve the current `/api/admin/v2/*` or mixed admin/runtime semantics
- preserve runtime YAML watching as a long-term mechanism
- preserve automatic legacy import during gateway startup
- preserve full public endpoint parity in the first skeletal cut

### 1.4 First-Cut Product Boundary

The first skeletal MVP intentionally narrows the public data-plane surface.

`gatewayd` first-cut public endpoints:

- `GET /-/health`
- `GET /v1/models`
- `POST /v1/chat/completions`

The first skeletal MVP does not treat `responses`, admin UI embedding, pricing dashboards, config history, or benchmark pages as gateway concerns. Those either move to `controld` or remain out of scope until later phases.

This is a deliberate tradeoff. The first target is a correct native substrate, not feature parity.

---

## 2. Design Summary

### 2.1 Core Decisions

1. `controld` is the only configuration source of truth.
2. `save` and `publish` are separate operations.
3. `gatewayd` never reads authoring YAML directly.
4. `gatewayd` only executes an immutable compiled runtime snapshot.
5. `telemetryd` is the only owner of telemetry append, projection, and query state.
6. Internal high-frequency links use local binary RPC, not HTTP/JSON.
7. Legacy migration is explicit and offline, not hidden in runtime startup.
8. Audit belongs to `controld`, not `telemetryd`.

### 2.2 MVP Technical Choices

- Internal transport:
  - Linux: Unix domain sockets
  - Windows: named pipes
- Internal RPC:
  - `net/rpc` + `gob` for the skeletal MVP
- Telemetry fact model:
  - one canonical event type: `gateway.attempt.completed`
- Gateway runtime state:
  - full immutable snapshot replacement
- Config source:
  - one control-owned human-editable cluster manifest YAML
- Runtime publication:
  - immutable compiled snapshot + atomic active pointer switch

### 2.3 Primary Tradeoff

This design chooses architectural correctness over immediate compatibility.

The price is:

- first-cut public endpoint coverage is smaller
- first-cut telemetry/event vocabulary is smaller
- some current fields and behaviors are explicitly dropped from the new runtime path

The gain is:

- real plane separation
- deterministic publish semantics
- explicit telemetry ownership
- simpler cutover and simpler future extension

---

## 3. System Shape

### 3.1 Daemons

#### `gatewayd`

Responsibilities:

- public inference ingress
- request authentication
- model resolution
- provider selection
- protocol adapter execution
- retry/failover execution
- upstream network transport
- streaming/non-streaming response return
- asynchronous telemetry event emission
- read-only runtime status exposure to `controld`

Non-responsibilities:

- YAML parsing
- config history
- config save/rollback
- admin UI
- benchmark and economics query
- local telemetry query DB writes

#### `controld`

Responsibilities:

- authoring config source of truth
- config validation
- runtime snapshot compilation
- config revision history
- publish records
- rollback records
- audit log
- admin auth/session/RBAC
- admin API
- admin UI
- read-only query fan-out to `telemetryd`
- publish/query fan-out to `gatewayd`

Non-responsibilities:

- handling inference traffic
- direct mutation of gateway runtime objects
- direct scanning of telemetry storage

#### `telemetryd`

Responsibilities:

- local ingest RPC for gateway event batches
- append-only event log
- projection workers
- query store maintenance
- telemetry read RPC for `controld`
- legacy telemetry import
- optional pricing/economics enrichment after the skeletal MVP

Non-responsibilities:

- public admin auth
- config source of truth
- request routing
- direct authoring of control metadata

### 3.2 External Surfaces

Public:

- `gatewayd`
  - `GET /-/health`
  - `GET /v1/models`
  - `POST /v1/chat/completions`
- `controld`
  - `GET /admin`
  - `GET /admin/*`
  - `GET|POST|PUT /api/admin/*`

Internal-only:

- `gateway-control`
  - `controld -> gatewayd`
- `telemetry-ingest`
  - `gatewayd -> telemetryd`
- `telemetry-query`
  - `controld -> telemetryd`

`telemetryd` exposes no direct user-facing HTTP surface in the skeletal MVP.

### 3.3 Static Bootstrap Vs Dynamic Runtime State

Each daemon has a small static bootstrap config used only for process-local concerns:

- local IPC listen path
- log path
- service identity
- optional default data directory

Everything operationally meaningful to routing and execution belongs to control-owned dynamic config and compiled runtime snapshots.

This avoids rebuilding a second hidden authoring system in daemon-local files.

---

## 4. Native Control Configuration Model

### 4.1 Source Of Truth

The source of truth is one control-owned cluster manifest YAML.

This is the human-editable configuration model. It is not the same thing as the runtime snapshot consumed by `gatewayd`.

Top-level shape:

```yaml
version: 1

cluster:
  name: local-gateway

control:
  http:
    listen: 127.0.0.1:18081
  auth:
    bootstrap_token: "..."
    cookie_signing_key: "..."
    tokens: []

gateway:
  ingress:
    listen: 127.0.0.1:18080
    read_timeout_ms: 30000
    write_timeout_ms: 60000
    idle_timeout_ms: 120000
    max_body_bytes: 104857600
  contract:
    public_api: openai_chat_completions
  routing_policy:
    max_retries: 2
    retry_backoff:
      initial_ms: 3000
      max_ms: 30000
    health:
      enabled: true
      interval_sec: 10
      timeout_ms: 2000
      path: /v1/models
    failure_policy:
      threshold: 20
      cooldown_sec: 60
      quota_recovery_interval_min: 30
    retry:
      infinite_on_error: false
      status_codes: [408, 429]
      status_code_min: 500
      message_keywords: []
  providers: []

telemetry:
  ingest:
    batch_size: 256
    flush_interval_ms: 100
  storage:
    event_log_path: data/telemetry/events.db
    query_store_path: data/telemetry/query.db
    retention_days: 30
  pricing:
    cache_path: data/pricing-cache.json
    refresh_interval_hours: 12
```

### 4.2 Control-Only Fields

These fields are control-plane-only and must never be part of the gateway runtime snapshot:

- admin auth and sessions
- cookie signing keys
- revision history metadata
- publish records
- rollback metadata
- audit retention rules
- import job state

### 4.3 Gateway-Authoring Fields

These are authored in the control config and compiled into the gateway runtime snapshot:

- ingress listener settings
- public contract selection
- provider definitions
- model mappings
- routing and retry policy
- health probe policy

### 4.4 Provider Authoring Model

Each provider entry is explicit and capability-declared.

Minimum authored shape:

```yaml
providers:
  - id: openai-primary
    enabled: true
    protocol: openai_chat_completions
    base_url: https://api.openai.com/v1
    api_key: sk-...
    models:
      - gpt-4.1
      - gpt-4o-mini
    weight: 1
    timeout_ms: 30000
    same_retries: 1
    provider_class: quota_limited
    headers: {}
```

The authoring model is explicit about protocol. There is no runtime capability guessing.

### 4.5 Validation Rules

Validation must fail fast and precisely.

Minimum requirements:

- every provider has a stable `id`
- every enabled provider has a known protocol
- every enabled provider has at least one model
- every provider has usable credentials or a resolvable credential reference
- the selected public contract is supported by at least one enabled compiled route
- all emitted validation errors include exact field paths

No unsupported field may be silently dropped during native authoring or legacy import.

---

## 5. Runtime Snapshot Model

### 5.1 Snapshot Role

The runtime snapshot is a compiled immutable execution artifact for `gatewayd`.

It is:

- machine-oriented
- fully resolved
- versioned
- deterministic
- free of authoring-only metadata

It is not:

- a hand-edited config
- a merged runtime object graph
- a live mutable state container

### 5.2 Snapshot Shape

```yaml
meta:
  snapshot_id: snap_20260409_000001
  schema_version: 1
  revision_id: rev_20260409_000001
  generated_at: 2026-04-09T21:00:00Z

ingress:
  listen: 127.0.0.1:18080
  read_timeout_ms: 30000
  write_timeout_ms: 60000
  idle_timeout_ms: 120000
  max_body_bytes: 104857600

contract:
  public_api: openai_chat_completions
  enabled_routes:
    - POST /v1/chat/completions
    - GET /v1/models
    - GET /-/health

providers:
  - provider_id: openai-primary
    protocol_adapter: openai_chat_completions
    base_url: https://api.openai.com/v1
    credentials:
      kind: bearer
      value: sk-...
    headers: {}
    model_table:
      - public_model: gpt-4.1
        upstream_model: gpt-4.1
      - public_model: gpt-4o-mini
        upstream_model: gpt-4o-mini
    capability_table:
      supports_chat_completions: true
      supports_streaming: true
      usage_accounting: openai_usage
      error_classifier: openai_error
    execution_policy:
      enabled: true
      weight: 1
      timeout_ms: 30000
      same_retries: 1
      provider_class: quota_limited

routing_policy:
  max_retries: 2
  retry_backoff:
    initial_ms: 3000
    max_ms: 30000
  health:
    enabled: true
    interval_sec: 10
    timeout_ms: 2000
    path: /v1/models
  failure_policy:
    threshold: 20
    cooldown_sec: 60
    quota_recovery_interval_min: 30
  retry:
    infinite_on_error: false
    status_codes: [408, 429]
    status_code_min: 500
    message_keywords: []

telemetry_emit:
  channel: telemetry-ingest
  batching:
    max_batch_size: 256
    flush_interval_ms: 100
```

### 5.3 Omitted From Snapshot

The first native snapshot explicitly omits:

- admin auth
- admin UI metadata
- config history
- publish history
- audit policy
- pricing cache configuration
- telemetry query-store configuration
- legacy compat settings
- decorative legacy fields that are not materially executed

In particular, the current monolithic fields below are not carried forward unless they acquire native behavior later:

- `routing.strategy`
- `sticky_sessions.ttl_sec`
- `failure_policy.passthrough_after_sec`
- `compat.fallback.detect_repetition`

### 5.4 Snapshot Swap Semantics

`gatewayd` never mutates live runtime policy from incremental config callbacks.

Instead:

1. load full snapshot artifact
2. validate snapshot schema/hash
3. build a new immutable runtime graph from the snapshot
4. atomically swap active runtime graph pointer
5. report `loaded_snapshot_id`

The active runtime graph is always associated with exactly one snapshot ID.

---

## 6. Control Metadata And Publish Semantics

### 6.1 Core Data Sets

`controld` persists these immutable or append-only records:

- `config_revisions`
- `compiled_snapshots`
- `publishes`
- `audit_events`

and exactly one mutable pointer:

- `active_pointer`

### 6.2 Config Revisions

Minimum fields:

- `revision_id`
- `parent_revision_id`
- `created_at`
- `created_by`
- `source_config`
- `source_hash`
- `validation_ok`
- `validation_errors`

Saving a config creates a revision. Saving does not publish.

### 6.3 Compiled Snapshots

Minimum fields:

- `snapshot_id`
- `revision_id`
- `compiled_at`
- `schema_version`
- `runtime_payload`
- `runtime_hash`

Compilation must be deterministic for the same revision and compiler version.

### 6.4 Publish Records

Minimum fields:

- `publish_id`
- `revision_id`
- `snapshot_id`
- `requested_at`
- `requested_by`
- `previous_publish_id`
- `kind`
- `status`
- `reason`
- `error`
- `observed_at`

`kind` values:

- `publish`
- `rollback`

`status` values:

- `staged`
- `observed`
- `failed`

### 6.5 Atomicity Boundary

The atomicity boundary is the active pointer switch.

Publish flow:

1. load selected revision
2. normalize
3. validate
4. compile immutable runtime snapshot
5. persist snapshot record
6. persist publish record with status `staged`
7. atomically switch `active_pointer` to the new publish/snapshot
8. notify `gatewayd`
9. wait for `gatewayd.loaded_snapshot_id == snapshot_id`
10. mark publish `observed`

The active pointer must be switched with temp-file plus rename semantics or equivalent DB transaction semantics.

No publish may be represented as “successful” before the atomic pointer change.

No publish may be represented as “observed” before `gatewayd` confirms the load.

### 6.6 Rollback

Rollback is not a filesystem restore operation.

Rollback flow:

1. select prior publish or prior revision
2. compile or reuse the associated snapshot
3. create a new publish row with `kind=rollback`
4. switch the active pointer to the rollback target
5. wait for `gatewayd` observation
6. mark new publish row `observed`

Rollback is therefore a first-class publish event, not a special hidden path.

### 6.7 Audit Ownership

Audit belongs to `controld`.

Audit event types in the skeletal MVP:

- login
- logout
- revision_save
- revision_validate
- publish_request
- publish_observed
- publish_failed
- rollback_request
- rollback_observed
- import_config
- import_telemetry

Audit must not be stored in telemetry event/query storage in the target design.

---

## 7. Gatewayd Execution Model

### 7.1 Execution Graph

The data-plane execution graph is precompiled from the snapshot:

1. ingress
2. auth
3. model resolution
4. provider selection
5. protocol adapter
6. retry/failover
7. upstream transport
8. response encoding
9. async telemetry emit

The skeletal MVP keeps this graph deliberately small.

### 7.2 Allowed Hot-Path Operations

Allowed:

- read-only in-memory runtime structures
- local bounded async enqueue for telemetry emit
- network I/O
- stream copy/encoding

Disallowed:

- YAML parsing
- filesystem config lookup
- config history lookup
- admin DB query
- pricing query
- runtime reflection injection
- capability guessing

### 7.3 Public API Contract

First-cut contract:

- `GET /-/health`
- `GET /v1/models`
- `POST /v1/chat/completions`

The first native `gatewayd` does not implement:

- `/admin`
- `/api/admin/*`
- `responses`
- files/audio/images/moderations
- in-gateway benchmark/economics surfaces
- config mutation surfaces

### 7.4 Protocol Adapters

The skeletal MVP keeps protocol adapters explicit and compile-time declared.

Supported in MVP:

- `openai_chat_completions`

Reserved for later:

- `openai_responses`
- `anthropic_messages`
- provider-specific adapters

There is no runtime fallback from one public API family to another in the first skeletal cut.

### 7.5 Telemetry Emit Policy

`gatewayd` emits telemetry asynchronously in bounded batches.

Canonical event:

- `gateway.attempt.completed`

Canonical payload fields:

- request id
- timestamp
- path
- requested model
- effective model
- provider id
- route mode
- status code
- latency
- attempts
- prompt tokens
- cached prompt tokens
- completion tokens
- stream flag
- error text

This is attempt-scoped, not final-request-scoped.

That preserves the current retry fact model and keeps the first event system small.

### 7.6 Backpressure Rule

The default skeletal policy is:

- inference must not block on telemetry durability
- local emit queue is bounded
- when downstream is unavailable or the queue is full, `gatewayd` increments drop counters and continues serving inference

This matches the current best-effort behavior more closely than introducing synchronous telemetry writes.

If later durability guarantees change, they must be an explicit architecture revision.

---

## 8. Telemetryd Storage And Query Model

### 8.1 Layers

`telemetryd` owns two storage layers:

1. append-only `event_log`
2. read-optimized `query_store`

The event log is the fact source.

The query store is a projection layer.

### 8.2 Canonical Event Set

The skeletal MVP defines exactly one gateway event family:

- `gateway.attempt.completed`

The event envelope includes:

- `event_id`
- `event_type`
- `schema_version`
- `source_service`
- `source_instance`
- `emitted_at`
- `imported`
- `payload`

No separate error event is required in the first cut because recent-error views can be derived from request facts.

### 8.3 Query Projections

Required projections:

- `request_facts`
  - one flattened row per completed attempt
- `agg_time_buckets`
  - time-bucketed aggregates for summary and charts
- optional later:
  - route economics
  - pricing enrichment
  - benchmark materializations

### 8.4 Audit Separation

`telemetryd` does not own control audit in the target architecture.

If audit parity is temporarily needed during cutover, it must be treated as transitional only and not baked into the native telemetry model.

### 8.5 Read-Only Query Surface

High-level RPCs for `controld`:

- `GetOverview(window_set)`
- `GetTelemetry(window_hours, limit)`
- `GetTimeSeries(window_hours, bucket_minutes)`
- `GetModelBenchmark(window_hours, models[])`
- `Ping()`

Optional later:

- `GetRouteUsage`
- economics-specific query RPCs

The query RPC stays high-level in the skeletal MVP to minimize UI churn inside `controld`.

### 8.6 Pricing And Economics

The target owner is `telemetryd`.

The skeletal MVP does not require complete pricing/economics parity.

Allowed first-cut behavior:

- route-level benchmark cost may be `null` or `0`
- economics surfaces may be absent or degraded
- pricing catalog refresh may be deferred

What is not allowed is rebuilding pricing/economics logic back into `gatewayd`.

---

## 9. Internal IPC Contracts

### 9.1 Transport

- Linux: Unix domain sockets
- Windows: named pipes
- RPC: `net/rpc` + `gob`

This is the preferred skeletal choice because it is:

- binary
- local-only
- codegen-free
- repository-compatible

### 9.2 Endpoints

Separate local endpoints:

- `gateway-control`
- `telemetry-ingest`
- `telemetry-query`

Separation is deliberate so permissions and trust boundaries are explicit.

### 9.3 Gateway-Control RPC

`controld -> gatewayd`

Required methods:

- `ApplySnapshot(snapshot_bytes, snapshot_id) -> applied, active_snapshot_id, error`
- `GetStatus() -> active_snapshot_id, readiness, active_requests, listener, provider_health`
- `Drain() -> ok`

`gatewayd` must not expose config edit semantics over this channel.

### 9.4 Telemetry-Ingest RPC

`gatewayd -> telemetryd`

Required methods:

- `AppendBatch(events, batch_id) -> accepted, dropped, high_watermark`
- `Flush() -> ok`
- `Ping() -> version, server_time`

The gateway hot path writes to a local batcher, not directly to per-request RPC calls.

### 9.5 Telemetry-Query RPC

`controld -> telemetryd`

Required methods:

- `GetOverview(...)`
- `GetTelemetry(...)`
- `GetTimeSeries(...)`
- `GetModelBenchmark(...)`
- `Ping()`

This channel is read-only by contract.

### 9.6 ACL And Service Rules

Linux:

- socket paths under `/run/...` or `$XDG_RUNTIME_DIR`
- stale socket cleanup on startup
- strict socket and directory permissions

Windows:

- named pipes via `go-winio`
- byte-stream mode
- pipe ACLs restricted to intended local service accounts

All RPC calls require timeouts and reconnect/backoff logic.

---

## 10. Legacy Migration

### 10.1 Rules

Migration is explicit and offline.

There is no hidden runtime-side automatic conversion in the target design.

### 10.2 `migrate-config`

Purpose:

- import legacy YAML
- emit native control config YAML
- emit migration report

Required modes:

- `dry-run`
- `apply`
- `verify`

Required report fields:

- source path
- destination path
- mapping status: `mapped | generated | defaulted | unsupported`
- exact validation errors
- final output hash

No unsupported or ambiguous field may be silently ignored.

### 10.3 `migrate-telemetry`

Purpose:

- import legacy SQLite telemetry
- normalize to canonical event stream
- rebuild projections
- emit import report

Required behavior:

- repeatable
- resumable
- explicit checkpointing
- explicit checksum/count validation

Required report fields:

- source DB identity
- source row counts
- destination event count
- destination projection counts
- min/max timestamps
- duplicate detection
- ambiguous error rows
- success/status mismatches

### 10.4 Legacy Input Reality

The current repository already shows that documented automatic legacy import is not a reliable runtime contract.

The new architecture must treat legacy import as a first-class tooling concern, not a side effect of daemon startup.

### 10.5 Migration Acceptance

Config migration succeeds only if:

- native control config validates
- all fields are accounted for
- no ambiguous field is silently dropped

Telemetry migration succeeds only if:

- request-level identity and analytics facts are preserved
- projections are rebuilt from imported facts
- counts and checksums match the reported expectations

---

## 11. Repository Reorganization

### 11.1 New Entry Points

```text
cmd/
  gatewayd/
  controld/
  telemetryd/
  migrate-config/
  migrate-telemetry/
  clusterctl/
```

### 11.2 New Internal Layout

```text
internal/
  gateway/
    api/
    pipeline/
    routing/
    transport/
    health/
  control/
    api/
    auth/
    config/
    history/
    publish/
    audit/
    frontend/
    clients/
  telemetry/
    ingest/
    eventlog/
    project/
    query/
    pricing/
  contracts/
    gatewaycontrol/
    telemetryingest/
    telemetryquery/
  shared/
    config/
    httpserver/
    observability/
    pathsecurity/
    cli/
    i18n/
```

### 11.3 Harvest Vs Retire

Harvest:

- gateway execution logic from current `internal/app/*`
- config parsing/security utilities
- HTTP server utilities
- path security and observability helpers
- telemetry query/storage logic that can be rehomed under `telemetry/`

Retire as target architecture:

- `internal/app/run.go`
- `internal/app/admin_mount.go`
- `internal/runtime/runtime.go`
- `internal/infra/runtime/*`
- embedded admin sidecar assumptions
- monolithic `core.Config` as the final authoring/runtime contract

### 11.4 Compatibility Launcher

If a temporary compatibility launcher is needed, it lives only in the migration period and only in compatibility entry points.

It is not part of the target architecture.

---

## 12. Staged Cutover

### Phase 1: Substrate

- create new package roots
- create `cmd/gatewayd`, `cmd/controld`, `cmd/telemetryd`
- introduce contracts package and local IPC transport
- keep current monolith buildable during extraction

### Phase 2: Gateway Extraction

- move gateway ingress, routing, transport, health, and pipeline into `internal/gateway/*`
- replace local SQLite telemetry sink with telemetry ingest client
- remove admin mount from gateway composition

### Phase 3: Control Extraction

- move admin API/UI/auth/history/publish/audit into `internal/control/*`
- introduce revision/snapshot/publish metadata model
- make `controld` serve `/admin` and `/api/admin/*`

### Phase 4: Telemetry Extraction

- move telemetry append/projection/query into `internal/telemetry/*`
- introduce append-only event log and projections
- switch `controld` queries to telemetry RPC

### Phase 5: Legacy Tooling And Default Flip

- implement `migrate-config`
- implement `migrate-telemetry`
- implement `clusterctl`
- switch operational documentation and verification assets to the three-daemon model
- retire monolithic runtime as primary path

---

## 13. Testing And Acceptance

### 13.1 Gateway Tests

- snapshot load and full runtime graph replacement
- health endpoint behavior
- model listing from compiled snapshot
- chat-completions request routing
- retry and failover execution
- telemetry emit under downstream outage

### 13.2 Control Tests

- revision save/validate
- compile deterministic snapshot
- publish pointer atomic switch
- publish observed transition
- rollback as new publish event
- audit event generation

### 13.3 Telemetry Tests

- append-only event ingest
- projection rebuild
- summary query correctness
- benchmark query correctness
- timeseries query correctness
- replay from imported events

### 13.4 Migration Tests

- legacy YAML import success with exact field reports
- legacy YAML failure with exact field paths
- legacy SQLite import with matching counts
- rerun/resume behavior
- checksum/report verification

### 13.5 Cross-Platform Tests

- Linux Unix socket startup/shutdown
- Windows named pipe startup/shutdown
- separate service install/start/stop behavior
- degraded startup when one daemon is missing

### 13.6 Acceptance Criteria

The skeletal MVP is accepted when all of the following are true:

1. `gatewayd`, `controld`, and `telemetryd` start independently.
2. `gatewayd` serves inference using only a compiled snapshot.
3. `controld` can save, validate, publish, and rollback without mutating gateway memory directly.
4. `telemetryd` receives canonical gateway attempt events and answers read-only queries.
5. legacy config import is explicit and field-precise.
6. legacy telemetry import is explicit, resumable, and verifiable.
7. no target architecture path depends on the monolithic admin/runtime/telemetry composition.

---

## 14. Open Issues

These questions do not block the skeletal spec, but must be revisited before later phases:

- whether later public data-plane expansion should prioritize `responses` or additional chat-family routes
- whether gateway credentials remain inline in snapshot artifacts or move to local secret references
- whether telemetry backpressure remains best-effort permanently or later gains bounded disk spooling
- how pricing/economics mature after the first telemetry cut
- whether multi-gateway fan-out is introduced in the next control-plane revision

---

## 15. Final Position

The native three-plane skeletal MVP is intentionally smaller than the current feature surface.

That is correct.

The first milestone is not “feature parity with a cleaner repo.” The first milestone is:

- one real control plane
- one real data plane
- one real telemetry plane
- explicit publish semantics
- explicit telemetry ownership
- explicit migration tools

Only after those boundaries exist should broader public protocol coverage and richer analytics be added back.
