# CLI Guide

AI Model Gateway has two operator-facing command line tools:

- `aigw` is the local operations entrypoint. Use it to supervise the three daemons, inspect local runtime state, read logs, build and verify release manifests, back up state, apply updates, and generate client configuration snippets.
- `gateway-cli` is the remote control-plane CLI. It talks to `controld` over the Admin API and is used for config inspection, config preview/diff, health checks, audit logs, probes, replay, diagnostics, provider checks, telemetry, publish history, rollback, and verification benchmarks.

`gatewayd`, `controld`, and `telemetryd` still exist as standalone binaries, but they are daemon internals. Normal deployments should wrap `aigw supervise` with a service manager instead of running each daemon by hand.

## Operating Model

- Use `aigw supervise` as the production service command.
- Keep `aigw`, `gatewayd`, `controld`, and `telemetryd` from the same release bundle.
- `aigw supervise` starts daemons in this order: `telemetryd`, `gatewayd`, then `controld`.
- The `-config` flag on daemon binaries points to bootstrap JSON such as `configs/gatewayd.json`. It is not the authoring YAML.
- `controld -authoring-config` points to the human-authored YAML, usually `configs/config.yaml`.
- `gatewayd` does not read YAML directly. It serves snapshots published by `controld`.
- The default local runtime directory is `.gateway-runtime/`.
- The default local control plane URL is `http://127.0.0.1:18081`.
- The default local data plane URL is `http://127.0.0.1:18080`.

## `aigw`

### Version

```bash
./dist/aigw version
```

Prints the local operations binary version and platform.

### Supervise

```bash
./dist/aigw supervise \
  -runtime-root .gateway-runtime \
  -config-dir configs \
  -bin-dir ./dist \
  -manifest aigw-manifest.json
```

Common flags:

- `-runtime-root`: runtime state root, default `.gateway-runtime`.
- `-config-dir`: directory containing `gatewayd.json`, `controld.json`, `telemetryd.json`, and usually `config.yaml`; default `configs`.
- `-bin-dir`: directory containing `gatewayd`, `controld`, and `telemetryd`. If omitted, `aigw` checks its own directory, then `bin/`, then `PATH`.
- `-manifest`: release manifest path, default `aigw-manifest.json`.
- `-strict-manifest`: fail when the manifest is missing.
- `-startup-timeout`: startup health timeout, default `30s`.

Before starting daemons, `aigw supervise` verifies the bundle manifest when available and checks that each daemon reports the same product version as `aigw`.

Daemon logs are written to:

```text
.gateway-runtime/logs/telemetryd.log
.gateway-runtime/logs/gatewayd.log
.gateway-runtime/logs/controld.log
```

### Doctor

```bash
./dist/aigw doctor -runtime-root .gateway-runtime -config-dir configs -manifest aigw-manifest.json
./dist/aigw doctor -format json
```

Checks local bootstrap JSON files, the release manifest, and expected IPC socket files. Use this before service startup, bundle swaps, or support handoff.

### Status

```bash
./dist/aigw status
./dist/aigw status \
  -control-url http://127.0.0.1:18081 \
  -gateway-url http://127.0.0.1:18080 \
  -token "$ADMIN_TOKEN" \
  -format json
```

Queries control-plane runtime status and data-plane health. `-token` defaults to `ADMIN_TOKEN`.

### Logs

```bash
./dist/aigw logs
./dist/aigw logs -n 200 gatewayd
./dist/aigw logs -runtime-root .gateway-runtime telemetryd controld
```

Reads daemon logs from `.gateway-runtime/logs`. If no daemon names are supplied, it prints `telemetryd`, `gatewayd`, and `controld` logs.

### Backup

```bash
./dist/aigw backup -runtime-root .gateway-runtime -config-dir configs
./dist/aigw backup -out .gateway-runtime/backups/manual-001
```

Backs up config files, control/gateway/telemetry runtime state, migrated telemetry state, and the release manifest when those paths exist. Without `-out`, backups are written under `.gateway-runtime/backups/<utc timestamp>`.

### Bundle

```bash
./dist/aigw bundle build -root . -out aigw-manifest.json
./dist/aigw bundle build -root . -out aigw-manifest.json -git-commit "$GIT_SHA"
./dist/aigw bundle verify -root . -manifest aigw-manifest.json
./dist/aigw bundle verify -format json
```

The manifest records release identity and bundle integrity, including product version, git commit, platform, binary hashes, Admin UI dist hash, snapshot schema, IPC contracts, migration requirements, and default config paths.

### Update

```bash
./dist/aigw update check
./dist/aigw update check -repo SSC-STUDIO/Ai-Model-Gateway -format json
./dist/aigw update fetch -out .gateway-runtime/update/downloads
./dist/aigw update apply -bundle /path/to/bundle -install-dir /opt/ai-model-gateway
./dist/aigw update apply -bundle /path/to/bundle -install-dir /opt/ai-model-gateway -dry-run
./dist/aigw update rollback -install-dir /opt/ai-model-gateway
```

`update check` and `update fetch` use GitHub releases. `update apply` verifies the target bundle, backs up the existing install payload, then copies the new payload. Rollback restores the latest recorded update backup.

### Service

```bash
./dist/aigw service print
```

Prints the default systemd unit. On Windows, wrap `aigw.exe supervise` with NSSM, Windows Service Wrapper, or your normal service runner.

### Clients

```bash
./dist/aigw clients print
./dist/aigw clients print -gateway-url http://127.0.0.1:18080 -tools codex,claude-code
./dist/aigw clients apply -tools codex,claude-code,openclaw -api-key "$GATEWAY_API_KEY" -dry-run
./dist/aigw clients apply -tools all -openclaw-model gpt-4o
```

`clients print` shows shell snippets and planned file changes. `clients apply` can update local Codex, Claude Code, and OpenClaw configuration files, with backups enabled by default.

Supported client flags:

- `-config-dir`: directory containing `gatewayd.json`, default `configs`.
- `-gateway-url`: explicit data-plane base URL. This overrides `gatewayd.json`.
- `-tools`: comma-separated `codex`, `claude-code`, `openclaw`, or `all`.
- `-api-key`: gateway API key. If omitted, the command checks `GATEWAY_CLIENT_API_KEY`, `GATEWAY_API_KEY`, then `OPENAI_API_KEY`.
- `-backup`: back up existing files before writes, default `true`.
- `-dry-run`: with `apply`, print actions without writing files.
- `-openclaw-model`: public model id for OpenClaw, default `gpt-4o`.
- `-openclaw-set-primary`: set the OpenClaw default primary model, default `true`.

## `gateway-cli`

`gateway-cli` talks to `controld` over the Admin API.

Global options:

- `-server url`: control-plane URL, default `http://127.0.0.1:18081`.
- `-token token`: Admin API token, default `ADMIN_TOKEN`.
- `-format text|json|csv`: output format, default `text`. CSV is only supported for benchmark telemetry commands.

Examples:

```bash
./dist/gateway-cli health
./dist/gateway-cli health quick
./dist/gateway-cli status
./dist/gateway-cli status watch 10s
./dist/gateway-cli validate configs/config.yaml
./dist/gateway-cli reload

./dist/gateway-cli config show
./dist/gateway-cli config preview configs/config.yaml
./dist/gateway-cli config diff --file configs/config.yaml
./dist/gateway-cli config diff --from rev-001 --to rev-002

./dist/gateway-cli runtime status
./dist/gateway-cli runtime preflight
./dist/gateway-cli audit 100
./dist/gateway-cli probe provider openai-demo gpt-4o
./dist/gateway-cli probe model gpt-4o openai-demo
./dist/gateway-cli replay list
./dist/gateway-cli replay <request-id>
./dist/gateway-cli diagnostics
./dist/gateway-cli secrets check

./dist/gateway-cli provider list
./dist/gateway-cli provider test openai
./dist/gateway-cli telemetry events
./dist/gateway-cli publish history
./dist/gateway-cli publish rollback rev-001

./dist/gateway-cli benchmark baseline import public_standard baselines/openai.json OpenAI https://platform.openai.com/docs/models
./dist/gateway-cli benchmark baselines
./dist/gateway-cli benchmark run --all-active --public-snapshot <snapshot-id>
./dist/gateway-cli benchmark run --provider openai --model gpt-4o --public-snapshot <snapshot-id>
./dist/gateway-cli benchmark runs
./dist/gateway-cli benchmark show <run-id>
./dist/gateway-cli -format csv benchmark telemetry <run-id> --limit 200
./dist/gateway-cli -format csv benchmark telemetry-summary <run-id>
./dist/gateway-cli -format csv benchmark target-summary <run-id> --sort severity

./dist/gateway-cli test convert
./dist/gateway-cli version
```

### Health And Status

- `health` queries control-plane status and returns an error when gateway or telemetry is not healthy.
- `health quick` discovers the gateway listener from control-plane status, then probes the data-plane `/-/health` endpoint directly.
- `status` prints gateway readiness, active requests, listener, active snapshot id, and provider health when available.
- `status watch [duration]` repeats a compact status line. The default interval is `5s`.

### Config Workflow

Use `validate` for local YAML checks before publishing. Use `config preview` and `config diff` against the running control plane to inspect what will change.

```bash
./dist/gateway-cli validate configs/config.yaml
./dist/gateway-cli config preview configs/config.yaml
./dist/gateway-cli config diff --file configs/config.yaml
```

`config diff` requires either `--to <revision>` or `--file <config.yaml>`. `--from <revision>` is optional.

### Runtime Operations

```bash
./dist/gateway-cli -format json runtime status
./dist/gateway-cli runtime preflight
./dist/gateway-cli audit 50
./dist/gateway-cli diagnostics
./dist/gateway-cli secrets check
```

These commands are intended for scripts, support capture, and release checks. Diagnostics and secret checks are designed to avoid printing plaintext provider keys.

### Probes And Replay

```bash
./dist/gateway-cli probe provider <provider-id> [model]
./dist/gateway-cli probe model <public-model> [provider-id]
./dist/gateway-cli replay list
./dist/gateway-cli replay <request-id>
```

Provider and model probes exercise the control-plane probe APIs. Replay asks the control plane to fetch or replay captured request data according to the configured audit/replay support.

### Publish History

```bash
./dist/gateway-cli publish history
./dist/gateway-cli publish rollback <revision-id>
```

Rollback publishes an older revision through the control plane. Confirm the target revision with `publish history` and `config diff` first.

### Verification Benchmarks

Verification benchmark commands compare configured provider/model routes against imported public or vendor baselines.

```bash
./dist/gateway-cli benchmark baseline import public_standard baselines/openai.json
./dist/gateway-cli benchmark baselines
./dist/gateway-cli benchmark run --all-active --public-snapshot <snapshot-id>
./dist/gateway-cli benchmark runs
./dist/gateway-cli benchmark show <run-id>
./dist/gateway-cli benchmark target-summary <run-id> --sort severity
```

`benchmark run` requires either:

- `--all-active` plus at least one baseline snapshot; or
- `--provider <provider>` and `--model <public-model>` plus at least one baseline snapshot.

Supported optional filters for telemetry commands include:

- `--target <target-id>`
- `--case <case-id>`
- `--provider <provider-id>`
- `--model <public-model>`
- `--hours <hours>`
- `--limit <limit>`
- `--offset <offset>`

CSV output is supported for:

```bash
./dist/gateway-cli -format csv benchmark telemetry <run-id>
./dist/gateway-cli -format csv benchmark telemetry-summary <run-id>
./dist/gateway-cli -format csv benchmark target-summary <run-id>
```

## Admin API Endpoints

The CLI uses these primary Admin API routes:

- `GET /api/admin/runtime/status`
- `POST /api/admin/runtime/preflight`
- `GET /api/admin/audit`
- `GET /api/admin/config`
- `POST /api/admin/config/preview`
- `POST /api/admin/config/diff`
- `POST /api/admin/probe/provider`
- `POST /api/admin/probe/model`
- `GET|POST /api/admin/replay`
- `GET /api/admin/diagnostics`
- `GET /api/admin/secrets/status`
- `GET /metrics`

## Direct Daemon Commands

Use direct daemon commands only for advanced debugging or custom process managers. For normal operations, prefer `aigw supervise`.

### `gatewayd`

```text
gatewayd -config configs/gatewayd.json
gatewayd -listen 127.0.0.1:18080 -control .gateway-runtime/gateway-control.sock -telemetry .gateway-runtime/telemetry-ingest.sock -data-dir .gateway-runtime/gateway
gatewayd -version
```

Available flags:

- `-config`: bootstrap JSON path.
- `-listen`: data-plane HTTP listen address.
- `-control`: control-plane IPC socket or named pipe.
- `-telemetry`: telemetry ingest IPC socket or named pipe.
- `-data-dir`: gateway runtime data directory.
- `-version`: print daemon version.

### `controld`

```text
controld -config configs/controld.json
controld -listen 127.0.0.1:18081 -gateway .gateway-runtime/gateway-control.sock -telemetry .gateway-runtime/telemetry-query.sock -data-dir .gateway-runtime/control -authoring-config configs/config.yaml
controld -version
```

Available flags:

- `-config`: bootstrap JSON path.
- `-listen`: control-plane HTTP listen address.
- `-gateway`: `gatewayd` control IPC socket or named pipe.
- `-telemetry`: `telemetryd` query IPC socket or named pipe.
- `-data-dir`: control-plane state directory, including `publisher-state.db`.
- `-authoring-config`: operator-authored YAML config path.
- `-version`: print daemon version.

When `publisher-state.db` does not exist, `controld` seeds the initial revision from `-authoring-config`. When it already exists, persisted revision history takes precedence.

### `telemetryd`

```text
telemetryd -config configs/telemetryd.json
telemetryd -ingest .gateway-runtime/telemetry-ingest.sock -query .gateway-runtime/telemetry-query.sock -data-dir .gateway-runtime/telemetry
telemetryd -version
```

Available flags:

- `-config`: bootstrap JSON path.
- `-ingest`: ingest IPC socket or named pipe for events from `gatewayd`.
- `-query`: query IPC socket or named pipe for reads from `controld`.
- `-data-dir`: telemetry data directory.
- `-version`: print daemon version.

## Manual Debug Startup

Only use this when you need to bypass `aigw supervise` while debugging startup.

Linux/macOS:

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control

./dist/telemetryd -config configs/telemetryd.json
./dist/gatewayd -config configs/gatewayd.json
./dist/controld -config configs/controld.json
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force .gateway-runtime\telemetry, .gateway-runtime\gateway, .gateway-runtime\control

.\dist\telemetryd.exe -config .\configs\telemetryd.json
.\dist\gatewayd.exe -config .\configs\gatewayd.json
.\dist\controld.exe -config .\configs\controld.json
```

## Health URLs

Data plane:

- `http://127.0.0.1:18080/-/health`
- `http://127.0.0.1:18080/v1/models`

Control plane:

- `http://127.0.0.1:18081/-/health`
- `http://127.0.0.1:18081/admin`
- `http://127.0.0.1:18081/api/admin/runtime/status`

Prometheus:

- `http://127.0.0.1:18081/metrics`

## Removed Legacy Commands

The old single-entry `gateway` operator commands are not the supported operations path:

- `gateway validate`
- `gateway health`
- `gateway status`
- `gateway install`
- `gateway uninstall`
- `gateway service-start`
- `gateway service-stop`
- `gateway service-status`
- `gateway version`

Use `aigw` for local operations and `gateway-cli` for control-plane operations.
