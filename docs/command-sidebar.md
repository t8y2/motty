# Command sidebar and drafts

The terminal workspace shows a command library beside the terminal. It uses the
same snippet records as the Snippets management page and command palette.

- Click a library command to send it to the active terminal. Its existing
  **Run** setting still decides whether Motty presses Enter. The library stays
  open and the click preserves keyboard focus.
- Collapse the library with its header chevron; the narrow rail reopens it.
  The choice is saved on this device. When other panels leave too little room,
  the library temporarily becomes a rail or hides to preserve terminal space.
- The order button cycles through creation, name and group order. Use **…** to
  manage commands and their manual order on the Snippets page.
- A star pins a reference to that command above the draft. Editing or deleting
  the original updates both places. Pinned commands follow the manual order of
  the Snippets page, independently of the library's presentation order.
- Click a pinned command to append it to the draft and place the caret at its
  end. Its play button sends it directly using the existing snippet channel.
  Dynamic variables are resolved before inserting or executing the command.

Select terminal text and choose **Save as snippet…** from its context menu to
create a command. The first line supplies a suggested name. Review the command,
name, group and optional pin before saving. Captured text defaults to inserting
without pressing Enter. Saving highlights the command and displays a notice.
Empty selections are ignored; selections above the existing 4096-byte snippet
limit are refused without silently truncating executable text.

The draft supports multiple lines, native text selection, IME input and undo.
Enter adds a newline; **Ctrl+Enter** or **Send** sends the draft to the active
terminal and presses Enter. Sending uses the existing bracketed-paste and
broadcast channel. A send failure keeps the draft. Successful sends keep it by
default; **Clear after send** changes that behavior. Committed native edits are
saved automatically on this device; IME preedit is excluded from saved drafts.
The draft and pins sections can each be collapsed.

Draft sends use Motty's shell risk classifier. A high or critical assessment
displays a warning and a separate **Confirm and send** action. Confirmation is
bound to the exact command and active terminal, and is consumed after sending.
Keyboard repeat and input-method composition cannot submit a draft.

This change implements the three main stages of issue #663. It does not add a
separate script-file execution system: multi-line commands use the existing
snippet model and terminal channel. The accompanying Viva change makes
`ui.Box.AutoFocus` track its declarations across paints. It prevents terminal
autofocus from reclaiming the keyboard after the draft explicitly receives it.
This belongs in Viva because the same focus rule applies to every custom control
beside a sibling editor. Both repositories must be built with their paired
changes; a future upstream PR should link the two changes as required by #11.

## Validation

The focused suite exercises actual state/store and fake-shell paths, plus a
native text driver for focus retention, pin insertion, undo and responsive
layout:

```sh
go test ./internal/workbench -run 'TestCommandSidebar|TestTerminalSelectionCreates|TestCommandComposer|TestPinnedVariable' -count=1
```

To render the interface through Viva's offscreen renderer, set
`MOTTY_COMMAND_SCREENSHOTS` to an output directory and run that suite with
`-tags vivahead`. Screenshots use a fake local test connection.
