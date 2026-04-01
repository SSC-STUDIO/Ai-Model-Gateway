# 快速验证网关服务
param(
    [string]$Token = "ec6a94485ddd476b96cdc3d5a9a9fe14",
    [int]$Port = 18080
)

Write-Host "=== Gateway Quick Verify ===" -ForegroundColor Cyan

# 1. 检查进程
$proc = Get-Process | Where-Object { $_.ProcessName -eq "gateway" } | Select-Object -First 1
if($proc) {
    Write-Host "✅ Gateway process running: PID $($proc.Id)" -ForegroundColor Green
} else {
    Write-Host "❌ Gateway not running" -ForegroundColor Red
    exit 1
}

# 2. 测试模型列表
Write-Host "`nTesting /v1/models..."
try {
    $models = curl.exe -s "http://127.0.0.1:$Port/v1/models" --max-time 5
    $modelCount = ($models -split '"id"').Count - 1
    Write-Host "✅ Models endpoint OK ($modelCount models)" -ForegroundColor Green
    
    # 检查Kimi
    if($models -match "kimi-for-coding") {
        Write-Host "✅ kimi-for-coding available" -ForegroundColor Green
    }
    
    # 检查桥接配置
    if($models -match "gpt-5\.4") {
        Write-Host "✅ gpt-5.4 available (will bridge to kimi)" -ForegroundColor Green
    }
} catch {
    Write-Host "❌ Models endpoint failed: $_" -ForegroundColor Red
}

# 3. 测试管理界面
Write-Host "`nTesting admin interface..."
try {
    $admin = curl.exe -s "http://127.0.0.1:$Port/-/admin/data" -H "Authorization: Bearer $Token" --max-time 5
    if($admin -match "upstreams") {
        Write-Host "✅ Admin endpoint OK" -ForegroundColor Green
    } else {
        Write-Host "⚠️ Admin endpoint returned unexpected data" -ForegroundColor Yellow
    }
} catch {
    Write-Host "❌ Admin endpoint failed: $_" -ForegroundColor Red
}

# 4. 验证桥接规则
Write-Host "`nBridge configuration:"
try {
    $config = curl.exe -s "http://127.0.0.1:$Port/-/admin/config" -H "Authorization: Bearer $Token" --max-time 5 | ConvertFrom-Json
    $config.bridge.rules | ForEach-Object {
        Write-Host "  $($_.from) -> $($_.to)" -ForegroundColor Gray
    }
} catch {
    Write-Host "  Could not fetch bridge rules" -ForegroundColor Yellow
}

Write-Host "`n=== Access URLs ===" -ForegroundColor Cyan
Write-Host "Admin:    http://127.0.0.1:$Port/admin?token=$Token" -ForegroundColor Yellow
Write-Host "Settings: http://127.0.0.1:$Port/admin/settings?token=$Token" -ForegroundColor Yellow
Write-Host "API:      http://127.0.0.1:$Port/v1/chat/completions" -ForegroundColor Yellow
