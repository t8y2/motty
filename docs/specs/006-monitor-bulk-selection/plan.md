# Monitor Bulk Selection Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans task by task. Follow TDD and preserve the evidence below.

**Goal:** Complete issue #782 using existing monitor selection and ordering paths.

**Architecture:** Compute bulk scope from dashboardPickerGroups, preserving selected
IDs outside the scope. Reorder only dashboard.editor.selected using existing host
comparators; submitDashboard already persists that sequence. Reuse Viva Checkbox,
Select and ReorderList and validate through headless input dispatch.

**Tech Stack:** Go 1.26.11 arm64, Motty base 74823020, Viva CI pin
08ef0fa296f3305c87a7149c8f6b4296f566663e.

**Spec:** docs/specs/006-monitor-bulk-selection/spec.md

## Global Constraints

- Only #782 changes; #690 uses a separate worktree and PR.
- No schema/epoch changes, global host sorting or automatic collection.
- Evidence: /Users/ccq/Documents/ChatGPT/motty-782-690-evidence-20261113/782.
- Screenshots committed under this package's evidence directory are synthetic fixtures.
- User already authorized implementation and GitHub submission; retain worktree.

## Review Focus

- Bulk selection below viewport and selected items outside active filters.
- Empty scope, unavailable IDs and unsupported protocols.
- Numeric IPv4/IPv6 ordering and language-aware deterministic name ties.
- Draft/cancel/save/reload boundaries without opening remote connections.
- Menu keyboard dismissal and no overlapping controls at narrow widths/scales.

### Task 1: Bulk Selection And Draft Ordering

**Files:** internal/workbench/monitor_dashboard.go,
monitor_dashboard_picker.go, monitor_dashboard_management.go,
monitor_dashboard_input.go, monitor_dashboard_paint.go;
internal/i18n/dashboard.go; internal/workbench/monitor_dashboard_bulk_test.go.

**Interfaces:** State.dashboardPickerGroups supplies matching eligible hosts.
State.toggleDashboardMatches() updates only matching selected IDs.
State.sortDashboardHosts(mode hostOrderMode) sorts the draft; existing
State.submitDashboard() persists it and moveDashboardHost resets manual mode.

- [x] Write TestDashboardBulkSelectionRespectsFilters and empty/ineligible cases;
  expect every filtered ID once, preserved outside IDs and reversible scope toggle.
- [x] Write TestDashboardOrderModes with literal expected name/IP/manual/missing
  ID sequences and canceled/saved/reloaded draft assertions.
- [x] Run focused tests and retain the expected missing-behavior failures.
- [x] Implement scoped selection, tri-state header checkbox, arrangement select,
  keyboard/scroll routing and localized labels with existing components.
- [x] Run Dashboard and i18n tests; retain green results.

### Task 2: Headless Acceptance And Delivery

**Files:** internal/workbench/monitor_dashboard_bulk_headless_test.go;
this package's verification.md and evidence/*.png.

**Interfaces:** headless.Open with State.Paint, Pointer, Key, Shortcut, Scroll,
Resize and TextInput. Capture via MOTTY_DASHBOARD_BULK_SCREENSHOTS.

- [x] Drive checkbox/filter/select/menu/key/manual-reorder/cancel/save with real
  headless pointer and keyboard events, including a scrolling 96-host fixture.
- [x] Cover English/Chinese and desktop/narrow windows; capture and visually inspect
  screenshots. Assert control hit bounds, tri-state accessibility and no remote dials.
- [x] Run focused race, full go test ./..., release/dev vet, format, diff and native
  build; preserve complete logs and versions. Fix only attributable regressions.
- [x] Independently review the full diff, address findings with red/green tests,
  and record verification.

Delivery uses the user's existing authorization for an English commit and an
independent PR fixing #782, with the screenshots linked from its description.

## Self-Review

Both issue requests map to Task 1 and real input acceptance in Task 2. Existing
manual drag and panel persistence remain authoritative. No shared interface with
the parallel #690 implementation is required.
