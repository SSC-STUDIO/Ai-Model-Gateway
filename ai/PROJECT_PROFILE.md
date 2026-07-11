# AI Model Gateway Project Profile

- Mission: provide a reliable OpenAI-compatible model gateway on port 18080.
- Runtime: Go three-plane architecture with gatewayd, controld, and telemetryd.
- Critical surfaces: routing, fallback, streaming, circuit breaking, config publication, telemetry, and secret handling.
- Primary risk: duplicate model or tool execution caused by retries after a response stream has started.
- Invariant: a healthy long stream may run while data continues; idle or pre-header failures terminate predictably.
- Change style: small compatibility-preserving changes with focused Go tests.
