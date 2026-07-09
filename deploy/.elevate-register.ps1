$ErrorActionPreference = 'Stop'
$here = 'D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway-src'
$vbs  = 'D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway-src\deploy\start-aigw-task.vbs'
$taskNm = 'AI-Model-Gateway'

$action  = New-ScheduledTaskAction -Execute 'wscript.exe' -Argument ""$vbs"" -WorkingDirectory $here
$trigger = New-ScheduledTaskTrigger -AtLogOn
$principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -RunLevel Highest -LogonType Interactive
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -ExecutionTimeLimit (New-TimeSpan -Hours 0)

Register-ScheduledTask -TaskName $taskNm -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
Write-Host "Scheduled Task '$taskNm' registered (RunLevel=Highest)."