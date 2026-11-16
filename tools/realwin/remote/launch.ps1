# Start the probe build in the desktop session (run through itask.ps1) with its own data
# directory and trace files, so it never touches the user's Motty settings or hosts.
#   -Keep  reuse the existing data directory (default: start from an empty one)
param([switch]$Keep)
. (Join-Path $PSScriptRoot "desktop.ps1")
Get-Process motty-trace -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 500
$data = Join-Path $Work "data"
if (-not $Keep) { Remove-Item $data -Recurse -Force -ErrorAction SilentlyContinue }
Remove-Item (Join-Path $Work "*.log") -ErrorAction SilentlyContinue
$info = New-Object System.Diagnostics.ProcessStartInfo
$info.FileName = Join-Path $Work "motty-trace.exe"
$info.WorkingDirectory = $Work
$info.UseShellExecute = $false
$info.EnvironmentVariables["MOTTY_DATA_DIR"] = $data
$info.EnvironmentVariables["VIVA_TRACE"] = Join-Path $Work "trace.log"
$info.EnvironmentVariables["MOTTY_PUMP_TRACE"] = Join-Path $Work "pump.log"
$info.EnvironmentVariables["MOTTY_SHELL_TRACE"] = Join-Path $Work "shell.log"
[System.Diagnostics.Process]::Start($info) | Out-Null
Start-Sleep -Seconds 3
$p = Get-ProbeProcess
if ($p) { "started pid=$($p.Id) session=$($p.SessionId)" } else { "FAILED to start" }
