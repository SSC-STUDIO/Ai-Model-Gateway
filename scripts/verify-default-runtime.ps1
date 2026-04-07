param(
    [int]$Port = 18081,
    [switch]$SkipBuild,
    [switch]$KeepWorkdir
)

$ErrorActionPreference = "Stop"

$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } elseif ($PSCommandPath) { Split-Path -Parent $PSCommandPath } else { (Get-Location).Path }
$projectRoot = Split-Path -Parent $scriptDir

if (-not (Test-Path (Join-Path $projectRoot "go.mod") -PathType Leaf)) {
    throw "unable to locate go.mod from $projectRoot"
}

$workdir = Join-Path $env:TEMP ("aigw-default-runtime-smoke-" + [guid]::NewGuid().ToString("N"))
$gatewayPath = Join-Path $workdir "gateway.exe"
$configPath = Join-Path $workdir "config.yaml"
$stdoutPath = Join-Path $workdir "gateway.stdout.log"
$stderrPath = Join-Path $workdir "gateway.stderr.log"
$healthURL = "http://127.0.0.1:$Port/-/health"
$modelsURL = "http://127.0.0.1:$Port/v1/models"
$overviewURL = "http://127.0.0.1:$Port/api/admin/v2/overview"
$bootstrapToken = "0123456789abcdef0123456789abcdef"
$smokeModel = "smoke-model"

function Wait-ForReady {
    param(
        [string]$URL,
        [int]$TimeoutSec = 30
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        try {
            $resp = Invoke-WebRequest -UseBasicParsing -Uri $URL -TimeoutSec 3
            if ($resp.StatusCode -eq 200) {
                return $resp
            }
        } catch {
        }
        Start-Sleep -Milliseconds 500
    }

    throw "timed out waiting for $URL"
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

New-Item -ItemType Directory -Force -Path $workdir | Out-Null
Remove-Item $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue

if (-not $SkipBuild) {
    Push-Location $projectRoot
    try {
        go build -o $gatewayPath ./cmd/gateway
        if ($LASTEXITCODE -ne 0) {
            throw "go build ./cmd/gateway failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
} elseif (-not (Test-Path $gatewayPath -PathType Leaf)) {
    throw "-SkipBuild was used but $gatewayPath does not exist"
}

$config = @"
listen: :$Port

router:
  strategy: round_robin

health:
  enabled: false
  interval_sec: 10
  timeout_ms: 2000
  path: /v1/models

admin:
  enabled: true
  auth_token: "$bootstrapToken"
  language: en

telemetry:
  sqlite_path: telemetry.db

pricing:
  cache_path: pricing-cache.json

bridge:
  enabled: false
  exclude_user_agents: []
  rules: []

fallback:
  enabled: false
  detect_repetition: false
  models: {}

upstreams:
  - name: smoke-provider
    base_url: https://example.invalid/v1
    api_key: sk-smoke-local-do-not-use
    provider_class: quota_limited
    models:
      - $smokeModel
    enabled: true
"@

Set-Content -Path $configPath -Value $config -Encoding ascii

$proc = $null
try {
    $proc = Start-Process -FilePath $gatewayPath -ArgumentList @("-config", $configPath) -PassThru -WindowStyle Hidden -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath

    $health = Wait-ForReady -URL $healthURL
    $models = Invoke-WebRequest -UseBasicParsing -Uri $modelsURL -TimeoutSec 5
    $overview = Invoke-WebRequest -UseBasicParsing -Uri $overviewURL -Headers @{ Authorization = "Bearer $bootstrapToken" } -TimeoutSec 5

    $healthBody = $health.Content | ConvertFrom-Json
    $modelsBody = $models.Content | ConvertFrom-Json
    $overviewBody = $overview.Content | ConvertFrom-Json

    Assert-ArrayContains -Values $healthBody.available_models -Expected $smokeModel -Label "health.available_models"
    Assert-ArrayContains -Values ($modelsBody.data | ForEach-Object { $_.id }) -Expected $smokeModel -Label "models.data[].id"
    Assert-ArrayContains -Values $overviewBody.available_models -Expected $smokeModel -Label "overview.available_models"

    if ($null -eq $overviewBody.runtime) {
        throw "overview response did not include runtime"
    }
    if ([int]$overviewBody.runtime.provider_count -lt 1) {
        throw "overview.runtime.provider_count expected >= 1, got $($overviewBody.runtime.provider_count)"
    }
    if ([int]$overviewBody.runtime.enabled_provider_count -lt 1) {
        throw "overview.runtime.enabled_provider_count expected >= 1, got $($overviewBody.runtime.enabled_provider_count)"
    }

    $stderrText = ""
    $deadline = (Get-Date).AddSeconds(5)
    while ((Get-Date) -lt $deadline) {
        if (Test-Path $stderrPath) {
            $stderrText = Get-Content -Path $stderrPath -Raw -ErrorAction SilentlyContinue
            if ($stderrText -match "\[v2\]") {
                break
            }
        }
        Start-Sleep -Milliseconds 200
    }
    if ($stderrText -notmatch "\[v2\]") {
        throw "gateway stderr did not show a v2 runtime marker. stderr: $stderrText"
    }

    [pscustomobject]@{
        GatewayBinary            = $gatewayPath
        ConfigPath               = $configPath
        HealthStatus             = [int]$health.StatusCode
        ModelsStatus             = [int]$models.StatusCode
        OverviewStatus           = [int]$overview.StatusCode
        HealthModels             = (@($healthBody.available_models) -join ",")
        OverviewProviderCount    = [int]$overviewBody.runtime.provider_count
        OverviewEnabledProviders = [int]$overviewBody.runtime.enabled_provider_count
        RuntimeLogHasV2Marker    = $true
        ProcessId                = $proc.Id
    } | Format-List
} catch {
    if (Test-Path $stderrPath) {
        Write-Host "--- gateway stderr ---"
        Get-Content -Path $stderrPath -ErrorAction SilentlyContinue
    }
    if (Test-Path $stdoutPath) {
        Write-Host "--- gateway stdout ---"
        Get-Content -Path $stdoutPath -ErrorAction SilentlyContinue
    }
    throw
} finally {
    if ($proc -and -not $proc.HasExited) {
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        $proc.WaitForExit()
    }

    if (-not $KeepWorkdir) {
        Remove-Item $workdir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
