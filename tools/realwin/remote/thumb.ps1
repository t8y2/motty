# Minimize and restore the probe window repeatedly and capture the taskbar thumbnail each time
# (issue #513: after about ten minimizes the preview turns into the application icon).
#   -Cycles N     how many minimize/restore rounds (default 15)
#   -Prefix NAME  files are written to mt-probe\NAME-<n>.png
# Run through itask.ps1: it moves the real pointer onto the taskbar button.
param([int]$Cycles = 15, [string]$Prefix = "thumb", [string]$Match = "*otty*",
  [switch]$ClickMinimize,   # press the window's own minimize button instead of calling ShowWindow
  [switch]$Busy)            # open a local terminal that keeps printing while the window is minimized
. (Join-Path $PSScriptRoot "desktop.ps1")
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
Add-Type @'
using System; using System.Runtime.InteropServices;
public static class RWT {
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr FindWindow(string c, string t);
  [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr h);
}
'@

function Find-TaskbarButton([string]$Pattern) {
  $tray = [RWT]::FindWindow("Shell_TrayWnd", $null)
  if ($tray -eq [IntPtr]::Zero) { throw "no taskbar window" }
  $root = [System.Windows.Automation.AutomationElement]::FromHandle($tray)
  $buttons = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants,
    (New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ControlTypeProperty, [System.Windows.Automation.ControlType]::Button)))
  foreach ($b in $buttons) {
    if ($b.Current.Name -like $Pattern -and $b.Current.Name -notlike "*Claude*") { return $b }
  }
  throw "no taskbar button matching $Pattern; buttons: " + (($buttons | ForEach-Object { $_.Current.Name }) -join " | ")
}

# A full-resolution crop of the screen.
function Save-Crop([string]$Path, [int]$X, [int]$Y, [int]$W, [int]$H) {
  $bmp = New-Object System.Drawing.Bitmap $W, $H
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen($X, $Y, 0, 0, $bmp.Size)
  $bmp.Save($Path)
  $g.Dispose(); $bmp.Dispose()
}

$p = Get-ProbeProcess
if (-not $p) { throw "probe is not running; run: realwin.sh start" }
$h = $p.MainWindowHandle
Focus-Window $p
$screen = [System.Windows.Forms.SystemInformation]::VirtualScreen
if ($Busy) {
  $script:Guard = $p.Id
  Send-Keys "^+t" 9000                         # a fresh local shell needs time before it takes input
  Send-Keys "ping -t 127.0.0.1{ENTER}" 1500
}
for ($i = 1; $i -le $Cycles; $i++) {
  if ($ClickMinimize) {
    # The button the user presses: three caption buttons at the right edge, minimize first.
    $r = Get-WindowRect $p
    Click-At ($r.R - [int](110 * $Scale)) ($r.T + [int](18 * $Scale))
  } else {
    [RW]::ShowWindow($h, 6) | Out-Null          # SW_MINIMIZE
  }
  Start-Sleep -Milliseconds 900
  $button = Find-TaskbarButton $Match
  $r = $button.Current.BoundingRectangle
  $cx = [int]($r.Left + $r.Width / 2); $cy = [int]($r.Top + $r.Height / 2)
  # Move like a mouse: absolute input events (SetCursorPos teleports without input), walking
  # onto the button from above, then resting; the taskbar opens the preview after a hover.
  $vw = $screen.Width; $vh = $screen.Height
  for ($k = 0; $k -le 16; $k++) {
    $px = [int](($cx - $screen.Left) * 65535 / $vw)
    $py = [int](($cy - 300 + 300 * $k / 16 - $screen.Top) * 65535 / $vh)
    [RW]::mouse_event(0x8001, $px, $py, 0, [UIntPtr]::Zero)   # MOVE | ABSOLUTE
    Start-Sleep -Milliseconds 30
  }
  Start-Sleep -Milliseconds 2800
  $x = [Math]::Max($screen.Left, $cx - 450)
  Save-Crop (Join-Path $Work "$Prefix-$i.png") $x ($screen.Bottom - 520) 900 520
  "cycle $i iconic=$([RWT]::IsIconic($h)) button=$($button.Current.Name) at $cx,$cy"
  $py = [int](($screen.Top + 300) * 65535 / $vh)
  [RW]::mouse_event(0x8001, [int](($cx - $screen.Left) * 65535 / $vw), $py, 0, [UIntPtr]::Zero)   # leave the taskbar so the preview closes
  Start-Sleep -Milliseconds 700
  [RW]::ShowWindow($h, 9) | Out-Null          # SW_RESTORE
  Start-Sleep -Milliseconds 900
}
"done"
