# Rule Management Verification

Date: 2026-11-13. Motty base `7482302027c110a818d541b6ceef4b0bea5bb4c7`, Viva `08ef0fa296f3305c87a7149c8f6b4296f566663e`, `/Users/ccq/sdk/go/bin/go` (Go 1.26.11).

## Verified Behavior

- Exact full-configuration duplicates are filtered within a highlight file and against the saved list before capacity is consumed. Distinct regex/case/color configurations remain distinct. Repeat imports do not grow the list or misreport duplicate-only input as capacity exhaustion.
- Both Settings lists are compact. Managers expose select-all/clear, stable off-screen selection, highlight/autofill edit, confirmed batch deletion, cancellation and return-to-list behavior.
- Highlight edits preserve ordering and validate before mutation; invalid/duplicate edits remain in the editor. Editing at capacity and persistence after edit/deletion pass.
- Autofill IDs/enabled state and first-send confirmation are unchanged. Fixed text never appears in the manager; selected deletion removes only that rule's vault entry. A failed vault write keeps the editor and original rule/vault text intact.
- Actual Viva headless pointer/key/wheel/scrollbar routes pass at 1400x900 with scale 1 and 900x600 with scale 1.5, including import/re-import, selection, keyboard edit, delete cancel/confirm and editor Escape.

## Commands And Evidence

External logs: `/Users/ccq/Documents/ChatGPT/motty-782-690-evidence-20261113/690/`.

| Check | Result | Log |
| --- | --- | --- |
| Existing focused baseline for imports/highlight/autofill | Passed before implementation | Terminal baseline run |
| New import dedup regression | Failed as expected, then passed | `import-red.log`, `import-green.log` |
| Manager API/state regression | Failed as expected before implementation, then passed | `manager-red.log`, `manager-green.log` |
| Related highlight/autofill/rule/modal/settings tests and i18n | Passed | `focused-regression.log` |
| Native import, edit, batch, vault and layout tests | Passed | `manager-native-all.log` |
| Scrollbar visibility/drag regression | Failed as expected, then passed | `headless-scrollbar.log`, `headless-green.log` |
| Review finding: unpressed scrollbar hover changed offset | Failed in both layouts, then passed after accepted-event restriction | `scrollbar-hover-red.log`, `scrollbar-hover-green.log` |
| Full release suite: `go test -p 2 ./... -count=1 -timeout 15m` | Passed; workbench 115.816s | `full-release.log` |
| Focused tagged race: workbench/highlight/autofill/i18n with `Highlight\|Autofill\|Rule\|Modal\|Settings` | Passed; workbench 78.766s | `race-focused.log` |
| `go vet ./...` and `go vet -tags dev ./internal/workbench` | Passed | `vet-release.log`, `vet-dev.log` |
| `make test-dev` | Passed | `dev-components.log` |
| Native `go build ./cmd/motty` | Passed; Mach-O arm64 | `build-release.log` |
| Parent rerun of tagged headless/state/import acceptance | Passed; 1.237s | `headless-parent-final.log` |

Focused regression command:

```sh
/Users/ccq/sdk/go/bin/go test ./internal/i18n ./internal/highlight ./internal/autofill ./internal/workbench -run 'Highlight|Autofill|Rule|Modal|Settings' -count=1 -timeout 5m
```

Reproduce headless acceptance and screenshots:

```sh
MOTTY_RULE_MANAGEMENT_SCREENSHOTS="$PWD/docs/specs/007-rule-management/evidence" \
  /Users/ccq/sdk/go/bin/go test -tags vivahead ./internal/workbench \
  -run '^TestRuleManager|^TestHighlightEditing|^TestImportHighlight|^TestImporting' -count=1 -timeout 5m
```

On macOS `vivahead` is required for actual screenshot rendering. Every headless view is closed by test cleanup, and `Rest()` is called before screenshot capture. The screenshots were visually inspected and contain synthetic rule metadata only.

## Screenshots

- [Highlight management](evidence/highlight-management.png): selected rule, color/flags, edit controls and visible draggable scrollbar.
- [Confirmed batch deletion](evidence/highlight-delete-confirmation.png): two selected rules with destructive confirmation.
- [Autofill management](evidence/autofill-management.png): sending kinds, enable switches and selection without fixed text.

## Review And Boundaries

Independent read-only review found a scrollbar-hover mutation (P2); a native red/green regression confirms it is resolved. The reviewer rechecked accepted primary-down/active-drag/primary-up routing and found no additional actionable core findings.

The initial scroll-area issue is local app integration: Motty embeds `windowPage().Draw` while routing pointer events centrally, so the page-owned hover scrollbar is not notified. This change uses the existing toolbar settings pattern with `components.ScrollArea` geometry and local pointer routing; Viva is unmodified.

Configuration saving retains the existing nontransactional `save()` behavior and status reporting. This work does not promise rollback for configuration-store failures or atomic multi-rule/vault transactions. No real SSH, credential-store interaction, production data, physical desktop, cross-platform runtime, installation, release or merge was exercised. Commit, push and PR creation use the separately authorized issue-specific branch after these gates.

## CI Follow-Up

The initial PR 803 Ubuntu job `43391780083 / 131776324780` tested merge
`820ef6eb74e8a2c65a8cd8c03fe9f84711353c6f` against upstream `a4bafd0a`.
Workbench passed (157.085s), but the newly inherited Mosh interoperability test
waited for a literal `%` prompt while the runner's bash prompt ended in `$`.

The branch now includes that exact upstream baseline and the same correction
verified by PR 802's Ubuntu unit/integration job: reuse the shell-independent
first-screen wait and seed the test display with its drained bytes. Typing,
single-execution and screen assertions are unchanged. Local macOS has no
`mosh-server`; that real-server test is skipped locally and is validated by the
Ubuntu CI job that installs the reference implementation. Follow-up evidence is
retained under the external evidence directory's `ci-repair/` folder.

After this correction, `go test -p 2 ./... -count=1 -timeout 15m` passed locally
(workbench 123.094s), the tagged headless rule/import acceptance passed (2.016s),
Mosh/terminal race tests passed, and focused vet/format/diff checks passed. The
local real-server skip remains explicit; the corrected PR must still complete
its own Ubuntu and other remote checks.
