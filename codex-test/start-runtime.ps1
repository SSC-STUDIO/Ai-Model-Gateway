param(
  [int]$StartupTimeoutSec = 40
)
$ErrorActionPreference = 'Stop'
$root = 'D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway'
Set-Location $root
# Pull env vars from User scope so supervised daemons inherit the same secrets
# that the project's own start script expects.
foreach($n in 'NOWCODING_API_KEY','OPENCODE_API_KEY','ADMIN_BOOTSTRAP_TOKEN','COOKIE_SIGNING_KEY','ADMIN_TOKEN','VIEWER_TOKEN'){
  $v = [Environment]::GetEnvironmentVariable($n,'User')
  if($v){ Set-Item -Path "env:$n" -Value $v }
}
# Ensure runtime subdirs exist (mirrors mkdir -p .gateway-runtime/...)
foreach($d in 'logs','gateway','control','telemetry-migrated','telemetry','update'){
  New-Item -ItemType Directory -Path "$root\.gateway-runtime\$d" -Force | Out-Null
}
$supLog = "$root\codex-test\out\supervise.out.log"
$supErr = "$root\codex-test\out\supervise.err.log"
# Kill any stale supervise tree from prior runs (best effort)
Get-Process -Name aigw,gatewayd,controld,telemetryd -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 400
$p = Start-Process -FilePath "$root\bin\aigw.exe" `
  -ArgumentList @('supervise','-runtime-root','.gateway-runtime','-config-dir','configs','-bin-dir','bin','-manifest','aigw-manifest.json','-strict-manifest=true') `
  -WorkingDirectory $root -WindowStyle Hidden -PassThru `
  -RedirectStandardOutput $supLog -RedirectStandardError $supErr
"supervise pid=$($p.Id)" | Out-File "$root\codex-test\out\runtime.pid" -Encoding utf8
# Poll data-plane health
$deadline = (Get-Date).AddSeconds($StartupTimeoutSec)
$ready = $false
while((Get-Date) -lt $deadline){
  try{
    $r = Invoke-WebRequest -Uri 'http://127.0.0.1:18080/-/health' -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
    "HEALTH $($r.StatusCode) $($r.Content)" | Out-File "$root\codex-test\out\health.first" -Encoding utf8
    $ready = $true; break
  }catch{ Start-Sleep -Milliseconds 500 }
}
if(-not $ready){ Write-Host 'RUNTIME_NOT_READY'; exit 1 }
# Confirm control plane too
try{ $c = Invoke-WebRequest -Uri 'http://127.0.0.1:18081/healthz' -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop; "CONTROL $($c.StatusCode)" }catch{ "CONTROL probe failed: $($_.Exception.Message)" }
Write-Host "RUNTIME_READY pid=$($p.Id)"
