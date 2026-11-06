# Host selection and ordering verification

Implementation for [Issue #507](https://github.com/t8y2/motty/issues/507), using the approved proposal plus IP sorting. Source baseline `b96f5143a1748a4dc433c1173ebfe98ff3f1f8d3`; Viva `6d0800f31165d92139f935320362878bdea889f1`. Validation host: macOS, Go 1.26.3. No real host connections or installed application changes.

## Implemented behavior

- Three-state select-all and group subtree selection in both list/card views, with search/group scopes, keyboard activation and existing subset export. Collapsed/offscreen matching hosts belong to the scope; other selections are preserved.
- Manual, Name and numeric IP ordering, stable identity tie breakers, IPv4-mapped address normalization and domain fallback without DNS. Nested group structure and stored manual ordering survive automatic modes.
- Optional shared `host_order` preference uses the existing config records and compatibility epoch. WebDAV tests cover IP → Name → Manual, manual host/group order, local fold state and restart.
- Reused Viva native components and all three locales. Narrow host panels place the new controls on a separate row to avoid overlaps with filter and expansion controls.

## Evidence

| Verification | Result |
| --- | --- |
| Selection and ordering TDD | Observed missing controls/ordering fail before implementation; focused regressions pass. |
| Native headless driver | PASS: 24 combinations of 1400×900/960×600, list/card, English/simplified/traditional Chinese, light/dark; checkbox mixed state, Space, ordering and real JSON exports. |
| Minimum host panel | PASS: 1262×900 with the AI panel maximized, approximately 345px host panel; controls fit, do not overlap and respond to clicks. |
| Fake WebDAV | PASS: two isolated config databases converge on ordering preference and manual order; folds stay local; restart preserves the result. |
| Full suite | PASS: `go test -p 1 ./... -count=1 -timeout=300s`; 93 tested packages, workbench 158.639s. |
| Release/dev vet, headless dev build, shell checks | PASS, including final release/dev vet and `go build -tags vivahead,dev ./cmd/motty` after the race gate. |
| Development tests | PASS: `go test -tags dev -p 1 ./cmd/motty ./internal/workbench -count=1 -timeout=300s`; workbench 177.327s. |
| Full race suite | PASS: `go test -race -p 1 ./... -count=1 -timeout=1200s`; 93 tested packages, workbench 847.367s, no data race report. |
| Fresh independent review | 0 Critical, Important or Minor findings; coordinator completed screenshot inspection and owns the final test gates. |

Full session logs and RED/GREEN evidence are kept in the ignored `.superpowers/sdd/plan/` workspace. An independent Go build cache avoids interference from cleanup of the shared machine cache.

`make check` initially stops on pre-existing `terminal.go` indentation in baseline `b96f514`; the same source from upstream `0c79086` is formatted. This patch does not modify that unrelated file. Its remaining checks are executed individually. The initial older-baseline full run had a local monitoring sample failure, which passed targeted retry; the feature baseline's full suite passes.

## Screenshots and limits

- [Partial selection, light list](screenshots/selection-light.png)
- [Order menu, dark cards](screenshots/order-menu-dark.png)
- [Filtered name order and subset export](screenshots/name-filtered.png)
- [Minimum host panel beside AI](screenshots/compact-controls.png)

Screenshots are real Viva headless renders and have been visually inspected. Windows/Linux physical UI and an external live WebDAV provider have not been exercised locally; the tests use the real configuration/backup/export implementations with a local fake DAV server.
