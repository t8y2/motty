# Terminal selection actions

The terminal context menu supports two actions for text selected in the
terminal:

- **Copy** copies the selected text to the system clipboard.
- **Paste selection** sends the selected text into the terminal input.

To use them, select terminal text, press the right mouse button, then press
`C` or `E`. Viva renders those mnemonic hints beside the menu labels. The keys
are active only while that terminal context menu is open;
typing `C` or `E` in the terminal continues to behave normally everywhere
else. Modifier keys, key repeats and key releases do not trigger these actions.
Only key presses activate the two shortcuts. While the menu is open, text
from an input method, even a single `c` or `e`, neither activates a menu item
nor reaches the shell.

`Paste selection` uses the same input path as an ordinary clipboard paste:
the terminal's input safety checks, multiline paste review, bracketed-paste
encoding, shell charset conversion and workspace broadcast rules all still
apply. It inserts the selected text without appending Enter, so a selected
command is not executed automatically. When input is sent successfully, the
selection is cleared in the same way as other terminal input.

The action is unavailable when there is no selection. It is also unavailable
when the selected terminal has no live shell. A right click handled by a remote
program that enabled mouse reporting keeps its existing behavior; use
Shift+right-click to open Motty's menu in that mode.

On Windows, a menu takes keyboard focus while the terminal keeps its input
method context and uncommitted text. Motty asks Viva to capture menu keys
before character translation and handles the two letters before the menu
searches its rows by name.

On macOS and Linux, the input method may consume a key before Motty receives
a key press. If this happens, click the menu item to copy or send the selection.

To check on Windows with a Pinyin IME, select `hello` in the terminal, type `ni` without
choosing a candidate, then right-click and press `E`. Only `hello` should be
sent. Without clicking again, type `hao` and choose 你好. Repeat with `C`:
the clipboard should contain `hello`, and only the chosen Chinese text should
reach the shell. Escape or clicking away from the menu should also leave the
composition available when focus returns to the terminal.
