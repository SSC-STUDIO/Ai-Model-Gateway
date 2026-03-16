param(
    [switch]$SkipElevation
)

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

$project = Resolve-ProjectRoot
$selfScriptPath = if ($PSCommandPath) { $PSCommandPath } else { Join-Path $scriptDir "rebuild-and-restart.ps1" }
$go = "C:\Program Files\Go\bin\go.exe"
$serviceName = "AIModelGateway"
$gatewayScript = Join-Path $scriptDir "start-gateway.ps1"
$binaryPath = Join-Path $project "bin\gateway.exe"
$stagedBinaryPath = Join-Path $project "bin\gateway.staged.exe"
$backupBinaryPath = Join-Path $project "bin\gateway.previous.exe"
$healthURL = "http://127.0.0.1:18080/-/health"
$adminURL = "http://127.0.0.1:18080/-/admin/data"
$adminToken = "ec6a94485ddd476b96cdc3d5a9a9fe14"
$env:GOPROXY = "https://goproxy.cn,direct"

function Get-Listener {
    Get-NetTCPConnection -LocalPort 18080 -State Listen -ErrorAction SilentlyContinue |
        Select-Object -First 1
}

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Restart-Elevated {
    param(
        [string]$ScriptPath
    )

    $args = @(
        "-ExecutionPolicy", "Bypass",
        "-File", "`"$ScriptPath`"",
        "-SkipElevation"
    )

    try {
        Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList $args | Out-Null
    } catch {
        throw "restart requires elevation; approve the UAC prompt or run this script from an elevated PowerShell session"
    }
}

function Get-ServiceRegistration {
    param(
        [string]$Name
    )

    return Get-CimInstance Win32_Service -Filter "Name='$Name'" -ErrorAction SilentlyContinue
}

function Test-ServiceRegistrationMatches {
    param(
        [object]$Registration,
        [string]$ExpectedBinaryPath,
        [string]$ExpectedConfigPath
    )

    if (-not $Registration) {
        return $false
    }

    $pathName = [string]$Registration.PathName
    if ([string]::IsNullOrWhiteSpace($pathName)) {
        return $false
    }

    return $pathName -like "*$ExpectedBinaryPath*" -and $pathName -like "*$ExpectedConfigPath*"
}

function Repair-ServiceRegistration {
    param(
        [string]$InstallScriptPath
    )

    & powershell.exe -ExecutionPolicy Bypass -File $InstallScriptPath
    if ($LASTEXITCODE -ne 0) {
        throw "failed to repair Windows service registration"
    }
}

function Wait-ForServiceStatus {
    param(
        [string]$Name,
        [string]$Status,
        [int]$TimeoutSec = 20
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        $svc = Get-Service -Name $Name -ErrorAction SilentlyContinue
        if ($svc -and $svc.Status -eq $Status) {
            return $true
        }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Invoke-ServiceControl {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,
        [Parameter(Mandatory = $true)]
        [string]$FailureAction
    )

    $stdoutPath = [System.IO.Path]::GetTempFileName()
    $stderrPath = [System.IO.Path]::GetTempFileName()
    try {
        $proc = Start-Process -FilePath "sc.exe" -ArgumentList $Arguments -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        $output = @(
            (Read-TextOrEmpty -Path $stdoutPath),
            (Read-TextOrEmpty -Path $stderrPath)
        ) -join "`n"
        $output = $output.Trim()

        if ($proc.ExitCode -ne 0) {
            if ($output -match 'Access is denied') {
                throw "${FailureAction}: access denied; rerun this script from an elevated PowerShell session"
            }
            if ([string]::IsNullOrWhiteSpace($output)) {
                $output = "sc.exe exit code $($proc.ExitCode)"
            }
            throw "${FailureAction}: $output"
        }

        return $output
    } finally {
        Remove-Item $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
    }
}

function Read-TextOrEmpty {
    param(
        [string]$Path
    )

    if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path $Path)) {
        return ""
    }

    $text = [System.IO.File]::ReadAllText($Path)
    if ($null -eq $text) {
        return ""
    }
    return $text.Trim()
}

function Stop-ListenerProcess {
    param(
        [int]$ProcessId
    )

    if ($ProcessId -le 0) {
        throw "refusing to stop invalid listener PID ${ProcessId}"
    }

    $existing = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    if (-not $existing) {
        $listener = Get-Listener
        if (-not $listener -or $listener.OwningProcess -ne $ProcessId) {
            return
        }
    }

    try {
        Stop-Process -Id $ProcessId -Force -ErrorAction Stop
    } catch {
        $stdoutPath = [System.IO.Path]::GetTempFileName()
        $stderrPath = [System.IO.Path]::GetTempFileName()
        try {
            $taskkill = Start-Process -FilePath "taskkill.exe" -ArgumentList @("/PID", $ProcessId, "/F") -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
            if ($taskkill.ExitCode -ne 0) {
                $stdout = Read-TextOrEmpty -Path $stdoutPath
                $stderr = Read-TextOrEmpty -Path $stderrPath
                $message = @($stdout, $stderr) -join "`n"
                $message = $message.Trim()
                if ($message -match 'Access is denied') {
                    throw "failed to stop listener process PID ${ProcessId}: access denied; rerun this script from an elevated PowerShell session"
                }
                if ($message -match 'not found') {
                    $listener = Get-Listener
                    if (-not $listener -or $listener.OwningProcess -ne $ProcessId) {
                        return
                    }
                }
                if ([string]::IsNullOrWhiteSpace($message)) {
                    $message = "taskkill exit code $($taskkill.ExitCode)"
                }
                throw "failed to stop listener process PID ${ProcessId}: $message"
            }
        } finally {
            Remove-Item $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
        }
    }

    $deadline = (Get-Date).AddSeconds(10)
    while ((Get-Date) -lt $deadline) {
        $listener = Get-Listener
        if (-not $listener -or $listener.OwningProcess -ne $ProcessId) {
            return
        }
        Start-Sleep -Milliseconds 500
    }

    throw "failed to stop listener process PID $ProcessId before timeout"
}

function Wait-ForHealth {
    param(
        [int]$TimeoutSec = 30
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        try {
            $resp = Invoke-WebRequest -UseBasicParsing -Uri $healthURL -TimeoutSec 5
            if ($resp.StatusCode -eq 200) {
                return $resp
            }
        } catch {
        }
        Start-Sleep -Milliseconds 500
    }

    throw "gateway health check did not become ready within ${TimeoutSec}s"
}

function Start-Gateway {
    param(
        [string]$RestartMode,
        [object]$Service
    )

    if ($RestartMode -eq "service") {
        if (-not $Service) {
            throw "service mode requested but $serviceName is not installed"
        }
        Invoke-ServiceControl -Arguments @("start", $serviceName) -FailureAction "failed to start service $serviceName" | Out-Null
        if (-not (Wait-ForServiceStatus -Name $serviceName -Status "Running")) {
            throw "service $serviceName did not start cleanly"
        }
        return
    }

    & $gatewayScript
}

function Restore-PreviousBinary {
    if (-not (Test-Path $backupBinaryPath)) {
        return
    }

    if (Test-Path $binaryPath) {
        Remove-Item $binaryPath -Force
    }
    Move-Item -Path $backupBinaryPath -Destination $binaryPath -Force
}

if (Test-Path $stagedBinaryPath) {
    Remove-Item $stagedBinaryPath -Force
}
if (Test-Path $backupBinaryPath) {
    Remove-Item $backupBinaryPath -Force
}

Push-Location $project
try {
    & $go test ./...
    if ($LASTEXITCODE -ne 0) {
        throw "go test failed with exit code $LASTEXITCODE"
    }

    & $go build -o $stagedBinaryPath "./cmd/gateway"
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

$service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
$serviceRegistration = Get-ServiceRegistration -Name $serviceName
$listener = Get-Listener
$originalPid = $null
$restartMode = "process"
$isAdmin = Test-IsAdministrator

if ($listener) {
    $originalPid = [int]$listener.OwningProcess
}

$needsElevation = -not $isAdmin -and (($service -and $service.Status -ne "Stopped") -or $listener)
if ($needsElevation -and -not $SkipElevation) {
    Write-Warning "restart requires administrator rights; relaunching with UAC"
    Restart-Elevated -ScriptPath $selfScriptPath
    exit 0
}

$serviceRegistrationMatches = Test-ServiceRegistrationMatches -Registration $serviceRegistration -ExpectedBinaryPath $binaryPath -ExpectedConfigPath (Join-Path $project "configs\config.yaml")
if ($service -and -not $serviceRegistrationMatches) {
    if (-not $isAdmin -and -not $SkipElevation) {
        Write-Warning "service registration points to an old path; relaunching with UAC to repair it"
        Restart-Elevated -ScriptPath $selfScriptPath
        exit 0
    }
    if ($isAdmin) {
        Write-Warning "service registration points to an old path; reinstalling service with the current project root"
        Repair-ServiceRegistration -InstallScriptPath (Join-Path $scriptDir "install-service.ps1")
        $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
        $serviceRegistration = Get-ServiceRegistration -Name $serviceName
        $serviceRegistrationMatches = Test-ServiceRegistrationMatches -Registration $serviceRegistration -ExpectedBinaryPath $binaryPath -ExpectedConfigPath (Join-Path $project "configs\config.yaml")
        if (-not $serviceRegistrationMatches) {
            throw "service registration still does not match the current project root after repair"
        }
    }
}

if ($service -and $isAdmin) {
    $restartMode = "service"
} elseif ($service -and -not $isAdmin) {
    Write-Warning "service $serviceName is installed but this shell is not elevated; falling back to process mode"
}

if ($listener) {
    if ($restartMode -eq "service") {
        if ($service.Status -ne "Stopped") {
            Invoke-ServiceControl -Arguments @("stop", $serviceName) -FailureAction "failed to stop service $serviceName" | Out-Null
            if (-not (Wait-ForServiceStatus -Name $serviceName -Status "Stopped")) {
                throw "service $serviceName did not stop cleanly"
            }
        }
        if ($originalPid) {
            Stop-ListenerProcess -ProcessId $originalPid
        }
    } else {
        Stop-ListenerProcess -ProcessId $originalPid
    }
}

if (Get-Listener) {
    $currentPid = (Get-Listener).OwningProcess
    throw "port 18080 is still occupied by PID $currentPid after shutdown"
}

$swappedBinary = $false
try {
    if (Test-Path $binaryPath) {
        Move-Item -Path $binaryPath -Destination $backupBinaryPath -Force
    }
    Move-Item -Path $stagedBinaryPath -Destination $binaryPath -Force
    $swappedBinary = $true

    Start-Gateway -RestartMode $restartMode -Service $service
    $health = Wait-ForHealth
} catch {
    if ($swappedBinary) {
        try {
            Restore-PreviousBinary
            Start-Gateway -RestartMode $restartMode -Service $service
        } catch {
        }
    }
    throw
} finally {
    if (Test-Path $stagedBinaryPath) {
        Remove-Item $stagedBinaryPath -Force -ErrorAction SilentlyContinue
    }
}

if (Test-Path $backupBinaryPath) {
    Remove-Item $backupBinaryPath -Force -ErrorAction SilentlyContinue
}

$newListener = Get-Listener
if (-not $newListener) {
    throw "gateway became healthy but no listener was found on port 18080"
}

$newPid = [int]$newListener.OwningProcess
if ($originalPid -and $newPid -eq $originalPid) {
    throw "listener PID did not change after restart; old process may still be serving traffic"
}

$body = @{
    model = "gpt-5.4"
    input = "Reply with exactly: token-fix-ok"
} | ConvertTo-Json -Depth 10

$resp = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:18080/v1/responses" -Method Post -Headers @{
    Authorization = "Bearer sk-local-gateway"
    "Content-Type" = "application/json"
} -Body $body -TimeoutSec 60

$admin = Invoke-WebRequest -UseBasicParsing -Uri $adminURL -Headers @{
    Authorization = "Bearer $adminToken"
} -TimeoutSec 15
$adminJson = $admin.Content | ConvertFrom-Json
$latest = $adminJson.telemetry.requests | Select-Object -First 1

[pscustomobject]@{
    RestartMode        = $restartMode
    PreviousPid        = $originalPid
    CurrentPid         = $newPid
    ServicePresent     = [bool]$service
    ServiceStatus      = (Get-Service -Name $serviceName -ErrorAction SilentlyContinue).Status
    HealthStatus       = [int]$health.StatusCode
    ResponseStatus     = [int]$resp.StatusCode
    LatestModel        = $latest.model
    LatestUpstream     = $latest.upstream
    PromptTokens       = $latest.usage.prompt_tokens
    CompletionTokens   = $latest.usage.completion_tokens
    TotalTokens        = $latest.usage.total_tokens
    SummaryTotalTokens = $adminJson.telemetry.summary.total_tokens
} | Format-List
