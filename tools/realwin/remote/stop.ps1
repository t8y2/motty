# Stop the probe build only. Never touches an installed motty.exe or any other process.
Get-Process motty-trace -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 500
"stopped: " + -not (Get-Process motty-trace -ErrorAction SilentlyContinue)
