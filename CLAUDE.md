# AI Model Gateway

## Project Overview
AI model routing gateway service. Go project with modular internal architecture.

## Tech Stack
- Language: Go
- Dependencies: go.mod, go.sum
- Entry: cmd/
- Config: configs/

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
- `cmd/` — application entry points
- `internal/` — internal packages
- `configs/` — configuration files
- `bin/` — compiled binaries
- `docs/` — documentation
