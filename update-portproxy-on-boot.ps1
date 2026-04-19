# Update Port Proxy for WSL2 AI Gateway
# This script should be run after WSL starts

$ErrorActionPreference = "SilentlyContinue"
. (Join-Path $PSScriptRoot 'scripts\wsl-networking.ps1')

$distro = 'Ubuntu'
$port = 18080
$networkMode = Get-WslNetworkingMode

Write-Host "WSL networking mode: $networkMode"

if ($networkMode -eq 'mirrored') {
    Remove-GatewayPortProxyRules -Port $port
    Write-Host "Mirrored networking uses localhost directly; removed any legacy portproxy rules for port $port."
    exit 0
}

# Wait for WSL to be ready
for ($i = 0; $i -lt 30; $i++) {
    $wslIp = Get-WslIPv4 -Distro $distro
    if ($wslIp) {
        break
    }
    Start-Sleep -Seconds 1
}

if (-not $wslIp) {
    Write-Host "Failed to get WSL2 IP"
    exit 1
}

Write-Host "WSL2 IP: $wslIp"

# Update portproxy (requires admin)
Set-GatewayPortProxyRules -ConnectAddress $wslIp -Port $port

# Ensure firewall rule exists
Ensure-GatewayFirewallRule -Port $port
Write-Host "Firewall rule ensured"

Write-Host "Port proxy updated to $wslIp"
