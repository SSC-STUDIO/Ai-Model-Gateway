# Local CI Checklist

This guide mirrors the checks that GitHub Actions runs for pull requests. Run it before asking for review when a change touches Go code, admin UI code, packaging, scripts, or config behavior.

## Toolchain

- Go: use the version declared in [go.mod](../go.mod).
- Node.js: use Node 20 for the admin UI.
- npm: use `npm ci` inside `web/admin` when preparing a fresh checkout.

On networks where the default Go module proxy is slow, configure a mirror:

```powershell
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.google.cn
```

If Go was installed outside `PATH`, prepend the binary directory for the current PowerShell session:

```powershell
$env:PATH = "D:\EliuaK_Csy\Working-Paper\My-Program\_tools\go1.25.9\bin;$env:PATH"
```

## Fast Loop

Use this for most code changes:

```powershell
go test ./...
npm --prefix .\web\admin test
npm --prefix .\web\admin run build
```

## Full Local Gate

Use this before larger pull requests:

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
git status --short --ignored
```

## Runtime Smoke Checks

Windows:

```powershell
.\scripts\verify-default-runtime.ps1
```

Linux release-bundle smoke checks are covered by CI. Locally, keep `configs/config.yaml`, runtime directories, logs, coverage files, and generated frontend output unstaged.
