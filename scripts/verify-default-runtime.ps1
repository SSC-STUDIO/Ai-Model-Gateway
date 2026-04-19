param(
    [int]$Port = 18080,
    [switch]$SkipBuild,
    [switch]$KeepWorkdir
)

$ErrorActionPreference = "Stop"

$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } elseif ($PSCommandPath) { Split-Path -Parent $PSCommandPath } else { (Get-Location).Path }
$projectRoot = Split-Path -Parent $scriptDir

if (-not (Test-Path (Join-Path $projectRoot "go.mod") -PathType Leaf)) {
    throw "unable to locate go.mod from $projectRoot"
}

$controlPort = $Port + 1
$runId = [guid]::NewGuid().ToString("N")
$workdir = Join-Path $env:TEMP ("aigw-three-plane-smoke-" + $runId)
$runtimeRoot = Join-Path $workdir ".gateway-runtime"
$gatewaydPath = Join-Path $workdir "gatewayd.exe"
$controldPath = Join-Path $workdir "controld.exe"
$telemetrydPath = Join-Path $workdir "telemetryd.exe"
$configPath = Join-Path $workdir "config.yaml"
$telemetryStdoutPath = Join-Path $workdir "telemetryd.stdout.log"
$telemetryStderrPath = Join-Path $workdir "telemetryd.stderr.log"
$gatewaydStdoutPath = Join-Path $workdir "gatewayd.stdout.log"
$gatewaydStderrPath = Join-Path $workdir "gatewayd.stderr.log"
$controldStdoutPath = Join-Path $workdir "controld.stdout.log"
$controldStderrPath = Join-Path $workdir "controld.stderr.log"
$healthURL = "http://127.0.0.1:$Port/-/health"
$modelsURL = "http://127.0.0.1:$Port/v1/models"
$adminURL = "http://127.0.0.1:$controlPort/admin"
$statusURL = "http://127.0.0.1:$controlPort/api/admin/status"
$historyURL = "http://127.0.0.1:$controlPort/api/admin/config/history"
$smokeModel = "smoke-model"
$adminToken = "0123456789abcdef0123456789abcdef"
$gatewaySocket = "aigw-gateway-control-$runId"
$telemetryIngestSocket = "aigw-telemetry-ingest-$runId"
$telemetryQuerySocket = "aigw-telemetry-query-$runId"
$telemetryDataDir = Join-Path $runtimeRoot "telemetry"
$gatewayDataDir = Join-Path $runtimeRoot "gateway"
$controlDataDir = Join-Path $runtimeRoot "control"
$wslProject = $null

function Get-WslContext {
    param(
        [string]$Path
    )

    if ($Path -match '^\\\\wsl\.localhost\\([^\\]+)\\(.+)$') {
        return [pscustomobject]@{
            Distro = $matches[1]
            Path = "/" + (($matches[2] -replace '\\', '/').TrimStart('/'))
        }
    }

    return $null
}

function Convert-ToWslSingleQuotedLiteral {
    param(
        [string]$Value
    )

    return "'" + ($Value -replace "'", "'`"'`"'") + "'"
}

function Convert-WindowsPathToWslPath {
    param(
        [string]$Path
    )

    if ($Path -match '^([A-Za-z]):\\(.*)$') {
        $drive = $matches[1].ToLowerInvariant()
        $rest = $matches[2] -replace '\\', '/'
        return "/mnt/$drive/$rest"
    }

    return $null
}

$wslProject = Get-WslContext -Path $projectRoot

function Wait-ForReady {
    param(
        [string]$URL,
        [int]$TimeoutSec = 30,
        [hashtable]$Headers = @{}
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        try {
            $resp = Invoke-WebRequest -UseBasicParsing -Uri $URL -TimeoutSec 3 -Headers $Headers
            if ($resp.StatusCode -eq 200) {
                return $resp
            }
        } catch {
        }
        Start-Sleep -Milliseconds 500
    }

    throw "timed out waiting for $URL"
}

function Wait-ForJsonCondition {
    param(
        [string]$URL,
        [scriptblock]$Condition,
        [string]$Description,
        [int]$TimeoutSec = 30,
        [hashtable]$Headers = @{}
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    $lastResponse = ""
    while ((Get-Date) -lt $deadline) {
        try {
            $resp = Invoke-WebRequest -UseBasicParsing -Uri $URL -TimeoutSec 3 -Headers $Headers
            if ($resp.StatusCode -eq 200) {
                $lastResponse = $resp.Content
                $body = $resp.Content | ConvertFrom-Json
                if (& $Condition $body) {
                    return [pscustomobject]@{
                        Response = $resp
                        Body = $body
                    }
                }
            }
        } catch {
            $lastResponse = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 500
    }

    throw "timed out waiting for $Description at $URL. Last response: $lastResponse"
}

function Assert-ArrayContains {
    param(
        [object]$Values,
        [string]$Expected,
        [string]$Label
    )

    $items = @($Values)
    if ($items -notcontains $Expected) {
        throw "$Label did not contain '$Expected': $($items -join ', ')"
    }
}

function Publish-Binary {
    param(
        [string]$Package,
        [string]$OutputPath
    )

    if ($wslProject) {
        $outputPathWsl = Convert-WindowsPathToWslPath -Path $OutputPath
        if ([string]::IsNullOrWhiteSpace($outputPathWsl)) {
            throw "unable to convert Windows output path to WSL path: $OutputPath"
        }

        $command = "cd $(Convert-ToWslSingleQuotedLiteral $wslProject.Path) && GOOS=windows GOARCH=amd64 go build -o $(Convert-ToWslSingleQuotedLiteral $outputPathWsl) $(Convert-ToWslSingleQuotedLiteral $Package)"
        & wsl.exe -d $wslProject.Distro bash -lc $command
        if ($LASTEXITCODE -ne 0) {
            throw "go build $Package failed with exit code $LASTEXITCODE"
        }
        return
    }

    Push-Location $projectRoot
    try {
        go build -o $OutputPath $Package
        if ($LASTEXITCODE -ne 0) {
            throw "go build $Package failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
}

function Copy-ExistingBinary {
    param(
        [string]$SourcePath,
        [string]$OutputPath
    )

    if (-not (Test-Path $SourcePath -PathType Leaf)) {
        throw "-SkipBuild was used but $SourcePath does not exist"
    }
    Copy-Item $SourcePath $OutputPath -Force
}

function Start-Plane {
    param(
        [string]$BinaryPath,
        [string[]]$Arguments,
        [string]$StdoutPath,
        [string]$StderrPath
    )

    return Start-Process -FilePath $BinaryPath `
        -ArgumentList $Arguments `
        -PassThru `
        -WindowStyle Hidden `
        -WorkingDirectory $workdir `
        -RedirectStandardOutput $StdoutPath `
        -RedirectStandardError $StderrPath
}

function Show-Log {
    param(
        [string]$Label,
        [string]$Path
    )

    if (Test-Path $Path) {
        Write-Host "--- $Label ---"
        Get-Content -Path $Path -ErrorAction SilentlyContinue
    }
}

New-Item -ItemType Directory -Force -Path $workdir, $runtimeRoot, $telemetryDataDir, $gatewayDataDir, $controlDataDir | Out-Null
Remove-Item $telemetryStdoutPath, $telemetryStderrPath, $gatewaydStdoutPath, $gatewaydStderrPath, $controldStdoutPath, $controldStderrPath -Force -ErrorAction SilentlyContinue

$binaries = @(
    @{ Package = "./cmd/gatewayd";   Output = $gatewaydPath;   Dist = Join-Path $projectRoot "dist/gatewayd.exe" },
    @{ Package = "./cmd/controld";   Output = $controldPath;   Dist = Join-Path $projectRoot "dist/controld.exe" },
    @{ Package = "./cmd/telemetryd"; Output = $telemetrydPath; Dist = Join-Path $projectRoot "dist/telemetryd.exe" }
)

foreach ($binary in $binaries) {
    if (-not $SkipBuild) {
        Publish-Binary -Package $binary.Package -OutputPath $binary.Output
    } else {
        Copy-ExistingBinary -SourcePath $binary.Dist -OutputPath $binary.Output
    }
}

$config = @"
server:
  listen: :$Port

admin:
  enabled: true
  bootstrap_token: "$adminToken"
  cookie_signing_key: "abcdef0123456789abcdef0123456789"
  language: en

routing:
  strategy: health_weighted_rr
  health:
    enabled: false
    interval_sec: 10
    timeout_ms: 2000
    path: /v1/models

providers:
  - name: smoke-provider
    base_url: https://example.invalid/v1
    api_key: sk-smoke-local-do-not-use
    provider_class: quota_limited
    models:
      - $smokeModel
    weight: 1
    timeout_ms: 10000
    same_retries: 0
    enabled: true

telemetry:
  sqlite_path: telemetry.db

pricing:
  cache_path: pricing-cache.json

compat:
  bridge:
    enabled: false
    exclude_user_agents: []
    rules: []
  fallback:
    enabled: false
    detect_repetition: false
    models: {}
"@

Set-Content -Path $configPath -Value $config -Encoding ascii

$telemetryProc = $null
$gatewaydProc = $null
$controldProc = $null

try {
    $telemetryProc = Start-Plane -BinaryPath $telemetrydPath -Arguments @(
        "-ingest", $telemetryIngestSocket,
        "-query", $telemetryQuerySocket,
        "-data-dir", $telemetryDataDir
    ) -StdoutPath $telemetryStdoutPath -StderrPath $telemetryStderrPath

    $gatewaydProc = Start-Plane -BinaryPath $gatewaydPath -Arguments @(
        "-listen", "127.0.0.1:$Port",
        "-control", $gatewaySocket,
        "-telemetry", $telemetryIngestSocket,
        "-data-dir", $gatewayDataDir
    ) -StdoutPath $gatewaydStdoutPath -StderrPath $gatewaydStderrPath

    $controldProc = Start-Plane -BinaryPath $controldPath -Arguments @(
        "-listen", "127.0.0.1:$controlPort",
        "-gateway", $gatewaySocket,
        "-telemetry", $telemetryQuerySocket,
        "-data-dir", $controlDataDir,
        "-authoring-config", $configPath
    ) -StdoutPath $controldStdoutPath -StderrPath $controldStderrPath

    $authHeaders = @{
        Authorization = "Bearer $adminToken"
    }

    $modelsResult = Wait-ForJsonCondition -URL $modelsURL -Description "models list containing $smokeModel" -Condition {
        param($body)
        @($body.data | ForEach-Object { $_.id }) -contains $smokeModel
    }
    $healthResult = Wait-ForJsonCondition -URL $healthURL -Description "gateway health status healthy" -Condition {
        param($body)
        $body.status -eq "healthy"
    }
    $statusResult = Wait-ForJsonCondition -URL $statusURL -Headers $authHeaders -Description "control status showing connected planes" -Condition {
        param($body)
        $body.gateway_status -eq "connected" -and $body.telemetry_status -eq "connected"
    }
    $historyResult = Wait-ForJsonCondition -URL $historyURL -Headers $authHeaders -Description "seeded config history" -Condition {
        param($body)
        @($body).Count -ge 1
    }
    $admin = Wait-ForReady -URL ($adminURL + "?token=" + $adminToken)

    $health = $healthResult.Response
    $models = $modelsResult.Response
    $status = $statusResult.Response
    $history = $historyResult.Response
    $healthBody = $healthResult.Body
    $modelsBody = $modelsResult.Body
    $statusBody = $statusResult.Body
    $historyBody = $historyResult.Body

    if ($healthBody.status -ne "healthy") {
        throw "health status expected 'healthy', got '$($healthBody.status)'"
    }

    Assert-ArrayContains -Values ($modelsBody.data | ForEach-Object { $_.id }) -Expected $smokeModel -Label "models.data"

    if ($statusBody.gateway_status -ne "connected") {
        throw "status.gateway_status expected 'connected', got '$($statusBody.gateway_status)'"
    }
    if ($statusBody.telemetry_status -ne "connected") {
        throw "status.telemetry_status expected 'connected', got '$($statusBody.telemetry_status)'"
    }

    if (@($historyBody).Count -lt 1) {
        throw "config history expected at least one seeded revision"
    }

    if ($admin.Content -notmatch 'id="app"' -or $admin.Content -notmatch "/admin/assets/") {
        throw "/admin did not return the embedded control-plane admin shell"
    }

    [pscustomobject]@{
        GatewaydBinary   = $gatewaydPath
        ControldBinary   = $controldPath
        TelemetrydBinary = $telemetrydPath
        ConfigPath       = $configPath
        HealthStatus     = [int]$health.StatusCode
        ModelsStatus     = [int]$models.StatusCode
        ControlStatus    = [int]$status.StatusCode
        HistoryStatus    = [int]$history.StatusCode
        AdminStatus      = [int]$admin.StatusCode
        Models           = (@($modelsBody.data | ForEach-Object { $_.id }) -join ",")
        HistoryCount     = @($historyBody).Count
        GatewayStatus    = $statusBody.gateway_status
        TelemetryStatus  = $statusBody.telemetry_status
        GatewaydPid      = $gatewaydProc.Id
        ControldPid      = $controldProc.Id
        TelemetrydPid    = $telemetryProc.Id
    } | Format-List
} catch {
    Show-Log -Label "telemetryd stderr" -Path $telemetryStderrPath
    Show-Log -Label "telemetryd stdout" -Path $telemetryStdoutPath
    Show-Log -Label "gatewayd stderr" -Path $gatewaydStderrPath
    Show-Log -Label "gatewayd stdout" -Path $gatewaydStdoutPath
    Show-Log -Label "controld stderr" -Path $controldStderrPath
    Show-Log -Label "controld stdout" -Path $controldStdoutPath
    throw
} finally {
    foreach ($proc in @($controldProc, $gatewaydProc, $telemetryProc)) {
        if ($proc -and -not $proc.HasExited) {
            Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
            $proc.WaitForExit()
        }
    }

    if (-not $KeepWorkdir) {
        Remove-Item $workdir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
