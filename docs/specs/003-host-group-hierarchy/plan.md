# Host Group Hierarchy Implementation Plan

> Execute inline using superpowers:executing-plans and test-driven-development.

**Goal:** Nested host groups with safe subtree renaming and child creation (#350).
**Architecture:** Extend the existing path-based catalog and native Viva tree/menu/dialog controls. Keep hosts and catalog IDs stable; persist explicit empty groups using an optional field.
**Tech Stack:** Go 1.26, Viva, existing saved-state and catalog JSON/SQLite records.
**Spec:** `docs/specs/003-host-group-hierarchy/spec.md`

## Global Constraints

Keep `Host.Group` and schema version 4. No credentials, connection contracts or installed-app changes. Work from upstream `main` in an isolated worktree; remote submission requires authorization.

## Review Focus

Segment prefix collisions; collapsed ancestors during search/filter; case-only renames; empty explicit groups across persistence/export; deep paths and compact viewport clipping.

### Task 1: Hierarchy and Filters

Files: `host_catalog.go`, `host_group_hierarchy_test.go`.
Interfaces: `hostGroupWithin(path, parent string) bool`, group depth/leaf label, existing refresh/filter/tree rebuild methods.

- [x] Add failing tests for ancestor synthesis, subtree counts/order, independent collapse, search reveal, and segment-safe parent filters.
- [x] Run the tests and inspect expected failures.
- [x] Implement ordered ancestor groups, depth-aware rows and parent filter semantics.
- [x] Run focused tests plus existing host catalog/group tests.

### Task 2: Group Operations and Persistence

Files: `host_group_actions.go`, `host_management.go`, `workbench.go`, `paint.go`, `host_grid.go`, `persist.go`, `host_catalog_transfer.go`, `internal/i18n/*.go`, hierarchy tests.
Interfaces: Task 1's path helpers; `renameHostGroup(path, name string) bool`, `createHostChildGroup(parent, name string) bool`; existing move dialog reused with an explicit operation mode.

- [x] Add failing tests for rename atomicity/boundaries, conflicts, case-only names, filter remapping, empty child persistence and catalog round trip.
- [x] Observe failures, implement segment validation, group action menu/dialog, persistence and list/card depth rendering.
- [x] Verify stable host IDs, stale action safety, cancellation, keyboard submission, existing host move behavior, and right-click New Host prefilled with the clicked group (user follow-up).

### Task 3: Native UI and Final Verification

Files: `host_group_hierarchy_headless_test.go`, `verification.md`.
Interfaces: Task 2's group actions hits/menu and native paint/event flow.

- [x] Add native pointer/keyboard regression coverage for both host views and compact/wide sizes, and collect screenshots.
- [x] Run focused native tests, full Go suite, workbench race and vet; record any baseline failures precisely.
- [x] Review diff for persistence/import/sync compatibility, clipping and stable painting. Record results and preserve worktree for review.

### Task 4: Authorized Group Management Enhancements

User approved all four suggestions during implementation. Execute before final verification in Task 3.
Files: group action/model/persistence files from Task 2, `host_group_organization_test.go`, native tests.
Interfaces: Task 2's atomic path remapper; add move, keep-hosts delete, pin/order commands and group drag state.

- [x] Add failing regression tests for subtree moves, invalid drop destinations, confirmed delete destinations, sibling pin/order and persistence round trips.
- [x] Implement group parent selection, delete confirmation with destination choices, full-path hints, sibling ordering and drag feedback.
- [x] Verify cancellation, no connection/credential changes, stale targets, conflicts and native pointer/key behavior.

Completion evidence: `verification.md`. Submit branch `codex/issue-350-host-groups` to upstream `main`, linked to #350. Preserve the isolated worktree; merging, installation and release are outside this change.

Self-review: all spec requirements map to the tasks above; no new connection or public API contract is introduced.
