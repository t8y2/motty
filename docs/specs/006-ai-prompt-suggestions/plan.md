# AI Prompt Suggestions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement task-by-task; keep this worktree and evidence. User explicitly requested implementation, issue submission, headless validation and the repository's issue/PR flow.

**Goal:** Replace fixed suggestions with a configurable rotating library or privacy-bounded AI generation, and support explicitly sent, gate-approved bound commands.

**Architecture:** Persist global library overrides and mode through the existing AI settings store. Keep the three visible suggestions and active binding in ephemeral panel state; generate asynchronously only on an empty-conversation lifecycle event. Execute a selected bound command through the existing target-locked gate before calling the model, exposing its output as bounded context and as a tool row.

**Tech Stack:** Go 1.26, Viva `flex`/`components` UI, `internal/ai` agent/gate/llm/store, headless UI tests.

**Spec:** `docs/specs/006-ai-prompt-suggestions/spec.md`

## Global Constraints

- Default is library rotation; AI generation is opt-in and shows the terminal-output transmission disclosure and preview.
- Automatic context must be redacted and limited to the selected terminal's last 30 visible lines and at most 4 KiB, with no history or tools.
- Clicking suggestions never sends or executes. Bound commands execute only after explicit Send in Agent mode through the existing risk gate and locked target.
- Chinese/English, no database schema migration, no unrelated PR #453 code.

## Review Focus

- Switching locale, target, mode, provider/model or conversation invalidates a stale generated suggestion or armed command.
- Retrying an Ask-mode run or manually typing a matching prompt never executes a bound command.
- An unready target, denied approval, cancellation or failed command cannot be mistaken for successful work.
- UTF-8 truncation, ANSI/control codes, concealed cells, long lines, secrets and adversarial terminal text cannot escape the bounded request or make it a tool instruction.
- The 300px empty panel and settings editor remain usable and non-overlapping at 1x and 2x scales.

---

### Task 1: Global library and rotation

**Files:** Create `internal/workbench/ai_suggestions.go`, `internal/workbench/ai_suggestions_test.go`; modify `internal/workbench/ai_state.go`, `internal/workbench/ai_panel.go`, `internal/workbench/ai_panel_paint.go`, `internal/i18n/{ai_panel,chinese,english}.go`.

**Interfaces:** Stable built-in IDs, localized defaults and user overrides; `aiSuggestion` carries ID, context, text and optional command. Rotation returns up to three enabled items, persists changes through `ai.store.Setting`, and changes only on lifecycle/refresh.

- [x] Write failing tests for defaults, rotation, custom/edited/disabled/reordered/restored entries and reload; observe missing behavior.
- [x] Implement the library, persistence and empty-state presentation with refresh and click-only arming.
- [x] Run focused workbench and i18n tests; verify legacy empty-state tests remain meaningful.

### Task 2: Private async AI suggestions

**Files:** Create `internal/workbench/ai_suggestions_generate.go`, `internal/workbench/ai_suggestions_generate_test.go`; modify `internal/workbench/{ai_state,ai_panel}.go`.

**Interfaces:** A bounded snapshot of `aiTargetSession` enters a tool-less request via the selected provider. A generation sequence and cancellable context guard UI updates. Library suggestions remain the fallback.

- [x] Write failing tests for 30-line/4-KiB sanitized/redacted input, no tools/history, stale results, invalid output and unavailable provider.
- [x] Implement one asynchronous request per empty-conversation entry, explicit refresh and fallback.
- [x] Run focused generation and agent tests, including race checks.

### Task 3: Settings and library editor

**Files:** Create `internal/workbench/ai_suggestions_settings.go`, `internal/workbench/ai_suggestions_settings_test.go`, `internal/workbench/ai_suggestions_headless_test.go`; modify `internal/workbench/{ai_settings,ai_settings_paint}.go`, `internal/i18n/{ai_settings,chinese,english}.go`.

**Interfaces:** The assistant behavior group exposes source selection, privacy disclosure, preview and a library manager. Built-in and custom rows support editing, disabling/removing, ordering and reset through existing settings/modal input patterns.

- [x] Write failing settings and headless tests for configuration, preview, editor actions and narrow layouts.
- [x] Implement localized controls and persistence with visible errors and keyboard access.
- [x] Run focused release/dev tests and inspect headless screenshots.

### Task 4: Gate-approved bound command

**Files:** Modify `internal/workbench/{ai_state,ai_panel,ai_panel_input}.go`; create `internal/workbench/ai_suggestions_command_test.go`; update `internal/workbench/ai_suggestions_settings.go`.

**Interfaces:** Send consumes a still-armed suggestion; pre-model command executes exactly once via the existing `gate.Gate.Run(terminal_exec, ...)`. Result joins UI and bounded model context; approval and target policy are unchanged.

- [x] Write failing tests for click versus send, irreversible editing/mode/target invalidation, threshold approvals, denial, cancellation, attached images and one-time result delivery.
- [x] Implement command preview and gated pre-model execution, without bypassing existing terminal/tool contracts.
- [x] Run focused and broad headless release/dev suites, vet, build and race tests; inspect screenshots and final diff.
