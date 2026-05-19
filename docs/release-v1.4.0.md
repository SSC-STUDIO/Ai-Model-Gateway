# AI Model Gateway v1.4.0

Release date: 2026-05-20

AI Model Gateway v1.4.0 is the professional admin UI and positioning release. It sharpens the project around a self-hosted LLM operations gateway: provider routing, config publishing, telemetry, benchmarks, diagnostics, and day-2 operations in one compact Go runtime.

## Highlights

- Professionalized the admin console with quieter loading states, clearer workspace grouping, reduced decorative motion, and a more operations-focused first screen.
- Consolidated Monitoring, Ops, and Benchmark workflows while keeping legacy admin links compatible.
- Added focused admin UI tests for icons, toasts, URL canonicalization, formatting helpers, config editor behavior, API normalization, and navigation.
- Added public differentiation docs comparing LiteLLM, Portkey, Helicone, OpenRouter, Envoy AI Gateway, Kong, and related Go-native gateways.
- Rewrote the README into a clearer GitHub landing page with current screenshots.
- Expanded release automation to Linux amd64, Linux arm64, Windows amd64, and macOS arm64 bundles, SHA-256 checksums, manifest verification, and GHCR image publishing.
- Hardened admin authentication, same-origin behavior, WebSocket DNS pinning, WebSocket origin validation, retry behavior, and forwarding/streaming coverage.

## Upgrade Notes

- Production deployments should continue to run `aigw supervise`; do not mix daemon binaries from different versions.
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

## Promotion Copy

### Short Chinese

AI Model Gateway v1.4.0 发布：这次把项目定位收敛成“自托管 LLM 运维网关”，重点打磨了专业化 Admin UI、Monitoring/Ops/Benchmark 工作区、配置发布/回滚、遥测、基准测试和诊断链路。相比只做统一代理或托管模型市场，它更适合想把 key、路由策略和 telemetry 留在自己环境里的团队。

### Short English

AI Model Gateway v1.4.0 is out. This release sharpens the project into a self-hosted LLM operations gateway with a more professional admin console, clearer Monitoring/Ops/Benchmark workflows, config publish/rollback, telemetry, benchmarks, diagnostics, release checksums, and multi-platform bundles.

### Social Post

Released AI Model Gateway v1.4.0.

This release moves the project toward a focused self-hosted LLM operations gateway:

- quieter, more professional admin UI
- clearer Monitoring, Ops, and Benchmark workspaces
- config publish/rollback workflow
- telemetry, provider health, diagnostics, audit, and replay
- updated differentiation docs vs LiteLLM, Portkey, Helicone, OpenRouter, Kong, and Envoy AI Gateway
- multi-platform release bundles, checksums, and GHCR images

Project: https://github.com/SSC-STUDIO/Ai-Model-Gateway
