# Troubleshooting

## If Motty closes unexpectedly

Motty keeps a log and, after an abnormal exit, a crash report in the `logs`
folder of its data directory:

| Platform | Location |
| --- | --- |
| Windows | `%APPDATA%\motty\logs` |
| macOS | `~/Library/Application Support/motty/logs` |
| Linux | `~/.config/motty/logs` |

A portable copy keeps them in `motty-data\logs` beside the program. Attach
`motty.log` and the newest `crash-*.log` to a bug report. When the program
cannot start at all, it says why in a message box and writes the reason to the
log.

If the program disappears and `crash.log` stays empty, the process was ended by
a native fault (a graphics driver, for instance) before Go could write
anything. On Windows 11 22H2 or later Motty then asks Windows Error Reporting
for a dump in `%LOCALAPPDATA%\Motty\CrashDumps`; the Application Error event in
Event Viewer (Windows Logs → Application) names the faulting module.

To tell the usual suspects apart, start Motty with one of these set:

- `VIVA_SOFTWARE_RENDERING=1` — no graphics driver.
- `VIVA_ACCESSIBILITY=0` — no UI Automation tree.

A dump is a copy of the program's memory: look before you share it.

## If Windows Smart App Control blocks Motty

Windows 11 with Smart App Control turned on blocks programs it cannot verify,
including any whose signature is missing. The Windows packages are not
Authenticode-signed yet, so a copy that Windows does not already trust is
stopped with a *Smart App Control has blocked a potentially unsafe app* dialog
(智能应用控制已阻止可能不安全的应用) naming `motty.exe`, whether it came from
the installer or from the portable ZIP.

Clearing the mark of the web (`Unblock-File`) does not lift the block: the check
applies to the executable itself, not to the download, and Windows offers no way
to allow a single app. On such a system Motty runs only after Smart App Control
is turned off under Windows Security → App & browser control → Smart App
Control. Depending on the Windows build, turning it back on later may require
resetting or reinstalling Windows. The release notes will say when a
code-signed build is available.
