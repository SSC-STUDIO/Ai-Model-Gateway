# AI Gateway Port Proxy Setup
# Run this as Administrator or add to Task Scheduler

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

# Get WSL2 IP
$wslIp = Get-WslIPv4 -Distro $distro
if (-not $wslIp) {
    Write-Host "Failed to get WSL2 IP"
    exit 1
}

Write-Host "WSL2 IP: $wslIp"

# Remove old rules and add new ones
Set-GatewayPortProxyRules -ConnectAddress $wslIp -Port $port

# Add firewall rule (if not exists)
Ensure-GatewayFirewallRule -Port $port
Write-Host "Firewall rule ensured"

# Show current rules
Write-Host "Current portproxy rules:"
netsh interface portproxy show all

Write-Host "Port forwarding setup complete!"
