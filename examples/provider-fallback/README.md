# Provider Fallback Demo

This example is a small executable test for the provider fallback path.

It starts two local OpenAI-compatible fake upstreams:

- `primary-provider` returns HTTP `429` for the requested model.
- `fallback-provider` returns a successful Chat Completions response.

The gateway snapshot routes the public model `demo-primary` to the primary provider and configures `demo-fallback` as its fallback model. The test sends one request through the gateway handler and verifies that:

- the client receives the fallback response
- the primary upstream was attempted first
- the fallback upstream was attempted second
- the fallback request body was rewritten to the fallback upstream model
- telemetry records `route_mode=model_fallback`

Run it from the repository root:

```bash
go test ./examples/provider-fallback -run TestProviderFallbackDemo -v
```

Expected output includes a log line similar to:

```text
primary returned 429; fallback provider=fallback-provider route_mode=model_fallback effective_model=demo-fallback-upstream
```

This demo uses local fake upstream servers and a test-only SSRF checker override. Production gateway runs keep the default SSRF protection enabled.
