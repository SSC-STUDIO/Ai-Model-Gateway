# AI Model Gateway - Agent Guide

This document provides comprehensive guidance for AI agents (Claude Code, Codex, OpenClaw) working on the AI Model Gateway codebase.

## Project Overview

AI Model Gateway is a production-grade AI model routing service with a **native three-plane architecture**:

- **Data Plane (`gatewayd`)**: Handles all `/v1/*` API requests, OpenAI-compatible endpoints, and health checks
- **Control Plane (`controld`)**: Manages configuration, admin API (`/api/admin/*`), and admin frontend
- **Telemetry Plane (`telemetryd`)**: Collects, stores, and queries metrics, events, and pricing data

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Client Requests                          │
└─────────────────────────────────────────────────────────────────┘
                                │
        ┌───────────────────────┼───────────────────────┐
        ▼                       ▼                       ▼
┌───────────────┐       ┌───────────────┐       ┌───────────────┐
│   gatewayd    │       │   controld    │       │  telemetryd   │
│   :18083      │       │   :18081      │       │   :18082      │
│               │       │               │       │               │
│ /v1/chat/*    │       │ /admin        │       │ RPC ingest    │
│ /v1/models    │       │ /api/admin/*  │       │ SQLite store  │
│ /-/health     │       │ Config mgmt   │       │ Query API     │
└───────┬───────┘       └───────┬───────┘       └───────┬───────┘
        │                       │                       │
        └───────────────────────┼───────────────────────┘
                                │
                    Unix Domain Sockets (RPC)
```

## Key Directories

| Path | Purpose |
|------|---------|
| `cmd/gatewayd/` | Data plane daemon entry point |
| `cmd/controld/` | Control plane daemon entry point |
| `cmd/telemetryd/` | Telemetry daemon entry point |
| `cmd/gateway-cli/` | CLI tool for operations |
| `internal/gateway/` | Request routing, OpenAI handlers, caching |
| `internal/control/` | Config management, admin API, publisher |
| `internal/telemetry/` | Event ingest, projections, queries |
| `internal/contracts/` | Cross-plane RPC interface definitions |
| `internal/core/` | Core config types and validation |
| `internal/infra/` | Shared utilities (HTTP, auth, logging) |
| `web/admin/` | Preact + Vite admin frontend |
| `configs/` | Configuration files |
| `deploy/` | Deployment artifacts (systemd, docker-compose) |

## Configuration Schema

The configuration is defined in `internal/core/config.go`. Key sections:

```yaml
server:
  listen: ":18080"
  read_timeout_ms: 30000
  idle_timeout_ms: 120000

admin:
  enabled: true
  bootstrap_token: "${ADMIN_BOOTSTRAP_TOKEN}"
  cookie_signing_key: "${COOKIE_SIGNING_KEY}"
  tokens:
    - name: "admin"
      token: "${ADMIN_TOKEN}"
      role: "admin"

routing:
  strategy: health_weighted_rr
  max_retries: 2
  health:
    enabled: true
    interval_sec: 10

providers:
  - name: "openai"
    base_url: "https://api.openai.com/v1"
    api_key: "${OPENAI_API_KEY}"
    models:
      - public_model: "gpt-4"
        upstream_model: "gpt-4-turbo"

telemetry:
  sqlite_path: "data/telemetry.db"
  retention_days: 365

pricing:
  cache_path: "data/pricing-cache.json"
  refresh_interval_hours: 12
```

## Common Tasks

### Adding a New API Endpoint

1. Define the route in `internal/control/api/routes.go`
2. Create the handler function following existing patterns
3. Add types in `internal/contracts/` if cross-plane communication is needed
4. Update frontend in `web/admin/src/` if admin UI changes

### Modifying Frontend

1. Edit files in `web/admin/src/`
2. Build: `cd web/admin && npm run build`
3. Copy to embedded assets: `cp -r web/admin/dist/* internal/control/api/embedded_admin_dist/`
4. Rebuild controld: `go build ./cmd/controld`

### Adding CLI Commands

1. Create new file in `cmd/gateway/commands/`
2. Register in `cmd/gateway-cli/main.go`
3. Follow the pattern of existing commands

### Database Schema Changes

Telemetry uses SQLite. Schema migrations should be handled in `internal/telemetry/store/`.

## Code Patterns

### Error Handling

```go
// Prefer wrapped errors with context
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}

// Use structured logging
log.Printf("[component] operation succeeded: %s", result)
```

### Configuration Access

```go
// Config is validated before use
cfg, err := core.LoadConfig(path)
if err != nil {
    return fmt.Errorf("load config: %w", err)
}
```

### RPC Communication

```go
// Cross-plane communication via Unix sockets
client := gatewaycontrol.NewClient(socketPath)
status, err := client.GetStatus(ctx)
```

## Testing

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/gateway/...

# Run with coverage
go test -cover ./...

# Frontend type check
cd web/admin && npx tsc --noEmit
```

## Build Commands

```bash
# Build all daemons
go build ./cmd/...

# Build specific daemon
go build -o bin/gatewayd ./cmd/gatewayd
go build -o bin/controld ./cmd/controld
go build -o bin/telemetryd ./cmd/telemetryd

# Build CLI
go build -o bin/gateway-cli ./cmd/gateway-cli

# Build frontend
cd web/admin && npm run build
```

## Deployment

### Systemd

```bash
# Copy service files
sudo cp deploy/*.service /etc/systemd/system/

# Enable and start
sudo systemctl enable --now gatewayd controld telemetryd
```

### Docker Compose

```bash
cd deploy
docker compose up -d
```

### Manual Start

```bash
./bin/telemetryd -config configs/config.yaml &
./bin/controld -config configs/config.yaml &
./bin/gatewayd -config configs/config.yaml &
```

## API Reference

### Admin API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/admin/login` | POST | Authenticate with token |
| `/api/admin/session` | GET | Get current session info |
| `/api/admin/status` | GET | System status |
| `/api/admin/config` | GET | Get current config |
| `/api/admin/config/validate` | POST | Validate config |
| `/api/admin/config/update` | POST | Update config |
| `/api/admin/config/history` | GET | Config revision history |
| `/api/admin/telemetry` | GET | Telemetry data |
| `/api/admin/overview` | GET | Overview metrics |
| `/api/admin/timeseries` | GET | Time series data |
| `/api/admin/benchmark` | GET | Model benchmark data |

### Gateway API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | Chat completions |
| `/v1/completions` | POST | Legacy completions |
| `/v1/models` | GET | List models |
| `/-/health` | GET | Health check |

## Security Considerations

1. **Never commit secrets**: API keys, tokens, and passwords must be environment variables
2. **Token authentication**: Admin API requires cookie or bearer token auth
3. **Same-origin policy**: Write operations require same-origin validation
4. **Input validation**: All user input must be validated

## Known Issues

- Embedded admin assets must be rebuilt after frontend changes
- Telemetry database can grow large; consider retention settings
- Provider health checks may fail if upstream is unreachable during startup

## Contributing

See `CONTRIBUTING.md` for detailed contribution guidelines.

## Related Documentation

- `CLAUDE.md` - Claude Code specific instructions
- `CONTRIBUTING.md` - Contribution guidelines
- `SECURITY.md` - Security policy
- `docs/cli.md` - CLI documentation
