# Run another PowerShell file in the logged-in user's desktop session, wait, then clean up.
# An SSH login is in session 0 and cannot see windows or the screen; a scheduled task with an
# Interactive principal runs in the console session. conhost --headless avoids a console
# window (and never opens Windows Terminal).
#
# The script's output and errors are captured to mt-probe\last.out and echoed here; the exit
# code is 1 when the script threw, so callers can tell.
param([string]$Script, [string]$ScriptArgs = "")
$name = "MottyRealWin"
$work = Split-Path $Script
$log = Join-Path $work "last.out"
Remove-Item $log -ErrorAction SilentlyContinue
$inner = "& '$Script' $ScriptArgs *>&1 | Out-File -FilePath '$log' -Encoding utf8; exit `$(if (`$?) {0} else {1})"
$encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($inner))
$action = New-ScheduledTaskAction -Execute "conhost.exe" -Argument "--headless powershell.exe -NoProfile -ExecutionPolicy Bypass -EncodedCommand $encoded"
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive
Register-ScheduledTask -TaskName $name -Action $action -Principal $principal -Force | Out-Null
Start-ScheduledTask -TaskName $name
Start-Sleep -Milliseconds 400
$deadline = (Get-Date).AddSeconds(150)
while ((Get-ScheduledTask -TaskName $name).State -eq 'Running' -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 300 }
$result = (Get-ScheduledTaskInfo -TaskName $name).LastTaskResult
Unregister-ScheduledTask -TaskName $name -Confirm:$false
if (Test-Path $log) { Get-Content $log -Encoding utf8 }
exit $(if ($result -eq 0) { 0 } else { 1 })
