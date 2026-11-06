# Host selection and display ordering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement task by task. The user authorized implementation and PR submission; continue without another execution-choice prompt.

**Goal:** Select and export hosts in bulk, with deterministic Name/IP display order synchronized across devices.

**Architecture:** Keep the host catalog and manual order intact. Sort the visible index projection, aggregate selection outside painting, and share one optional scalar preference through existing configuration records. Reuse native Viva checkboxes, menus and headless driver.

**Tech Stack:** Go 1.26, Viva sibling checkout, existing SQLite configuration store and fake WebDAV tests; no new dependency.

**Spec:** [spec.md](./spec.md), v1.0.

## Global Constraints

- Preserve host catalog schema 4, config compatibility epoch 23, stable catalog IDs and existing connection/export fields.
- Manual remains default. Search/filter scope includes collapsed/offscreen matching hosts; unrelated selections survive bulk toggles.
- IP ordering uses numeric IPv4 then IPv6, domains/non-IP targets last; no DNS requests.
- Sorting and selection aggregation happen outside Paint; keep warm-paint allocation checks.
- All UI validation uses native headless mode. Preserve source checkout, dependency checkout and worktree evidence.

## Review Focus

1. Partial subtree selection and similarly prefixed sibling paths must not affect unrelated hosts: Task 1.
2. Search, group filters, collapsed groups, empty groups and stale selections must yield accurate three-state controls: Task 1.
3. Duplicate names, IPv4 lexical traps, IPv6 and domain fallback must remain deterministic after reversed input: Task 2.
4. Automatic ordering must never overwrite manual order or allow a hidden drag to reorder hosts: Task 2.
5. Sync/restart must preserve display preference, manual order and per-device fold state: Task 3.

### Task 1: Scoped bulk host selection

**Files:** Create `internal/workbench/host_selection.go`, `host_selection_test.go`; modify `host_management.go`, `host_catalog.go`, `host_grid.go`, `paint.go`, `workbench.go`; add localized strings in `internal/i18n/host_catalog.go`.

**Interfaces:** `hostSelectionCount{total, selected int}`; `(*State).toggleHostScope(group string, grouped bool)`; `(*State).refreshHostSelection()` prepares the toolbar and group counts and labels; `(*State).paintHostScopeCheckbox(...)` paints prepared state. IDs live in a dedicated hosts selection feature.

- [x] Write tests through Click/Key for select-all, partial-to-all, scoped deselection, search/filter, nested/empty/unnamed groups, preserved unrelated selections and exported catalog contents.
- [x] Run `go test ./internal/workbench -run '^TestHostBulk' -count=1`; observe missing behavior, then implement aggregation/toggling and UI controls.
- [x] Re-run bulk tests plus host/group/chrome regressions; expected PASS including warm-paint allocation tests.

### Task 2: Deterministic display orders

**Files:** Create `internal/workbench/host_order.go`, `host_order_test.go`; modify `host_catalog.go`, `workbench.go`, `host_sort.go`, `host_group_actions.go`, `host_group_organization.go`, `host_grid.go`, `paint.go`, `persist.go`, `persist_records.go` and localized strings.

**Interfaces:** `hostOrderMode` with Manual/Name/IP; `hostOrderFromString(string) hostOrderMode`; `(hostOrderMode).saved() string`; `(*State).setHostOrder(hostOrderMode)`; `(*State).sortVisibleHosts()` sorts visible indices, `(*State).clickHostOrder(ui.ID) bool` owns the menu.

- [x] Write literal expected-order tests for nested names, reversed catalogs, IPv4/IPv6/domains, restart, selection identity and returning to Manual.
- [x] Run `go test ./internal/workbench -run '^TestHostDisplayOrder' -count=1`; observe RED, then implement index sorting, shared preference and menu.
- [x] Verify manual reorder entry/gesture guards and group up/down disablement in automatic order; run display/manual/group regression tests, expected PASS.

### Task 3: WebDAV and native headless verification

**Files:** Create `internal/workbench/host_catalog_controls_headless_test.go`, extend selection/order tests with fake WebDAV; add `verification.md` and representative screenshots in this work package.

**Interfaces:** Consume Task 1 selection IDs and Task 2 order-menu IDs through the native driver; use existing `newFakeDAV`, `configureSync`, `syncAndWait` fixtures.

- [x] Add WebDAV tests for order preference and host/group manual ordering convergence, device fold state, and stable row identities; run focused tests and verify actual stored/exported results.
- [x] Add native headless driver coverage for select-all/group mixed state, keyboard, ordering menu, export and narrow layout in both views/locales/themes. Capture and inspect representative PNGs.
- [x] Run formatting, vet, full tests, dev tests, headless build and race gate following the repository's Makefile; record initial failures and justified targeted retries.
- [x] Request one fresh-context whole-change review; fix material findings with regression tests. Verify the final diff, upstream target and authorized PR content.

**Delivery handoff:** Commit on `codex/issue-507-host-catalog-controls`, push to `caichangqing1120/motty`, create the authorized upstream PR against `t8y2/motty:main` referencing #507, and inspect its checks. GitHub records the resulting commit and PR identity; no merge or release is authorized.

## Self-review

All acceptance items map to Tasks 1–3. Shared touchpoints are `State`, `refreshHostSelection`, `syncVisible`, tree rebuild and toolbar layout; implementation is sequential. Task 3 reuses public event routes and real storage/export behavior, with fake WebDAV only at the network boundary. No requirement or execution approval remains pending.
