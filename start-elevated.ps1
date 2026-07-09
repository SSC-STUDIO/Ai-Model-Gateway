# start-elevated.ps1 - Scheduled task / startup entry point
# Delegates to start-aigw-background.ps1 to avoid code duplication.
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
& "$scriptDir\start-aigw-background.ps1"
