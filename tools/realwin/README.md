# realwin: driving Motty on the physical Windows machine

Scripts for the physical Windows 11 machine used for platform testing (real GPU,
2880x1800, 200% scale): a real event loop, GPU timing, refresh rate and high DPI.
Developer-only; nothing here ships.

The endpoint is `MOTTY_WIN_REAL_HOST` / `MOTTY_WIN_REAL_USER`, read from `local.sh` here
(git-ignored). Login is by SSH key, no password. See `local.example.sh`.

## Why two kinds of remote command

An SSH login on Windows runs in session 0 and cannot see the desktop. Anything touching
windows, input or the screen goes through `remote/itask.ps1`, which runs a script as a
one-shot scheduled task in the logged-in user's console session (`rw_desktop`). Plain
commands use `rw_ssh` / `rw_ps`. `conhost --headless` keeps a console window from appearing.

## Typical run

```sh
tools/realwin/realwin.sh setup            # once
tools/realwin/realwin.sh build            # instrumented build, no change to this checkout or ../viva
tools/realwin/realwin.sh push
tools/realwin/realwin.sh start            # replaces a running probe, nothing else
tools/realwin/realwin.sh run local-git-bash
tools/realwin/realwin.sh run typing
tools/realwin/realwin.sh run flood
tools/realwin/realwin.sh fetch
tools/realwin/realwin.sh report out/run --from typing-begin --to typing-end
tools/realwin/realwin.sh stop
```

`status` shows what is running. `shot` saves a screenshot to `out/`. `clean` removes the
work directory (`%USERPROFILE%\mt-probe`).

## Scenarios

`scenarios/*.steps`, one verb per line (list in `remote/drive.ps1`). `mark` writes a label
on the same clock as the probes, which is how `report --from/--to` isolates a stretch.
Keep them free of absolute pixel coordinates: use `newlocal` (Ctrl+Shift+T) and keyboard
input, or `clickw` which is window-relative and DPI-scaled.

## What the probe measures

`trace/build.sh` copies this repository and `../viva` to a scratch directory and patches
timing lines in there. Window thread, output parsing and the local shell write to
`trace.log`, `pump.log`, `shell.log` on one clock. `report` prints key-to-screen latency,
frame gaps, per-frame work and bytes parsed per frame. It does not measure vsync or
compositor delay: that needs a camera or a present-statistics probe.

## Safety

- The installed Motty and its data are never touched: the probe is `motty-trace.exe` with
  `MOTTY_DATA_DIR` set, and `stop` / `start` only act on that name.
- Input is real. Do not run a scenario while someone is using the machine; check
  `realwin.sh status` first.
- Anything uploaded lands in `%USERPROFILE%\mt-probe`.
