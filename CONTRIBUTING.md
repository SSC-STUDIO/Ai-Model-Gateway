# Contributing

Thanks for contributing to AI Model Gateway.

This project is operational software first: changes should improve reliability, observability, compatibility, or maintainability without leaking local state or secrets into the repository.

## Ground rules

- Never commit real API keys, local `config.yaml`, telemetry databases, logs, or machine-specific temp files.
- Keep changes narrowly scoped. Large refactors need a concrete operational reason.
- Preserve OpenAI-compatible behavior unless the change is explicitly about protocol compatibility.
- Prefer configuration over hard-coded policy when the behavior may need runtime tuning.
- Do not leave `TODO` or `FIXME` comments in tracked files. CI rejects common comment styles across Go, PowerShell, YAML, HTML, VBS, and shell-like files. Open an issue or document the follow-up in the pull request instead.

## Local setup

1. Copy the example config:

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
```

2. Fill in your own upstreams and set the environment variables referenced by `configs\config.yaml` such as `ADMIN_BOOTSTRAP_TOKEN`, `COOKIE_SIGNING_KEY`, and provider API keys.
3. Start the three-daemon stack. The full command examples live in [docs/cli.md](docs/cli.md). For a quick Windows smoke check you can run:

```powershell
.\scripts\verify-default-runtime.ps1
```

## Development workflow

For CI-equivalent commands and toolchain notes, see [docs/ci-local.md](docs/ci-local.md).

Before opening a pull request:

1. Format Go files:

```powershell
git ls-files '*.go' | ForEach-Object { gofmt -w $_ }
```

2. Run tests:

```powershell
go test ./...
```

3. Build the admin frontend:

```powershell
npm --prefix .\web\admin test
npm exec --prefix .\web\admin -- tsc -p .\web\admin\tsconfig.json --noEmit
npm --prefix .\web\admin run build
```

4. Verify that no `TODO`/`FIXME` comment markers remain in tracked files:

```powershell
.\scripts\check-no-todo.ps1
```

5. Verify that no local-only files are staged:

```powershell
git status --short --ignored
```

## Pull request expectations

- Explain the operational problem being solved.
- Call out protocol or routing behavior changes clearly.
- Mention config schema changes and migration impact, if any.
- Include screenshots when the admin UI changes.
- Include tests for routing, proxy, telemetry, or server behavior when practical.

## Good first contributions

- Admin UI clarity improvements that preserve the existing visual language
- More route and compatibility tests
- Better error reporting and observability
- Safer config validation and documentation improvements
