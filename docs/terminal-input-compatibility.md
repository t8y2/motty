# Terminal input compatibility

Motty keeps the terminal as the input surface. It does not silently convert
Chinese text, punctuation, paths or passwords into another command.

## Mistyped command characters

At a recognized Bash/Zsh command prompt, an unquoted fullwidth space,
minus-like character or smart quote can look like ordinary shell syntax while
meaning something different. Before sending that text, Motty shows the exact
character/code point and offers **Use ASCII**, **Keep original**, or **Cancel
input**. The initial focus is Cancel. Confirming a choice sends only the held
text, never Enter; the command still needs the user's normal execution key.

Typing, the application paste shortcut and the terminal context menu use the
same check. Normal Chinese, quoted or escaped literal content is preserved.
Paired smart quotes remain paired through ordinary body editing. A transport
that cannot queue the text keeps the choice open for an explicit retry.

## Chinese editing and the current shell

Some non-UTF-8 shells edit Chinese input one byte at a time. Deleting one
character can leave invalid UTF-8 bytes in an otherwise ordinary command.
Requesting LANG over SSH is insufficient: the server may reject it, and startup
files or unexported shell locale variables can override it.

The existing session-local Bash/Zsh integration reports the actual shell locale
after startup. Detection changes no environment variable. A local shell started
with no locale in its environment at all — the application opened from Finder,
for example — is given `C.UTF-8` before it starts, so non-ASCII file names are
handled as UTF-8 from the first prompt; an environment that names a locale is
left as it is. If that shell is still not using UTF-8, a status row offers
**Enable UTF-8** when a verified UTF-8 locale is available. The action requires
an empty, idle, owned shell prompt and explicit confirmation. It changes LC_ALL
in that shell only; locale-sensitive formatting may also change. It does not
install locales or modify system settings or user startup files. The shell's own
history/audit may record the explicit repair command, as it does other commands.

Motty reports success only after the owning shell confirms UTF-8. If no
candidate is verified, the action stays unavailable. A partly typed command is
never cleared to run the repair. The warning can be hidden for this session.

## Safety and supported scope

- Input-method composition stays local; confirming a candidate does not also
  execute a shell command.
- Both opening and confirming an action first consume already-received owner
  output. A changed prompt, program, terminal identity or connection invalidates
  the pending action instead of sending it elsewhere.
- If the bounded output pump still has received data waiting to be parsed,
  prompt proof is retired. Confirmation never waits in an unbounded drain loop
  or assumes that an unread program transition is harmless.
- Shell reports are bound to this session's private prompt token. They do not
  authenticate a malicious remote host that already controls that token.
- Password/program input, alternate-screen editors, raw keyboard protocols,
  broadcast, multi-line/control-bearing paste and unsupported shell grammar
  are not normalized. Parent-shell status can remain visible during an ordinary
  program, but its repair action is disabled.
- Cursor edits or syntax that cannot be tracked safely disable correction for
  the current line. Parameter/process/command substitutions, brace expressions,
  heredocs and backticks are deliberately left alone.
- Unknown shells and sessions without the supported integration remain
  transparent. To remove the prompt proof this feature needs, turn off both
  remote folder-follow directions, select **Connection name** or **Terminal
  title** under **Settings > Terminal > Tab name**, and reconnect. **Current
  path** independently enables shell integration; Motty does not force a
  different shell startup just to enable correction.

This is not a general shell parser, a second command editor, automatic locale
installation, or a blanket rewrite of Unicode input. It cannot infer a remote
program transition that has not yet been reported to the client.
