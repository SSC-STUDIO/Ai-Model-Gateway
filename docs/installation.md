# Installation Guide

AI Model Gateway runs as one local operations entry point, `aigw`, supervising three internal daemons:

- `gatewayd` for inference traffic
- `controld` for admin APIs, config publishing, probes, diagnostics, and benchmarks
- `telemetryd` for event ingestion and query projections

The old `gateway` / `gateway.exe` launcher is no longer shipped. The default local run mode is `aigw supervise`.

## Choose An Install Path

| Path | Best for | Start with |
| --- | --- | --- |
| Release archive | Trying the latest packaged runtime without rebuilding | [Install From A Release Archive](#install-from-a-release-archive) |
| Source build | Development, auditing code, or changing the runtime | This guide |
| Service deployment | Running a long-lived local or server process | [Deployment guide](deployment.md) |

## Prerequisites

For a source build:

- Go matching the version in [`go.mod`](../go.mod)
- Node.js and npm if you want to rebuild the Admin UI
- PowerShell on Windows or a POSIX shell on Linux/macOS
- One free data-plane port, normally `127.0.0.1:18080`
- One free control-plane port, normally `127.0.0.1:18081`

Local development note: if another service already owns `127.0.0.1:18080`, stop it first or move one runtime to different ports. Running two local gateways on the same listener will fail.

## Install From A Release Archive

Use this path when you want to try the packaged runtime without cloning or rebuilding the repository.

Choose the archive for your platform from the [latest release](https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/latest):

- Windows x64: `ai-model-gateway-windows-amd64.zip`
- Linux x64: `ai-model-gateway-linux-amd64.tar.gz`
- Linux arm64: `ai-model-gateway-linux-arm64.tar.gz`
- macOS arm64: `ai-model-gateway-darwin-arm64.tar.gz`
- Checksums: `SHA256SUMS.txt`

Linux x64 example:

```bash
version=v1.4.4
base="https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/download/${version}"
curl -LO "${base}/ai-model-gateway-linux-amd64.tar.gz"
curl -LO "${base}/SHA256SUMS.txt"
grep "ai-model-gateway-linux-amd64.tar.gz" SHA256SUMS.txt | sha256sum -c -
mkdir -p ai-model-gateway
tar -xzf ai-model-gateway-linux-amd64.tar.gz -C ai-model-gateway
cd ai-model-gateway
```

Windows PowerShell example:

```powershell
$version = "v1.4.4"
$base = "https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/download/$version"
Invoke-WebRequest "$base/ai-model-gateway-windows-amd64.zip" -OutFile ai-model-gateway-windows-amd64.zip
Invoke-WebRequest "$base/SHA256SUMS.txt" -OutFile SHA256SUMS.txt
Select-String "ai-model-gateway-windows-amd64.zip" SHA256SUMS.txt
Get-FileHash .\ai-model-gateway-windows-amd64.zip -Algorithm SHA256
Expand-Archive .\ai-model-gateway-windows-amd64.zip -DestinationPath .\ai-model-gateway -Force
Set-Location .\ai-model-gateway
```

Compare the `Get-FileHash` value with the matching line in `SHA256SUMS.txt`.

Then create a local authoring config and runtime root:

```bash
cp configs/config.example.yaml configs/config.yaml
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control
```

Windows PowerShell:

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
New-Item -ItemType Directory -Force -Path `
  .\.gateway-runtime\telemetry, `
  .\.gateway-runtime\gateway, `
  .\.gateway-runtime\control | Out-Null
```

Set temporary local secrets before starting:

```bash
export ADMIN_BOOTSTRAP_TOKEN=change-me-32-characters-minimum
export COOKIE_SIGNING_KEY=change-me-32-characters-minimum
export ADMIN_TOKEN=change-me-admin-token
export VIEWER_TOKEN=change-me-viewer-token
```

Windows PowerShell:

```powershell
$env:ADMIN_BOOTSTRAP_TOKEN = "change-me-32-characters-minimum"
$env:COOKIE_SIGNING_KEY = "change-me-32-characters-minimum"
$env:ADMIN_TOKEN = "change-me-admin-token"
$env:VIEWER_TOKEN = "change-me-viewer-token"
```

Start the packaged runtime:

```bash
bin/aigw supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir bin -manifest aigw-manifest.json -strict-manifest=true
```

Windows PowerShell:

```powershell
.\bin\aigw.exe supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir .\bin -manifest .\aigw-manifest.json -strict-manifest=true
```

Then open `http://127.0.0.1:18080/admin` and check `http://127.0.0.1:18080/-/health`.

## Build From Source

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force -Path .\dist | Out-Null
go build -o .\dist\aigw.exe .\cmd\aigw
go build -o .\dist\gatewayd.exe .\cmd\gatewayd
go build -o .\dist\controld.exe .\cmd\controld
go build -o .\dist\telemetryd.exe .\cmd\telemetryd
go build -o .\dist\gateway-cli.exe .\cmd\gateway-cli
```

Linux/macOS:

```bash
mkdir -p ./dist
go build -o ./dist/aigw ./cmd/aigw
go build -o ./dist/gatewayd ./cmd/gatewayd
go build -o ./dist/controld ./cmd/controld
go build -o ./dist/telemetryd ./cmd/telemetryd
go build -o ./dist/gateway-cli ./cmd/gateway-cli
```

Before packaging or service deployment, build and verify a manifest:

```bash
./dist/aigw bundle build -root . -out aigw-manifest.json
./dist/aigw bundle verify -root . -manifest aigw-manifest.json
```

On Windows, use `.\dist\aigw.exe` for the same commands.

## Prepare Config

Copy the example operator config:

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
```

Set local secrets for the admin session and API access. Use your own values for real deployments.

```powershell
$env:ADMIN_BOOTSTRAP_TOKEN = "change-me-32-characters-minimum"
$env:COOKIE_SIGNING_KEY = "change-me-32-characters-minimum"
$env:ADMIN_TOKEN = "change-me-admin-token"
$env:VIEWER_TOKEN = "change-me-viewer-token"
```

Linux/macOS:

```bash
cp configs/config.example.yaml configs/config.yaml
export ADMIN_BOOTSTRAP_TOKEN=change-me-32-characters-minimum
export COOKIE_SIGNING_KEY=change-me-32-characters-minimum
export ADMIN_TOKEN=change-me-admin-token
export VIEWER_TOKEN=change-me-viewer-token
```

`configs/config.yaml` is the operator authoring config. `controld` reads it through `-authoring-config`. `gatewayd` and `telemetryd` do not read it directly.

The daemon bootstrap JSON files are separate:

- `configs/gatewayd.json`
- `configs/controld.json`
- `configs/telemetryd.json`

Do not pass `configs/config.yaml` to a daemon `-config` flag.

## Create Runtime Directories

Use one shared runtime root:

```text
.gateway-runtime/
|-- telemetry/
|-- gateway/
`-- control/
```

Linux/macOS:

```bash
mkdir -p .gateway-runtime/telemetry .gateway-runtime/gateway .gateway-runtime/control
```

Windows PowerShell:

```powershell
$runtimeRoot = Join-Path $PWD ".gateway-runtime"
New-Item -ItemType Directory -Force -Path `
  (Join-Path $runtimeRoot "telemetry"), `
  (Join-Path $runtimeRoot "gateway"), `
  (Join-Path $runtimeRoot "control") | Out-Null
```

## Start The Runtime

Linux/macOS:

```bash
./dist/aigw supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir ./dist
```

Windows PowerShell:

```powershell
.\dist\aigw.exe supervise -runtime-root .gateway-runtime -config-dir configs -bin-dir .\dist
```

The Admin UI should be available at:

```text
http://127.0.0.1:18080/admin
```

## Verify

Basic health checks:

```powershell
curl.exe http://127.0.0.1:18080/-/health
curl.exe http://127.0.0.1:18080/v1/models
curl.exe http://127.0.0.1:18081/admin
curl.exe http://127.0.0.1:18081/-/health
```

Admin API checks:

```powershell
curl.exe -H "Authorization: Bearer $env:ADMIN_TOKEN" http://127.0.0.1:18081/api/admin/runtime/status
curl.exe -H "Authorization: Bearer $env:ADMIN_TOKEN" http://127.0.0.1:18081/api/admin/config/history
```

Windows users can also run the repository smoke script:

```powershell
.\scripts\verify-default-runtime.ps1
```

## What Gets Persisted

- `publisher-state.db` lives under the `controld` data directory.
- If `publisher-state.db` does not exist, `controld` seeds the initial revision from `-authoring-config`.
- If `publisher-state.db` already exists, `controld` restores the existing revision and publish history first.
- Production service wrappers should normally manage only `aigw supervise`. Managing the internal daemons separately is an advanced debugging mode.

## Next Steps

- [15-minute evaluation path](evaluate-in-15-minutes.md)
- [Current release summary](release-v1.4.4.md)
- [Deployment guide](deployment.md)
- [CLI guide](cli.md)
- [Troubleshooting](troubleshooting.md)
- [Self-hosted LLM gateway checklist](self-hosted-llm-gateway-checklist.md)
- [LLM gateway comparison guide](llm-gateway-comparison.md)
