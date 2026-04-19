function Get-WslNetworkingModeFromConfigText {
    param(
        [string]$ConfigText
    )

    if ([string]::IsNullOrWhiteSpace($ConfigText)) {
        return 'nat'
    }

    foreach ($line in ($ConfigText -split "\r?\n")) {
        $trimmed = $line.Trim()
        if ($trimmed -eq '' -or $trimmed.StartsWith('#') -or $trimmed.StartsWith(';')) {
            continue
        }

        if ($trimmed -match '^(?i)networkingMode\s*=\s*(.+?)\s*$') {
            $mode = $matches[1].Trim().Trim('"', "'").ToLowerInvariant()
            if ($mode) {
                return $mode
            }
        }
    }

    return 'nat'
}

function Get-WslNetworkingMode {
    param(
        [string]$ConfigPath = (Join-Path $env:USERPROFILE '.wslconfig')
    )

    if (-not (Test-Path -LiteralPath $ConfigPath)) {
        return 'nat'
    }

    try {
        $configText = Get-Content -LiteralPath $ConfigPath -Raw -ErrorAction Stop
    } catch {
        return 'nat'
    }

    return Get-WslNetworkingModeFromConfigText -ConfigText $configText
}

function Get-WslIPv4FromOutput {
    param(
        [AllowNull()]
        [string]$Output
    )

    if ([string]::IsNullOrWhiteSpace($Output)) {
        return $null
    }

    $parsed = $null
    $matches = [regex]::Matches($Output, '\b(?:\d{1,3}\.){3}\d{1,3}\b')
    foreach ($match in $matches) {
        if (-not [System.Net.IPAddress]::TryParse($match.Value, [ref]$parsed)) {
            continue
        }
        if ($parsed.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetwork) {
            continue
        }

        $bytes = $parsed.GetAddressBytes()
        if ($bytes[0] -eq 127) {
            continue
        }
        if ($bytes[0] -eq 169 -and $bytes[1] -eq 254) {
            continue
        }

        return $parsed.IPAddressToString
    }

    return $null
}

function Get-WslIPv4 {
    param(
        [string]$Distro = 'Ubuntu'
    )

    try {
        $output = wsl -d $Distro -- hostname -I 2>$null
    } catch {
        return $null
    }

    return Get-WslIPv4FromOutput -Output $output
}

function Get-WslGatewayAccessPlan {
    param(
        [string]$NetworkMode = 'nat',
        [string]$WslIp,
        [int]$Port = 18080
    )

    $normalizedMode = $NetworkMode
    if ([string]::IsNullOrWhiteSpace($normalizedMode)) {
        $normalizedMode = 'nat'
    } else {
        $normalizedMode = $normalizedMode.Trim().ToLowerInvariant()
    }

    $requiresPortProxy = $normalizedMode -ne 'mirrored'
    if ($requiresPortProxy -and [string]::IsNullOrWhiteSpace($WslIp)) {
        throw "WSL IP is required when networking mode is '$normalizedMode'"
    }

    return [pscustomobject]@{
        NetworkMode       = $normalizedMode
        Port              = $Port
        RequiresPortProxy = $requiresPortProxy
        HealthUrl         = "http://127.0.0.1:$Port/-/health"
        ConnectAddress    = if ($requiresPortProxy) { $WslIp } else { $null }
        ListenAddresses   = if ($requiresPortProxy) { @('0.0.0.0', '127.0.0.1') } else { @() }
    }
}

function Remove-GatewayPortProxyRules {
    param(
        [int]$Port = 18080
    )

    foreach ($listenAddress in @('0.0.0.0', '127.0.0.1')) {
        netsh interface portproxy delete v4tov4 listenaddress=$listenAddress listenport=$Port 2>$null | Out-Null
    }
}

function Set-GatewayPortProxyRules {
    param(
        [string]$ConnectAddress,
        [int]$Port = 18080
    )

    if ([string]::IsNullOrWhiteSpace($ConnectAddress)) {
        throw 'ConnectAddress is required to configure portproxy'
    }

    Remove-GatewayPortProxyRules -Port $Port
    foreach ($listenAddress in @('0.0.0.0', '127.0.0.1')) {
        netsh interface portproxy add v4tov4 listenaddress=$listenAddress listenport=$Port connectaddress=$ConnectAddress connectport=$Port 2>$null | Out-Null
    }
}

function Ensure-GatewayFirewallRule {
    param(
        [string]$DisplayName = 'WSL2 Gateway 18080',
        [int]$Port = 18080
    )

    $fwRule = Get-NetFirewallRule -DisplayName $DisplayName -ErrorAction SilentlyContinue
    if (-not $fwRule) {
        New-NetFirewallRule -DisplayName $DisplayName -Direction Inbound -Action Allow -Protocol TCP -LocalPort $Port -ErrorAction SilentlyContinue | Out-Null
    }
}
