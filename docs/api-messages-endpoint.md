# Anthropic Messages Endpoint

AI Model Gateway exposes `POST /v1/messages` for Anthropic Messages-style clients. Use it when Claude-oriented tools need to enter the same self-hosted gateway that already owns provider routing, fallback, telemetry, config publishing, and rollback.

This endpoint is part of the gateway's supported data-plane routes alongside:

- `POST /v1/chat/completions` for OpenAI Chat Completions-style clients
- `POST /v1/responses` for OpenAI Responses-style clients bridged through Chat Completions-compatible upstreams

## Endpoint

```text
POST /v1/messages
```

## What It Supports

- Anthropic Messages-shaped request and response bodies.
- Streaming and non-streaming requests.
- Anthropic provider routing through providers configured with `anthropic_base_url` or `protocol_adapter: anthropic_messages`.
- OpenAI-to-Anthropic and Anthropic-to-OpenAI bridge paths when the selected provider uses the opposite upstream protocol.
- Usage accounting translation for request telemetry, including cached input token fields where upstream responses provide them.
- Fallback routing and route telemetry when a primary upstream returns a recoverable failure.

This is not a promise of full Anthropic product API coverage. Test the exact request shapes, model features, tool behavior, streaming behavior, and usage fields your client depends on before promoting traffic.

## Provider Configuration

Configure a provider with an Anthropic-compatible upstream URL:

```yaml
providers:
  - provider_id: anthropic-primary
    anthropic_base_url: https://api.anthropic.com
    protocol_adapter: anthropic_messages
    auth:
      header_name: x-api-key
      value: ${ANTHROPIC_API_KEY}
    models:
      - public_model: claude-sonnet-4-6
        upstream_model: claude-sonnet-4-6
```

Notes:

- `anthropic_base_url` is used for `/v1/messages` upstream requests.
- `protocol_adapter: anthropic_messages` marks the provider as Anthropic Messages-compatible.
- Anthropic upstream auth normally uses `x-api-key`, not `Authorization: Bearer`.
- If both OpenAI-style and Anthropic-style upstream URLs are present in a broader config, verify the compiled snapshot before publishing.

## Non-Streaming Request

```bash
curl -sS http://localhost:18080/v1/messages \
  -H "content-type: application/json" \
  -H "x-api-key: local-gateway-client-key" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [
      {"role": "user", "content": "Reply with one short sentence."}
    ],
    "max_tokens": 128
  }'
```

## Streaming Request

```bash
curl -N http://localhost:18080/v1/messages \
  -H "content-type: application/json" \
  -H "x-api-key: local-gateway-client-key" \
  -d '{
    "model": "claude-sonnet-4-6",
    "messages": [
      {"role": "user", "content": "Stream a two-item checklist."}
    ],
    "max_tokens": 128,
    "stream": true
  }'
```

## Bridge Behavior

The gateway chooses a provider from the compiled snapshot, then builds a compatibility plan for the client request and selected upstream:

| Client request | Selected upstream | Gateway behavior |
| --- | --- | --- |
| Anthropic Messages | Anthropic Messages | Forward to `/v1/messages` with Anthropic-compatible auth and response shape |
| Anthropic Messages | OpenAI Chat Completions | Convert the request to Chat Completions, forward to `/v1/chat/completions`, and adapt the response back to Messages |
| OpenAI Chat Completions | Anthropic Messages | Convert the request to Messages, forward to `/v1/messages`, and adapt the response back to Chat Completions |
| OpenAI Responses | Chat Completions-compatible path | Convert Responses-style input to Chat Completions-compatible upstream traffic and adapt the response back |

Fallback behavior still applies. For example, if a primary Anthropic upstream returns a recoverable `429`, the gateway can try a fallback provider and record route telemetry such as bridged or fallback route mode.

## Verify Before Publishing

Run local checks before wiring real provider keys:

```bash
go test ./internal/gateway/api -run 'TestHandleMessages|TestHandleChatCompletionBridge|TestHandleResponses' -count=1
go test ./cmd/gatewayd -run TestGatewaydBridgeAnthropicE2EWithLocalUpstreams -count=1
```

For a shorter visitor-facing trial, start with:

- [15-minute evaluation path](evaluate-in-15-minutes.md)
- [OpenAI Anthropic gateway page](https://ssc-studio.github.io/Ai-Model-Gateway/openai-anthropic-gateway.html)
- [Architecture notes](architecture.md)
- [Provider fallback and health operations](provider-fallback-health.md)

## Limitations

- The gateway does not claim full coverage of every OpenAI or Anthropic product API.
- Unsupported OpenAI-family routes such as embeddings, image/audio APIs, Assistants, Batch, and Realtime WebSocket proxying are outside the current data-plane route set.
- Multimodal, tool, and streaming behavior should be verified against the exact upstream provider and client SDK version you plan to use.
- For production changes, use config preview, diff, publish history, and rollback rather than editing a live runtime file.
