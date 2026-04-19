# AI Model Gateway

## Project Overview
AI model routing service with a native three-plane architecture. Go project with modular internal packages and a Preact admin frontend.

## Tech Stack
- Language: Go
- Dependencies: go.mod, go.sum
- Entry points:
  - `cmd/gatewayd/` for the data plane
  - `cmd/controld/` for the control plane
  - `cmd/telemetryd/` for the telemetry plane
- Authoring config: `configs/config.yaml`

## Development Rules
- Branch convention: work on `codex/ai-Ai-Model-Gateway` branch
- Always update CHANGELOG.md and VERSION on releases
- Run `go build ./...` and `go test ./...` before committing
- Follow CONTRIBUTING.md and SECURITY.md guidelines

## Code Style
- Follow Go conventions (gofmt, golint)
- Keep internal packages modular
- Document API changes in docs/

## Key Paths
- `cmd/gatewayd/` — data plane daemon
- `cmd/controld/` — control plane daemon
- `cmd/telemetryd/` — telemetry daemon
- `internal/control/` — control plane logic and admin API
- `internal/gateway/` — data plane runtime and OpenAI-compatible handlers
- `internal/telemetry/` — telemetry ingest, projection, and query logic
- `internal/contracts/` — cross-plane RPC contracts
- `internal/infra/` — shared infrastructure helpers
- `configs/` — configuration files
- `bin/` — compiled binaries
- `docs/` — documentation
- `web/admin/` — admin frontend (Preact + Vite)

## Architecture
- Three-plane runtime:
  - `gatewayd` owns `/v1/*` and `/-/health`
  - `controld` owns `/admin` and `/api/admin/*`
  - `telemetryd` owns telemetry ingest/query persistence
- The old `gateway` launcher has been removed. Operators now start and supervise the three daemons directly.
