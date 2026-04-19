# AI Gateway + OpenClaw auto-recovery script
# Run this at logon to ensure the WSL runtime is reachable and healthy.

$ErrorActionPreference = "Continue"
$logFile = "$env:USERPROFILE\ai-gateway.log"
. (Join-Path $PSScriptRoot 'scripts\wsl-networking.ps1')

$distro = 'Ubuntu'
$port = 18080
$gatewayService = 'ai-gateway.service'
$openClawGatewayService = 'openclaw-gateway.service'
$openClawNodeService = 'openclaw-node.service'
$openClawServices = @($openClawGatewayService, $openClawNodeService)
$openClawGatewayPort = 18789
$wslGatewayHealthUrl = "http://127.0.0.1:$port/-/health"
$wslGatewayModelsUrl = "http://127.0.0.1:$port/v1/models"
$openClawGatewayReadyUrl = "http://127.0.0.1:$openClawGatewayPort/"

function Write-Log($msg) {
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    "$timestamp - $msg" | Tee-Object -FilePath $logFile -Append
}

function Invoke-WslCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $output = & wsl.exe -d $distro -- @Arguments 2>&1
    $exitCode = $LASTEXITCODE
    return [pscustomobject]@{
        ExitCode = $exitCode
        Output   = (($output | ForEach-Object { "$_" }) -join "`n").Trim()
    }
}

function Wait-WslUserSystemd {
    param(
        [int]$Attempts = 30
    )

    for ($i = 0; $i -lt $Attempts; $i++) {
        $result = Invoke-WslCommand -Arguments @('systemctl', '--user', 'show-environment')
        if ($result.ExitCode -eq 0) {
            return $true
        }
        Start-Sleep -Seconds 1
    }

    return $false
}

function Test-WslUserServiceInstalled {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ServiceName
    )

    $result = Invoke-WslCommand -Arguments @('systemctl', '--user', 'list-unit-files', $ServiceName, '--no-legend')
    return $result.ExitCode -eq 0 -and $result.Output -match [regex]::Escape($ServiceName)
}

function Get-WslUserServiceState {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ServiceName
    )

    if (-not (Test-WslUserServiceInstalled -ServiceName $ServiceName)) {
        return 'missing'
    }

    $result = Invoke-WslCommand -Arguments @('systemctl', '--user', 'is-active', $ServiceName)
    if (-not [string]::IsNullOrWhiteSpace($result.Output)) {
        return $result.Output.Trim()
    }

    return 'unknown'
}

function Ensure-WslUserService {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ServiceName
    )

    $state = Get-WslUserServiceState -ServiceName $ServiceName
    if ($state -eq 'missing') {
        Write-Log "WARNING: $ServiceName is not installed"
        return $false
    }

    if ($state -eq 'failed') {
        Write-Log "$ServiceName is failed, resetting systemd failure state"
        [void](Invoke-WslCommand -Arguments @('systemctl', '--user', 'reset-failed', $ServiceName))
    }

    if ($state -ne 'active') {
        Write-Log "Starting $ServiceName"
        [void](Invoke-WslCommand -Arguments @('systemctl', '--user', 'start', $ServiceName))
        Start-Sleep -Seconds 2
    }

    $state = Get-WslUserServiceState -ServiceName $ServiceName
    Write-Log "$ServiceName state: $state"
    return $state -eq 'active'
}

function Wait-WslHttpReady {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Url,
        [string]$Label = $Url,
        [int]$Attempts = 45,
        [switch]$UseHead
    )

    for ($i = 0; $i -lt $Attempts; $i++) {
        $curlArgs = @('/usr/bin/curl', '-fsS', '--max-time', '2')
        if ($UseHead) {
            $curlArgs += '-I'
        }

        $curlArgs += $Url
        $result = Invoke-WslCommand -Arguments $curlArgs
        if ($result.ExitCode -eq 0) {
            Write-Log "$Label is responding"
            return $true
        }

        Start-Sleep -Seconds 1
    }

    Write-Log "WARNING: $Label did not become ready in time"
    return $false
}

function Ensure-OpenClawRuntime {
    if (-not (Ensure-WslUserService -ServiceName $openClawGatewayService)) {
        return $false
    }

    [void](Wait-WslHttpReady -Url $openClawGatewayReadyUrl -Label 'OpenClaw gateway HTTP endpoint' -UseHead)

    return Ensure-WslUserService -ServiceName $openClawNodeService
}

function Log-OpenClawHealth {
    $health = $null
    $attempts = 3

    for ($attempt = 1; $attempt -le $attempts; $attempt++) {
        $health = Invoke-WslCommand -Arguments @('/usr/local/bin/openclaw', 'health')
        if ($health.ExitCode -eq 0) {
            if ($attempt -eq 1) {
                Write-Log "OpenClaw health check passed"
            } else {
                Write-Log "OpenClaw health check passed after retry $attempt/$attempts"
            }
            return
        }

        if ($attempt -lt $attempts) {
            Start-Sleep -Seconds 10
        }
    }

    $summary = ($health.Output -split "`n" | Select-Object -First 8) -join ' | '
    Write-Log "WARNING: OpenClaw health check failed: $summary"
}

function Test-GatewayHealthyStatus {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Response
    )

    if ($null -eq $Response) {
        return $false
    }

    $status = ""
    if ($Response.PSObject.Properties.Name -contains 'status' -and $null -ne $Response.status) {
        $status = [string]$Response.status
    }

    switch ($status.ToLowerInvariant()) {
        'ok' { return $true }
        'healthy' { return $true }
        'ready' { return $true }
        default { return $false }
    }
}

function Get-GatewayModelCount {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ModelsUrl
    )

    try {
        $models = Invoke-RestMethod -Uri $ModelsUrl -Method GET -TimeoutSec 5
        if ($null -eq $models) {
            return 0
        }

        if ($models.PSObject.Properties.Name -contains 'data' -and $null -ne $models.data) {
            return @($models.data).Count
        }

        return @($models).Count
    } catch {
        return 0
    }
}

Write-Log "=== Starting WSL runtime recovery ==="

$networkMode = Get-WslNetworkingMode
Write-Log "WSL networking mode: $networkMode"

$accessPlan = $null
if ($networkMode -eq 'mirrored') {
    $accessPlan = Get-WslGatewayAccessPlan -NetworkMode $networkMode -Port $port
    Write-Log "Mirrored networking detected, Windows should reach the gateway via localhost without portproxy"
} else {
    $ipHelper = Get-Service iphlpsvc -ErrorAction SilentlyContinue
    if ($ipHelper -and $ipHelper.Status -ne 'Running') {
        Write-Log "IP Helper not running, attempting to start..."
        try {
            Set-Service -Name iphlpsvc -StartupType Automatic -ErrorAction SilentlyContinue
            Start-Service -Name iphlpsvc -ErrorAction Stop
            Write-Log "IP Helper started successfully"
            Start-Sleep -Seconds 2
        } catch {
            Write-Log "ERROR: Failed to start IP Helper: $_"
        }
    }

    Write-Log "Waiting for WSL IPv4 address..."
    $wslIp = $null
    for ($i = 0; $i -lt 60; $i++) {
        $wslIp = Get-WslIPv4 -Distro $distro
        if ($wslIp) {
            break
        }
        Start-Sleep -Seconds 1
    }

    if (-not $wslIp) {
        Write-Log "ERROR: Could not get WSL IPv4 address"
        exit 1
    }

    Write-Log "WSL IP: $wslIp"
    $accessPlan = Get-WslGatewayAccessPlan -NetworkMode $networkMode -WslIp $wslIp -Port $port
}

if (-not (Wait-WslUserSystemd)) {
    Write-Log "ERROR: WSL user systemd did not become ready in time"
    exit 1
}

[void](Ensure-WslUserService -ServiceName $gatewayService)
[void](Wait-WslHttpReady -Url $wslGatewayHealthUrl -Label 'AI gateway health endpoint')
[void](Ensure-OpenClawRuntime)

if ($accessPlan.RequiresPortProxy) {
    Write-Log "Ensuring port proxy points to $($accessPlan.ConnectAddress)"
    Set-GatewayPortProxyRules -ConnectAddress $accessPlan.ConnectAddress -Port $accessPlan.Port
    Ensure-GatewayFirewallRule -Port $accessPlan.Port
} else {
    Write-Log "Removing legacy port proxy rules because mirrored networking uses localhost directly"
    Remove-GatewayPortProxyRules -Port $accessPlan.Port
}

Start-Sleep -Seconds 2
try {
    $response = Invoke-RestMethod -Uri $accessPlan.HealthUrl -Method GET -TimeoutSec 5
    if (Test-GatewayHealthyStatus -Response $response) {
        $modelCount = 0
        if ($response.PSObject.Properties.Name -contains 'available_models' -and $null -ne $response.available_models) {
            $modelCount = @($response.available_models).Count
        } else {
            $modelCount = Get-GatewayModelCount -ModelsUrl $wslGatewayModelsUrl
        }
        Write-Log "SUCCESS: Gateway is accessible at $($accessPlan.HealthUrl -replace '/-/health$', '')"
        Write-Log "Available models: $modelCount"
    } else {
        $status = if ($response.PSObject.Properties.Name -contains 'status') { [string]$response.status } else { '<missing>' }
        Write-Log "WARNING: Gateway responded but status is not healthy: $status"
    }
} catch {
    Write-Log "WARNING: Could not connect to gateway: $_"
}

Log-OpenClawHealth
Write-Log "=== Recovery complete ==="
