# Batch Host Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Batch-edit explicitly selected authentication fields without changing unselected hosts or shared key passphrases.

**Architecture:** Reuse existing host selection and editor conventions. One batch draft carries opt-in fields; validate all targets, then apply host metadata and vault changes. A host-bound encrypted passphrase override is preferred over the shared key-path secret in every authentication path, with exact key-path binding.

**Tech Stack:** Go 1.26, Viva UI, existing `internal/vault`, `internal/otp`, `internal/keyidentity`.

**Spec:** `docs/specs/004-bulk-host-password/spec.md`

## Global Constraints

- Use the existing isolated PR #366 worktree; do not touch the user's main checkout or PR #365.
- A checked field is the only field changed. Never silently overwrite a shared key-path passphrase, reveal a secret or change a remote server password.
- Scope includes SSH username, auth policy, identity, password, OTP, agent forwarding and selected-host key passphrase; VNC/RDP retain password support.
- Keep PR Draft until implementation, verification and CI pass.

## Review Focus

- Selected and unselected hosts share an encrypted key: only selected-host passphrase lookup changes.
- Password-only and OTP-only edits preserve authentication policy and unrelated credentials.
- Switching to Key/Agent with a retained old password never offers it in that mode.
- Duplicate endpoints after username change and unavailable identities reject the batch.
- Vault/config failure or cancel preserves state; modal inputs are masked and cleared.

---

### Task 1: Host-bound passphrase lookup

**Files:** `internal/workbench/host_passphrase.go`, `internal/workbench/vault_ui.go`, `internal/workbench/connection.go`, `internal/workbench/host_test_connection.go`, `internal/workbench/agent_mcp.go`, `internal/workbench/host_secret_prefill.go`, `internal/workbench/host_management.go`, `internal/workbench/host_passphrase_test.go`.

**Interfaces:** `hostPassphraseSecretName(catalogID string) string`; `savedPassphraseName(reader passwordReader, owner, path string) string` chooses a matching host override or `passphraseSecretName(path)`. The override payload binds the exact key path and passphrase inside the vault.

- [ ] Write failing tests for selected/unselected lookup, wrong-key binding, rejection cleanup and host deletion.
- [ ] Run targeted tests red.
- [ ] Add host-bound encoding and thread lookup through terminal, SFTP, host test and MCP agent paths.
- [ ] Rerun focused and existing vault/key tests.

### Task 2: Batch auth draft and validation

**Files:** `internal/workbench/host_bulk_password.go`, `internal/workbench/host_bulk_password_test.go`, `internal/workbench/host_management.go`, `internal/i18n/i18n.go`, `internal/i18n/english.go`, `internal/i18n/chinese.go`.

**Interfaces:** The draft holds explicit apply flags for username/auth/identity/password/OTP/passphrase/agent-forward. Captured host IDs are validated before mutation. `vault.Apply([]vault.Change)` commits all selected secrets in one batch.

- [ ] Write failing tests for opt-in semantics, per-protocol skips, duplicate destinations, invalid OTP/key/passphrase, cancel and locked vault.
- [ ] Run targeted tests red.
- [ ] Implement validation, metadata updates and one vault batch with eligible/skipped/error counts.
- [ ] Rerun targeted tests; inspect no secret text in metadata/result strings.

### Task 3: Batch editor UI and delivery

**Files:** `internal/workbench/host_bulk_password.go`, `internal/workbench/host_menu.go`, `internal/workbench/paint.go`, `internal/workbench/workbench.go`, `internal/workbench/chrome_test.go`, `website/content/docs/connections/hosts-and-authentication.mdx`, `website/content/docs/connections/hosts-and-authentication.zh.mdx`, `README.md`, `README.zh-CN.md`.

- [ ] Add tests for mode/key choice, masked inputs, opt-in toggles, keyboard focus and narrow layout.
- [ ] Run tests red; replace password-only dialog and update labels/docs.
- [ ] Run targeted tests green, `go test ./...`, focused `go test -race`, `go vet ./...`, `test -z "$(gofmt -l .)"` and native/headless build.
- [ ] Review final diff against the Spec, then push to #366 and watch CI before marking ready.
