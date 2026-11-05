# Terminal Command Hints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The user authorized inline execution, validation and a PR after the four follow-up optimizations. Do not deploy or merge.

**Goal:** Complete issue #510 with broad common-command hints, opt-in host discovery and paths, platform-aware localized details, and guarded live preview.

**Architecture:** Keep `internal/completion` pure and deterministic; add a separate audited common catalog beside the updateable Carapace snapshot. The workbench combines candidates, asynchronous availability, and a single-terminal preview state while existing session/prompt proofs remain authoritative. The Viva popover stays the UI surface.

**Tech Stack:** Go 1.26, Viva native UI/headless, Carapace catalog, existing `sshconn` and `localshell` adapters.

**Spec:** `docs/specs/510-command-hints/spec.md` (v1.1.0, 2026-11-05; user-confirmed migration and four follow-up optimizations)

## Global Constraints

- Default completion Off only for new installations, persisted in local `command_completion_default_off`. Existing configurations without it keep the old Off/List/Both interpretation. Only an explicit user mode choice creates shared `command_completion`; new automatic list, dynamic probe and preview switches default false.
- No Netcatty GPL source, runtime Fig generators, arbitrary `--help`, persisted availability or external API.
- Only validated fixed command names and safe two-character discovery prefixes reach bounded, cancelable host probes; a failure never blocks static completion.
- Host-only names and file paths are read-only, opt-in, in memory, tied to the current connection/confirmed line and never persisted or inserted without `SafeInsert`.
- The shell's own Tab passes through once; accepted completion and first Enter never execute a command.
- Physical preview only for a verified POSIX bash/zsh prompt with current safe Ctrl-U binding; drain before every write, no speculative rollback after stale output.
- All new candidate text obeys `SafeInsert` and session/prompt/epoch checks; painting starts no I/O.

## Review Focus

1. A new installation must persist Off locally without publishing a shared default, while existing configurations, including the ambiguous old Both, restore unchanged; Task 1 and Task 6 test both.
2. Updating the Carapace cache must not remove supplemental `cat -` flags; Task 2 tests both stores.
3. A refused extra SSH channel or malicious probe output must not mark commands installed; Task 3 tests rejection and cancellation.
4. An IME commit or delayed prompt after auto-popup scheduling must not accept a stale candidate; Task 4 tests it.
5. A preview with pending echo followed by Escape, Enter, user typing or reconnect must not write an unsafe rollback; Task 5 tests each transition.
6. A prefix with no static match can still discover a safe executable; an unsafe prefix, stale connection or unknown shell cannot; Task 7 tests each case.
7. A changed CWD, SFTP refusal or filename with shell syntax cannot become a path candidate or PTY write; Task 8 tests each case.
8. `dir` on a known CMD/PowerShell/Unix shell and GNU-only `tail` flags on macOS must not claim false applicability; Task 9 tests these with Chinese descriptions and ranking.

---

### Task 1: Preferences And Compatibility

**Files:** Modify `internal/workbench/completion.go`, `internal/workbench/persist.go`, `internal/workbench/persist_records.go`, `internal/workbench/workbench.go`, `internal/workbench/settings_paint.go`, `internal/workbench/settings.go`, `internal/i18n/completion.go`, `internal/i18n/english.go`, `internal/i18n/chinese.go`, `internal/i18n/traditional.go`. Test `internal/workbench/completion_settings_test.go`.

**Interfaces:** `completionMode()` retains Off/List/Both; `savedState.CommandCompletion string` is an optional authoritative mode record. `completionUI` owns `autoPopup`, `probeEnabled`, `previewEnabled` booleans and the settings IDs; settings are shared independent records.

- [x] Write tests: new state Off across restart and a second window; existing old Off/List/Both preserve behavior; explicit new mode survives restart; all three extra switches default false, round-trip, and Off disables their effects.
- [x] Run `go test ./internal/workbench -run 'Test.*Completion.*(Preference|Setting|Mode)' -count=1`; confirm the new tests fail.
- [x] Implement authoritative user-selected `command_completion` with old-boolean fallback; detect a truly new config before opening it and persist its device-local Off marker promptly, leaving legacy defaults unchanged. Persist new independent switches and expose English, simplified and traditional Chinese labels/toggles without changing shortcut semantics.
- [x] Rerun the focused command; verify all cases pass.

### Task 2: Audited Common Catalog

**Files:** Create `internal/completion/common.go`, `internal/completion/common_test.go`, `internal/completion/data/COMMON_SOURCE.txt`. Modify `internal/workbench/completion.go`, `internal/workbench/completion_test.go`.

**Interfaces:** `completion.Common() (*completion.Catalog, error)` returns a once-decoded, immutable supplemental catalog; workbench calls `completion.Complete` on both catalogs and merges by insertion. Supplemental candidates remain `FromCatalog: true`.

- [x] Write failing coverage tests for the spec's common-command matrix and `cat -` seven options, representative `ls -`, `kill -`, `ip `, `ss `, `netstat `, plus unsafe token rejection and non-overwrite after public catalog updates.
- [x] Run `go test ./internal/completion ./internal/workbench -run 'Test(Common|CompletionCommon)' -count=1`; confirm the new tests fail.
- [x] Add a bounded audited supplemental catalog only for demonstrable gaps, with original short descriptions and provenance; keep Carapace's own aliases and options authoritative on duplicate inserts. Do not change the updateable snapshot or its metadata.
- [x] Rerun focused tests and `go test ./internal/completion -count=1`; inspect coverage audit output for any unfilled matrix row.

### Task 3: Opt-In Host Availability

**Files:** Create `internal/completion/availability.go`, `internal/completion/availability_test.go`, `internal/workbench/completion_probe.go`, `internal/workbench/completion_probe_test.go`, `internal/localshell/availability.go`, `internal/localshell/availability_test.go`. Modify `internal/workbench/completion.go`, `internal/workbench/completion_workers.go`.

**Interfaces:** `completion.ProbeAvailable(ctx context.Context, runner completion.CommandRunner, kind shellkind.Kind, names []string) (map[string]bool, error)` runs fixed validated POSIX/PowerShell inventory queries with 2-second timeout and bounded output. `State.startCompletionProbe(session *Session)` publishes only when session ID, connection identity and probe generation still match. Local adapter implements `RunCommand` using the configured shell program in an isolated subprocess; unsupported transports return no result.

- [x] Write failing unit tests for valid/invalid names, POSIX/PowerShell script shape, truncation, timeout, cancellation, and unknown shell; workbench tests for opt-in, single in-flight request, stale connection, NoHistory and static fallback.
- [x] Run `go test ./internal/completion ./internal/localshell ./internal/workbench -run 'Test.*(Availability|CompletionProbe)' -count=1`; confirm new tests fail.
- [x] Implement limited probe runner and local adapter; start workers only on demand, parse bounded exact-name output, publish on UI thread, cancel on close/reconnect/settings change, and rank available roots without deleting unknown roots.
- [x] Rerun focused tests and `go test -race -p 1 ./internal/completion ./internal/localshell ./internal/workbench -run 'Test.*(Availability|CompletionProbe)' -count=1`.

### Task 4: Automatic Popup And Details

**Files:** Create `internal/workbench/completion_auto.go`, `internal/workbench/completion_auto_test.go`, `internal/workbench/completion_headless_test.go`. Modify `internal/workbench/completion.go`, `internal/workbench/terminal.go`, `internal/workbench/settings_search.go` and relevant `internal/i18n` completion texts.

**Interfaces:** `State.scheduleAutoCompletion(session *Session)` snapshots the echoed line and waits a bounded 100 ms quiet interval. `State.adoptAutoCompletion()` revalidates `completionEligible`, prompt, line, Secret, IME, mode and setting before opening the existing list. `paintCompletionList` draws a stable source/description detail region within its viewport.

- [x] Write failing workbench tests: auto-open after confirmed `ca`/`cat -` input; no auto-open when Off, NoHistory, secret, IME, AltScreen, stale prompt or unknown line; full description/keyboard selection and narrow-headless layout.
- [x] Run `go test ./internal/workbench -run 'Test.*(AutoCompletion|CompletionDetails|HeadlessCompletion)' -count=1`; confirm the new tests fail.
- [x] Hook scheduling after accepted input and UI Wake, avoid paint-time work, preserve manual shortcut/Tab/Enter, add bounded source labels and fixed-size detail with viewport clamping and readable CJK widths.
- [x] Rerun focused tests and inspect headless frames at 320x240 and 900x600 in Chinese, traditional Chinese and English.

### Task 5: Guarded Live Preview

**Files:** Create `internal/workbench/completion_preview.go`, `internal/workbench/completion_preview_test.go`, `internal/workbench/completion_preview_headless_test.go`. Modify `internal/workbench/completion.go`, `internal/workbench/terminal.go`, `internal/workbench/completion_input.go`.

**Interfaces:** `completionPreview` tracks baseline, selected line, owner/prompt identity and `idle/awaitingEcho/previewed/restoring` phases. `State.previewCompletion(session *Session, index int)` may send a bounded POSIX line-replace sequence only after `completionEligible`, known input guard and `shellkind.POSIX` with integration. `State.reconcileCompletionPreview(session *Session)` handles output acknowledgements; Escape restores only when the displayed line equals the previewed line.

- [x] Write failing state-machine tests for first selection, queued selection, confirmed echo, Escape restore, first Enter no CR, second Enter normal shell behavior, duplicate key, delayed output, IME/password/AltScreen, reconnect and unsupported shell.
- [x] Run `go test ./internal/workbench -run 'Test.*CompletionPreview' -count=1`; confirm the new tests fail.
- [x] Implement a single pending write/ack transition; suppress ordinary list-close only for preview-owned writes. If echo or owner proof fails, close preview without guessing recovery; keep terminal input usable and report the limitation.
- [x] Rerun focused tests and headless keyboard acceptance; exercise `go test -race -p 1 ./internal/workbench -run 'Test.*CompletionPreview' -count=1`.

### Task 6: Headless Acceptance And Regression

**Files:** Create `docs/specs/510-command-hints/verification.md` and task-owned headless screenshots under that package. Modify tests only to fix demonstrated regressions in scope.

- [x] Run specific coverage, settings, probe, auto-popup and preview tests in the isolated worktree with the compatible Viva sibling; record fresh results.
- [x] Run `go vet` on affected packages, macOS and Windows/Linux `vivahead,dev` builds, and unrestricted `go test ./... -count=1 -timeout=180s` with the offline module cache. The fake-window test uses Viva's in-memory Secret driver to avoid a locked native Keychain.
- [x] Run headless layout and input scenarios, capture screenshots for wide/narrow and Chinese/English, inspect actual frames; no real remote commands or user-owned browser sessions.
- [x] Compare each FR/SC with implemented tests and document any remaining platform-specific acceptance gap without claiming it passed.

### Task 7: Bounded Host Command Discovery

**Files:** Create `internal/completion/discovery.go`, `internal/completion/discovery_test.go`, `internal/workbench/completion_discovery.go`, `internal/workbench/completion_discovery_test.go`. Modify `internal/workbench/completion.go`, `internal/workbench/completion_workers.go`, `internal/workbench/completion_auto.go`, `internal/workbench/completion_probe.go`.

**Interfaces:** `completion.DiscoverCommands(ctx context.Context, runner completion.CommandRunner, kind shellkind.Kind, prefix string) ([]string, error)` returns at most 64 validated PATH executable names, from at most 4096 bytes in 2 seconds. `State.startCompletionDiscovery(session *Session, line string)` uses the existing opt-in host-query switch and snapshots owner/connection/prompt/generation. `completionCandidates` merges current-query discovered names while static and history candidates retain priority guarantees.

- [x] Add failing pure tests for safe/unsafe prefix, POSIX/PowerShell fixed script, malicious/oversize output, timeout, cancellation and truncation.
- [x] Add failing workbench tests for an empty-static root, no opt-in/NoHistory/unknown shell, stale connection or changed line, delayed result during preview, TTL and duplicate names.
- [x] Implement one bounded worker and per-connection prefix cache; let an empty-static list await its result without painting an empty popover. Rebuild only from the same confirmed baseline; recheck `SafeInsert` at acceptance.
- [x] Run `go test ./internal/completion ./internal/workbench -run 'TestDiscoverCommands|TestCompletionDiscovery' -count=1`, then the matching race tests.

### Task 8: Safe Local And SSH Path Candidates

**Files:** Create `internal/completion/path_query.go`, `internal/completion/path_query_test.go`, `internal/completion/path_list.go`, `internal/completion/path_list_test.go`, `internal/workbench/completion_path.go`, `internal/workbench/completion_path_test.go`. Modify `internal/workbench/completion.go`, `internal/workbench/completion_workers.go`, `internal/workbench/completion_auto.go`, `internal/i18n/completion.go` and the three language files.

**Interfaces:** `completion.ParsePathQuery(line, cwd string) (completion.PathQuery, bool)` accepts only safe, simple POSIX file-argument contexts with trusted absolute CWD. `completion.ListPathCandidates(ctx context.Context, runner completion.CommandRunner, query completion.PathQuery) ([]completion.Candidate, error)` uses a fixed, safely quoted, read-only POSIX script on the bounded/cancelable existing SSH/local command runner. `State.startCompletionPath(session *Session, query completion.PathQuery)` starts one worker; `State.adoptCompletionPath()` adds only suffixes passing `SafeInsert` and checks owner, connection, prompt, CWD and query generation. The existing probe opt-in controls both host queries and paths.

- [x] Add failing parser tests for `tail -f /var/lo`, `cd ./Do`, safe relative paths, empty/unsafe/quoted/secret/multiline paths, unsupported commands and missing CWD.
- [x] Add failing worker tests for local and mock SSH files/directories, unsafe filenames, timeout/cancellation, result caps, connection/CWD change, command-channel refusal and preserving shell Tab.
- [x] Implement fixed read-only path query with at most 64 candidates, 4096 output bytes and a 2-second deadline; skip symlink ambiguity and unsupported shell modes. Ensure no I/O in Paint and no partial result can be accepted stale.
- [x] Run `go test ./internal/completion ./internal/workbench -run 'TestParsePathQuery|TestCompletionPath' -count=1`, then matching race and `vivahead` real local/loopback fixtures.

### Task 9: Platform Syntax, Locale And Ranking

**Files:** Create `internal/completion/common_locale.go`, `internal/completion/common_locale_test.go`, `internal/workbench/completion_platform_test.go`. Modify `internal/workbench/completion.go`, `internal/workbench/completion_probe.go`, `internal/completion/common.go`, `internal/i18n/completion.go` and the three language files.

**Interfaces:** `completion.LocalizedDescription(language, root, token string) string` supplies reviewed Chinese descriptions for the 65-root floor and supplemental options; empty means retain the catalog's English. `State.staticCompletionCandidates(session *Session, line string)` applies known shell/system constraints without changing the stored Carapace snapshot. `completion.CommonPriority(name string) int` is the stable order after history and confirmed availability.

- [x] Add failing tests for `dir /` vs `dir -` in known POSIX/CMD/PowerShell, Darwin `tail --`, unknown-system reference fallback and no false installed label.
- [x] Add failing tests for 65 root descriptions in simplified/traditional Chinese, supplemental `cat` and `tail` options, English fallback, history > installed > common > other stable ranking.
- [x] Implement narrowly scoped platform filtering, original human-reviewed translations and root-only sorting; do not mutate shared catalog instances or alter user preferences.
- [x] Run focused tests, `go test ./internal/completion ./internal/workbench -count=1` and headless CJK/narrow visual checks.

### Task 10: Acceptance And Pull Request

**Files:** Modify `docs/specs/510-command-hints/verification.md`, `docs/specs/510-command-hints/research.md` and task-owned screenshots. No unrelated source changes.

- [x] Run focused red/green coverage, real local Bash/Zsh and loopback SSH headless acceptance including unknown command, path, platform and locale cases; inspect screenshots and visual/pixel layout.
- [x] Run full offline `go test ./... -count=1 -timeout=180s`, affected race tests, `go vet`, macOS/Linux/Windows builds and Windows test cross-compilation; record actual skips and unavailable runtime checks.
- [ ] Inspect diff and worktree ownership, fetch upstream for the target-base check, create a `codex/` feature branch from the isolated worktree, commit only this issue's files, push to the fork and create a PR against `t8y2/motty:main` closing #510.
- [ ] Observe PR CI and report whether Windows/Linux runners pass; do not merge, deploy, or claim external bastion/native Windows runtime acceptance from cross-compilation.
