$ErrorActionPreference = "Stop"

$serviceName = "AIModelGateway"
$displayName = "AI Model Gateway"
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

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Administrator privileges are required to install the Windows service."
}

if (-not (Test-Path $binaryPath)) {
    throw "gateway binary not found: $binaryPath"
}

if (-not (Test-Path $configPath)) {
    throw "gateway config not found: $configPath"
}

$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existing) {
    sc.exe stop $serviceName | Out-Null
    Start-Sleep -Seconds 2
    sc.exe delete $serviceName | Out-Null
    Start-Sleep -Seconds 2
}

$binPath = "`"$binaryPath`" -config `"$configPath`""
sc.exe create $serviceName binPath= $binPath start= auto DisplayName= $displayName | Out-Null
sc.exe description $serviceName "OpenAI-compatible local AI gateway." | Out-Null
sc.exe failure $serviceName reset= 86400 actions= restart/60000/restart/60000/restart/60000 | Out-Null
sc.exe start $serviceName | Out-Null

Get-Service -Name $serviceName | Select-Object Name, Status, StartType
