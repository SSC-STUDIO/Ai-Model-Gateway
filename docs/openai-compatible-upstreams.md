# OpenAI-Compatible Upstreams

AI Model Gateway can route to any upstream that exposes OpenAI-style HTTP endpoints. Use this guide when the upstream is a hosted aggregator, an internal proxy, LiteLLM, AIgateway.sh, Vercel AI Gateway, Helicone Gateway, or another service that accepts `/v1/chat/completions` requests with a bearer token.

This is different from the client side of AI Model Gateway. Clients talk to AI Model Gateway; AI Model Gateway then forwards to the upstream providers listed in `configs/config.yaml`.

## When To Use This

Use an OpenAI-compatible upstream when you want AI Model Gateway to keep the local operations layer while another endpoint supplies model access:

- keep local routing policy, telemetry, audit records, config publish history, and rollback in AI Model Gateway
- point one provider entry at an aggregator or internal proxy that already handles model access
- add fallback between multiple upstream endpoints, accounts, or provider keys
- test an external gateway behind the same admin, probe, benchmark, and request-log workflows

Do not use this pattern if you want AI Model Gateway to hide provider terms, pricing, rate limits, or data policy differences. Verify those with the upstream provider before production use.

## Basic Provider Entry

For upstreams that use the normal OpenAI path shape, set `base_url` to the endpoint origin or prefix before `/v1`. AI Model Gateway appends `/v1/chat/completions` when it forwards chat requests.

```yaml
providers:
  - name: compatible-upstream
    base_url: https://example-gateway.com
    api_key: "${COMPATIBLE_UPSTREAM_API_KEY}"
    provider_class: quota_limited
    models:
      - gpt-4o-mini
      - claude-3-5-sonnet-latest
    weight: 1
    timeout_ms: 180000
    same_retries: 1
    enabled: true
```

With this configuration, an inbound request for `gpt-4o-mini` is forwarded to:

```text
https://example-gateway.com/v1/chat/completions
```

If upstream docs advertise a base URL ending in `/v1`, usually remove that final `/v1` in AI Model Gateway's provider config. Keep only a required deployment prefix, if the service uses one, and verify with a probe before routing traffic.

## AIgateway.sh Example

AIgateway.sh exposes an OpenAI-compatible endpoint at `https://api.aigateway.sh/v1`. A minimal provider entry looks like this:

```yaml
providers:
  - name: aigateway-sh
    base_url: https://api.aigateway.sh
    api_key: "${AIGATEWAY_API_KEY}"
    provider_class: quota_limited
    models:
      - anthropic/claude-opus-4.7
      - openai/gpt-4o-mini
    weight: 1
    timeout_ms: 180000
    same_retries: 1
    enabled: true
```

Use the model identifiers supported by your AIgateway.sh account. Keep provider keys in environment variables and avoid committing real keys into `configs/config.yaml`.

## OpenRouter Example

OpenRouter exposes an OpenAI-compatible chat completions endpoint at `https://openrouter.ai/api/v1/chat/completions`. Because AI Model Gateway appends `/v1/chat/completions`, keep the `/api` prefix and remove the final `/v1` from the provider `base_url`:

```yaml
providers:
  - name: openrouter
    base_url: https://openrouter.ai/api
    api_key: "${OPENROUTER_API_KEY}"
    provider_class: quota_limited
    models:
      - openrouter/auto
      - openai/gpt-4o
    weight: 1
    timeout_ms: 180000
    same_retries: 1
    enabled: true
```

With this configuration, an inbound request for `openrouter/auto` is forwarded to:

```text
https://openrouter.ai/api/v1/chat/completions
```

Use model identifiers supported by your OpenRouter account. Keep the OpenRouter key in `OPENROUTER_API_KEY`, verify the route with `gateway-cli provider test openrouter`, and review OpenRouter's [API reference](https://openrouter.ai/docs/api-reference/overview) and [chat completions endpoint](https://openrouter.ai/docs/api-reference/chat-completion) before production use.

## Internal Proxy Or LiteLLM Example

For a local or internal proxy, point `base_url` at the proxy listener:

```yaml
providers:
  - name: internal-litellm
    base_url: http://127.0.0.1:4000
    api_key: "${INTERNAL_LITELLM_API_KEY}"
    provider_class: quota_limited
    models:
      - gpt-4o-mini
      - team-default
    weight: 1
    timeout_ms: 180000
    same_retries: 0
    enabled: true
```

This keeps AI Model Gateway's admin UI, telemetry, fallback, config publish, and rollback path in front of the internal proxy.

## Fallback Between Compatible Upstreams

You can list more than one compatible upstream for the same public model. AI Model Gateway can then retry or fall back when a provider returns configured retryable failures.

```yaml
routing:
  strategy: health_weighted_rr
  max_retries: 2
  retry:
    status_codes:
      - 408
      - 429
    status_code_min: 500

providers:
  - name: primary-compatible
    base_url: https://primary.example.com
    api_key: "${PRIMARY_API_KEY}"
    provider_class: quota_limited
    models:
      - team-chat
    weight: 5
    timeout_ms: 180000
    same_retries: 0
    enabled: true

  - name: fallback-compatible
    base_url: https://fallback.example.com
    api_key: "${FALLBACK_API_KEY}"
    provider_class: quota_limited
    models:
      - team-chat
    weight: 1
    timeout_ms: 180000
    same_retries: 0
    enabled: true
```

Start with the executable [provider fallback demo](../examples/provider-fallback/) if you want to see this behavior without using real API keys.

## Verify The Route

After publishing or restarting with the provider entry, verify the route before sending production traffic:

```powershell
./dist/gateway-cli provider list
./dist/gateway-cli provider test aigateway-sh
./dist/gateway-cli probe model openai/gpt-4o-mini aigateway-sh
```

Then send a client request through AI Model Gateway:

```bash
curl -sS http://127.0.0.1:18080/v1/chat/completions \
  -H "Authorization: Bearer $AI_MODEL_GATEWAY_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"Reply with ok"}]}'
```

Watch the Admin UI, telemetry, request logs, provider health, and audit records to confirm the route behaves as expected.

## Review Checklist

- The upstream supports the endpoint you are routing, usually `/v1/chat/completions`.
- The configured model names match the upstream account and route policy.
- Timeouts, rate limits, and retries are appropriate for the upstream.
- Provider terms, data policy, retention, and pricing are acceptable for your workload.
- Sensitive keys are supplied through environment variables or a secrets manager.
- The route has been verified with `gateway-cli provider test` and a model probe.
