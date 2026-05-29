# AI Model Gateway v1.4.0

Release date: 2026-05-20

AI Model Gateway v1.4.0 was the professional admin UI and positioning release. It sharpened the project around a self-hosted LLM operations gateway: provider routing, config publishing, telemetry, benchmarks, diagnostics, and day-2 operations in one compact Go runtime.

For the current public release, see [AI Model Gateway v1.4.4](release-v1.4.4.md) or the [GitHub release page](https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/tag/v1.4.4).

## Highlights

- Professionalized the admin console with quieter loading states, clearer workspace grouping, reduced decorative motion, and a more operations-focused first screen.
- Consolidated Monitoring, Ops, and Benchmark workflows while keeping legacy admin links compatible.
- Added focused Admin UI tests for icons, toasts, URL canonicalization, formatting helpers, config editor behavior, API normalization, and navigation.
- Added public differentiation docs comparing LiteLLM, Portkey, Helicone, OpenRouter, Envoy AI Gateway, Kong, and related Go-native gateways.
- Rewrote the README into a clearer GitHub landing page with current screenshots.
- Expanded release automation to Linux amd64, Linux arm64, Windows amd64, and macOS arm64 bundles, SHA-256 checksums, manifest verification, and GHCR image publishing.
- Hardened admin authentication, same-origin behavior, WebSocket DNS pinning, WebSocket origin validation, retry behavior, and forwarding/streaming coverage.

## Upgrade Notes

- Production deployments should continue to run `aigw supervise`.
- Do not mix daemon binaries from different versions.
- Release bundles include `aigw`, `gatewayd`, `controld`, `telemetryd`, `gateway-cli`, configs, deploy files, the admin frontend, and `aigw-manifest.json`.
- Verify downloaded bundles before replacing a running install:

```bash
./bin/aigw bundle verify -root . -manifest aigw-manifest.json
```

## Install

Download the matching asset from the GitHub release:

- `ai-model-gateway-linux-amd64.tar.gz`
- `ai-model-gateway-linux-arm64.tar.gz`
- `ai-model-gateway-darwin-arm64.tar.gz`
- `ai-model-gateway-windows-amd64.zip`
- `SHA256SUMS.txt`

Container images are published to GHCR by the release workflow.

## Share Copy

AI Model Gateway v1.4.0 moved the project toward a focused self-hosted LLM operations gateway:

- quieter, more professional Admin UI
- clearer Monitoring, Ops, and Benchmark workspaces
- config publish and rollback workflow
- telemetry, provider health, diagnostics, audit, and replay
- differentiation notes versus LiteLLM, Portkey, Helicone, OpenRouter, Kong, and Envoy AI Gateway
- multi-platform release bundles, checksums, and GHCR images

Project: https://github.com/SSC-STUDIO/Ai-Model-Gateway
