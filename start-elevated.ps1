# start-elevated.ps1 - Called by scheduled task to start Ai-Model-Gateway
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptDir

# Load env vars from User scope
$env:NOWCODING_API_KEY=[Environment]::GetEnvironmentVariable('NOWCODING_API_KEY','User')
$env:OPENCODE_API_KEY=[Environment]::GetEnvironmentVariable('OPENCODE_API_KEY','User')
$env:ADMIN_BOOTSTRAP_TOKEN=[Environment]::GetEnvironmentVariable('ADMIN_BOOTSTRAP_TOKEN','User')
$env:COOKIE_SIGNING_KEY=[Environment]::GetEnvironmentVariable('COOKIE_SIGNING_KEY','User')
$env:ADMIN_TOKEN=[Environment]::GetEnvironmentVariable('ADMIN_TOKEN','User')
$env:VIEWER_TOKEN=[Environment]::GetEnvironmentVariable('VIEWER_TOKEN','User')

# Start aigw supervise (telemetryd + gatewayd + controld)
Start-Process -FilePath '.\bin\aigw.exe' -ArgumentList 'supervise','-runtime-root','.gateway-runtime','-config-dir','configs','-bin-dir','.\bin','-manifest','.\aigw-manifest.json','-strict-manifest=true' -WindowStyle Hidden
