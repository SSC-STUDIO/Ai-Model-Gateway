# AI Model Gateway

## Project Overview
AI model routing gateway service. Go project with modular internal architecture.

## Tech Stack
- Language: Go
- Dependencies: go.mod, go.sum
- Entry: cmd/gateway/
- Config: configs/config.yaml

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
- `cmd/gateway/` — application entry point
- `internal/` — internal packages (core, app, infra, adminapi, etc.)
- `configs/` — configuration files
- `bin/` — compiled binaries
- `docs/` — documentation
- `web/admin/` — admin frontend (Preact + Vite)

## Architecture
- Clean separation: core (domain), app (application), infra (infrastructure)
- Admin API: `/api/admin/*`
- Gateway proxy: `/v1/*` (OpenAI-compatible)
