# Bulk Host Password Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans task by task. Steps use checkbox syntax for tracking.

**Goal:** Set one password for selected eligible hosts with one atomic vault mutation.

**Architecture:** Reuse host checkbox selection and host secret naming. Capture IDs on opening the dialog, show eligible/skipped counts, and apply one list of `vault.Change` on confirmation. No host or config migration is needed.

**Tech Stack:** Go 1.26, Viva workbench UI, `internal/vault`.

**Spec:** `docs/specs/004-bulk-host-password/spec.md`

## Global Constraints

- Work on the detached isolated worktree; no automatic commit or push.
- Never show or log existing/new secret values; no Telnet/serial or key/agent-only SSH mutations.
- Preserve host metadata and authentication policy; all eligible secrets change together or not at all.

## Review Focus

- Stale/deleted selected host IDs: do not mutate unrelated secrets.
- Mixed SSH/key/desktop selection: report eligible/skipped counts correctly.
- Empty password, cancel and no eligible hosts: no vault writes.
- Vault error after confirmation: no partial replacement.
- Narrow layout and right-click selection: action remains discoverable and correctly scoped.

---

### Task 1: Add batch command and dialog

**Files:** `internal/workbench/host_management.go`, `internal/workbench/host_menu.go`, `internal/workbench/paint.go`, `internal/workbench/workbench.go`, `internal/i18n/english.go`, `internal/i18n/chinese.go`, `internal/workbench/host_management_test.go`.

**Interfaces:** Reuse `captureHostActionIDs` and `hostPasswordSecretName`. One `vault.Apply([]vault.Change)` commits the replacement.

- [x] Add tests for mixed selection, capture semantics, counts, cancel, empty password, unavailable vault and restored secrets after restart.
- [x] Run focused tests and confirm expected failures.
- [x] Add one masked dialog and a selected-host toolbar/context command with eligibility checks; commit a single vault batch and clear the editor on exit.
- [x] Run focused tests and the workbench package; inspect narrow-layout hit bounds and input focus.

### Task 2: Verify and document

**Files:** `website/content/docs/connections/hosts-and-authentication.mdx`, `website/content/docs/connections/hosts-and-authentication.zh.mdx`.

- [x] Document eligibility, skipped hosts and the meaning of replacing a saved password (no remote password change).
- [x] Run `gofmt`, `go test ./...`, focused race tests and native/headless build; inspect diff and test output before completion claims.
