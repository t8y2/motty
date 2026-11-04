# Host Group Toolbar Implementation Plan

> Execute inline with superpowers:executing-plans and test-driven-development.

**Goal:** Implement issue #433: expand/collapse all host groups and keep filters visible above a separate selection toolbar.
**Architecture:** Reuse existing group expansion state, hit routing, icon buttons, flex header and batch actions. Reserve an additional fixed-height row only while hosts are checked; preserve the existing filter-change selection reset.
**Tech Stack:** Go 1.26, Viva native UI, existing saved host-group collapse metadata.
**Spec:** https://github.com/t8y2/motty/issues/433 (read on 2026-11-04; user explicitly authorized implementation and PR submission).

## Global Constraints

- Only Viva headless execution (`vivahead`, `VIVA_HEADLESS=1`); do not open application windows.
- Add expand/collapse-all commands only in All with named groups. Preserve individual toggles, search reveal, host identities and connection settings.
- Keep group filters and view selection visible with zero, one or multiple checked hosts. Put batch commands and checked count on their own row beneath filters, hiding it when empty.
- Use icon buttons with tooltips for group controls, and existing condensed batch controls when labels do not fit. Preserve filter switching's existing behavior of clearing checks.
- Validate list/card, light/dark and 960x600 compact layouts; keep PNG evidence under `artifacts/headless/`.

## Review Focus

Nested/mixed expansion, search reveal over saved collapse state, toolbar size changes and scroll clips, compact action/control collisions, and action anchors after moving the batch row.

### Task 1: Group Expansion Controls

Files: `internal/workbench/host_group_toolbar.go`, `host_management.go`, `paint.go`, `internal/i18n/{i18n,english,chinese}.go`, toolbar tests.
Interfaces: existing `hostGroups`, `rebuildHostTreeRows()` and `save()`; add `setHostGroupsExpanded(expanded bool)` and stable expand/collapse hit IDs.

- [x] Add failing tests: all-expanded/all-collapsed/mixed nested groups converge; saved states survive restart; search still reveals matches; no controls for a flat filtered view or a catalog without named groups.
- [x] Observe failures, implement group-state commands and icon controls alongside the view switch, then run focused grouping tests.

### Task 2: Persistent Filter Row and Selection Toolbar

Files: `paint.go`, toolbar tests and native headless tests.
Interfaces: existing batch hit IDs, `hostSelectionText`, `fitHostChips`, `groupMenu.button`, `dashboard.addAnchor`, `chrome.hostList`.

- [x] Add failing tests proving chips stay visible, batch controls sit below them, count/enablement follows checks and clearing selection restores the list viewport.
- [x] Split the header rows, reserve the selection row only while checked, and retain compact icon controls with accessible labels.
- [x] Verify filter switching, batch move anchors, single/multiple-host enablement, selection cancellation, clipping and steady allocation behavior.

### Task 3: Headless Evidence and Submission

Files: `host_group_toolbar_headless_test.go`, `artifacts/headless/`.

- [x] Drive real pointer and keyboard events across both views, themes and desktop/compact sizes, including forced More overflow and group expansion controls; save and inspect settled PNGs.
- [x] Run full headless suite, dev suite, focused race and vet. Review against the issue and resolve concrete regressions.
- [x] Prepare branch `codex/issue-433-host-group-toolbar` and the PR description targeting upstream main and closing #433. Preserve the worktree and installed application; remote submission is tracked by the linked PR.

Self-review: the issue's expected behavior and validation map to Tasks 1-3. PR #430 remains independent; its top-chip context-menu change is not bundled here.

Evidence: `artifacts/headless/issue-433.md`. The independent review found no
actionable behavior defects; its coverage suggestions were implemented and
verified with forced overflow and real keyboard delivery.
