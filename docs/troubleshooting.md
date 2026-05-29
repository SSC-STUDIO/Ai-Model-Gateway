# Troubleshooting Guide

AI Model Gateway normally runs through `aigw supervise`. Start every incident
from the supervisor, then drill into the internal daemons only when needed:

- `aigw`
- `gatewayd`
- `controld`
- `telemetryd`

If you are running a local development copy, first confirm that another gateway
or legacy monolith is not already bound to `127.0.0.1:18080`. A port conflict
can make the data plane look broken even when the bundle is otherwise healthy.

## Fast Triage

Check the public health endpoints:

```bash
curl http://127.0.0.1:18080/-/health
curl http://127.0.0.1:18081/-/health
```

Check the supervisor's view of the runtime:

```bash
aigw status \
  -gateway-url http://127.0.0.1:18080 \
  -control-url http://127.0.0.1:18081 \
  -token "$ADMIN_TOKEN"

aigw doctor \
  -runtime-root .gateway-runtime \
  -config-dir configs \
  -manifest aigw-manifest.json

aigw logs -runtime-root .gateway-runtime -n 120
```

For systemd deployments:

```bash
systemctl status aigw --no-pager
journalctl -u aigw -n 200 --no-pager
```

For Docker Compose deployments:

```bash
docker compose -f deploy/docker-compose.yaml ps
docker compose -f deploy/docker-compose.yaml logs --tail=200
```

## Data Plane Health Fails

Symptom:

```bash
curl http://127.0.0.1:18080/-/health
```

returns a non-2xx response, stays in `starting`, or cannot connect.

Process and port checks:

```bash
# Linux
ps aux | grep gatewayd
ps aux | grep aigw
ss -tlnp | grep 18080

# Windows
tasklist | findstr gatewayd
netstat -ano | findstr 18080
```

Common causes:

- `gatewayd -listen` does not match the address you are probing.
- Another process already owns `127.0.0.1:18080`.
- `controld` cannot connect to `gatewayd`.
- No active snapshot has been published to `gatewayd` yet.
- `aigw supervise` rejected a mixed or invalid bundle.
- The runtime directory is not writable by the service account.

Next checks:

```bash
aigw logs -runtime-root .gateway-runtime gatewayd
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://127.0.0.1:18081/api/admin/runtime/status
```

If the bundle was recently updated, verify that all binaries and the manifest
come from the same release:

```bash
aigw bundle verify -root /opt/ai-model-gateway -manifest /opt/ai-model-gateway/aigw-manifest.json
```

## Control Plane Or Admin UI Is Unreachable

Symptoms:

```bash
curl http://127.0.0.1:18081/-/health
curl http://127.0.0.1:18081/admin
```

cannot connect or return an error.

Process and port checks:

```bash
# Linux
ps aux | grep controld
ss -tlnp | grep 18081

# Windows
tasklist | findstr controld
netstat -ano | findstr 18081
```

Check:

- The `controld.json` file exists in the active `-config-dir`.
- `controld` can read the authoring config, usually `config.yaml`.
- `controld -gateway` points at the same control socket or named pipe used by
  `gatewayd`.
- `controld -telemetry` points at the same telemetry query socket or named pipe
  used by `telemetryd`.
- The service account can read the admin frontend files if you are serving the
  packaged `web/admin/dist` assets.

Useful commands:

```bash
aigw logs -runtime-root .gateway-runtime controld
aigw doctor -runtime-root .gateway-runtime -config-dir configs -manifest aigw-manifest.json
```

## Telemetry Is Missing

Symptoms:

- Admin overview, telemetry, timeseries, or benchmark views show no data.
- `/api/admin/runtime/status` reports telemetry as disconnected.
- Request logs are present but pricing or usage metrics are incomplete.

Process checks:

```bash
# Linux
ps aux | grep telemetryd

# Windows
tasklist | findstr telemetryd
```

Check:

- `telemetryd -ingest` matches `gatewayd -telemetry`.
- `telemetryd -query` matches `controld -telemetry`.
- The telemetry data directory is writable.
- The runtime root has enough disk space.
- Traffic has actually passed through the gateway after startup.

Useful commands:

```bash
aigw logs -runtime-root .gateway-runtime telemetryd
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://127.0.0.1:18081/api/admin/runtime/status
```

## Config Changes Do Not Apply

Symptoms:

- You edited `config.yaml`.
- After restart, `/api/admin/config/history` does not show a new revision.
- The gateway still serves the previous routing or provider policy.

Expected behavior:

`controld` owns revision, publish, history, and rollback state. When
`publisher-state.db` exists, `controld` restores persisted revision history
instead of automatically seeding a new revision from YAML on every restart.

Preferred fixes:

- Use the Admin UI or Admin API publish flow to preview, diff, publish, and
  audit config changes.
- Use `gateway-cli config preview` and `gateway-cli config diff` before
  publishing.
- Use rollback if a published revision is wrong.

Reset-only option:

If you explicitly want to seed from YAML again, stop the service, back up the
control data directory, remove `publisher-state.db`, and restart. Do this only
when you understand that it resets persisted publish history for that runtime.

## Provider Calls Fail

Symptoms:

- Client requests return `502`, `503`, or upstream timeout errors.
- Admin provider health is degraded.
- Model probes fail.

Check provider status:

```bash
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://127.0.0.1:18081/api/admin/status
```

Check:

- Provider `base_url` is correct and reachable from the gateway host.
- Provider API keys are present in the environment or config source used by the
  running service.
- The configured public model name maps to the intended upstream model.
- Provider quota or rate limits are not exhausted.
- Corporate proxy, firewall, DNS, or TLS inspection settings are not blocking
  outbound traffic.

Run a focused probe from the CLI:

```bash
gateway-cli provider list
gateway-cli provider test <provider-id>
gateway-cli probe model <model> <provider-id>
```

## Logs And Runtime Files

`aigw supervise` writes internal daemon logs under the runtime root:

```text
.gateway-runtime/
|-- telemetry/
|-- gateway/
|-- control/
`-- logs/
    |-- telemetryd.log
    |-- gatewayd.log
    `-- controld.log
```

Important files:

- `control/publisher-state.db`
- telemetry SQLite files under the telemetry data directory
- `logs/gatewayd.log`
- `logs/controld.log`
- `logs/telemetryd.log`

`aigw` supervisor output usually lives in the host service manager:

- systemd journal for Linux services
- Docker Compose logs for containers
- NSSM, Windows Service Wrapper, or Task Scheduler logs for Windows services
- `deploy/start.sh` logs when using the helper script directly

## Service Wrapper Problems

The repository provides one default Linux systemd unit for `aigw supervise`:

```text
deploy/aigw.service
```

Production service managers should normally wrap only this command shape:

```bash
aigw supervise \
  -runtime-root <runtime-root> \
  -config-dir <config-dir> \
  -bin-dir <bin-dir> \
  -strict-manifest=true \
  -manifest <manifest-path>
```

Run `gatewayd`, `controld`, and `telemetryd` directly only for advanced
debugging. Managing those daemons as separate production services makes updates
and rollback easier to get wrong.

For Windows, wrap `aigw.exe supervise` with NSSM, Windows Service Wrapper, Task
Scheduler, or an equivalent host management tool.

## Automatic Recovery Behavior

Control plane:

- `controld` reconnects to `gatewayd` after RPC disconnects.
- When gateway RPC is reachable but the data plane is not ready, `controld`
  retries publishing the current revision with throttling.
- The default readiness republish interval is controlled by
  `gateway_readiness_republish_min_interval_sec`.

Data plane:

- `gatewayd` attempts to restore the last successfully applied snapshot from
  disk on startup.
- Providers with multiple API keys can move away from keys that return
  authentication failures and later try cooled-down keys again.
- Runtime status exposes recent auto-remediation details, including
  `last_auto_remediation_reason` and `last_auto_remediation_at`.

Process supervision:

- Use systemd, Docker, a supervisor, or a Windows service wrapper with an
  on-failure restart policy.
- The repository's `ensure-gateway-running.ps1` is intended for local WSL
  helper workflows, not as the primary production service manager.

## Information To Include In A GitHub Issue

When opening an issue, include:

- The `aigw supervise` command or service unit.
- Output from `aigw doctor`.
- Output from `aigw status`.
- Relevant `.gateway-runtime/logs/` files.
- Redacted `config.yaml`.
- Whether `control/publisher-state.db` exists.
- OS, architecture, deployment mode, and release version or commit SHA.
- Any custom socket, named pipe, port, proxy, or container settings.
