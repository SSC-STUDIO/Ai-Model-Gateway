$ErrorActionPreference = "Stop"

$serviceName = "AIModelGateway"

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Administrator privileges are required to uninstall the Windows service."
}

$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if (-not $existing) {
    Write-Output "Service not installed."
    exit 0
}

sc.exe stop $serviceName | Out-Null
Start-Sleep -Seconds 2
sc.exe delete $serviceName | Out-Null
Write-Output "Service removed."
