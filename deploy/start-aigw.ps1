$ErrorActionPreference = "Stop"

$here = "D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway-src"
$bin = "$here\bin"
$log = "$here\logs"
$rt  = "$here\.gateway-runtime"

New-Item -ItemType Directory -Force -Path $log,$rt\control,$rt\gateway,$rt\telemetry | Out-Null

# Required secrets (loaded from deploy/secrets.env if present)
$envFile = "$here\deploy\secrets.env"
if (Test-Path $envFile) {
  Get-Content $envFile | ForEach-Object {
    if ($_ -match '^\s*([A-Z_][A-Z0-9_]*)=(.*)$') {
      Set-Item -Path "Env:$($matches[1])" -Value $matches[2]
    }
  }
}

# Required data-plane / control-plane env vars (override whatever is in config)
$env:XIAOMI_MIMO_API_KEY   = "tp-c7gby7omhsrano8iewhx042mg8bfmlegeqqvrfnlodk36bdf"
$env:XIAOMI_MIMO_API_KEY_2 = "sk-Z5FYBb0BM1AyhEUIc0ZoM9bmkUKNHEd5G2fn5fZ6wT5p5TgFWBMIlLUDc15CRXgh"
$env:OPENCODE_API_KEY     = "sk-Z5FYBb0BM1AyhEUIc0ZoM9bmkUKNHEd5G2fn5fZ6wT5p5TgFWBMIlLUDc15CRXgh"
$env:ADMIN_BOOTSTRAP_TOKEN = "local-bootstrap-please-rotate-3210-not-prod-9kx"
$env:COOKIE_SIGNING_KEY    = "local-cookies-please-rotate-4321-not-prod-7yt"
$env:ADMIN_TOKEN           = "admin-token-local-please-rotate-7yt5432-not-prod"
$env:VIEWER_TOKEN          = "viewer-token-local-please-rotate-8zu6543-not-prod"

Push-Location $here
try {
  & "$bin\aigw.exe" supervise `
      -config-dir   "configs" `
      -bin-dir      "bin" `
      -runtime-root ".gateway-runtime" `
      -manifest     "aigw-manifest.json" `
      -strict-manifest=true `
      -startup-timeout 20s
} finally {
  Pop-Location
}
