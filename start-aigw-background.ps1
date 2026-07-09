# Start aigw supervise in background with env vars
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptDir

$env:NOWCODING_API_KEY=[Environment]::GetEnvironmentVariable('NOWCODING_API_KEY','User')
$env:OPENCODE_API_KEY=[Environment]::GetEnvironmentVariable('OPENCODE_API_KEY','User')
$env:ADMIN_BOOTSTRAP_TOKEN=[Environment]::GetEnvironmentVariable('ADMIN_BOOTSTRAP_TOKEN','User')
$env:COOKIE_SIGNING_KEY=[Environment]::GetEnvironmentVariable('COOKIE_SIGNING_KEY','User')
$env:ADMIN_TOKEN=[Environment]::GetEnvironmentVariable('ADMIN_TOKEN','User')
$env:VIEWER_TOKEN=[Environment]::GetEnvironmentVariable('VIEWER_TOKEN','User')

Start-Process -FilePath '.\bin\aigw.exe' -ArgumentList 'supervise','-runtime-root','.gateway-runtime','-config-dir','configs','-bin-dir','.\bin','-manifest','.\aigw-manifest.json','-strict-manifest=true' -WindowStyle Hidden
