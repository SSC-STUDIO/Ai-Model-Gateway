param(
    [string]$RepoRoot
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Resolve-RepoRoot {
    param(
        [string]$Path
    )

    $candidate = $Path
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        if ($PSScriptRoot) {
            $candidate = Split-Path -Parent $PSScriptRoot
        } else {
            $candidate = (Get-Location).Path
        }
    }

    $resolved = Resolve-Path -LiteralPath $candidate -ErrorAction Stop
    return $resolved.Path
}

$root = Resolve-RepoRoot -Path $RepoRoot
$pattern = "(^|[[:space:]])(//|#|/\\*+|\\*|<!--|--|'|;|REM)[[:space:]]*(FIXME|TODO)\\b"

Push-Location $root
try {
    & git grep -n -I -i -E -- $pattern
    $exitCode = $LASTEXITCODE

    if ($exitCode -eq 0) {
        Write-Error "Remove TODO/FIXME markers before merging."
    }

    if ($exitCode -eq 1) {
        Write-Host "No TODO/FIXME markers found in tracked comment lines."
        exit 0
    }

    throw "git grep failed with exit code $exitCode"
} finally {
    Pop-Location
}
