$ErrorActionPreference = "Stop"

$serviceName = "AIModelGateway"
$displayName = "AI Model Gateway"
$projectRoot = "C:\Users\96152\My-Project\Application_Project\AI-Model-Gateway"
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
