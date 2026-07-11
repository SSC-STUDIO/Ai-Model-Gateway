# AI Model Gateway Goal Contract

## Outcome
Improve gateway stability and observability without changing the public OpenAI-compatible API.

## Priority order
1. Prevent request amplification, endless retries, and duplicate streamed execution.
2. Make timeout, fallback, circuit-breaker, queue, and sticky-session behavior explicit and tested.
3. Remove secrets from tracked configuration and keep runtime recovery documented.

## Working loop
Choose one measurable defect, prove it from code or a test, implement the minimum fix, run focused tests, inspect the diff, and stop for master review.

## Stop conditions
Stop for human input before changing credentials, public API shapes, persistent telemetry schemas, or deployment topology.
