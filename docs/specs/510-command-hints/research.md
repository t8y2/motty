# Issue #510: Netcatty Comparison And Motty Design

Inspected the public Netcatty `main` tree at `0e3f282657df29a5dc6939c86dbf4ea3ad2be8ea` on 2026-11-05. The issue provides screenshots, not an implementation contract. Netcatty is GPL-3.0; no source or Fig generators are copied into Motty.

## Observed Implementation

| Netcatty module | Actual behavior | Motty decision |
|---|---|---|
| [figSpecLoader](https://github.com/binaricat/Netcatty/blob/0e3f282657df29a5dc6939c86dbf4ea3ad2be8ea/components/terminal/autocomplete/figSpecLoader.ts) | Electron IPC loads `@withfig/autocomplete` specs; caches in-flight loads and preloads common specs in batches. | Retain Motty's audited, offline Carapace snapshot and a separately tested supplemental catalog. No runtime generator or GPL dependency. |
| [completionEngine](https://github.com/binaricat/Netcatty/blob/0e3f282657df29a5dc6939c86dbf4ea3ad2be8ea/components/terminal/autocomplete/completionEngine.ts) and [terminalCompletionProviders](https://github.com/binaricat/Netcatty/blob/0e3f282657df29a5dc6939c86dbf4ea3ad2be8ea/components/terminal/autocomplete/terminalCompletionProviders.ts) | Merge specs, host history, snippets, path and plugin candidates; bound plugin response time, deduplicate and rank. | Merge Motty history with both local catalogs, verified host availability and prefix-scoped executable discovery. The two opt-in host queries are bounded and kept in memory per connection. |
| [remotePathCompleter](https://github.com/binaricat/Netcatty/blob/0e3f282657df29a5dc6939c86dbf4ea3ad2be8ea/components/terminal/autocomplete/remotePathCompleter.ts) | Asynchronously lists remote/local paths for path-taking commands; keeps short-lived bounded caches. | Add a conservative, read-only POSIX path provider for safe bare tokens and confirmed per-session CWD. It uses Motty's cancelable command channel, not SFTP's context-free `ReadDir`; unsupported or complex paths keep native shell Tab. |
| [livePreviewSequence](https://github.com/binaricat/Netcatty/blob/0e3f282657df29a5dc6939c86dbf4ea3ad2be8ea/components/terminal/autocomplete/livePreviewSequence.ts) and [useTerminalAutocomplete](https://github.com/binaricat/Netcatty/blob/0e3f282657df29a5dc6939c86dbf4ea3ad2be8ea/components/terminal/autocomplete/useTerminalAutocomplete.ts) | Append suffix or rewrite with Ctrl-U on POSIX and backspaces on Windows; track a logical baseline during late updates. Network-device sessions skip writes. | Opt-in POSIX-only physical preview requires a current shell-reported line-clear binding and verified echoed line; drain queued output before each write; do not rewrite Windows, network-device, or unknown editors. |

## Ownership And Remaining Opportunities

`internal/completion` owns immutable syntax and `SafeInsert`; `internal/sshconn` and `internal/localshell` own bounded host probes; `internal/workbench` owns session/prompt identity, cache TTL, ordering, popup and preview. The local new-install Off marker is not published as a synced mode. This keeps old Off/List/Both states intact and ensures that enabling the list alone does not execute a probe or write to the PTY.

1. **Delivered in v1.1:** prefix-filtered PATH discovery, conservative path arguments, known `dir`/BSD `tail` dialect guards, 65 common-root Chinese descriptions, all supplemental option translations and available/common ranking. Neither discovery nor path listing runs while the existing host-query toggle is off.
2. **Remaining shell coverage:** quoted/space-containing paths, shell aliases/functions, path symlinks and dynamic argument generators remain native Tab's responsibility. An exec channel's PATH can differ from the interactive shell's environment.
3. **Platform acceptance:** validate native PowerShell/CMD syntax, Windows input methods and representative external bastions before release. Cross-compilation and local loopback SSH are not those checks; the ignored `tools/realwin/local.sh` is absent in this worktree.
4. **Catalog stewardship:** run the 65-command root-and-syntax audit on every catalog update, record source/version, and review broader GNU/BSD/BusyBox differences instead of treating a static reference as proof of installation.
