# Host Group Hierarchy

Source: user request on 2026-11-03 and upstream issue #350.
Implementation authorized by the user's request to claim and develop unowned work.

- Retain the existing slash-separated `Host.Group` and catalog schema version 4.
- Render ancestor groups and descendants in tree order in list and card views.
- Collapse hides the entire subtree. Search reveals matching ancestors without changing saved expansion state. Parent filters include descendants, with segment boundaries (`Prod` does not include `Production`).
- Offer an accessible group actions button and context menu for creating a child and renaming a named group. The unnamed bucket cannot be renamed.
- Right-click a group to rename it or create a host preassigned to that group, as requested in the user's follow-up screenshot. The new-host form retains its normal validation and cancellation behavior.
- Rename edits one path segment, updates every descendant host/group and the active filter, preserves host IDs/settings and expansion state, and rejects invalid/blank names and case-insensitive sibling collisions before mutation.
- Explicitly created empty groups survive refresh/restart and full catalog export/import. Implicit groups with no remaining hosts disappear as before.
- Existing flat groups, host move/select/create controls and imports remain compatible. Existing valid slash paths become a hierarchy. No connection behavior or quick-command grouping changes.
- Follow-up authorization: implement the four suggested improvements together. Show leaf labels with full-path tooltips and full paths in pickers. Move a whole subtree by selecting a destination parent or dragging onto a group; reject self/descendant destinations and conflicting siblings without mutation.
- Deleting a group never deletes hosts. Confirm one of two explicit destinations for the subtree's hosts: the deleted group's parent, or the unnamed bucket. Remove that subtree's group records, keep host IDs/settings, and clear an affected active filter. This avoids silently merging or flattening child group structures into existing siblings.
- Pin/unpin and move up/down apply among siblings in the same pin partition, preserving tree contiguity. Persist pin/order metadata and include it in catalog export/import. Alphabetical ordering remains the fallback for unordered siblings.
- Validate with Go tests, race/vet checks appropriate to the changed package, and native headless UI interaction at desktop and compact sizes in light/dark appearances. Preserve all unrelated worktrees and the installed app.
