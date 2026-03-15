Set shell = CreateObject("WScript.Shell")
command = "powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File ""C:\Users\96152\My-Project\Application_Project\AI-Model-Gateway\scripts\start-gateway.ps1"""
shell.Run command, 0, False
