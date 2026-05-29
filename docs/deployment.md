# Deployment Guide

AI Model Gateway is deployed as a single `aigw` operations entry point.
`gatewayd`, `controld`, and `telemetryd` remain separate internal process
boundaries, but normal service managers should run only `aigw supervise`.

Use this guide after you have completed the [installation guide](installation.md).

## Deployment Model

The production bundle should keep these files from the same build together:

- `aigw`
- `gatewayd`
- `controld`
- `telemetryd`
- `gateway-cli`
- `aigw-manifest.json`
- `configs/`
- `deploy/`
- Optional `web/admin/dist/`

`aigw supervise` verifies the manifest and daemon versions before it starts.
If one daemon binary is replaced independently, the supervisor can reject the
mixed bundle instead of running an inconsistent deployment.

## Recommended Layout

Linux:

```text
/opt/ai-model-gateway/
|-- bin/
|-- configs/
|-- deploy/
|-- web/admin/dist/
`-- aigw-manifest.json

/var/lib/ai-model-gateway/
|-- telemetry/
|-- gateway/
|-- control/
`-- logs/

/etc/ai-model-gateway/
|-- gatewayd.json
|-- controld.json
|-- telemetryd.json
|-- config.yaml
`-- secrets.env
```

Windows:

```text
C:\AI-Model-Gateway\
|-- bin\
|-- configs\
|-- deploy\
|-- web\admin\dist\
|-- aigw-manifest.json
`-- .gateway-runtime\
    |-- telemetry\
    |-- gateway\
    |-- control\
    `-- logs\
```

Default local ports:

- Data plane: `127.0.0.1:18080`
- Control plane and Admin UI: `127.0.0.1:18081`

For container deployments, the Docker config files bind HTTP listeners to
`0.0.0.0` so published ports work correctly. For direct host runs, use the
regular files in `configs/`.

## Build A Bundle From Source

Linux:

```bash
mkdir -p dist/bin
go build -trimpath -ldflags="-s -w" -o dist/bin/aigw        ./cmd/aigw
go build -trimpath -ldflags="-s -w" -o dist/bin/gatewayd    ./cmd/gatewayd
go build -trimpath -ldflags="-s -w" -o dist/bin/controld    ./cmd/controld
go build -trimpath -ldflags="-s -w" -o dist/bin/telemetryd  ./cmd/telemetryd
go build -trimpath -ldflags="-s -w" -o dist/bin/gateway-cli ./cmd/gateway-cli
dist/bin/aigw bundle build -root dist -out dist/aigw-manifest.json
dist/bin/aigw bundle verify -root dist -manifest dist/aigw-manifest.json
```

Windows:

```powershell
New-Item -ItemType Directory -Force -Path dist\bin | Out-Null
go build -trimpath -ldflags="-s -w" -o dist\bin\aigw.exe        .\cmd\aigw
go build -trimpath -ldflags="-s -w" -o dist\bin\gatewayd.exe    .\cmd\gatewayd
go build -trimpath -ldflags="-s -w" -o dist\bin\controld.exe    .\cmd\controld
go build -trimpath -ldflags="-s -w" -o dist\bin\telemetryd.exe  .\cmd\telemetryd
go build -trimpath -ldflags="-s -w" -o dist\bin\gateway-cli.exe .\cmd\gateway-cli
.\dist\bin\aigw.exe bundle build -root dist -out dist\aigw-manifest.json
.\dist\bin\aigw.exe bundle verify -root dist -manifest dist\aigw-manifest.json
```

Release archives already include the bundle files and manifest. If you use a
release archive, unpack it first and run the same manifest verification before
installing it.

## Configure Secrets

Keep secrets out of committed config files. The provided systemd unit reads:

```text
/etc/ai-model-gateway/secrets.env
```

Example:

```bash
ADMIN_TOKEN=replace-with-a-long-random-token
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

Use your own provider variables and reference them from `config.yaml` or the
admin publish workflow used in your environment.

## Linux Systemd

Install the release files:

```bash
sudo useradd --system --home /var/lib/ai-model-gateway --shell /usr/sbin/nologin gateway
sudo install -d -o root -g root /opt/ai-model-gateway
sudo install -d -o gateway -g gateway /var/lib/ai-model-gateway
sudo install -d -o root -g gateway -m 0750 /etc/ai-model-gateway

sudo cp -R bin deploy web aigw-manifest.json /opt/ai-model-gateway/
sudo cp configs/*.json /etc/ai-model-gateway/
sudo cp configs/config.example.yaml /etc/ai-model-gateway/config.yaml
sudo cp deploy/aigw.service /etc/systemd/system/aigw.service
```

If the `gateway` user already exists, keep the existing account and skip the
`useradd` command.

If you prefer to generate the unit from the installed binary:

```bash
/opt/ai-model-gateway/bin/aigw service print | sudo tee /etc/systemd/system/aigw.service
```

Review paths, user, group, memory limits, and `EnvironmentFile` before enabling
the service. The checked-in `deploy/aigw.service` uses these default paths:

- Binary root: `/opt/ai-model-gateway/bin`
- Runtime root: `/var/lib/ai-model-gateway`
- Config root: `/etc/ai-model-gateway`
- Manifest: `/opt/ai-model-gateway/aigw-manifest.json`

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now aigw
sudo systemctl status aigw --no-pager
```

Follow logs:

```bash
journalctl -u aigw -f
```

## Direct Linux Run

For a server without systemd or for a one-off smoke test:

```bash
mkdir -p /opt/ai-model-gateway/.gateway-runtime/{telemetry,gateway,control,logs}

/opt/ai-model-gateway/bin/aigw supervise \
  -runtime-root /opt/ai-model-gateway/.gateway-runtime \
  -config-dir /opt/ai-model-gateway/configs \
  -bin-dir /opt/ai-model-gateway/bin \
  -strict-manifest=true \
  -manifest /opt/ai-model-gateway/aigw-manifest.json
```

## Windows Service Wrapper

Windows does not use the checked-in `aigw.service`. Wrap `aigw.exe supervise`
with NSSM, Windows Service Wrapper, Task Scheduler, or your host management
tool.

Manual smoke test:

```powershell
$root = "C:\AI-Model-Gateway"
$runtimeRoot = Join-Path $root ".gateway-runtime"

New-Item -ItemType Directory -Force -Path `
  (Join-Path $runtimeRoot "telemetry"), `
  (Join-Path $runtimeRoot "gateway"), `
  (Join-Path $runtimeRoot "control"), `
  (Join-Path $runtimeRoot "logs") | Out-Null

& "$root\bin\aigw.exe" supervise `
  -runtime-root $runtimeRoot `
  -config-dir "$root\configs" `
  -bin-dir "$root\bin" `
  -strict-manifest=true `
  -manifest "$root\aigw-manifest.json"
```

The service wrapper should set the working directory to `C:\AI-Model-Gateway`
and should restart the process on failure.

## Docker Compose

The repository includes `deploy/docker-compose.yaml` for local container
deployment:

```bash
docker compose -f deploy/docker-compose.yaml up -d
docker compose -f deploy/docker-compose.yaml logs -f
```

The compose file builds the `runtime` Docker target, runs `aigw supervise`, and
mounts `../configs` read-only into the container. Its optional env file path is:

```text
deploy/secrets.env
```

Use `configs/docker/*.json` inside containers. Those files are adjusted for
published ports and container paths.

## Health Checks

After the process starts:

```bash
curl http://127.0.0.1:18080/-/health
curl http://127.0.0.1:18080/v1/models
curl http://127.0.0.1:18081/-/health
curl http://127.0.0.1:18081/admin
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://127.0.0.1:18081/api/admin/runtime/status
```

Local diagnostics:

```bash
aigw doctor -runtime-root .gateway-runtime -config-dir configs -manifest aigw-manifest.json
aigw status -control-url http://127.0.0.1:18081 -gateway-url http://127.0.0.1:18080
aigw logs -runtime-root .gateway-runtime -n 120
```

## Update And Rollback

Before replacing a running deployment:

```bash
aigw backup -runtime-root /var/lib/ai-model-gateway -config-dir /etc/ai-model-gateway
```

Then stage the new bundle, verify its manifest, restart the service, and check
health:

```bash
/opt/ai-model-gateway/bin/aigw bundle verify \
  -root /opt/ai-model-gateway \
  -manifest /opt/ai-model-gateway/aigw-manifest.json

sudo systemctl restart aigw
sudo systemctl status aigw --no-pager
```

If a deployment fails, restore the previous bundle and runtime backup, then
restart `aigw`. Do not roll back only one daemon binary; keep `aigw`,
`gatewayd`, `controld`, `telemetryd`, and `gateway-cli` from the same bundle.

## Operational Notes

- `gatewayd -listen` must match the target data-plane address.
- `controld` remains the owner of revision, publish, history, and rollback
  state.
- The repository no longer provides `gateway install`, `service-start`,
  `service-stop`, or `service-status`.
- Run internal daemons directly only for advanced debugging.
- Do not run the local development runtime on `127.0.0.1:18080` while another
  legacy local gateway already owns that port.
- For deeper incident handling, use the [troubleshooting guide](troubleshooting.md).
