# Configurable AI Assistant Suggestions

Issue: https://github.com/t8y2/motty/issues/455 (claimed). Approved in chat on 2026-11-04; this feature is separate from PR #453, which is not yet merged.

## Intent and modes

- The assistant's empty conversation displays at most three suggestions. Clicking one only fills the composer; never sends or executes automatically.
- The persisted recommendation source is either library rotation (default) or opt-in AI generation. A refresh control cycles library suggestions or explicitly regenerates AI suggestions; repainting or reopening an unchanged empty panel does not trigger repeated requests.
- The library is global, not per host, grouped by no selected/ready terminal versus selected/ready terminal. Its built-in Chinese and English entries can be edited, disabled, reordered and individually restored; custom entries can be added, edited, reordered and removed. An edit to one language does not overwrite the other language. User changes survive upgrades to built-in defaults. Empty/disabled libraries are represented honestly, without silently re-enabling entries.
- The library can bind one optional fixed shell command to a connected-terminal entry. No bundled entry has an executable command by default. The UI visibly shows the command before the user sends it.

## AI suggestions and privacy

- Selecting AI generation discloses that opening an empty conversation automatically sends the currently selected terminal's ready/disconnected status and recent visible output to the selected model. The user can preview the sanitized snapshot for the current selection; generation samples again at request time if the terminal changes. Redaction is mandatory for this automatic request even when normal AI conversation redaction is off. Warn that redaction is best-effort.
- Use only the selected terminal's visible, non-concealed cells, never other tabs, references, credentials or the full scrollback. Keep no raw snapshot on disk or in Motty conversation history. At most 30 rows/lines and 4 KiB of terminal text are transmitted, whichever is less; preserve UTF-8 and sanitize control characters. No terminal means status only.
- AI suggestion requests have no tools or conversation history and are limited in time and output. Treat terminal text as untrusted data; accept only three short, nonempty suggestions without commands from a structured response. A failure, invalid response, missing/unavailable model or stale completion falls back to the visible library without disrupting the conversation. Cancel or ignore old results when the context, settings, conversation or panel changes.

## Bound commands and safety

- A command is armed only by clicking its library suggestion, and only while its composer text stays exactly equal to that suggestion without any intervening edit. Manually typing identical text, editing and undoing it, switching targets away and back, changing the mode, or starting a new conversation cannot arm or retain the command. AI-generated suggestions never carry commands.
- On explicit Send in Agent mode with a ready selected terminal, run the exact bound command once against the locked selected target through the existing `terminal_exec` gate. Honor the configured low/medium/high auto-approval threshold and all existing risk blocks; Critical always requires typed confirmation, while High follows the chosen threshold. A suggestion is not a new permission. Do not execute in Ask mode or when no valid target is selected.
- Show the run/approval and output in the conversation. Give a bounded, redacted result and any user-attached images to the model for analysis; do not run the model first or let it substitute another command. On denial/cancellation, do not execute and do not invent an output. Do not carry a pending command into a retry or a different conversation.

## Scope and verification

Chinese and English controls and disclosure; existing settings persistence, mode selection and tool permissions. No new provider protocols, remote execution API, database schema migration or automatic command execution on card click. Verify rotation and library CRUD/reset/reload, async AI fallback/stale cancellation, privacy budget and preview, exact command binding and approval modes, and layout with headless tests/screenshots at 300px panel width and multiple display scales.
