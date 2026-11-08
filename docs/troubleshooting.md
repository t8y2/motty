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
