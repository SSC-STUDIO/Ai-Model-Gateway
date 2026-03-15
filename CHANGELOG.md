# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, with a lightweight structure suitable for a small operational project.

## [Unreleased]

- No unreleased changes yet.

## [2026-03-15] Public release baseline

### Added

- OpenAI-compatible gateway endpoints for chat, responses, embeddings, files, audio, images, models, health, and admin APIs.
- Admin dashboard with throughput, latency, token usage, success/failure, cache hit, upstream health, pricing, and recent request/error views.
- Runtime config editing for health checks, bridge rules, retry policy, response intercepts, upstream providers, config export, history, diff preview, and rollback.
- SQLite-backed telemetry persistence and pricing cache persistence.
- Claude compatibility fallback from `Responses API` to `chat/completions` when the upstream does not implement responses natively.
- Sticky session routing for Responses API to improve prompt cache hit rate.
- Public documentation assets: project icon, admin screenshot, rewritten README, LICENSE, and CI workflow.

### Changed

- Admin UI layout was tightened for higher information density and better readability.
- Runtime settings were moved off the overview page to a dedicated `/admin/settings` view.
- Pricing aggregation now merges duplicate display-model rows and preserves bridge-pricing attribution.
- Hot paths were optimized with shared HTTP transport reuse, cached admin data payloads, and SQLite WAL/busy-timeout tuning.

### Fixed

- `/v1/responses/compact` route handling now avoids misrouting into `responses/{response_id}` semantics.
- SSE completion detection now distinguishes between Responses streams and Chat Completions streams.
- Claude requests sent through `Responses API` now work against chat-only upstreams via compatibility conversion.
- Prompt cache accounting is surfaced in pricing and admin aggregations.
