# Which window has the keyboard focus, and who owns it. Output: mt-probe\who.out
. (Join-Path $PSScriptRoot "desktop.ps1")
Add-Type @'
using System; using System.Text; using System.Runtime.InteropServices;
public static class FG {
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
}
'@
$h = [FG]::GetForegroundWindow(); $pid2 = 0; [FG]::GetWindowThreadProcessId($h, [ref]$pid2) | Out-Null
$sb = New-Object System.Text.StringBuilder 256; [FG]::GetWindowText($h, $sb, 256) | Out-Null
"foreground: pid=$pid2 name=$((Get-Process -Id $pid2).ProcessName) title=$($sb.ToString())" | Out-File (Join-Path $Work "who.out") -Encoding utf8
