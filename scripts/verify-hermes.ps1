$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$toolchain = Get-ChildItem (Join-Path $env:USERPROFILE 'go/pkg/mod/golang.org/toolchain@*/bin/go.exe') -ErrorAction SilentlyContinue | Sort-Object FullName -Descending | Select-Object -First 1
if (-not $toolchain) { throw 'Go toolchain not found' }
$env:GOROOT = Split-Path -Parent (Split-Path -Parent $toolchain.FullName)
$env:PATH = (Split-Path -Parent $toolchain.FullName) + ';' + $env:PATH
$env:GOCACHE = Join-Path $repo '.gocache'
$env:GOMODCACHE = Join-Path $repo '.gomodcache'
Set-Location $repo
go test ./internal/gateway/api ./internal/core ./internal/control/compiler
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
