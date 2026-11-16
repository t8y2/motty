# Shared desktop helpers: dot-source from other remote scripts that run through itask.ps1.
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type @'
using System; using System.Runtime.InteropServices;
public static class RW {
  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, int dx, int dy, uint d, UIntPtr e);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr h, int x, int y, int w, int hh, bool repaint);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
  [DllImport("user32.dll")] public static extern uint GetDpiForSystem();
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern short GetKeyState(int vk);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] public static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
}
'@
[RW]::SetProcessDPIAware() | Out-Null
$Work = Join-Path $env:USERPROFILE "mt-probe"
$Scale = [RW]::GetDpiForSystem() / 96.0

function Get-ProbeProcess([string]$Name = "motty-trace") { Get-Process $Name -ErrorAction SilentlyContinue | Select-Object -First 1 }

function Foreground-Pid {
  $pidOut = 0
  [RW]::GetWindowThreadProcessId([RW]::GetForegroundWindow(), [ref]$pidOut) | Out-Null
  return $pidOut
}

# Windows refuses SetForegroundWindow from a process that did not receive the last input, and
# a synthetic Alt press does not lift that here. Minimizing and restoring the window does, and
# so does WScript.Shell.AppActivate. Throws when the window still is not in front: typing would
# otherwise go into whatever the user has open.
function Focus-Window($Process) {
  $h = $Process.MainWindowHandle
  [RW]::ShowWindow($h, 9) | Out-Null   # SW_RESTORE
  [RW]::SetForegroundWindow($h) | Out-Null
  Start-Sleep -Milliseconds 200
  if ((Foreground-Pid) -ne $Process.Id) {
    [RW]::ShowWindow($h, 6) | Out-Null   # SW_MINIMIZE
    Start-Sleep -Milliseconds 300
    [RW]::ShowWindow($h, 9) | Out-Null
    Start-Sleep -Milliseconds 400
  }
  if ((Foreground-Pid) -ne $Process.Id) {
    (New-Object -ComObject WScript.Shell).AppActivate($Process.Id) | Out-Null
    Start-Sleep -Milliseconds 400
  }
  if ((Foreground-Pid) -ne $Process.Id) {
    $other = Get-Process -Id (Foreground-Pid) -ErrorAction SilentlyContinue
    throw "could not bring the probe to the front; foreground is $($other.ProcessName) (pid $($other.Id))"
  }
}

# Put the window at a known place and size (physical pixels) so screenshots are comparable.
function Place-Window($Process, [int]$X = 200, [int]$Y = 100, [int]$W = 2000, [int]$H = 1300) {
  [RW]::MoveWindow($Process.MainWindowHandle, $X, $Y, $W, $H, $true) | Out-Null
  Focus-Window $Process
}

function Get-WindowRect($Process) {
  $r = New-Object RW+RECT
  [RW]::GetWindowRect($Process.MainWindowHandle, [ref]$r) | Out-Null
  return $r
}

# Click at physical screen coordinates.
function Click-At([int]$X, [int]$Y, [string]$Button = "left") {
  [RW]::SetCursorPos($X, $Y) | Out-Null
  Start-Sleep -Milliseconds 200
  if ($Button -eq "right") { [RW]::mouse_event(8,0,0,0,[UIntPtr]::Zero); Start-Sleep -Milliseconds 60; [RW]::mouse_event(16,0,0,0,[UIntPtr]::Zero) }
  else { [RW]::mouse_event(2,0,0,0,[UIntPtr]::Zero); Start-Sleep -Milliseconds 60; [RW]::mouse_event(4,0,0,0,[UIntPtr]::Zero) }
  Start-Sleep -Milliseconds 300
}

# SendKeys types what the keyboard state says, so two things on the user's machine must be
# neutral for a run and put back afterwards:
#  - Caps Lock: "settings" would arrive as SETTINGS. Toggled with a raw key event, read back.
#  - The IME conversion mode: the user's Chinese IME is in native mode, which turns letters
#    into a candidate list and types nothing. Mode 0 passes letters straight through.
Add-Type @'
using System; using System.Runtime.InteropServices;
public static class RWIME {
  [DllImport("imm32.dll")] public static extern IntPtr ImmGetDefaultIMEWnd(IntPtr h);
  [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
}
'@
$script:CapsWasOn = $false
$script:ImeMode = $null
function Set-CapsLock([bool]$On) {
  for ($i = 0; $i -lt 3 -and ((([RW]::GetKeyState(0x14) -band 1) -eq 1) -ne $On); $i++) {
    [RW]::keybd_event(0x14, 0x3A, 0, [UIntPtr]::Zero)
    [RW]::keybd_event(0x14, 0x3A, 2, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 200
  }
  if ((([RW]::GetKeyState(0x14) -band 1) -eq 1) -ne $On) { throw "could not set Caps Lock to $On" }
}
function Set-ImeMode($Window, [int]$Mode) {
  $ime = [RWIME]::ImmGetDefaultIMEWnd($Window)
  if ($ime -eq [IntPtr]::Zero) { return $null }
  # WM_IME_CONTROL 0x283: IMC_GETCONVERSIONMODE 1, IMC_SETCONVERSIONMODE 2
  $old = [int][RWIME]::SendMessage($ime, 0x283, [IntPtr]1, [IntPtr]0)
  [RWIME]::SendMessage($ime, 0x283, [IntPtr]2, [IntPtr]$Mode) | Out-Null
  return $old
}
function Normalize-Keyboard($Process) {
  if (([RW]::GetKeyState(0x14) -band 1) -eq 1) { $script:CapsWasOn = $true; Set-CapsLock $false }
  $script:ImeWindow = $Process.MainWindowHandle
  $script:ImeMode = Set-ImeMode $script:ImeWindow 0
}
function Restore-Keyboard {
  if ($script:CapsWasOn) { Set-CapsLock $true; $script:CapsWasOn = $false }
  if ($null -ne $script:ImeMode) { Set-ImeMode $script:ImeWindow $script:ImeMode | Out-Null; $script:ImeMode = $null }
}

# SendKeys syntax: ^ Ctrl, + Shift, % Alt, {ENTER} etc. Literal + ^ % ~ ( ) { } must be wrapped in braces.
function Send-Keys([string]$Keys, [int]$AfterMs = 300) {
  if ($script:Guard -and (Foreground-Pid) -ne $script:Guard) { throw "focus left the probe (foreground pid $(Foreground-Pid)); refusing to type" }
  [System.Windows.Forms.SendKeys]::SendWait($Keys)
  Start-Sleep -Milliseconds $AfterMs
}

# Type text one character at a time with a gap, as a person would (default 250 ms).
function Type-Slowly([string]$Text, [int]$GapMs = 250) {
  foreach ($ch in $Text.ToCharArray()) {
    $s = [string]$ch
    if ($s -match '[+^%~(){}\[\]]') { $s = "{$s}" }
    Send-Keys $s $GapMs
  }
}

# Screenshot of the whole virtual screen, scaled to Width px wide.
function Save-Screenshot([string]$Path, [int]$Width = 1440) {
  $b = [System.Windows.Forms.SystemInformation]::VirtualScreen
  $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size)
  $h = [int]($Width * $b.Height / $b.Width)
  $small = New-Object System.Drawing.Bitmap $Width, $h
  $g2 = [System.Drawing.Graphics]::FromImage($small)
  $g2.InterpolationMode = 'HighQualityBicubic'
  $g2.DrawImage($bmp, 0, 0, $Width, $h)
  $small.Save($Path)
  $g.Dispose(); $g2.Dispose(); $bmp.Dispose(); $small.Dispose()
}

# Timestamp on the same clock the trace patch writes (microseconds since epoch, mod 1e9, in ms).
function Trace-Clock { $t = ([DateTime]::UtcNow - [DateTime]'1970-01-01').Ticks / 10; return ($t % 1000000000) / 1000 }
function Mark([string]$Label) { ("{0,9:F3} {1}" -f (Trace-Clock), $Label) | Add-Content -Path (Join-Path $Work "marks.log") -Encoding ascii }
