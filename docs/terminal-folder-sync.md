# Terminal and File Panel Folder Sync

The attached file panel follows the SSH terminal's working directory by default.
New Bash, Zsh and PowerShell sessions install session-only folder reporting,
without editing the server's existing startup files. Directory reports are kept while the panel
is opening, busy or being edited; only the latest pending directory is applied.

In **Settings > Files**, the two directions are independent:

- **Follow the terminal's folder** is on by default.
- **Terminal follows file navigation** is off by default. Turn it on to also
  change the owning terminal's folder after entering a remote directory.

The attached remote file panel also has **Go to this folder in the terminal**
beside the follow button. It changes the owning terminal to the displayed folder
once, independently of either follow preference. It validates the folder with a
fresh directory read. Integrated shells use the same empty-prompt checks as
reverse sync. An ordinary POSIX shell, including an **Interactive session only**
connection, can also receive this explicit action at a recognized empty shell
prompt. The prompt must stay unchanged during the directory read. This fallback
does not enable automatic reverse sync. A skipped or canceled request is never
retried automatically. If the displayed folder has since been deleted, the panel
stays on the server and reports that the folder no longer exists; it neither
sends `cd` nor falls back to local files.

Reverse sync requires a fresh successful directory read and an untouched,
ready shell prompt belonging to the same session, with command-start reporting
(Bash 4.4+ PS0, a DEBUG trap on older Bash, or Zsh preexec). It skips partial command
lines, running programs, nested SSH prompts, alternate screens and assistant
commands, with feedback in the file panel. A skipped request is never run later;
navigate again once the original shell is ready. Paths are quoted as literal
arguments to `builtin cd`, rather than invoking aliases or shell functions.

Complete submitted typeahead also blocks synchronization until the shell
acknowledges those command starts. Multiline commands or input sent to a running
program can conservatively leave unacknowledged input: at an empty shell prompt,
press Ctrl+C once to clear that ambiguity, then navigate again. Motty never
sends an interrupt automatically. A reconnect rotates the session token and
requires entirely fresh prompt authorization.

## Remembered Directory

The last directory reported by a remote shell's own prompt is saved with the
tab. A restored tab that reconnects starts its integrated Bash or Zsh there,
instead of in the account's home directory. The directory is still only a hint:
if it has since been removed, the shell starts where it normally would. Splits
made before that reconnect inherit the same remembered directory.

Only trusted prompt reports are remembered. A running program, a nested login,
or a report that does not belong to this session's integration cannot change
the next start directory. Disabling both integration directions, setting **Command completions** to
**Shortcut**, and leaving **Tab name** off **Current path** keeps the ordinary
shell startup, so neither remembers a remote directory in this way.

Local panes, standalone file windows and container command sessions do not
control terminals. Refreshing a list or following a terminal report never sends
another `cd` back. Both preferences use the existing shared-settings storage.

## PowerShell on Windows hosts

A Windows host's OpenSSH server starts the account's default shell. When the
probe finds PowerShell (Windows PowerShell 5.1 or PowerShell 7), Motty types one
line into the running shell, after the banner and the first prompt, and hides
its echo until a private ready marker. The line wraps the shell's `prompt`
function, so a prompt of your own still draws, and reports the folder at every
prompt. The file panel names a folder the way the host's SFTP server does:
`C:\Users\me` is `/C:/Users/me`. A folder that has no such name, such as a UNC
share, is not reported.

No file is written to the server: a script file would be subject to the host's
execution policy, a typed line is not. PowerShell's line editor records every
submitted line and offers no way to take one back, so the activation line stays
in that shell's history. A shell in constrained language mode cannot redefine
its prompt and is left as it was; the same goes for cmd.exe and other shells.

PowerShell has no hook that runs before every command, so Motty cannot tell an
idle prompt from a running program there. The panel follows the terminal and a
reconnect returns to the last folder, but a PowerShell prompt is never typed
into on the panel's behalf: **Terminal follows file navigation**, **Go to this
folder in the terminal** and path completion stay off.

## Local Terminal Folder

Local shells report their folder the same way. **Settings > Terminal > Local
terminal folder** chooses what that is used for:

Restored local tabs always start when Motty reopens, whatever **Settings >
Connection > On restore** says: starting a local shell connects to nothing, so
that setting only governs remote tabs. The choice below is where they start.

- **Last folder** is the default. Reopening Motty starts each restored local tab
  in the folder it was last in. A new local
  terminal starts in the folder of the local tab in front, or else in the last
  folder any local shell reported; that folder outlives the tab and the app.
- **Home folder** starts every local tab at home: restored local tabs forget
  their folder.

A split always starts in its pane's folder, whichever is chosen. The choice and
the last folder stay on this device rather than in synced settings.

## Startup Compatibility

- Bash uses a private runtime rc file that sources `/etc/profile` and the first
  readable user login profile, then appends to the existing string or array
  `PROMPT_COMMAND`. `~/.bashrc` is reached only when that login profile sources
  it, as in a login shell. It runs interactively, but Bash's `login_shell` flag
  and automatic `.bash_logout` processing differ from a true login shell.
- Zsh uses a private runtime ZDOTDIR, sources the user's startup files in order,
  restores ZDOTDIR, and adds a `precmd_functions` entry without replacing
  `precmd`. A `.zsh_history` default derived from the private directory is
  restored to the user's directory before their startup files run; custom or
  disabled history settings are kept. Startup files that deliberately disable
  further startup processing can also disable integration.
- The runtime files are removed after startup, with an exit cleanup as a
  fallback. Existing startup files are never rewritten, and the inherited umask
  is restored before starting the user shell.
- An unsupported shell, refused probe or refused integration exec falls back
  to the normal SSH shell request. The existing manual OSC 7 setup remains
  available for forward following; automatic reverse sync fails closed when
  this session's integration cannot prove prompt readiness.
- Bash before 4.4 has no `PS0`, so it reports command starts from a `DEBUG` trap
  (ADR 0004): `PROMPT_COMMAND` arms it at the end, and the first command of the
  next line reports once and disarms. An existing `DEBUG` trap of the user is
  kept and runs first, so it sees the same `$?` and `BASH_COMMAND`. A `DEBUG`
  trap that the user's startup files set *after* Motty's replaces it; reverse
  sync then stays off for that shell and the file panel says why. An array
  `PROMPT_COMMAND` (Bash 5.1+) always has `PS0` and never takes this path.
- To keep ordinary SSH shell startup entirely, disable both directions, set
  **Settings > Terminal > Tab name** to **Connection name** or **Terminal title**,
  and reconnect. **Current path** independently enables directory reporting.
  Re-enabling integration for an already open ordinary shell also
  requires reconnecting. There is no command injection into a live prompt to
  install integration.

## WSL

On Windows, each installed WSL distribution is an entry in **Settings > Terminal
> Local shell**. Motty starts it with `wsl.exe -d <distribution> --cd ~ -e
/bin/sh -c <script>`; the script travels Base64-encoded in a single argument
with no quote, backslash or newline for `wsl.exe` to re-split, and it takes the
distribution's login shell from `getent passwd`. Bash and Zsh then start through
the same private startup files as above. Any other shell starts as a plain login
shell without reporting.

The folder is reported as a Windows path, which `wslpath -w` makes of the Linux
one, in Windows Terminal's OSC 9;9, with the prompt markers bound to the
session's token. A folder on a Windows drive arrives as `C:\...`; one in the
distribution as `\\wsl.localhost\<distribution>\...` (`\\wsl$\` on older Windows
10). The file panel lists that share, which works whether the distribution is
WSL 1 or 2 but is slower than a native folder. `wslpath` runs only when the
folder changes, and a folder name with a control character is not reported,
since it could end the report early.

A tab's remembered folder is passed to the distribution in the environment
(`MOTTY_WSL_CD`, carried by `WSLENV`) and changed into by the script, so a
folder that has since gone leaves the shell in the home folder instead of
failing the launch. Paths dropped on a WSL terminal, or inserted from the file
panel, are typed as the distribution sees them: `/mnt/c/...` for a Windows
drive, assuming the default mount point, and `/...` for the distribution's own
files.

Typing `wsl` into a PowerShell or CMD terminal starts a shell Motty did not
start, so it reports nothing and the panel stays where it was. Choose the
distribution as the local shell instead.

The automated checks use isolated local Bash/Zsh PTYs, a real loopback SSH PTY,
and the workbench's scripted transports. They do not constitute acceptance on
every server, user profile, shell version or operating system.
