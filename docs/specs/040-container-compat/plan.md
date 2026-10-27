# Docker Container Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make all read-only remote-tools tabs handle old/new host command formats truthfully and open an interactive container Shell in a separate Motty tab without adding a toolbar icon.

**Architecture:** Extend Motty's existing Go `remotetools` CLI collector and Viva remote-tools dialog. Reuse the current authenticated SSH client and PTY for a new tab, with a fixed validated container ID and probed Shell. Do not introduce a second UI framework, Docker daemon socket protocol, or saved-host write operation for validation.

**Tech Stack:** Go 1.26, Viva, `golang.org/x/crypto/ssh`, native Motty terminal.

**Spec:** `docs/specs/040-container-compat/spec.md` (approved scope from Motty #40, user screenshot and explicit implementation request).

## Global Constraints

- No new toolbar icon; preserve the existing remote-tools dialog and its other tabs.
- The original SSH terminal and user-saved host/container state are never modified by a shell-opening action.
- No automatic sudo, TCP daemon configuration, remote installation or container lifecycle action on the user's saved hosts.
- Do not promise an exact minimum Docker version without live/version-matrix evidence; collect truthful diagnostics when a host cannot run a feature.
- Dependency `github.com/vivaui/viva` is a sibling source via `replace ../viva` and may require workspace wiring; do not edit another agent's Viva checkout.
- Current `origin/main` baseline is `e1536051be94f56ca6b54982bb8fb3cc5ad8d562`; the reference SSH plugin's Vue/Rust code is not a source implementation for Motty.

## Review Focus

- Old CLI rejects `.State`: only that unsupported template field triggers a legacy retry, which must parse Up/Exited/Paused/Created/Restarting/Dead/unknown correctly (Task 1).
- Both formats fail or the daemon is offline: do not return an apparently successful empty container list (Task 1).
- A stopped/vanished container, no supported Shell, or an SSH session without sharing: show a clear error; never write to the original terminal (Task 2/3).
- The user changes tab or closes the dialog during probe/open: the command remains bound to the selected original SSH session and no stale result targets a new host (Task 3).
- Existing Docker/resource/Compose tabs and fake-SSH tests retain their behavior; entering a running container with a failing user command never reruns shell detection (Task 3/4).
- Optional resource formatter fields (`.CreatedSince`, `.Scope`), older `ps` options and Compose capability errors either have evidence-driven read fallbacks or a truthful unsupported state; never a false empty row (Task 4).

---

### Task 1: Docker list old-format fallback

**Files:** Modify `internal/remotetools/remotetools.go`; test `internal/remotetools/remotetools_test.go`.

**Interfaces:** `Containers(context.Context, Runner) ([]Container,error)` remains unchanged. Add `legacyContainerCommand`, `parseLegacyContainers([]byte) ([]Container,error)`, and `stateFromStatus(string) string` privately. Keep modern `ParseContainers` unchanged.

- [ ] **Step 1: Write failing tests** for exact screenshot `.State` template error, modern-only one call, legacy six Docker status variants, malformed fallback and generic daemon errors not retried. Fake Runner records each command and its output/error. Assert legacy results preserve name/image/ID and state while empty success stays empty.
- [ ] **Step 2: Run** `go test ./internal/remotetools -run 'TestContainers.*Legacy|TestContainers.*Version' -count=1`; expected FAIL at missing fallback or missing symbol.
- [ ] **Step 3: Implement** retry only for the `.State` template-unsupported signature, four-column `ID|Names|Image|Status`, state derivation from human Status, and the same bounded count/strict parse as the existing reader. Preserve error classification for non-template failures.
- [ ] **Step 4: Run** focused tests and `go test ./internal/remotetools -count=1`; expected PASS.
- [ ] **Step 5: Commit** collector and test with `fix(docker): fall back for legacy ps state templates`.

### Task 2: Independent SSH PTY command tab plumbing

**Files:** Modify `internal/sshconn/shell.go`, `internal/workbench/terminal.go`, `internal/workbench/model.go`, `internal/workbench/workbench.go`; test `internal/sshconn/shell_test.go`, `internal/workbench/share_test.go`.

**Interfaces:** Extend `sshconn.ShellConfig` with optional `Command string`: empty means existing `session.Shell()`, nonempty means `session.Start(Command)` after PTY request. Add an ephemeral `Session.containerCommand` (never save command in durable host catalog); new container tab owns its own Shell/terminal and borrows the chosen existing SSH transport, not an arbitrary matching host. A declined channel reports error without starting a fresh connection.

- [ ] **Step 1: Write failing SSH fake-server test** that asserts PTY then `Start` when `Command` is nonempty, and existing Shell request when empty. Add workbench fake-sharer test for an independent tab/PTY bound to the chosen source session, and no fallback to Dial when Share is refused.
- [ ] **Step 2: Run** focused `go test ./internal/sshconn ./internal/workbench -run 'Test.*CommandShell|Test.*ContainerTab' -count=1`; expected FAIL until command/tab plumbing exists.
- [ ] **Step 3: Implement** the optional command request and dedicated pre-shared session setup, with existing terminal screen/resize/input pipeline. Prevent command-tab reconnect/restore from silently turning into an ordinary shell with a container title.
- [ ] **Step 4: Run** focused SSH/workbench tests and existing tab duplication tests; expected PASS.
- [ ] **Step 5: Commit** PTY plumbing and tests with `feat(terminal): support isolated SSH command tabs`.

### Task 3: Shell compatibility and existing remote-tools UI

**Files:** Modify `internal/remotetools/remotetools.go`, `internal/workbench/remote_tools.go`, `internal/i18n/terminal_tools.go`, `internal/i18n/chinese.go`, `internal/i18n/english.go`; test `internal/remotetools/remotetools_test.go`, `internal/workbench/remote_tools_test.go`.

**Interfaces:** Add `ShellCommand(context.Context, Runner, string) (string,error)` which validates full ID, probes a fixed candidate set with bounded commands, returns the exact validated `docker exec -it ID SHELL` command or an actionable error. Add one `hitRemoteShell` action to the existing container tab (the dialog has capacity eight); no new icon or menu. The UI invokes Task 2's new-tab helper for the selected running container only.

- [ ] **Step 1: Write failing tests** for sh/busybox path, bash fallback, no Shell, stopped selection, wrong-host/stale result, distinct new terminal, unchanged original shell input, and no double-send on Shell nonzero exit. Assert new button is painted only in the container tab; other tabs are unchanged.
- [ ] **Step 2: Run** focused `go test ./internal/remotetools ./internal/workbench -run 'Test.*ContainerShell|Test.*RemoteTools.*Shell' -count=1`; expected FAIL before implementation.
- [ ] **Step 3: Implement** bounded async Shell probe and UI action using the selected session captured before async work; project status errors through existing dialog. Initialize new tab only if the probe succeeded and its source is still live. Use existing button styling/i18n and `session.Start` PTY transport from Task 2.
- [ ] **Step 4: Run** focused tests, `go test ./internal/workbench ./internal/sshconn ./internal/remotetools -count=1`; expected PASS.
- [ ] **Step 5: Commit** feature, i18n and tests with `feat(docker): open running containers in independent terminal`.

### Task 4: Read compatibility of the other remote-tools tabs

**Files:** Modify `internal/remotetools/docker_resources.go`, `internal/remotetools/remotetools.go`, `internal/remotetools/compose.go` if capability recognition needs it; test `internal/remotetools/remotetools_test.go` and `internal/workbench/remote_tools_test.go` if UI error text changes. Do not alter resource creation/removal or other writing contracts.

**Interfaces:** `Resources(context.Context, Runner, Kind)` and `Processes(context.Context, Runner, SortKey)` signatures remain stable. Add private fallback command generators and parsers for legacy optional resource columns; `Resource.Key` and `Cells` still have the current count/order (missing age/scope is a neutral `"—"` or empty cell, never invented data). Compose remains `Compose(context.Context, Runner)`; `ErrNoCompose` is for a missing plugin and new `ErrComposeListUnsupported` distinguishes a present but too-old `ls`/JSON capability from empty projects.

- [ ] **Step 1: Write failing tests** for images unsupported `.CreatedSince`, volumes/networks unsupported `.Scope`, unchanged modern one-call behavior, both versions failing, ordinary daemon error not retried. Cover a known `ps -eo` format refusal with a safe compatible format; if none parses, assert `ErrUnsupported` rather than a false empty process list. Cover older Compose missing-plugin/unsupported format errors returning an explanatory error, never `[]`.
- [ ] **Step 2: Run** `go test ./internal/remotetools -run 'Test.*LegacyResource|Test.*LegacyProcess|Test.*ComposeCompatibility' -count=1`; expected FAIL at the missing fallbacks/diagnostics.
- [ ] **Step 3: Implement** error-signature-gated read-only fallbacks without assuming Docker version number, preserve strict parsing and stable resource keys; avoid turning generic `not found` from Docker daemon into `ErrNoDocker` without CLI evidence. For Compose, do not invent a v1 project listing if `ls --format json` is unsupported.
- [ ] **Step 4: Run** focused tests and `go test ./internal/remotetools ./internal/workbench -count=1`; expected PASS, including existing resource actions and process management tests.
- [ ] **Step 5: Commit** tests and compatibility with `fix(remote-tools): degrade unsupported legacy listing fields`.

### Task 5: Verification, local installation and PR

**Files:** Update `docs/specs/040-container-compat/spec.md` with actual verified compatibility limits only; add focused headless E2E fixture if the current driver supports this dialog's click path. No unrelated release/version files.

- [ ] **Step 1: Run** `test -z "$(gofmt -l .)"`, `go vet ./...`, `go test ./... -count=1`, `go test -race ./... -count=1` and native/headless build for the final HEAD; record commands and results. A failed missing dependency is not a successful test.
- [ ] **Step 2: Package/install** a development Motty build locally without overwriting the existing signed `/Applications/Motty.app` or altering saved-host/known-host/vault data without a safe copy. Verify actual built executable identity/path and app startup, then connect a saved host only with existing user-approved authentication and perform read-only Docker list. If a safe test container is present, verify independent interactive shell and exit; otherwise report that live Shell verification was not possible. Never issue destructive Docker commands on saved hosts.
- [ ] **Step 3: Review** diff for CLI escape/target/transport/error safety; ensure no source from MIT plugin was pasted without license obligations. Recheck issue #40 assignment and target `t8y2/motty`/fork state.
- [ ] **Step 4: Commit** documentation/tests as needed, push the verified `codex/issue-40-docker-compat` branch to `caichangqing1120/motty`, create a PR against `t8y2/motty:main` with `Fixes #40`, testing/evidence/limitations, then attach PR to this task. Do not merge or close issue before merge.
