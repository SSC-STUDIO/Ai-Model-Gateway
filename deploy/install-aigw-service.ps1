$ErrorActionPreference = "Stop"

$here   = "D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway-src"
$taskNm = "AI-Model-Gateway"
$vbs    = "$here\deploy\start-aigw-task.vbs"

# --- 1. Kill anything running ------------------------------------------------
Get-Process aigw, gatewayd, controld, telemetryd -ErrorAction SilentlyContinue |
  Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

# --- 2. Re-launch the gateway in the background (no console window) ---------
Write-Host "Launching AIGW in background..."
$proc = Start-Process wscript.exe `
    -ArgumentList "`"$vbs`"" `
    -WindowStyle Hidden `
    -WorkingDirectory $here `
    -PassThru
Write-Host "AIGW host process: PID $($proc.Id)"

# --- 3. Register the Scheduled Task *with UAC elevation* ---------------------
# We re-launch ourselves elevated to register the task (Run with highest privileges,
# Run only when user is logged on OR Run whether user is logged on or not).
$regBlock = @"
`$ErrorActionPreference = 'Stop'
`$here = '$here'
`$vbs  = '$vbs'
`$taskNm = '$taskNm'

`$action  = New-ScheduledTaskAction -Execute 'wscript.exe' -Argument "`"`$vbs`"" -WorkingDirectory `$here
`$trigger = New-ScheduledTaskTrigger -AtLogOn
`$principal = New-ScheduledTaskPrincipal -UserId "`$env:USERDOMAIN\`$env:USERNAME" -RunLevel Highest -LogonType Interactive
`$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -ExecutionTimeLimit (New-TimeSpan -Hours 0)

Register-ScheduledTask -TaskName `$taskNm -Action `$action -Trigger `$trigger -Principal `$principal -Settings `$settings -Force | Out-Null
Write-Host "Scheduled Task '`$taskNm' registered (RunLevel=Highest)."
"@
$regPath = "$here\deploy\.elevate-register.ps1"
[System.IO.File]::WriteAllText($regPath, $regBlock, (New-Object System.Text.UTF8Encoding $false))

try {
  Write-Host "Requesting UAC to register the Scheduled Task..."
  $elev = Start-Process powershell.exe `
            -ArgumentList "-NoProfile","-ExecutionPolicy","Bypass","-File", $regPath `
            -Verb RunAs `
            -PassThru `
            -ErrorAction Stop
  $elev.WaitForExit()
  Write-Host "UAC registration exited with code $($elev.ExitCode)."
} catch {
  Write-Warning "UAC path not available (no GUI / declined). Falling back to per-user startup-folder entry."
  $startup = [Environment]::GetFolderPath('Startup')
  $dest = Join-Path $startup 'AI-Model-Gateway.vbs'
  if (-not (Test-Path $dest)) {
    Copy-Item -LiteralPath $vbs -Destination $dest -Force
    Write-Host "Copied launcher to Startup folder: $dest"
  } else {
    Write-Host "Startup entry already present: $dest"
  }
}

# --- 4. Wait for daemons to come up ------------------------------------------
Write-Host "Waiting for AIGW to become healthy..."
for ($i=0; $i -lt 30; $i++) {
  Start-Sleep -Seconds 2
  $s = & "$here\bin\aigw.exe" status 2>&1 | Out-String
  if ($s -match 'gateway:\s*ok') {
    Write-Host "GATEWAY HEALTHY after $((($i+1)*2))s"
    break
  }
}
& "$here\bin\aigw.exe" status 2>&1 | Select-Object -First 5
