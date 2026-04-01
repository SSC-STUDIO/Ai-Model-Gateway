<#
.SYNOPSIS
    Restart AI Model Gateway service with verification (no admin check, run as admin)
#>

param(
    [int]$WaitSeconds = 120,
    [int]$VerifyPort = 18080,
    [string]$AdminToken = "ec6a94485ddd476b96cdc3d5a9a9fe14"
)

$ErrorActionPreference = "Stop"
$startTime = Get-Date

Write-Host "=== AI Model Gateway Service Restart ===" -ForegroundColor Cyan
Write-Host "Start time: $startTime" -ForegroundColor Gray
Write-Host "WARNING: Ensure running as Administrator!" -ForegroundColor Red

# Step 1: Stop service
Write-Host "`n[1/5] Stopping service..." -ForegroundColor Yellow
$service = Get-Service -Name "AIModelGateway" -ErrorAction SilentlyContinue
if ($service -and $service.Status -eq "Running") {
    Stop-Service -Name "AIModelGateway" -Force
    Write-Host "    Service stopped" -ForegroundColor Green
}

# Step 2: Kill any remaining process
Write-Host "`n[2/5] Cleaning up processes..." -ForegroundColor Yellow
Get-Process | Where-Object { $_.ProcessName -eq "gateway" } | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 3

# Step 3: Start service
Write-Host "`n[3/5] Starting service..." -ForegroundColor Yellow
Start-Service -Name "AIModelGateway"
Write-Host "    Service started" -ForegroundColor Green

# Step 4: Wait for ready
Write-Host "`n[4/5] Waiting for service ready (max ${WaitSeconds}s)..." -ForegroundColor Yellow
$ready = $false
$elapsed = 0
while (-not $ready -and $elapsed -lt $WaitSeconds) {
    Start-Sleep -Seconds 5
    $elapsed += 5
    try {
        $r = curl.exe -s "http://127.0.0.1:$VerifyPort/v1/models" --max-time 5
        if ($r -match "qwen") {
            $ready = $true
            Write-Host "    Ready after ${elapsed}s!" -ForegroundColor Green
        }
    } catch {
        Write-Host "    Waiting... ${elapsed}s" -ForegroundColor Gray
    }
}

# Step 5: Verify
Write-Host "`n[5/5] Verifying..." -ForegroundColor Yellow
$config = curl.exe -s "http://127.0.0.1:$VerifyPort/-/admin/config" -H "Authorization: Bearer $AdminToken" --max-time 10
$hasQuota = $config -match "quota_block_recovery"
Write-Host "    Quota recovery field: $hasQuota" -ForegroundColor $(if ($hasQuota) {"Green"} else {"Red"})

$endTime = Get-Date
$duration = ($endTime - $startTime).TotalSeconds
Write-Host "`n=== Complete in $([Math]::Round($duration,1))s ===" -ForegroundColor Cyan
Write-Host "URL: http://127.0.0.1:$VerifyPort/admin?token=$AdminToken" -ForegroundColor Cyan
