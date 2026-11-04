# Suggestion Library and AI Generation Verification

Date: 2026-11-04

Issue: https://github.com/t8y2/motty/issues/455. The `/claim` bot confirmed assignment to `caichangqing1120` before implementation. This worktree began at upstream `main` commit `068dc8e` and uses an isolated sibling Viva checkout. The open PR #453 is a separate change.

## Evidence

- The default source is the local library; it rotates three cards at a time without repaint-triggered churn. Custom and built-in edits persist in the existing settings store, are separated by language and terminal availability, and can be disabled, reordered or restored.
- AI generation is opt-in and asynchronous. It reads only the selected terminal's non-concealed visible cells, at most 30 lines and 4 KiB, sanitizes controls, always attempts redaction, sends no chat history or tools, and falls back on malformed responses, missing models or cancellation. Unchanged reopened empty panels reuse their settled recommendations.
- Settings disclose automatic model transmission and show the current sanitized snapshot. This is a preview of the current selection, not a promise that terminal output cannot change before a later generation request.
- A bound command is shown before Send, and after an explicit Agent-mode Send passes through the existing locked-target risk gate. It runs once, leaves a tool row, and supplies a bounded, redacted result and attached images to the model. Ask mode, denial, manual entry, editing and undoing, changed target and mode cannot trigger the command.

## Validation

| Check | Result |
| --- | --- |
| `go test -tags vivahead ./... -count=1` | Passed, 92 test packages; `.superpowers/sdd/plan/exact-headless-all.log` |
| `go test ./... -count=1` | Passed, 92 test packages; `.superpowers/sdd/plan/exact-release-all.log` |
| `go test -tags vivahead,dev ./cmd/motty ./internal/workbench -count=1` | Passed; `.superpowers/sdd/plan/exact-headless-dev.log` |
| `go vet ./...` and `go vet -tags vivahead,dev ./internal/workbench` | Passed; `.superpowers/sdd/plan/exact-vet.log` |
| Release and `vivahead,dev` builds | Passed; `exact-motty-release` and `exact-motty-head` in ignored `.superpowers/sdd/plan/` |
| Focused agent/gate and workbench race tests | Passed; `.superpowers/sdd/plan/exact-race-*.log` |
| Formatting, diff whitespace checks and headless screenshots | Passed locally |

Headless snapshots: [Chinese panel](screenshots/zh-panel.png), [English settings](screenshots/settings.png), [bound command before Send](screenshots/bound-before-send.png). Panel layout was checked at 300px and display scales 1 and 2. All screenshots use synthetic test sessions.

The independent read-only review identified four defects; each had a focused failing test followed by a passing fix: concealed SGR-8 data in generated context, edit/undo and away/back target rearming, provider/model changes leaving stale recommendations, and images omitted from bound-command requests. A full-screen-with-trailing-blank-rows case was also fixed so a sanitized snapshot retains the visible upper rows. A disabled provider has its own stale-result regression test.

## Limits

The actual accuracy and billing of third-party model responses, CLI-provider retention and remote OS behavior require runtime acceptance. Redaction is best effort and is disclosed as such. The local test suite does not establish deployment or business acceptance. No history schema, provider wire protocol or remote execution API changed.
