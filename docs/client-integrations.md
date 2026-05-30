# Client Integrations

Use this guide when you want existing local tools or OpenAI-compatible SDKs to send traffic through AI Model Gateway.

AI Model Gateway exposes one data-plane listener for client traffic. OpenAI-compatible clients should use the gateway origin plus `/v1`; Anthropic Messages-style clients should use the same gateway origin through their Anthropic base URL setting.

## Quick Start

After the runtime is started, print client snippets from the local gateway configuration:

```bash
./dist/aigw clients print -config-dir configs
```

For a running gateway on `http://127.0.0.1:18080`, the OpenAI-compatible base URL is:

```text
http://127.0.0.1:18080/v1
```

Use the gateway API key configured for application clients. Do not use provider API keys in client tools; provider keys stay in AI Model Gateway's provider configuration.

## Supported Local Tool Updates

`aigw clients apply` can update selected local tool config files and optionally back them up first:

```bash
./dist/aigw clients apply -tools codex,claude-code,openclaw -api-key "$GATEWAY_API_KEY" -dry-run
./dist/aigw clients apply -tools codex,claude-code,openclaw -api-key "$GATEWAY_API_KEY"
```

| Tool | File | Fields |
| --- | --- | --- |
| Codex | `~/.codex/config.toml` | `openai_base_url` |
| Claude Code | `~/.claude/settings.json` | `env.ANTHROPIC_BASE_URL`, optional `env.ANTHROPIC_AUTH_TOKEN` |
| OpenClaw | `~/.openclaw/openclaw.json` | `models.providers.ai-model-gateway`, optional default primary model |

Use `-tools all` to target every supported tool, or pass a comma-separated subset such as `codex,openclaw`.

## Environment Variables

Many OpenAI-compatible clients can be pointed at AI Model Gateway with environment variables:

```bash
export OPENAI_BASE_URL="http://127.0.0.1:18080/v1"
export OPENAI_API_KEY="$GATEWAY_API_KEY"
```

PowerShell:

```powershell
$env:OPENAI_BASE_URL = "http://127.0.0.1:18080/v1"
$env:OPENAI_API_KEY = $env:GATEWAY_API_KEY
```

For Anthropic Messages-style clients:

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:18080/v1"
export ANTHROPIC_AUTH_TOKEN="$GATEWAY_API_KEY"
```

The gateway supports `/v1/chat/completions`, `/v1/messages`, and `/v1/responses`. Test the exact request shapes your client depends on before routing production traffic.

## Generic OpenAI SDK

Most OpenAI-compatible SDKs need two values: the base URL and the gateway API key.

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:18080/v1",
    api_key="your-gateway-api-key",
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Reply with ok"}],
)

print(response.choices[0].message.content)
```

Use a model name that exists in the active AI Model Gateway routing snapshot. The gateway maps public model names to configured upstream provider models.

## Curl Smoke Test

Before changing tool config files, send one request through the data plane:

```bash
curl -sS http://127.0.0.1:18080/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Reply with ok"}]}'
```

Then inspect the Admin UI request logs, provider health, and telemetry views to confirm that the request was routed through the expected provider.

## Safety Notes

- Keep upstream provider keys in AI Model Gateway provider config, not in local client configs.
- Use a gateway client key for tools and SDKs.
- Run `aigw clients apply -dry-run` before writing local tool config files.
- Keep backups enabled unless you already manage these files with another tool.
- If the gateway is bound to `0.0.0.0`, `aigw clients print` resolves the client-facing local origin as `127.0.0.1`.
- If your gateway is behind a reverse proxy, pass `-gateway-url https://your-gateway.example.com` so generated client settings use the external URL.

## Related Docs

- [CLI guide](cli.md)
- [Messages endpoint](api-messages-endpoint.md)
- [OpenAI-compatible upstreams](openai-compatible-upstreams.md)
- [Troubleshooting](troubleshooting.md)
