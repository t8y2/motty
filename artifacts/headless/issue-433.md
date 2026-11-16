# Host Group Toolbar Headless Evidence

Issue: https://github.com/t8y2/motty/issues/433
Captured: 2026-11-04.

All pictures render the real workbench through Viva `headless.Open` with the
`vivahead` build tag and `VIVA_HEADLESS=1`. No application window was opened.
Hosts and groups are test fixtures; installed application data and live
connections were not used. Each headless session is closed after its test.

| Screenshot | State |
| --- | --- |
| [433-expanded-1400x900.png](433-expanded-1400x900.png) | Expanded nested groups with global controls beside the view switch |
| [433-collapsed-1400x900.png](433-collapsed-1400x900.png) | All subtrees collapsed to root headers |
| [433-single-selected-1400x900.png](433-single-selected-1400x900.png) | Persistent filters above the separate batch row |
| [433-multiple-selected-960x600.png](433-multiple-selected-960x600.png) | Compact Chinese layout with two hosts checked |
| [433-compact-overflow-selected.png](433-compact-overflow-selected.png) | Compact English dark layout with forced filter overflow and two hosts checked |

Additional PNGs in this directory cover expanded, collapsed and single/multiple
selection states at 1400x900 and 960x600. Rendering scale is 1.25.

Regenerate from the repository root:

```sh
VIVA_HEADLESS=1 MOTTY_GROUP_TOOLBAR_SCREENSHOTS="$PWD/artifacts/headless" \
  go test -tags vivahead ./internal/workbench \
  -run '^TestHostGroupToolbarNative' -count=1
```

## Coverage

- Mixed expansion, expand all, collapse all, saved collapse state after restart,
  repeated commands, and existing search reveal behavior.
- Availability only in All with named groups; single-group flat views and
  unnamed-only catalogs do not offer tree controls.
- Native pointer delivery across list/card, dark/light and desktop/compact
  viewports, selection counts and single-host command enablement.
- A separate 16-group fixture forces More overflow. With hosts checked, native
  End/Enter selects the final overflow group and clears checks using the
  existing filter-change behavior.
- Native Shift+Tab navigation reaches both global controls, and Enter executes
  them. Clearing all checks removes the batch row and restores the list clip.
- Header controls are checked for overlap and viewport bounds. Existing batch
  menu anchoring and zero-allocation steady-paint tests are retained.

## Environment and Review

Motty base: `91fef607d7186c599f478592fa369950a42bad9c`.
Viva dependency: `76cd6f82c7c948fd4dbec4f7cc8ef7aee1a2d119`.
Go: `go1.26.3 darwin/amd64`.

The new behavior tests failed before implementation and pass after it. An
independent read-only review found no actionable behavior regression. Its
coverage suggestions led to the forced-overflow and keyboard test above.

The first concurrent full-suite run had two system-probe timing failures:
`TestShellSeesChineseFileNamesWhenNoLocaleIsInherited` and
`TestDarwinCommandsOnThisMac`. Both packages passed a subsequent unchanged,
serial run. No changes were made to those packages.

## Final Checks

| Command | Result |
| --- | --- |
| `VIVA_HEADLESS=1 go test -p 1 -tags vivahead ./... -count=1` | PASS; workbench 93.297s, both system-probe packages pass |
| `VIVA_HEADLESS=1 go test -tags vivahead,dev ./cmd/motty ./internal/workbench -count=1` | PASS; workbench 152.232s |
| `VIVA_HEADLESS=1 go test -tags vivahead,dev ./internal/workbench -run '^TestHostGroupToolbar' -count=1` | PASS; 2.972s, including the later forced-overflow/keyboard coverage |
| `VIVA_HEADLESS=1 go test -race -tags vivahead ./internal/workbench -run '^Test(HostGroupToolbar\|HostGroup\|NestedHostGroup\|HostManagement\|GroupChips\|HostCatalogTree\|WithoutNamedGroups)' -count=1` | PASS; 41.531s |
| `VIVA_HEADLESS=1 go test -race -tags vivahead ./internal/workbench -run '^TestHostGroupToolbarNativeOverflowAndKeyboard$' -count=1` | PASS; 5.356s |
| `go vet -tags vivahead,dev ./...` | PASS |
| `git diff --check` | PASS |

Race coverage is focused, not a full repository race run. The final complete
suite used `-p 1` to reduce interference with time-sensitive system probes.

No Windows/Linux runtime or installed-app acceptance is claimed locally. PR
#430 remains separate; this change does not include its chip context menus.
