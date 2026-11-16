# Facts about the machine and the probe, printed as plain text.
$gpu = Get-CimInstance Win32_VideoController | Where-Object { $_.CurrentHorizontalResolution } | Select-Object -First 1
"gpu:        $($gpu.Name) $($gpu.CurrentHorizontalResolution)x$($gpu.CurrentVerticalResolution) @ $($gpu.CurrentRefreshRate) Hz"
"os:         " + (Get-CimInstance Win32_OperatingSystem).Version
"git bash:   " + (Test-Path 'C:\Program Files\Git\bin\bash.exe')
"console:    " + ((quser 2>$null) -join ' | ')
Get-Process | Where-Object { $_.ProcessName -match '^motty' } | ForEach-Object { "process:    $($_.ProcessName) pid=$($_.Id) session=$($_.SessionId) path=$($_.Path)" }
$w = Join-Path $env:USERPROFILE "mt-probe"
if (Test-Path $w) { Get-ChildItem $w -File | ForEach-Object { "work:       $($_.Name) $($_.Length)" } }
