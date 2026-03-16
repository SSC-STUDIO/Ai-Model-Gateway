$ErrorActionPreference = "Stop"

$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } elseif ($PSCommandPath) { Split-Path -Parent $PSCommandPath } else { (Get-Location).Path }
$legacyRoot = Split-Path -Parent $scriptDir

function Test-ProjectRoot {
    param(
        [string]$Path
    )

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $false
    }

    $goMod = Join-Path $Path "go.mod"
    $cmdGateway = Join-Path $Path "cmd\gateway"
    return (Test-Path $goMod -PathType Leaf) -and (Test-Path $cmdGateway -PathType Container)
}

function Resolve-ProjectRoot {
    $candidates = @(
        $env:AIGW_PROJECT_ROOT,
        $legacyRoot,
        "C:\Users\96152\My-Project\Active\Apps\AI-Model-Gateway"
    ) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }

    foreach ($candidate in $candidates) {
        if (Test-ProjectRoot -Path $candidate) {
            return $candidate
        }
    }

    $searchBase = Split-Path -Parent (Split-Path -Parent $legacyRoot)
    $match = Get-ChildItem -Path $searchBase -Filter go.mod -File -Recurse -ErrorAction SilentlyContinue |
        Where-Object {
            $_.FullName -like "*AI-Model-Gateway*" -and
            (Test-Path (Join-Path $_.Directory.FullName "cmd\gateway") -PathType Container)
        } |
        Select-Object -First 1

    if ($match) {
        return $match.Directory.FullName
    }

    throw "unable to locate AI-Model-Gateway module root; set AIGW_PROJECT_ROOT explicitly"
}

$projectRoot = Resolve-ProjectRoot
$binaryPath = Join-Path $projectRoot "bin\gateway.exe"
$configPath = Join-Path $projectRoot "configs\config.yaml"
$logsDir = Join-Path $projectRoot "logs"
$stdoutLog = Join-Path $logsDir "gateway.stdout.log"
$stderrLog = Join-Path $logsDir "gateway.stderr.log"

New-Item -ItemType Directory -Force -Path $logsDir | Out-Null
Set-Location $projectRoot

if (Get-NetTCPConnection -LocalPort 18080 -State Listen -ErrorAction SilentlyContinue) {
    exit 0
}

if (-not (Test-Path $binaryPath)) {
    throw "gateway binary not found: $binaryPath"
}

if (-not (Test-Path $configPath)) {
    throw "gateway config not found: $configPath"
}

Start-Process `
    -FilePath $binaryPath `
    -ArgumentList @("-config", $configPath) `
    -WorkingDirectory $projectRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput $stdoutLog `
    -RedirectStandardError $stderrLog | Out-Null
