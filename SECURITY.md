# Security Policy

## Supported Versions

This project currently supports only the latest state of the `main` branch.

If you run an older fork, release archive, or local copy, reproduce the issue against the latest code before reporting it.

## Reporting A Vulnerability

Do not open public GitHub issues for sensitive security problems.

Instead:

1. Prepare a minimal report with reproduction steps, impact, and affected endpoints or config areas.
2. Redact all secrets, API keys, tokens, local file paths, prompts, responses, and customer data.
3. Send the report to the repository maintainer through a private channel available to the maintainer account.

Include:

- affected version or commit SHA
- whether the issue requires authenticated admin access
- whether the issue can leak secrets, route traffic incorrectly, or bypass compatibility boundaries
- affected endpoints, config fields, or daemon flags
- logs or request/response samples with secrets removed

## Scope

Security-sensitive areas in this repository include:

- admin authentication, signed-cookie sessions, bearer-token API access, and role checks
- same-origin write protection for cookie-authenticated browser admin sessions
- upstream request forwarding, protocol conversion, and header propagation
- provider API key handling in config, diffs, diagnostics, audit logs, and telemetry
- SSRF defenses for HTTP and WebSocket upstream connections
- local file and archive extraction paths used by diagnostics, bundles, and update workflows
- telemetry and audit persistence when stored request metadata may be sensitive
- manifest-verified update and rollback workflows

For the current architecture and trust boundaries, see [Security And Trust Model](docs/security-trust-model.md).

## Admin Authentication Model

- Admin and viewer credentials are configured in the operator config.
- Admin credentials can perform write operations.
- Viewer credentials are read-only.
- Browser sessions are established through `POST /api/admin/login` or the Admin UI login form.
- URL token login is not supported; tokens should not be placed in query strings.
- Signed cookies are `HttpOnly` and `SameSite=Strict`.
- Cookie `Secure` is enabled outside loopback-local HTTP development.
- Bearer token authentication is available for CLI and automation access.

## Admin Browser Same-Origin Boundary

Cookie-authenticated browser write requests require same-origin validation through `Origin` or `Referer`.

This protects browser sessions from cross-site write requests. Bearer-token automation does not rely on browser origin headers and uses token authentication instead.

When reporting admin-browser issues, state whether the issue affects:

- same-origin validation bypass
- cookie-authenticated write requests
- bearer-token API requests
- viewer/admin role separation
- token or cookie exposure

## Configuration Secrets

- Never commit real secrets to version control.
- Prefer environment variable placeholders such as `${OPENAI_API_KEY}` in config files.
- Use deployment-specific secret stores or environment files for real credentials.
- The default config examples use placeholders only.
- Diagnostics, audit entries, config diffs, and secret-status APIs are designed to redact secret-looking values, but operators should still review logs before sharing them.

## Operational Reporting Notes

When reporting a security issue, include only the minimum data needed to reproduce the behavior. Redact:

- provider API keys
- admin and viewer tokens
- cookie signing keys
- bearer tokens
- prompts, completions, and customer data
- local filesystem paths when they identify private infrastructure
