# Screenshot of the whole desktop to mt-probe\<name>.png (default shot).
param([string]$Name = "shot", [int]$Width = 1440)
. (Join-Path $PSScriptRoot "desktop.ps1")
Save-Screenshot (Join-Path $Work "$Name.png") $Width
