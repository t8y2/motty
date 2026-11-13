# Rule Management

Version: 1.0, 2026-11-13

Source: https://github.com/t8y2/motty/issues/690, "自定义关键词高亮/自动填充规则优化".
Authorization: the user explicitly requested claiming, implementing, validating with Viva headless, preserving evidence and submitting a screenshot-backed PR. Existing UI patterns are required. Parent review precedes commits and remote writes.

## Scope

- Highlight imports keep the existing text format, parser validation, 1 MiB bound and rule cap. Skip exact duplicate full configurations (Pattern, Regex, CaseSensitive, Slot), both within a file and against existing rules, before capacity is consumed. Report imported/skipped counts; a repeat import must not claim capacity exhaustion when it only contains duplicates.
- Settings remain compact regardless of rule count: each existing rule type has a management button rather than every rule painted inline.
- Each management dialog provides a bounded scrollable list with a visible scrollbar, per-rule edit, multi-selection, select-all and confirmed batch deletion. Selection survives scrolling, canceled deletion and editing. Closing the manager clears selection.
- Highlight editing reuses the current editor, validates before saving and replaces the selected rule in place. Invalid or duplicate edits leave the current rule unchanged. Existing ordering, colors and persistence format remain unchanged.
- Autofill reuses its existing editor, enable/disable behavior and deletion primitives. Adding stays disabled by default; editing preserves ID and enabled state. Fixed text stays in the vault and is never exposed by the manager or persisted in configuration. Batch deletion warns about fixed-text removal and only removes confirmed selected rules and their own vault entries.
- Returning from add/edit or deletion returns to the manager. Cancel does not mutate persisted rules. Save/delete changes survive restore.
- English, Simplified Chinese and Traditional Chinese are supported using existing catalogs.

## Non-Goals

No autofill import, new file format, migration, rule reordering, matching-engine change, new credential access, automatic arming, redesign, Viva change or remote action before parent review.

## Acceptance

1. Repeating a mixed valid/invalid import adds each exact rule once; differing regex/case/color remain distinct and skipped counts include duplicates.
2. Import scans past duplicates to fill remaining capacity with unique rules; all-duplicate input warns without changing rules or saving.
3. Settings size is independent of rule count. Long lists scroll by wheel and scrollbar, and actions remain reachable at narrow viewport and scaled rendering.
4. A selected off-screen rule remains selected. Canceling batch deletion retains both rules and selection. Confirming removes only the snapshot selected, refreshes runtime behavior, saves, and restores correctly.
5. Highlight edit keeps index/color/flags, rejects invalid and duplicate input, and returns to the manager on save/cancel.
6. Autofill editing preserves identity and arming; disabled/first-send confirmation regressions pass; deletion cleans only selected fixed-text vault entries.
7. Actual Viva headless pointer/key/scroll routes validate list/edit/delete; screenshots contain only synthetic rules and no secrets.

## Environment

Motty base: `7482302027c110a818d541b6ceef4b0bea5bb4c7`; Go `1.26.11`; Viva CI pin `08ef0fa296f3305c87a7149c8f6b4296f566663e` through the existing `../viva` replacement. No dependency changes.
