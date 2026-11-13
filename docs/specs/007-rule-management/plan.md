# Rule Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Validate/deduplicate highlight imports and manage both existing rule lists in bounded dialogs.

**Architecture:** Preserve existing editors, runtime compilation, persistence and vault APIs. Add one workbench rule manager sharing list/selection behavior between highlighting and autofill; extend confirmed deletion with selected identity snapshots. Keep import filtering local to highlighting.

**Tech Stack:** Go 1.26.11, existing Viva UI/headless at CI pin.

**Spec:** `docs/specs/007-rule-management/spec.md` version 1.0, explicitly authorized by the user's implementation request.

## Global Constraints

- Keep persistence/API/matching contracts and credential safety unchanged.
- Existing UI patterns only; no Viva/dependency changes or autofill import.
- Work only in the assigned isolated worktree; parent review precedes commits and remote writes.

## Review Focus

- Import duplicates must not consume the last available capacity.
- Invalid edits must leave existing rules and runtime behavior intact.
- Selected off-screen rules must remain included in confirmed deletion.
- Autofill fixed text must not appear in list, screenshots or configuration.
- Dialog layout/actions must remain usable in narrow and scaled windows.

### Task 1: Validate Unique Highlight Rules

Files: `internal/workbench/highlight_ui.go`, `highlight_import_test.go`, `rule_management_test.go`.

Interfaces: full `highlightRule` value equality defines duplicate identity; existing `submitHighlightRule()` and `importHighlightRules()` retain signatures.

- [x] Add failing tests for repeated/in-file duplicates, distinct configurations, capacity scanning and invalid/duplicate edits.
- [x] Run focused tests and retain expected red output.
- [x] Filter validated imports with a seen-value map before applying cap; extend existing editor to replace a selected rule while validating duplicates.
- [x] Run focused tests; retain green output.

### Task 2: Manage Both Rule Lists

Files: new `internal/workbench/rule_management.go`, existing settings/editor/delete/modal/event dispatch files, `internal/i18n/{i18n,rule_management,english,chinese,traditional}.go`, `rule_management_test.go`.

Interfaces: `openRuleManager(kind ruleDeleteKind)`, `clickRuleManager(id gfx.ID)`, `declareRuleManager(c ui.C)`, `scrollRuleManager(event gfx.ScrollEvent) gfx.EventResult`; manager stores selection across viewport changes. Existing per-rule editor, enable and deletion methods remain the only mutation paths.

- [x] Add failing state tests for compact settings, selection, canceled/confirmed batch deletion, edit return, persistence and vault ownership.
- [x] Run tests and retain expected red output.
- [x] Add bounded scrolling UI, selection/check-all, edit/toggle controls and confirmed identity-snapshot batch deletion; return from child dialogs to manager.
- [x] Run focused tests and existing rule/autofill/modal regressions.

### Task 3: Verify Native Interaction And Evidence

Files: new `internal/workbench/rule_management_headless_test.go`, `docs/specs/007-rule-management/{verification.md,evidence/*.png}`.

Interfaces: `MOTTY_RULE_MANAGEMENT_SCREENSHOTS` is the optional headless screenshot output directory.

- [x] Add native pointer/key/wheel/scrollbar tests for highlight import/edit/delete and autofill safety at desktop/narrow/scaled viewports.
- [x] Run focused default/headless/dev/race tests and vet using `/Users/ccq/sdk/go/bin/go`; coordinate full suite with parent.
- [x] Render and inspect 1-3 real screenshots, save raw logs outside repo and concise verification inside package.
- [x] Review scoped diff, format and whitespace; hand exact evidence and residual limits to parent without committing.

Verification complete: focused default/headless, full release, tagged race, dev suite, release/dev vet and native arm64 build passed. The independent review's scrollbar-hover finding was fixed with retained red/green evidence. Delivery uses the authorized issue-specific commit/push/PR after review.

## Self-Review

Every Spec acceptance item maps to Tasks 1-3. Shared selection/deletion logic is scoped to the two existing rule types; no general settings framework is introduced. No unresolved product/security decisions remain in the approved scope.
