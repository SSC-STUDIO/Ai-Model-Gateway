@echo off
setlocal
set "HERE=D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway-src"
cd /d "%HERE%"
powershell -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway-src\deploy\start-aigw.ps1" >> "%HERE%\logs\aigw-task.log" 2>&1
endlocal