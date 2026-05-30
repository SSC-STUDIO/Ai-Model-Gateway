# AI Model Gateway v1.4.4

Release date: 2026-05-23

AI Model Gateway v1.4.4 is the current public release for evaluators who want a self-hosted LLM operations gateway with provider routing, fallback, telemetry, benchmarking, config publishing, diagnostics, updates, and rollback inside their own environment.

This release keeps the public project surface consistent across repository metadata, README, issue templates, Admin UI copy, release assets, docs, and promotion material so new visitors can understand the project faster.

## Highlights

- Clearer project positioning around a self-hosted LLM operations gateway rather than a hosted model marketplace.
- Repository metadata, issue templates, README, release notes, and promotion assets are aligned around the same message.
- Admin UI naming and language fallback are cleaned up so users do not see untranslated placeholders.
- Old CLI i18n dead code and stale generated/local artifacts were removed.
- Embedded Admin UI assets were refreshed so release bundles and repository assets stay consistent.
- Provider fallback and health docs now include an executable demo that proves a primary `429` can be served through a fallback provider with `route_mode=model_fallback` telemetry.
- The project roadmap, 15-minute evaluation path, quality evidence page, and security trust model give maintainers and evaluators shorter review paths.
- Repository topics are aligned around LLM gateway discovery terms such as `llm-gateway`, `openai-compatible`, `rate-limiting`, and `config-management`.

## Start Here

- [Project website](https://ssc-studio.github.io/Ai-Model-Gateway/)
- [GitHub release](https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/tag/v1.4.4)
- [15-minute evaluation path](evaluate-in-15-minutes.md)
- [Docker Compose trial path](deployment.md#docker-compose)
- [LLM client integrations page](https://ssc-studio.github.io/Ai-Model-Gateway/client-integrations.html)
- [Claude Code gateway page](https://ssc-studio.github.io/Ai-Model-Gateway/claude-code-gateway.html)
- [LangGPT Awesome Claude Code submission](https://github.com/LangGPT/awesome-claude-code/pull/81)
- [Quality evidence](quality-evidence.md)
- [Security and trust model](security-trust-model.md)
- [Architecture guide](architecture.md)
- [CLI guide](cli.md)
- [Client integrations](client-integrations.md)
- [Provider fallback demo](../examples/provider-fallback/)
- [100-star campaign log](100-star-campaign.md)
- [Maintainer discussion](https://github.com/SSC-STUDIO/Ai-Model-Gateway/discussions/25)

## Downloads

- Windows x64: `ai-model-gateway-windows-amd64.zip`
- Linux x64: `ai-model-gateway-linux-amd64.tar.gz`
- Linux arm64: `ai-model-gateway-linux-arm64.tar.gz`
- macOS arm64: `ai-model-gateway-darwin-arm64.tar.gz`
- Checksums: `SHA256SUMS.txt`

## Verification And Upgrade

- Verify downloaded archives with `SHA256SUMS.txt` before use.
- Prefer the Admin Ops or `aigw update` workflow: check, download, verify, dry-run, apply, and rollback.
- Back up configuration and runtime data before production upgrades.
- Keep `aigw`, `gatewayd`, `controld`, `telemetryd`, and `gateway-cli` from the same verified bundle.
