# AI request status verification

Date: 2026-11-04

Issue: https://github.com/t8y2/motty/issues/440

The issue is assigned to `caichangqing1120`; the `/claim` bot confirmed ownership before implementation continued. Implementation and verification used an isolated worktree based on Motty `346b685` and the sibling Viva checkout at `76cd6f8`. The delivery branch is `codex/issue-440-ai-run-status`, targeting `t8y2/motty:main`.

## Result

The conversation shows activity immediately before any model output, elapsed time during the request and the final outcome with total request duration. Tool execution and gate-approval waiting are included. After the request ends, provider-reported input/output tokens, their sum, cached input and known conversation totals remain visible without hovering. While running, token details and waiting-for-usage placeholders stay hidden. Unavailable usage is not presented as zero; interrupted and mixed-reporting requests explicitly identify potentially incomplete counts.

## Checks

| Command | Result |
| --- | --- |
| `go test -tags vivahead ./... -count=1` | Passed: 92 packages with tests |
| `go test -tags vivahead,dev ./cmd/motty ./internal/workbench -count=1` | Both packages passed |
| `go vet -tags vivahead ./...` | Passed |
| `go vet -tags vivahead,dev ./internal/workbench` | Passed |
| `go build -tags vivahead -o <evidence>/motty-head ./cmd/motty` | Passed |
| `go build -tags vivahead,dev -o <evidence>/motty-head-dev ./cmd/motty` | Passed |
| `go test -tags vivahead -race ./internal/ai/agent -count=1` | Passed |
| `go test -tags vivahead -race ./internal/workbench -run 'Test(AIRun\|LastAnswerShowsCopy\|AnswerActionsFollow\|Usage)' -count=1` | Passed |
| Formatting and `git diff --check` | Passed |

Local logs are in `.superpowers/sdd/plan/verified-*.log`. Headless screenshots in `.superpowers/sdd/plan/screenshots/` cover Chinese and English waiting/completed states; layout checks cover a 300-pixel assistant panel at display scales 1 and 2. Screenshot token values are controlled test data.

Committed previews: [waiting](screenshots/waiting.png) and [completed](screenshots/completed.png).

After the user's completion-only token refinement, focused release/development workbench tests, i18n checks, vet and the release headless build passed again. Logs are in `.superpowers/sdd/plan/completion-only-tokens-*.log`. Updated headless screenshots confirm that the running state has no token line or empty placeholder and the completed state retains its final usage. The full-suite results above precede this presentation-only refinement.

## Regression coverage and review

- Silent providers show a waiting state and request another animation frame before the first output.
- Timing includes tool/approval waiting and freezes when the request ends.
- Completion, errors, cancellation and Ask escalation retain reported usage and account for each request once.
- New requests do not borrow the preceding request's usage; conversation changes reset ephemeral metrics.
- Cancelled tools no longer appear to be running; copy remains available on hover while usage remains visible at rest.
- An independent reviewer identified two P2 issues: stale approval text after removing escalation cards and incomplete usage presented as complete. Both were reproduced by failing tests and fixed. Coverage also includes a declined escalation whose retry cannot start and successful runs mixing reported/unreported usage.

## Limits

Token values are taken from provider reports; actual provider billing accuracy was not tested. Statistics retain the existing in-memory lifetime and are not reconstructed when saved history is reopened. No provider wire protocol, tool authorization or history schema changed.
