$ErrorActionPreference = "Stop"

$projectRoot = "C:\Users\96152\My-Project\Application_Project\AI-Model-Gateway"
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
