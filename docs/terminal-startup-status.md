# Terminal startup status

## Ownership and meaning

This is Motty-owned product state, not a Viva rendering or lifecycle capability.
`sshconn.PhaseConnected` describes the authenticated transport. A local connection
has that phase immediately because there is no network to dial. Neither fact
proves that an interactive shell has opened or produced output.

The terminal's tab and pane status now distinguish:

- Shell request still pending: the existing opening-terminal text and busy light.
- Shell opened, no output yet: the existing waiting-for-first-output text and a
  steady warning light. An empty terminal remains usable; it is not hidden behind
  a readiness timeout or blocked from receiving input.
- Shell has produced output: the connected indicator. This is evidence of output,
  not a claim that a particular prompt is visible or that shell initialization is
  complete. A shell may emit control sequences, or run without a prompt.

Waiting for output does not animate or request frames. Output notification is
what wakes the application. Remote connection progress remains available where
applicable; a local terminal no longer offers the remote progress-page button.
Remote-desktop status retains its existing lifecycle.

## Local shell and text input

Starting a local shell can wait on the desktop shell, profile scripts, or a
system proxy lookup. During startup restoration, Motty opens local shells
through the background path used for remote shell requests. The terminal screen
and its native text-input surface are published as soon as the local connection
is created; the shell is attached by a later window wake. This keeps the first
Chinese or other IME composition from waiting on process startup while
preserving the existing shell and output lifecycle. A local terminal opened by
an explicit user action keeps its existing synchronous path unless it follows
the system proxy. The first-frame path also avoids synchronously enumerating
installed shells or writing the session store; the platform default is resolved
by the background shell start, and startup persistence runs after the window is
available.

### Windows Pinyin cold-start check

Enable a local terminal at startup and select Microsoft Pinyin in Chinese mode.
Quit Motty completely. Launch it again and, as soon as the local pane appears,
type `ni` without clicking or waiting for the shell prompt. Check that the
candidate window appears at the terminal cursor and the composition survives
the shell becoming ready. Then choose 你 and verify it appears once at the
prompt. Repeat by restoring a saved local tab, checking that it opens only once.

For a PR, record the Windows build, IME, local shell and what happened on each
launch. The automated test checks that the text-input surface exists before the
first wake; it cannot observe the native candidate window.

## Verification (2026-10-22)

- A blocked shell-opening test reproduces the old premature connected status and
  checks opening, silent, and output-received transitions.
- A real local PTY runs a shell script that starts silently and waits for input.
  The headless window remains idle and accepts typing and Enter through the
  public input path. The shell's real output reaches the rendered terminal and
  updates its readiness indication.
- The real PTY test passes on macOS and the shared Linux environment. The common
  startup-state test passes on macOS, Linux, and Windows.
- Headless screenshots preserve the silent local startup and the live terminal;
  both layout checks report zero problems. A loopback-only `tools/e2esshd` session
  also completes host-key verification, password authentication, shell startup,
  and a real `echo` command; its settled layout reports zero problems.
- Motty's formatting, vet, complete tests, and race tests pass. No Viva source is
  changed. No real application window is opened.

Artifacts are under the task worktree's `artifacts/` directory.

On an Apple M2 Pro/macOS arm64, warmed `BenchmarkShellStartupPaint` runs at a
median 39.011 µs for waiting output and 38.201 µs for a live terminal, both with
0 B/op and 0 allocs/op (`-benchtime=1s -count=3`). The fix adds no persistent
struct field, dependency, background worker, timer, or polling. A silent local
terminal advances one minute of headless time without painting again.

Stripped native Motty (`-trimpath -ldflags='-s -w'`) is 21,433,330 bytes, versus
21,416,818 bytes before this change: +16,512 bytes. These are local native build
measurements, not claims about startup speed, RSS, other platforms' binary sizes,
or real-window presentation. Physical keyboard/IME behavior and a real Windows
ConPTY with deliberately silent startup were not manually exercised.
