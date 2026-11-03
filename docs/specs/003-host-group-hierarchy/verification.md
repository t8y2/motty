# Host Group Hierarchy Verification

Date: 2026-11-03.
Issue: https://github.com/t8y2/motty/issues/350, open and assigned to `caichangqing1120` after confirmed `/claim`.

## Source and Environment

- Motty base: upstream `main`, `2684f26464a89cd8cba9ea53e7ac9376048d35f5`.
- Viva dependency: sibling `../viva`, `254118c9081d75e0f78e8d6f3d32b0c7ce3c5db8`.
- Go: `go1.26.3 darwin/amd64`.
- Verification was performed in a separate worktree. Submission branch: `codex/issue-350-host-groups`, targeting upstream `main`. The original checkout and installed Motty were not changed.
- Initial older Viva dependency could not compile current Motty (`app.OpenWith` missing). Updating only the isolated sibling dependency resolved that setup problem. The upstream-baseline suite then had only the deliberately introduced hierarchy test failures; the final suite is green.

## Final Checks

| Check | Result |
| --- | --- |
| `go test ./...` | PASS; pre-submission rerun: workbench 70.316s; no package failures |
| `go vet ./...` | PASS, no output |
| `go test -race -tags vivahead ./internal/workbench -run '^Test(NestedHostGroup\|HostGroup\|ExplicitHostGroup\|HostCatalog\|Persist\|Sync\|SessionLogLocate)' -count=1` | PASS, 22.048s |
| `go test -tags vivahead ./internal/workbench -run '^TestHostGroup(HierarchyNative\|LongPathsNative)' -count=1` | PASS, 5.805s |
| `git diff --check` | PASS |

The race check is focused on grouping, catalog, persistence, sync and Locate tests, not the entire repository race suite.

## Behavior Covered

- Ordered ancestors, subtree counts, segment boundaries, independent collapse, search reveal and parent filters.
- Rename conflicts and invalid names are rejected before mutation; case-only rename, descendant paths, active filters and expansion state are preserved.
- Right-click menus in list/card views offer prefilled New Host, child creation, rename, whole-group move, pinning, sibling ordering and keep-hosts deletion.
- Empty explicit groups remain usable and searchable after removing the last host, survive restart, and preserve ancestor metadata through catalog export/import.
- Moves reject self/descendant destinations and case-insensitive conflicts. Dragging supports cancellation and ignores clipped rows outside the list viewport.
- Deletion keeps host IDs/settings and moves all affected hosts to the chosen parent/unnamed destination. Cancellation changes nothing.
- Eight native interaction combinations cover list/card views, 1400x900/900x600, and dark/light themes. Forty screenshots were captured after animations settled using `MOTTY_HIERARCHY_SCREENSHOTS`; representative tree/menu/rename/move/delete screenshots were visually inspected.
- A 1.5x-scale native test checks long-path dialog overflow. Full path labels remain available to accessibility even when visible labels are elided.
- Editable Enter guards IME marked text; parent-selector Enter opens its choices. The IME test simulates marked text, not a physical input-method candidate session.

## Review

One independent read-only review identified five issues. Each was reproduced by a failing regression and fixed: Locate ancestor expansion, empty ancestor export metadata, parent-combobox Enter, offscreen drag targets, and composing Enter. A second narrow review found all five addressed and no remaining defect in those corrections.

No actual SSH session, physical IME candidate acceptance, live WebDAV synchronization, Windows/Linux runtime, or installed-app acceptance is claimed. Host records and existing persistence/sync tests were exercised locally.

Merge, deployment and installation are outside this change.
