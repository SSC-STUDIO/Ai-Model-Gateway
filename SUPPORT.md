# Support

## What to include when asking for help

Please include enough context to make the issue actionable:

- What you were trying to do
- Request path and model
- Expected upstream behavior
- Actual error message
- Whether the failure is in routing, proxy compatibility, pricing/telemetry, or admin UI
- Relevant logs with secrets removed
- Commit SHA or release tag

## Before opening an issue

1. Read the [README](README.md)
2. Check the [CHANGELOG](CHANGELOG.md)
3. Run the latest code on `main`, if possible
4. Confirm you did not accidentally use a local-only config or stale telemetry data

## What this repository does not provide

- Hosted gateway service
- Secret management for your environment
- Support for public reports that include live API keys or customer data

## Operational note

If you suspect a leaked key, rotate the key first and then report the issue.
