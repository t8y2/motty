# Host Filter Context Menus

Issue: https://github.com/t8y2/motty/issues/428
Captured: 2026-11-04.

These PNGs render the real workbench through Viva `headless.Open`, built with
`vivahead` and run with `VIVA_HEADLESS=1`. No application window was opened.
The hosts are test fixtures; installed application data and live connections
were not used.

| File | State |
| --- | --- |
| [01-selected-group-menu.png](01-selected-group-menu.png) | Right-click the selected top group chip while its list header is hidden |
| [02-rename-from-selected-group.png](02-rename-from-selected-group.png) | Rename opened directly from the selected chip |
| [03-new-host-in-pointed-group.png](03-new-host-in-pointed-group.png) | Right-click another chip and create a host with that group prefilled, retaining the original active filter |
| [04-overflow-group-menu.png](04-overflow-group-menu.png) | Right-click a group inside More in a compact dark window; the context menu replaces the picker |

The light screenshots use a 1400x900 logical viewport and the compact dark
screenshot uses 960x600. Rendering scale is 1.25. Native interaction tests also
exercise both list/card views and dark/light appearances, Escape, outside
presses, All/More exclusions, and normal left-click filter selection.

Regenerate from the repository root:

```sh
VIVA_HEADLESS=1 MOTTY_FILTER_CONTEXT_SCREENSHOTS="$PWD/artifacts/headless" \
  go test -tags vivahead ./internal/workbench \
  -run '^TestHostFilterContextNative' -count=1
```

## Verification

Motty base: `91fef607d7186c599f478592fa369950a42bad9c`.
Viva sibling dependency: `76cd6f82c7c948fd4dbec4f7cc8ef7aee1a2d119`.
Local Go environment: `go1.26.3 darwin/amd64`.

The selected/unselected chip and overflow regressions failed before the fix,
including native pointer delivery, and pass after sharing the existing group
row handler with filter chips and overflow entries. All four PNGs were visually
inspected after animations settled. Each headless session is closed by test
cleanup.

| Command | Result |
| --- | --- |
| `VIVA_HEADLESS=1 go test -tags vivahead ./... -count=1` | PASS; workbench 138.202s |
| `VIVA_HEADLESS=1 go test -tags vivahead,dev ./cmd/motty ./internal/workbench -count=1` | PASS; workbench 137.447s |
| `VIVA_HEADLESS=1 go test -race -tags vivahead ./internal/workbench -run '^Test(HostFilter\|HostGroup\|NestedHostGroup\|HostMenu\|HostCatalogTree)' -count=1` | PASS; 33.244s |
| `go vet -tags vivahead,dev ./...` | PASS |
| `git diff --check` | PASS |

The race run is focused. Windows/Linux runtime behavior and installed-app
acceptance were not tested locally. No merge, installation or release was
performed.
