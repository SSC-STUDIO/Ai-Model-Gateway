# Security Policy

## Supported versions

This project currently supports only the latest state of the `main` branch.

If you are running an older fork or local copy, reproduce the issue against the latest code before reporting it.

## Reporting a vulnerability

Please do not open public GitHub issues for sensitive security problems.

Instead:

1. Prepare a minimal report with reproduction steps, impact, and affected endpoints or config areas.
2. Redact all secrets, API keys, tokens, local file paths, and customer data.
3. Send the report to the repository maintainer through a private channel available to the maintainer account.

Include:

- Affected version or commit SHA
- Whether the issue requires authenticated admin access
- Whether the issue can leak secrets, route traffic incorrectly, or bypass compatibility boundaries
- Logs or request/response samples with secrets removed

## Scope

Security-sensitive areas in this repository include:

- admin auth and config endpoints
- upstream request forwarding and header propagation
- secret handling in config and logs
- file and multipart proxy routes
- pricing / telemetry persistence if it can expose sensitive request data

## Expectations

- Reports will be reviewed on a best-effort basis.
- Please allow time for triage before requesting public disclosure.
- If a fix requires config rotation, assume API keys should be rotated immediately.
