# AI Request Status Implementation Plan

> Execute inline with `superpowers:executing-plans` and test-driven development. Preserve the worktree and verification evidence after pull request submission.

**Goal:** Show truthful request activity and elapsed duration while running, then final duration and provider-reported tokens after each request ends.

**Architecture:** Keep request timing, phase and usage in window-owned run state. Render a wrapping status footer inside the conversation scroller, retaining the latest completed request until the next one. Reuse cumulative agent usage on existing tool events for final accounting without changing provider protocols. Token details and waiting-for-usage placeholders stay hidden until the request ends.

**Tech Stack:** Go 1.26, Viva UI/flex, existing headless workbench tests.

**Spec:** `docs/specs/005-ai-run-status/spec.md`

## Global Constraints

- Preserve tool authorization, provider protocols and history schema.
- Never estimate token usage; missing usage is unknown, and token details appear only after the request ends.
- Include tool execution and approval waits in request duration; stop counting when the request ends.
- Deliver through `codex/issue-440-ai-run-status` into `t8y2/motty:main`; preserve the worktree after submission.

## Review Focus

- A provider that emits no initial text still shows activity and schedules repainting.
- A stopped or failed tool round retains usage from previous completed model turns, exactly once.
- A new request never displays its predecessor's usage as its own.
- Opening history or creating a conversation clears ephemeral request statistics.
- Footer wrapping and scrolling remain valid at a 300-pixel panel width and when messages are outside the viewport.

### Task 1: Track request lifecycle and reported usage

**Files:** `internal/workbench/ai_state.go`, `internal/workbench/ai_run_status.go`, `internal/workbench/ai_run_status_test.go`, `internal/ai/agent/agent.go`, `internal/ai/agent/agent_test.go`.

**Interfaces:** Existing `aiEvent`, `aiFinished`, `aiStop` and conversation resets own run timing and final accounting. Existing agent tool events carry cumulative `Usage`. New status helpers consume the current/last run and return localized status and usage text.

- [x] Add tests for waiting, phase changes, duration freeze, cancellation/error/escalation usage and resets; run them to observe the missing behavior.
- [x] Implement run metrics and lifecycle accounting. Forward cumulative usage with tool events and usage-completeness metadata; preserve existing event ordering and tool behavior.
- [x] Run focused workbench and agent tests; existing usage accumulation tests must remain green.

### Task 2: Paint a persistent status footer

**Files:** `internal/workbench/ai_panel_paint.go`, `internal/i18n/ai_panel.go`, `internal/i18n/chinese.go`, `internal/i18n/english.go`, `internal/workbench/ai_run_status_test.go`.

**Interfaces:** The footer consumes Task 1's status/usage helpers, participates in scroller content height and schedules animation frames while running. Existing answer actions retain copy behavior without duplicating hover-only usage.

- [x] Add rendering tests for pre-response activity, tokens without hover, completed/cancelled/failed states and narrow widths; observe failure.
- [x] Add wrapping footer layout, running spinner and localized labels.
- [x] Run focused tests, release/development suites, vet and builds appropriate to the change. Inspect a headless screenshot if supported.
- [x] Review the full diff and record actual verification evidence in `verification.md`.
