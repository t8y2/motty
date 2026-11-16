# Play a scenario file against the probe window, in the desktop session.
#   -Steps <file>   one step per line, '#' starts a comment
#
#   place X Y W H      move and size the window in physical pixels (default 200 100 2000 1300)
#   focus              bring the window to the front
#   newlocal           open a local terminal (Ctrl+Shift+T)
#   click X Y          physical screen coordinates
#   clickw X Y         window-relative, in logical (DPI-independent) pixels
#   keys <sendkeys>    SendKeys syntax: ^ Ctrl  + Shift  % Alt  {ENTER}
#   type <text>        typed one character at a time, 250 ms apart
#   typefast <text>    typed in one go
#   sleep <ms>
#   shot <name>        screenshot to mt-probe\<name>.png
#   mark <label>       timestamp into marks.log, on the trace clock
param([string]$Steps)
. (Join-Path $PSScriptRoot "desktop.ps1")
$p = Get-ProbeProcess
if (-not $p) { throw "probe is not running; run: realwin.sh start" }
Focus-Window $p
Normalize-Keyboard $p
try {
$script:Guard = $p.Id   # Send-Keys refuses to type if another window takes the focus
foreach ($line in Get-Content $Steps) {
  $line = $line.Trim()
  if ($line -eq "" -or $line.StartsWith("#")) { continue }
  $verb, $rest = $line -split '\s+', 2
  $a = @(if ($rest) { $rest -split "\s+" })
  switch ($verb) {
    "place"     { if ($a.Count -ge 4) { Place-Window $p ([int]$a[0]) ([int]$a[1]) ([int]$a[2]) ([int]$a[3]) } else { Place-Window $p } }
    "focus"     { Focus-Window $p }
    "newlocal"  { Send-Keys "^+t" 1500 }
    "click"     { Click-At ([int]$a[0]) ([int]$a[1]) }
    "clickw"    { $r = Get-WindowRect $p; Click-At ($r.L + [int]([double]$a[0] * $Scale)) ($r.T + [int]([double]$a[1] * $Scale)) }
    "keys"      { Send-Keys $rest }
    "type"      { Type-Slowly $rest 250 }
    "typefast"  { Send-Keys $rest }
    "sleep"     { Start-Sleep -Milliseconds ([int]$a[0]) }
    "shot"      { Save-Screenshot (Join-Path $Work "$($a[0]).png") }
    "mark"      { Mark $rest }
    default     { throw "unknown step: $line" }
  }
}
} finally { Restore-Keyboard }
"done"
