# Session Password Import Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans task by task. Steps use checkbox syntax for tracking.

**Goal:** Import readable SSH passwords from Electerm and Netcatty with explicit consent, and fix MobaXterm Chinese text import.

**Architecture:** The parser returns ephemeral secret candidates alongside ordinary entries. A workbench preview separates host additions from vault mutations, resolves duplicates without overwrites, then commits eligible vault changes atomically before publishing new hosts. The MobaXterm reader handles its legacy encoding locally.

**Tech Stack:** Go 1.26, Viva workbench UI, `internal/vault`, `golang.org/x/text` (already required).

**Spec:** `docs/specs/003-session-password-import/spec.md`

## Global Constraints

- Work on the detached isolated worktree from `upstream/main`; do not push, commit, or touch the user's checkout.
- Never log, serialize in host metadata, or display the imported password. Keep key material and Telnet passwords out of scope.
- Existing vault values always win. An encrypted source is not guessed or decrypted in this plan.
- The #351 public attachment may contain credentials; only sanitized byte fixtures enter tests.

## Review Focus

- Existing host with a saved password: re-import must not overwrite it.
- One endpoint with conflicting import passwords: report conflict without saving either.
- Unavailable/locked vault: choosing password import must not add hosts without selected passwords.
- Key/agent-only and Telnet sessions: never store an unusable SSH password.
- Legacy encoded MobaXterm text alongside valid UTF-8/UTF-16: avoid cross-format regression.

---

### Task 1: Reproduce and repair MobaXterm decoding

**Files:** `internal/sessionimport/sessionimport.go`, `internal/sessionimport/sessionimport_test.go`.

**Interfaces:** Keep `ParseMobaXterm(path string) (Result, error)` unchanged. Introduce a source-specific byte decoder only if needed; do not change unrelated readers' contracts.

- [x] Add a synthetic GB18030 Chinese group/session fixture, plus UTF-8, UTF-16 and malformed input controls. The public report's exact code page remains unverified.
- [x] Run focused parser tests and confirm the regression fails with a garbled name/group and silent byte loss.
- [x] Implement bounded MobaXterm-specific decoding and an error for undecodable bytes.
- [x] Rerun the focused test and the full parser package.

### Task 2: Parse readable SSH password candidates

**Files:** `internal/sessionimport/sessionimport.go`, `internal/sessionimport/sessionimport_test.go`.

**Interfaces:** Add an ephemeral password field to `Entry`; only Electerm and Netcatty populate it for eligible SSH records. Preserve `PlainSecrets` accounting for plaintext key material.

- [x] Test distinct password values, empty values, non-SSH records, key auth, multiple files, invalid records and no secret in warnings.
- [x] Run focused parser tests to observe the missing behavior.
- [x] Populate only eligible password candidates from structured JSON/CSV and keep result strings free of values.
- [x] Rerun focused and parser-package tests.

### Task 3: Preview and apply an import safely

**Files:** `internal/workbench/session_import.go`, `internal/workbench/host_import.go`, `internal/workbench/workbench.go`, `internal/workbench/host_import_paint.go`, `internal/i18n/terminal_tools.go`, `internal/i18n/english.go`, `internal/i18n/chinese.go`, `internal/workbench/session_import_test.go`.

**Interfaces:** Build the password changes from stable `hostPasswordSecretName(Host)` and `vault.Change`. `vault.Apply` is the only batch secret writer. The preview must retain the planned host additions and changes until confirmation/cancel.

- [x] Add workbench tests for explicit consent, connection-only/cancel, safe duplicate backfill, existing secret, conflicting values, ineligible authentication, vault failure and no metadata leakage.
- [x] Run targeted tests and confirm the expected failures.
- [x] Add the preview dialog and counts; stage vault changes before `applyHostImportSummary`, with error leaving host state unchanged.
- [x] Run targeted workbench tests and the full Go suite.

### Task 4: Research and documentation

**Files:** `docs/specs/003-session-password-import/research.md`, `website/content/docs/connections/hosts-and-authentication.mdx`, `website/content/docs/connections/hosts-and-authentication.zh.mdx`, `README.md`, `README.zh-CN.md`.

- [x] Document sourced portability conditions and blockers for PuTTY, Xshell, MobaXterm and WindTerm; do not imply encrypted passwords are supported.
- [x] Update the import guide and README for consent, duplicates, plaintext export handling and #351 encoding scope.
- [x] Run `gofmt`, `go test ./...`, `go vet ./...`, focused race tests and a native/headless build; inspect test results and diff.

**External acceptance pending:** verify issue #351 against a sanitized copy of its public attachment. The attachment CDN was inaccessible here; the synthetic GB18030 case is not proof of the reporter's exact encoding.

**Review follow-up:** BOM-less encoding collisions pause before import and show UTF-8/GB18030 examples; malformed UTF-16 is rejected. A config-save error rolls back newly imported host state and restores prior encrypted vault records without a synced tombstone, with a visible recovery warning if restoration itself fails. Regression tests cover blocked and failed persistence.

**Residual:** A process crash or power loss between the vault's atomic file write and the config transaction can leave an encrypted orphan credential; a retry with the same source/endpoint recovers it. True crash-atomicity across the two stores would require a durable journal. Mixed-encoding multi-file imports are refused rather than applying one encoding to all; multi-window password races are prevented by an absent check inside the vault lock.
