# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, with a lightweight structure suitable for a small operational project.

## [Unreleased]

## [0.2.0] — 2026-03-22

### Added

- Anthropic Messages API support: `/v1/messages` and `/v1/messages/count_tokens` proxy endpoints with native `x-api-key` auth, sticky routing, and stream completion detection.
- Full Chinese/English/Japanese/Korean/Spanish/French/German i18n for admin dashboard with runtime language selector.
- Sparkline trend charts inside performance metric cards (RPM, latency, tokens).
- Success rate donut chart in hero priority grid.
- Model token distribution horizontal bar chart in economics section.
- Upstream token distribution horizontal bar chart in usage section.
- Cache hit rate trend line added to latency chart as 3rd series.
- Request table row coloring: red for 5xx, amber for 4xx, green left border for 2xx.
- Collapsible detail tables (model economics, upstream usage, cache ranking, requests) with smooth expand/collapse animation.
- Page load card entrance animations with staggered fadeSlideUp.
- Hero title shimmer gradient effect.
- Section expand/collapse sectionReveal animation.
- Chart SVG fade-in on render.
- Data refresh value-refresh opacity animation on metrics.
- Hero priority glow pulse on healthy cards.
- Print stylesheet for white-background printing.
- Firefox scrollbar support (`scrollbar-width`/`scrollbar-color`).
- `prefers-reduced-motion` media query disables all animations.
- Focus-visible outline rings on all interactive elements.
- Aria labels on nav, asides, inputs, selects.
- Aria-expanded on section/provider toggle buttons.
- Chart SVG `role=img` and `aria-hidden=true`.

### Changed

- Admin overview layout: performance/runtime 8/4→7/5 split, usage/cache 8/4→7/5 split.
- Card padding 12px→14px for primary cards, compact-card top padding aligned to 14px.
- Chart height 190→210px, compact chart 160→210px, y-axis padding 52→44px.
- Error feed max-height raised from 300px to 500px cap.
- Responsive breakpoints: merged 920/900px into single 920px, settings rail 1380→1100px.
- Settings page redesigned: hero hidden, nav changed to horizontal pill tabs, controls split into Filters + Actions panels, rail narrowed 320→260px.
- Settings jumpbar: card-style links replaced with compact horizontal pills matching topnav style.
- Removed redundant surface strips from economics and usage sections (replaced by horizontal bar charts).
- Upstream tile and error item border-radius unified to 16px.
- Table-shell border-radius 16→14px, table padding improved with first/last-child insets.
- Section-head align-items changed from end to flex-start.
- Policy-card and mode-preset transitions normalized to 160ms.
- Topnav/jumpbar transitions extended with box-shadow for smooth active state.
- Success/failure chart colors: failure changed from orange to red (#f87171).
- Stacked bar chart `drawStackedBarChart` accepts optional `customColors` parameter.
- `table()` helper accepts optional `rowClasses` parameter for row-level styling.
- Telemetry store: in-memory counters, async write batching, snapshot caching with 2s TTL.
- Router manager: shared health HTTP client, cursor-based weighted round-robin, sticky session pruning.
- Admin data endpoint: sync.Cond single-flight thundering herd protection.
- Bumped VERSION from 0.1.2 to 0.2.0.

### Fixed

- Anthropic Messages API token usage: compute `total_tokens` from `input_tokens + output_tokens` when absent, read `cache_read_input_tokens`, parse `message.usage` from streaming `message_start` events.
- SSE usage extraction: accumulate max values across all events instead of returning first match (fixes Anthropic streaming output_tokens=0).
- XSS: escape `pricing.source_url` in href, escape `pricing.input_per_1m_usd`/`output_per_1m_usd` in priceLine and economics table.
- Format string injection: `fmt.Errorf(reason)` → `fmt.Errorf("%s", reason)` at 8 call sites.
- Client `X-Api-Key` header no longer forwarded to non-Anthropic upstreams.
- Request body size limited to 100MB via `io.LimitReader`.
- API key inputs changed from `type=text` to `type=password`.
- Provider-summary-strip CSS: added 8th column for Probe (was 7 cols, 8 items rendered).
- Table-cache min-width reduced from 580px to 420px (fits span-5 container).
- Inline styles replaced with CSS classes (`config-field-wide`, `diff-preview-card`).
- Sparkline flicker: re-render immediately after `load()` updates metrics innerHTML.
- Card bottom alignment: `align-items: stretch` with flex content fill.
- Config-card-head sticky top: 10→86px (clears topbar properly).
- Config-section scroll-margin-top: 18→88px.
- Jumpbar link height consistency: `white-space: nowrap` on strong text.
- `markInvalid` now sets `aria-expanded=true` when auto-expanding collapsed cards.
- `load()` wrapped in try/catch, null guard on `data.telemetry`.
- Degraded/warn upstream tiles preserve status color on hover.
- Duplicate `loaded` key removed from Japanese i18n locale.
- Removed dead code: 4 JS functions, 3 CSS rules, 30 Go template replacer entries (~100 lines net reduction).

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
