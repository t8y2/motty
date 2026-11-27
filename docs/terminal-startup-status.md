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

When Motty restores a local tab or opens one at launch, it prepares the terminal
for the first paint. If that pane has focus, the text input system can start a
composition at the cursor before the shell is attached. Shell discovery and
saving a newly opened tab wait until the first window wake. The local shell
opens in the background; attaching it uses the same terminal so an unfinished
composition can continue. Printable text committed early can reach that same
focused local shell once it attaches. Backspace edits this pending text.
Moving focus away discards it, as does falling back from a missing shell to
the default. Enter, paste and broadcast input are not queued for startup.
The option to reconnect the last active tab still uses the saved workspace,
even if a new local tab is shown first. Local tabs opened later keep their
usual startup path.

### Windows Pinyin cold-start check

Test both ways of getting a local tab: (1) enable a local terminal at startup
with no saved tabs, and (2) turn that option off and leave a saved local tab
active. For each case:

1. Select Microsoft Pinyin in Chinese mode, fully quit Motty, and launch it.
2. When the pane first appears, type `ni` without clicking or waiting for the
   prompt. Check whether the candidate window appears at the terminal cursor.
3. Leave the composition open until the prompt appears, then select 你. Check
   that the prompt contains exactly one 你 and there is still just one local tab.
4. On a separate launch with a slow-starting shell, select 你 immediately and
   leave the tab focused. Check that it appears once when the shell is ready.

Record the Windows build, shell, Motty commit, and the observed result for each
launch in the PR. The workbench tests can check focus, composition events and
the saved session. They cannot verify where Windows draws the candidate window.

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
