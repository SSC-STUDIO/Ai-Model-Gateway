# Maintainers

CODEOWNERS currently routes repository review to `@Crs10259`.

Use these areas to assign review attention and describe pull request impact:

| Area | Paths | Review focus |
| --- | --- | --- |
| Routing and proxy compatibility | `internal/router`, `internal/proxy`, `configs` | OpenAI-compatible behavior, fallback semantics, config schema impact |
| Control plane and admin API | `cmd/controld`, `internal/control`, `web/admin/src/utils/controlApi.ts` | Auth boundaries, API shape, runtime status correctness |
| Admin UI | `web/admin` | UI behavior, accessibility, charts, frontend tests, screenshots |
| Telemetry and pricing | `internal/telemetry`, `internal/pricing`, `web/admin/src/components/tabs` | Data integrity, cost calculation, retention, display accuracy |
| Packaging and release | `cmd/aigw`, `internal/release`, `.github/workflows`, `scripts` | Cross-platform builds, manifests, runtime smoke tests |
| Documentation and examples | `README.md`, `docs`, `configs/*.example.yaml` | Fresh-clone usability, secret redaction, migration notes |

## Review Expectations

- Ask for a focused reviewer when a change affects protocol compatibility, routing, auth, pricing, or release packaging.
- Include local validation output in the pull request for the impacted area.
- Include screenshots or short recordings for visible admin UI changes.
- Document config schema changes and migration impact before merge.
