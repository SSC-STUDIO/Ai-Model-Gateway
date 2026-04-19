$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here 'wsl-networking.ps1')

Describe 'Get-WslNetworkingModeFromConfigText' {
    It 'returns mirrored when .wslconfig requests mirrored networking' {
        $mode = Get-WslNetworkingModeFromConfigText @'
[wsl2]
networkingMode=mirrored
'@

        $mode | Should Be 'mirrored'
    }

    It 'defaults to nat when networking mode is not configured' {
        $mode = Get-WslNetworkingModeFromConfigText @'
[wsl2]
memory=8GB
'@

        $mode | Should Be 'nat'
    }
}

Describe 'Get-WslIPv4FromOutput' {
    It 'accepts mirrored-mode IPv4 addresses that are not in 172.16.0.0/12' {
        $ip = Get-WslIPv4FromOutput '192.168.50.42 172.25.192.1'

        $ip | Should Be '192.168.50.42'
    }

    It 'returns null when hostname output has no IPv4 address' {
        $ip = Get-WslIPv4FromOutput 'fe80::215:5dff:fe01:2345'

        $ip | Should Be $null
    }
}

Describe 'Get-WslGatewayAccessPlan' {
    It 'skips portproxy when mirrored mode is enabled' {
        $plan = Get-WslGatewayAccessPlan -NetworkMode 'mirrored' -WslIp '192.168.50.42' -Port 18080

        $plan.RequiresPortProxy | Should Be $false
        $plan.HealthUrl | Should Be 'http://127.0.0.1:18080/-/health'
        $plan.ConnectAddress | Should Be $null
    }

    It 'uses portproxy in nat mode and preserves the detected WSL IP' {
        $plan = Get-WslGatewayAccessPlan -NetworkMode 'nat' -WslIp '172.28.144.10' -Port 18080

        $plan.RequiresPortProxy | Should Be $true
        $plan.HealthUrl | Should Be 'http://127.0.0.1:18080/-/health'
        $plan.ConnectAddress | Should Be '172.28.144.10'
    }
}
