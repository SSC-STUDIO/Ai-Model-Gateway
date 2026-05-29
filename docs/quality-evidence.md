# Quality Evidence

Use this page when evaluating whether AI Model Gateway is mature enough to inspect, list, deploy for a trial, or review for contribution.

The project is still early-stage by public adoption signals, so this document focuses on evidence that can be checked directly: CI gates, reproducible local commands, runtime smoke tests, feature proof points, and explicit limits.

## Current Snapshot

Checked on 2026-05-30:

| Signal | Evidence |
| --- | --- |
| Repository PR backlog | `0` open pull requests in `SSC-STUDIO/Ai-Model-Gateway` |
| Repository issue backlog | `0` open issues in `SSC-STUDIO/Ai-Model-Gateway` |
| GitHub stars | `2` stars, so public adoption is still early |
| Main CI | Current status is exposed by the README badge and the [CI workflow run list](https://github.com/SSC-STUDIO/Ai-Model-Gateway/actions/workflows/ci.yml) |
| Latest release | [v1.4.4](https://github.com/SSC-STUDIO/Ai-Model-Gateway/releases/tag/v1.4.4) |
| Project website | [ssc-studio.github.io/Ai-Model-Gateway](https://ssc-studio.github.io/Ai-Model-Gateway/) |

The star count is intentionally listed here because it matters for some curated directories. Treat the project as technically inspectable but not yet adoption-proven by broad community usage.

## CI Gates

The main CI workflow runs on pull requests and pushes to `main`.

| Gate | What it checks |
| --- | --- |
| Go formatting | Runs `gofmt` over tracked Go files and fails on diffs |
| Go lint | Runs `golangci-lint` with a five-minute timeout |
| Vulnerability scan | Runs `govulncheck` over all Go packages |
| TODO/FIXME policy | Rejects tracked comment markers through `scripts/check-no-todo.ps1` |
| Go test suite | Runs `go test -timeout 10m -coverprofile=coverage.out -covermode=atomic ./...` |
| Coverage threshold | Enforces a 60 percent total threshold on Linux |
| Binary build | Builds `aigw`, `gatewayd`, `controld`, `telemetryd`, and `gateway-cli` |
| Admin frontend | Runs `npm ci`, `npm run build`, and `npm test` in `web/admin` |
| Linux runtime smoke | Builds a manifest-verified bundle and starts `aigw supervise` against health endpoints |
| Windows runtime smoke | Runs `scripts/verify-default-runtime.ps1` |
| Runtime bundles | Builds and uploads Linux and Windows bundle artifacts |
| Docker image | Builds the runtime image and pushes `main` to GHCR on main-branch pushes |

The coverage artifact names are OS-specific (`coverage-report-${{ matrix.os }}`) so the Linux and Windows matrix jobs do not overwrite each other.

## Local Reproduction

The local checklist mirrors the CI workflow:

- [Local CI checklist](ci-local.md)
- [Installation guide](installation.md)
- [Deployment guide](deployment.md)
- [Troubleshooting guide](troubleshooting.md)

Fast local loop:

```powershell
go test ./...
npm --prefix .\web\admin test
npm --prefix .\web\admin run build
```

Full local gate:

```powershell
git ls-files '*.go' | ForEach-Object { gofmt -w $_ }
git diff --exit-code

go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
go test -timeout 10m -coverprofile=coverage.out -covermode=atomic ./...

go build ./cmd/aigw
go build ./cmd/gatewayd
go build ./cmd/controld
go build ./cmd/telemetryd
go build ./cmd/gateway-cli

npm --prefix .\web\admin ci
npm --prefix .\web\admin run build
npm --prefix .\web\admin test

.\scripts\check-no-todo.ps1 -RepoRoot .
```

Runtime smoke check on Windows:

```powershell
.\scripts\verify-default-runtime.ps1
```

## Feature Proof Points

These are the most useful entry points for validating that the repository is more than a request-forwarding proxy.

| Area | Evidence |
| --- | --- |
| Three-plane runtime | [Architecture guide](architecture.md) documents `aigw`, `gatewayd`, `controld`, `telemetryd`, HTTP routes, IPC, state files, and startup order |
| Config publishing | [Config publish and rollback](config-publish-rollback.md) explains authoring config, compiled snapshots, publish history, audit, and rollback |
| Provider fallback | [Provider fallback and health operations](provider-fallback-health.md) covers probes, cooldown, fallback, telemetry, and rollback decisions |
| Executable fallback proof | [Provider fallback demo](../examples/provider-fallback/) proves a primary `429` can be served through a fallback provider |
| Protocol entry points | [Messages endpoint guide](api-messages-endpoint.md) documents the Anthropic Messages-compatible route and limits |
| Operator CLI | [CLI guide](cli.md) covers `aigw`, `gateway-cli`, config workflows, runtime checks, update flows, and direct daemon flags |
| Security model | [Security and trust model](security-trust-model.md) covers admin auth, same-origin writes, secrets, SSRF, telemetry, local files, and update trust |
| Evaluation path | [15-minute evaluation path](evaluate-in-15-minutes.md) gives a short fit check and runtime trial sequence |
| Roadmap | [Project roadmap](roadmap.md) lists the current focus and contribution areas |

## Capability Boundaries

These limits are intentional and should be considered before listing or adopting the project.

- AI Model Gateway is not a hosted model marketplace.
- It does not claim the largest provider catalog.
- It is not a general-purpose replacement for Kong, Envoy, or every API gateway use case.
- It does not claim complete OpenAI or Anthropic product API coverage.
- The Anthropic Messages and OpenAI-compatible routes should be tested against the exact request shapes your clients need.
- Public adoption is still early; use CI, local reproduction, and feature proof points rather than star count as the first technical filter.

## Maintainer Review Path

For curated-list maintainers or reviewers, a practical review sequence is:

1. Check the latest [CI workflow](https://github.com/SSC-STUDIO/Ai-Model-Gateway/actions/workflows/ci.yml).
2. Read the [Architecture guide](architecture.md) to confirm the project scope.
3. Read the [Security and trust model](security-trust-model.md) to confirm auth, secret, SSRF, telemetry, and deployment boundaries.
4. Run the [15-minute evaluation path](evaluate-in-15-minutes.md) if a local trial is needed.
5. Run the [provider fallback demo](../examples/provider-fallback/) to verify one differentiating behavior.
6. Check [Capability Boundaries](#capability-boundaries) before approving broad compatibility wording.
7. Use [Discussion #25](https://github.com/SSC-STUDIO/Ai-Model-Gateway/discussions/25) for questions about scope, maturity, or roadmap direction.
