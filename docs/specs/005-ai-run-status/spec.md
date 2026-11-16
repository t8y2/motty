# AI request status, duration and token usage

Issue: https://github.com/t8y2/motty/issues/440 — claimed by caichangqing1120; bot confirmation received before continuing implementation.

Approved scope: the user accepted the waiting-feedback recommendations and requested elapsed time while running, total time after completion and consumed tokens on 2026-11-04.

- A sent question immediately shows an animated status beneath the conversation, including elapsed wall-clock time from request start.
- Actual reasoning, answer, tool and approval events update the status. Without such events, show waiting for the model rather than invented progress.
- Completion, cancellation and errors retain the latest request's final status and total wall-clock duration, including tool execution and approval waits.
- Token usage is visible alongside the final status only after the request ends. Show provider-reported input, output and their sum, cached input when reported, and the known conversation total. Never estimate usage or present missing usage as zero consumption.
- During a request, show only activity and elapsed time. Hide token counts, conversation totals and waiting-for-usage placeholders, while retaining reported usage internally for final accounting. This follows the user's 2026-11-04 refinement.
- Interrupted requests and successful requests containing both reported and unreported model turns retain known counts and explicitly warn that usage may be incomplete.
- Errors, cancellation and Ask-mode escalation preserve already reported usage. Count each request once in the conversation total.
- A new request replaces the previous request's status. A new or opened conversation clears ephemeral duration and usage statistics, matching the existing usage lifetime; this change does not introduce a history schema migration.
- The narrow assistant panel wraps status and usage without overlap or relying on hover. Preserve conversation scrolling, existing copy controls and tool approval behavior.
- Provide Chinese and English strings. Do not change provider wire protocols, authorization, tool contracts or execution policy.

Validation covers waiting before the first event, phase changes, elapsed/final duration, known versus unavailable usage, multi-turn accumulation, cancellation/error/escalation, reset and narrow-panel layout.
