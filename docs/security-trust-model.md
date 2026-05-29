# Security And Trust Model

Use this guide when evaluating AI Model Gateway for self-hosted environments where provider keys, routing policy, telemetry, and operator controls must stay local.

This document describes current trust boundaries and security controls. It is not a formal security audit and does not claim the project is vulnerability-free.

## Trust Boundaries

| Boundary | Trusted Side | Exposed Side | Notes |
| --- | --- | --- | --- |
| Data plane | `gatewayd`, active runtime snapshot, provider credentials delivered over control RPC | client traffic on `/-/health`, `/v1/models`, `/v1/chat/completions`, `/v1/messages`, `/v1/responses` | Expose only the data-plane listener to application clients. |
| Control plane | `controld`, Admin API, Admin UI, config revision state, audit log, update state | trusted operators and automation | Restrict the control-plane listener to operators or a protected network. |
| Telemetry plane | `telemetryd`, event log, query store, IPC contracts | no public HTTP listener | Telemetry is queried through the control plane. |
| Provider network | upstream LLM providers and configured base URLs | outbound HTTP/WebSocket requests from the gateway | SSRF checks and DNS pinning are applied by the proxy helpers. |
| Local filesystem | config files, runtime databases, manifests, update state, logs | service account and host operators | File permissions and backups are deployment responsibilities. |

## Authentication And Roles

Admin API authentication supports two paths:

- signed cookies for browser sessions
- bearer tokens for CLI and automation

Roles:

| Role | Access |
| --- | --- |
| `admin` | Read and write access to config, publish, rollback, probes, diagnostics, updates, and benchmark operations |
| `viewer` | Read-only access to status, config views, telemetry, audit, diagnostics, and other inspection surfaces |

Important behavior:

- Browser login uses `POST /api/admin/login`.
- URL token login is not supported.
- Signed cookies are `HttpOnly` and `SameSite=Strict`.
- Cookie `Secure` is enabled outside loopback-local HTTP development.
- Viewer requests are denied for write operations.
- Bearer tokens are intended for scripts and CLI use.

## Browser Write Protection

Cookie-authenticated browser write requests require same-origin validation through `Origin` or `Referer`.

This applies to mutating Admin API paths such as config publish, rollback, update, probe, benchmark, and diagnostics actions. Same-origin validation protects the browser session path. Bearer-token API calls are authenticated directly and do not depend on browser origin headers.

When deploying behind a reverse proxy, make sure the forwarded host and scheme reflect the public admin origin expected by operators.

## Provider Keys And Config Secrets

AI Model Gateway is self-hosted, so operators own provider credentials and gateway tokens.

Recommended handling:

- Keep real provider keys out of committed config.
- Use environment variable placeholders in `config.yaml`.
- Store real values in deployment-specific secret managers, service environment files, or host-level secret stores.
- Restrict read access to `configs/`, runtime state, backups, and logs.
- Treat update backups as sensitive because they can contain config and runtime state.

Redaction surfaces:

- config diffs redact common secret paths such as tokens, API keys, secrets, cookie signing keys, and credential headers
- audit events redact secret-looking detail fields
- diagnostics responses mark themselves as redacted and include status, runtime, and audit-tail data rather than plaintext provider keys
- secret-status APIs report presence, not values

Redaction is a defense-in-depth control, not a substitute for safe log handling.

## SSRF And Upstream Request Safety

The proxy helper rejects risky upstream URLs before outbound requests:

- unsupported schemes
- localhost and loopback addresses unless explicitly allowed for tests
- private, link-local, multicast, documentation, reserved, and broadcast address ranges
- common cloud metadata hosts such as `169.254.169.254` and `metadata.google.internal`
- URLs with user info
- path traversal patterns
- hex-encoded host tricks

HTTP and WebSocket outbound connections use DNS validation and pinned-IP dialing to reduce DNS rebinding risk between validation and connection establishment.

If your deployment intentionally needs private upstreams, review the SSRF configuration and network boundary before allowing private IP access.

## Local File And Archive Safety

Path safety controls exist for internal helpers and update workflows:

- path joins validate that resolved targets remain inside the intended base directory
- filenames reject traversal, null bytes, path separators, and Windows reserved names
- update archive extraction rejects absolute paths and traversal paths
- release updates use manifest verification and same-bundle binary expectations

Deployment responsibility remains important:

- run the service with a dedicated low-privilege account
- keep runtime state outside the repository checkout
- back up config and runtime state before upgrades
- avoid replacing one daemon binary independently of the rest of the bundle

## Telemetry And Audit Data

Telemetry and audit data can be sensitive even when provider keys are redacted.

Potentially sensitive data includes:

- model names and provider identifiers
- route decisions and fallback modes
- request timing, status, token usage, and cost estimates
- audit actions, actor role, source address, and error text
- diagnostics runtime paths and recent audit event metadata

Default architecture:

- `telemetryd` exposes IPC only, not public HTTP
- `controld` is the query gateway for telemetry and diagnostics
- `audit.jsonl`, telemetry databases, benchmark databases, and publisher state are local runtime files

Protect runtime directories with host permissions and treat exported diagnostics or telemetry as operational data.

## Update And Rollback Trust

The update workflow is designed around manifest-verified bundles:

- check releases
- download platform bundle
- verify manifest and binaries
- dry-run apply
- apply
- roll back from local backup

Do not mix daemon binaries from different builds. Keep `aigw`, `gatewayd`, `controld`, `telemetryd`, and `gateway-cli` from the same verified bundle.

## Deployment Checklist

- Bind the data plane only where application clients need it.
- Keep the control plane behind operator-only network access or a trusted reverse proxy.
- Use strong admin, viewer, and cookie signing tokens.
- Keep real provider keys in environment variables or a deployment secret store.
- Restrict runtime directory permissions.
- Verify release archives and manifests before installing.
- Run `aigw doctor`, health checks, and the local CI smoke checks before production use.
- Review diagnostics and telemetry before sharing externally.
- Revisit [SECURITY.md](../SECURITY.md) before reporting vulnerabilities.
