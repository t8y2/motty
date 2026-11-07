package workbench

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

type shortcutAction uint8

const (
	shortcutNone shortcutAction = iota
	shortcutCommandPalette
	shortcutNewWindow
	shortcutFind
	shortcutCopy
	shortcutPaste
	shortcutNewTab
	shortcutLocalTerminal
	shortcutCloseTab
	shortcutNextTab
	shortcutPreviousTab
	shortcutTabNumber
	shortcutPanePrevious
	shortcutPaneNext
	shortcutPaneSplitHorizontal
	shortcutPaneSplitVertical
	shortcutPaneClose
	shortcutFilesPath
	shortcutSFTP
	shortcutAssistant
	shortcutZoomIn
	shortcutZoomOut
	shortcutZoomReset
	shortcutHistory
	shortcutClearScreen
	shortcutCompletion
	shortcutActionCount
)

// shortcutMap is the shortcut table: the chord of each action, and the label
// settings shows for it. keymap.Table keeps the chords and refuses a chord two
// actions would share; what an action means and how the digit row is read are
// Motty's.
type shortcutMap struct {
	chords keymap.Table[shortcutAction]
	labels [shortcutActionCount]string
}

func newShortcutMap() shortcutMap {
	return shortcutMap{chords: keymap.NewTable[shortcutAction](int(shortcutActionCount))}
}

func defaultShortcuts() shortcutMap {
	primary := keymap.Primary
	shortcuts := newShortcutMap()
	shortcuts.chords.Set(shortcutCommandPalette, keymap.Chord{Modifiers: primary | ui.ModifierShift, Char: 'p'})
	shortcuts.chords.Set(shortcutNewWindow, keymap.Chord{Modifiers: primary | ui.ModifierShift, Char: 'n'})
	shortcuts.chords.Set(shortcutFind, keymap.Chord{Modifiers: primary, Char: 'f'})
	shortcuts.chords.Set(shortcutCopy, keymap.Chord{Modifiers: primary, Char: 'c'})
	shortcuts.chords.Set(shortcutPaste, keymap.Chord{Modifiers: primary, Char: 'v'})
	shortcuts.chords.Set(shortcutNewTab, keymap.Chord{Modifiers: primary, Char: 't'})
	shortcuts.chords.Set(shortcutLocalTerminal, keymap.Chord{Modifiers: primary | ui.ModifierShift, Char: 't'})
	shortcuts.chords.Set(shortcutCloseTab, keymap.Chord{Modifiers: primary, Char: 'w'})
	// Control+Tab is how browsers, VS Code and Windows Terminal switch tabs on
	// every platform, the Mac included, where Command+Tab is the system's.
	shortcuts.chords.Set(shortcutNextTab, keymap.Chord{Modifiers: ui.ModifierControl, Key: ui.KeyTab})
	shortcuts.chords.Set(shortcutPreviousTab, keymap.Chord{Modifiers: ui.ModifierControl | ui.ModifierShift, Key: ui.KeyTab})
	// One binding covers the whole digit row: its modifiers plus 1 through 9.
	// The stored digit is the one the user pressed, and any digit matches.
	shortcuts.chords.Set(shortcutTabNumber, keymap.Chord{Modifiers: primary, Char: '1'})
	shortcuts.chords.Set(shortcutPanePrevious, keymap.Chord{Modifiers: primary | ui.ModifierAlt, Char: 'j'})
	shortcuts.chords.Set(shortcutPaneNext, keymap.Chord{Modifiers: primary | ui.ModifierAlt, Char: 'k'})
	splitHorizontal, splitVertical := defaultPaneSplitChords()
	shortcuts.chords.Set(shortcutPaneSplitHorizontal, splitHorizontal)
	shortcuts.chords.Set(shortcutPaneSplitVertical, splitVertical)
	shortcuts.chords.Set(shortcutPaneClose, keymap.Chord{Modifiers: primary | ui.ModifierAlt, Char: 'w'})
	shortcuts.chords.Set(shortcutFilesPath, keymap.Chord{Modifiers: primary, Char: 'l'})
	shortcuts.chords.Set(shortcutSFTP, keymap.Chord{Modifiers: primary | ui.ModifierShift, Char: 's'})
	shortcuts.chords.Set(shortcutAssistant, keymap.Chord{Modifiers: primary | ui.ModifierShift, Char: 'a'})
	shortcuts.chords.Set(shortcutZoomIn, keymap.Chord{Modifiers: primary, Char: '='})
	shortcuts.chords.Set(shortcutZoomOut, keymap.Chord{Modifiers: primary, Char: '-'})
	shortcuts.chords.Set(shortcutZoomReset, keymap.Chord{Modifiers: primary, Char: '0'})
	shortcuts.chords.Set(shortcutHistory, keymap.Chord{Modifiers: primary | ui.ModifierShift, Char: 'h'})
	shortcuts.chords.Set(shortcutClearScreen, keymap.Chord{Modifiers: primary, Char: 'k'})
	shortcuts.chords.Set(shortcutCompletion, keymap.Chord{Modifiers: primary | ui.ModifierShift, Char: 'j'})
	shortcuts.refreshLabels()
	return shortcuts
}

// shortcutDefaultsGeneration is the generation of default chords this build
// ships. Each device records the generation its table was last reconciled
// against, so a default a later build changed is replaced once rather than on
// every start.
const shortcutDefaultsGeneration = 1

// legacyPaneSplitChords are the split defaults builds before generation 1
// shipped. On macOS Command+Alt+H is the menu bar's Hide Others, which an
// application's key equivalents never reach, so the chord could not work.
func legacyPaneSplitChords() (horizontal, vertical keymap.Chord) {
	primary := keymap.Primary
	return keymap.Chord{Modifiers: primary | ui.ModifierAlt, Char: 'h'},
		keymap.Chord{Modifiers: primary | ui.ModifierAlt, Char: 'v'}
}

// migrateChangedDefaults replaces the default chords a later generation changed
// with their new defaults, where the table still holds the old one. A chord the
// user chose differently is theirs and stays, and a chord taken keeps its owner:
// taken reports the chords something other than a built-in action holds.
func (shortcuts *shortcutMap) migrateChangedDefaults(taken func(keymap.Chord) bool) {
	previousHorizontal, previousVertical := legacyPaneSplitChords()
	horizontal, vertical := defaultPaneSplitChords()
	for _, change := range [...]struct {
		action shortcutAction
		was    keymap.Chord
		now    keymap.Chord
	}{
		{shortcutPaneSplitHorizontal, previousHorizontal, horizontal},
		{shortcutPaneSplitVertical, previousVertical, vertical},
	} {
		if shortcuts.chords.Chord(change.action) != change.was {
			continue
		}
		if taken != nil && taken(change.now) {
			continue
		}
		shortcuts.bind(change.action, change.now)
	}
}

func (shortcuts *shortcutMap) refreshLabels() {
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		shortcuts.labels[action] = labelFor(action, shortcuts.chords.Chord(action))
	}
}

// chordLabel is how a chord reads in a list: its keys, or a dash for an action
// that has none.
func chordLabel(chord keymap.Chord) string {
	if label := chord.Label(); label != "" {
		return label
	}
	return "—"
}

// labelFor is the settings label of an action's chord: the digit row reads as
// the whole range it covers, so one row explains all nine keys.
func labelFor(action shortcutAction, chord keymap.Chord) string {
	if action == shortcutTabNumber && chord.Valid() && tabNumberChord(chord) {
		return strings.TrimSuffix(chord.Label(), string(chord.Char)) + "1…9"
	}
	return chordLabel(chord)
}

// validForSnippet is Valid with one more rule. Control alone is how a terminal
// reads its own keys (Ctrl+R searches history, Ctrl+D ends input), so where
// Command is not the modifier a snippet chord needs Shift or Alt as well.
func validForSnippet(chord keymap.Chord) bool {
	printable := chord.Char >= 'a' && chord.Char <= 'z' || chord.Char >= '0' && chord.Char <= '9' || chord.Char == '=' || chord.Char == '-'
	return chord.Valid() && printable && chord.Key == ui.KeyUnknown &&
		chord.Modifiers&(ui.ModifierControl|ui.ModifierMeta) != 0 &&
		chord.Modifiers&(ui.ModifierMeta|ui.ModifierShift|ui.ModifierAlt) != 0
}

// clipboardAliases are the classic clipboard chords every terminal and text
// editor on Windows and Linux honours next to the primary-modifier ones. They
// are fixed rather than bindable, and only apply while no action owns the
// chord, so rebinding Paste leaves Shift+Insert pasting and a user who gives
// Shift+Insert another job takes it over.
var clipboardAliases = [...]struct {
	chord  keymap.Chord
	action shortcutAction
}{
	{keymap.Chord{Modifiers: ui.ModifierShift, Key: ui.KeyInsert}, shortcutPaste},
	{keymap.Chord{Modifiers: ui.ModifierControl, Key: ui.KeyInsert}, shortcutCopy},
}

// actionFor is the action a chord runs: the one bound to it, or else the
// clipboard alias it stands for.
func (shortcuts *shortcutMap) actionFor(chord keymap.Chord) shortcutAction {
	if action := shortcuts.boundAction(chord); action != shortcutNone {
		return action
	}
	for _, alias := range clipboardAliases {
		if alias.chord == chord {
			return alias.action
		}
	}
	return shortcutNone
}

// boundAction is actionFor without the built-in aliases: the action the user's
// table binds to the chord, which is what a new binding can conflict with.
func (shortcuts *shortcutMap) boundAction(chord keymap.Chord) shortcutAction {
	if action := shortcuts.chords.Lookup(chord); action != shortcutNone {
		return action
	}
	// A digit row is one action: the same modifiers and any digit from 1 to 9.
	if tabNumberFamily(shortcuts.chords.Chord(shortcutTabNumber), chord) {
		return shortcutTabNumber
	}
	return shortcutNone
}

// tabNumberFamily reports whether chord is a digit the tab-number binding
// covers: the same modifiers and a digit from 1 to 9.
func tabNumberFamily(bound, chord keymap.Chord) bool {
	return bound.Modifiers == chord.Modifiers && tabNumberChord(bound) && tabNumberChord(chord)
}

// tabNumberChord reports whether a chord can be the tab-number binding: a
// plain digit from 1 to 9, never a named key.
func tabNumberChord(chord keymap.Chord) bool {
	return chord.Key == ui.KeyUnknown && chord.Char >= '1' && chord.Char <= '9'
}

func (shortcuts *shortcutMap) bind(action shortcutAction, chord keymap.Chord) (shortcutAction, bool) {
	if action == shortcutTabNumber && !tabNumberChord(chord) {
		return shortcutNone, false
	}
	if conflict := shortcuts.boundAction(chord); conflict != shortcutNone && conflict != action {
		return conflict, false
	}
	if _, ok := shortcuts.chords.Bind(action, chord); !ok {
		return shortcutNone, false
	}
	shortcuts.labels[action] = labelFor(action, chord)
	return shortcutNone, true
}

// unbind takes the chord away from an action, which then reads "—" in the
// settings and runs nothing until it is given one again or the defaults return.
func (shortcuts *shortcutMap) unbind(action shortcutAction) bool {
	if !shortcuts.chords.Unbind(action) {
		return false
	}
	shortcuts.labels[action] = labelFor(action, keymap.Chord{})
	return true
}

func (action shortcutAction) String() string {
	switch action {
	case shortcutCommandPalette:
		return "command_palette"
	case shortcutNewWindow:
		return "new_window"
	case shortcutFind:
		return "find"
	case shortcutCopy:
		return "copy"
	case shortcutPaste:
		return "paste"
	case shortcutNewTab:
		return "new_tab"
	case shortcutLocalTerminal:
		return "local_terminal"
	case shortcutCloseTab:
		return "close_tab"
	case shortcutNextTab:
		return "next_tab"
	case shortcutPreviousTab:
		return "previous_tab"
	case shortcutTabNumber:
		return "tab_number"
	case shortcutPanePrevious:
		return "pane_previous"
	case shortcutPaneNext:
		return "pane_next"
	case shortcutPaneSplitHorizontal:
		return "pane_split_horizontal"
	case shortcutPaneSplitVertical:
		return "pane_split_vertical"
	case shortcutPaneClose:
		return "pane_close"
	case shortcutFilesPath:
		return "files_path"
	case shortcutSFTP:
		return "sftp"
	case shortcutAssistant:
		return "assistant"
	case shortcutZoomIn:
		return "zoom_in"
	case shortcutZoomOut:
		return "zoom_out"
	case shortcutZoomReset:
		return "zoom_reset"
	case shortcutHistory:
		return "command_history"
	case shortcutClearScreen:
		return "clear_screen"
	case shortcutCompletion:
		return "command_completion"
	default:
		return ""
	}
}

func shortcutActionFromString(value string) shortcutAction {
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		if action.String() == value {
			return action
		}
	}
	return shortcutNone
}

// Shortcut handles an application-owned printable chord. It is wired
// to app.Config.OnShortcut so ordinary pages can own shortcuts without moving
// text insertion, dead keys, or IME composition out of the native text path.
func (state *State) Shortcut(event ui.KeyEvent) (outcome ui.EventResult) {
	if state.noteExit != nil {
		return ui.EventResult{Handled: true}
	}
	defer state.afterEvent(&outcome)
	if result, handled := state.devShortcut(event); handled {
		return result
	}
	if result, handled := state.desktopFullscreenKey(event); handled {
		return result
	}
	chord, ok := keymap.FromEvent(event)
	if state.modal == modalSnippet && state.snippets.capturing {
		return state.captureSnippetShortcut(event, chord, ok)
	}
	if !ok {
		if state.modal == modalShortcutCapture && !event.Release {
			state.shortcutError = state.messages.ShortcutsInvalid
			return ui.EventResult{Handled: true, Repaint: true}
		}
		return ui.EventResult{}
	}
	return state.shortcut(event, chord)
}

// shortcut shares contextual routing between printable and named bindings.
func (state *State) shortcut(event ui.KeyEvent, chord keymap.Chord) ui.EventResult {
	if state.notesVisible() && (state.notes.view.menu != "" || state.notes.view.popup != "" || state.notes.view.drawer || state.notes.confirm != "") {
		action := state.shortcuts.actionFor(chord)
		if action == shortcutCopy || action == shortcutPaste || action == shortcutNone {
			return ui.EventResult{}
		}
		return ui.EventResult{Handled: true}
	}
	if event.Release {
		// Swallow the release of every press this routing consumed, a snippet
		// chord included, so a terminal that asked for releases is not sent the
		// end of a press it never saw.
		consumed := state.shortcutActionFor(chord) != shortcutNone ||
			state.activeSection == sectionTerminal && state.snippetFor(chord, -1) >= 0
		return ui.EventResult{Handled: consumed}
	}
	if state.modal == modalShortcutCapture {
		return state.captureShortcut(event, chord)
	}
	if chord.Char == '+' && state.shortcutActionFor(chord) == shortcutNone && state.snippetNamed(chord) == "" {
		// Preserve the default zoom-in alias without collapsing a custom + binding.
		alias := keymap.Chord{Modifiers: chord.Modifiers &^ ui.ModifierShift, Char: '='}
		if state.shortcutActionFor(alias) == shortcutZoomIn {
			chord = alias
		}
	}
	if login := state.ai.login; login != nil && state.ai.cfg.dialog == aiDialogCLILogin {
		action := state.shortcutActionFor(chord)
		if action == shortcutNone {
			return ui.EventResult{}
		}
		if action == shortcutPaste && event.Target == hitAICLILoginTerminal && login.term.shell != nil && !login.finished {
			if text, err := state.options.ClipboardText(); err == nil {
				login.term.shell.Write(term.EncodePaste(text, login.term.screen.Modes().BracketedPaste))
			}
		}
		if action == shortcutCloseTab {
			state.aiStopCLILogin()
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	// Plain and Alt/Shift bindings must leave ordinary text editing usable.
	if chord.Modifiers&(ui.ModifierControl|ui.ModifierMeta) == 0 && state.shortcutTextInput(event.Target) {
		return ui.EventResult{}
	}
	if result := state.remoteSearchShortcut(event); result.Handled {
		return result
	}
	if result, handled := state.remoteDetailShortcut(event); handled {
		return result
	}
	if state.modal != modalNone {
		return ui.EventResult{}
	}
	if _, view := state.focusedDesktop(event.Target); view != nil && (isEscapeChord(event) || !view.released) {
		// The keyboard belongs to the remote desktop: none of Motty's shortcuts run.
		return state.desktopShortcut(view, event)
	}
	if result, handled := state.sftpTabShortcut(event); handled {
		return result
	}
	if result := state.noteEditingKey(event); result.Handled {
		return result
	}
	if result := state.notePreviewShortcut(event); result.Handled {
		return result
	}
	if state.noteSidebarVisible() && tree.Within(event.Target.Scope(), notesHits) && event.Target != hitNoteSearch && (state.shortcuts.actionFor(chord) == shortcutPaste || state.shortcuts.actionFor(chord) == shortcutCopy) {
		return ui.EventResult{Handled: true}
	}
	if result, handled := state.aiPanelShortcut(event); handled {
		return result
	}
	action := state.shortcutActionFor(chord)
	if action == shortcutNone {
		if index := state.snippetFor(chord, -1); index >= 0 && state.activeSection == sectionTerminal {
			if !event.Repeat {
				state.sendSnippet(index)
			}
			return ui.EventResult{Handled: true, Repaint: true}
		}
		return ui.EventResult{}
	}
	if action == shortcutTabNumber {
		return state.tabNumberShortcut(chord, event)
	}
	if action == shortcutCopy && chord.Modifiers == ui.ModifierControl && chord.Char == 'c' {
		// With no selection, Ctrl+C belongs to the foreground terminal
		// program. Let the normal key path encode it (including kitty and
		// win32-input-mode), rather than consuming an empty copy operation.
		_, target, embedded := state.decodePaneHit(event.Target)
		if !embedded {
			target = event.Target
		}
		if session := state.terminalSession(target); session != nil && session.term != nil && !session.term.selection.selected {
			return ui.EventResult{}
		}
	}
	if event.Repeat && !action.repeats() {
		return ui.EventResult{Handled: true}
	}
	if action == shortcutFind {
		if result, handled := state.sftpFilterShortcut(event.Target); handled {
			return result
		}
	}
	if action == shortcutFilesPath {
		if result, handled := state.sftpPathShortcut(event.Target); handled {
			return result
		}
		return ui.EventResult{}
	}
	return state.executeShortcut(action)
}

// shortcutTextInput includes native inputs owned by embedded presenters, whose
// editors are resolved separately from EditorFor by the main window.
func (state *State) shortcutTextInput(target ui.ID) bool {
	context := app.TextInputContext{ID: target}
	return state.EditorFor(target) != nil || state.SFTPTabInput(context) != nil ||
		state.SFTPInput(context) != nil || state.RemoteToolsInput(context) != nil ||
		state.AIPanelInput(context) != nil
}

// tabNumberShortcut jumps to the tab the pressed digit names. A digit with no
// tab behind it stays with the terminal where Control plus a digit is a control
// code; Command has no terminal meaning, so the Mac keeps the press.
func (state *State) tabNumberShortcut(chord keymap.Chord, event ui.KeyEvent) ui.EventResult {
	index := int(chord.Char - '1')
	if !event.Repeat && state.activateTopTab(index) {
		return ui.EventResult{Handled: true, Repaint: true, Focus: state.activeTopTabFocusHit()}
	}
	if chord.Modifiers&ui.ModifierMeta == 0 && index >= state.topTabCount() {
		return ui.EventResult{}
	}
	return ui.EventResult{Handled: true}
}

// captureShortcut binds the chord the shortcut dialog is waiting for, or says
// why it cannot be: another action or a snippet already has it.
func (state *State) captureShortcut(event ui.KeyEvent, chord keymap.Chord) ui.EventResult {
	if event.Repeat {
		return ui.EventResult{Handled: true}
	}
	if owner := state.snippetNamed(chord); owner != "" {
		state.shortcutError = fmt.Sprintf(state.messages.ShortcutsConflictFmt, owner)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	conflict, bound := state.shortcuts.bind(state.shortcutCapture, chord)
	if !bound {
		if conflict != shortcutNone {
			state.shortcutError = fmt.Sprintf(state.messages.ShortcutsConflictFmt, state.shortcutActionLabel(conflict))
		} else if state.shortcutCapture == shortcutTabNumber {
			state.shortcutError = state.messages.ShortcutsTabNumberInvalid
		} else {
			state.shortcutError = state.messages.ShortcutsInvalid
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	state.modal = modalNone
	state.shortcutCapture = shortcutNone
	state.shortcutError = ""
	state.save()
	return ui.EventResult{Handled: true, Repaint: true}
}

// clearShortcut removes the chord the capture dialog is waiting for and closes
// it. The action keeps its row, reading "—", and can be given a new chord later.
func (state *State) clearShortcut(action shortcutAction) {
	if !state.shortcuts.unbind(action) {
		return
	}
	state.modal = modalNone
	state.shortcutCapture = shortcutNone
	state.shortcutError = ""
	state.save()
}

// keyShortcut runs a binding delivered as a raw key event. The keyboard stays
// with a modal dialog and with a remote desktop that has it, as for any other
// shortcut. A release of the key is swallowed too, so a terminal that asked for
// releases is not sent the end of a press it never saw.
func (state *State) keyShortcut(event ui.KeyEvent) (ui.EventResult, bool) {
	chord, ok := keymap.FromEvent(event)
	if !ok {
		return ui.EventResult{}, false
	}
	if state.modal == modalShortcutCapture {
		if event.Release || event.Repeat {
			return ui.EventResult{Handled: true}, true
		}
		if event.Key == ui.KeyEscape && event.Modifiers == 0 {
			state.clickShortcutCapture(hitShortcutCaptureCancel)
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
		return state.captureShortcut(event, chord), true
	}
	if state.tabDrag.active {
		return ui.EventResult{}, false
	}
	if _, view := state.focusedDesktop(event.Target); view != nil && !view.released {
		return ui.EventResult{}, false
	}
	// A terminal owns Command chords: terminalKey drives the tabs and the
	// program's line edits with them, so the application table does not take
	// them first.
	if event.Modifiers&ui.ModifierMeta != 0 && state.terminalHasMetaChords(event.Target) {
		return ui.EventResult{}, false
	}
	result := state.shortcut(event, chord)
	return result, result.Handled
}

// terminalHasMetaChords reports whether the target is a running terminal, which
// handles Command chords itself. An event with no target still belongs to the
// active terminal, the way the raw key path reads it.
func (state *State) terminalHasMetaChords(target ui.ID) bool {
	if session := state.terminalSession(target); session != nil {
		return session.term != nil
	}
	if session, _, ok := state.decodePaneHit(target); ok {
		return session != nil && session.term != nil
	}
	if target.IsZero() {
		session, ok := state.sessionByIDPointer(state.activeSessionID)
		return ok && session != nil && session.term != nil
	}
	return false
}

// repeats reports whether holding the chord runs the action again and again:
// holding Control+Tab walks through the tabs.
func (action shortcutAction) repeats() bool {
	return action == shortcutNextTab || action == shortcutPreviousTab
}

func (state *State) executeShortcut(action shortcutAction) ui.EventResult {
	result := ui.EventResult{Handled: true, Repaint: true}
	session, hasSession := state.sessionByIDPointer(state.activeSessionID)
	switch action {
	case shortcutCommandPalette:
		state.openCommandPalette()
		result.Focus = hitCommandPaletteSearch
	case shortcutNewWindow:
		state.openNewWindow()
	case shortcutFind:
		if hasSession && session.term != nil {
			return state.openTerminalFind(session)
		}
	case shortcutCopy:
		if hasSession && session.term != nil {
			return state.copySelection(session)
		}
	case shortcutPaste:
		if hasSession {
			state.pasteIntoTerminal(session)
		}
	case shortcutNewTab:
		state.Click(hitNewTab)
	case shortcutLocalTerminal:
		state.openLocalTerminal()
		result.Focus = state.activeTerminalFocusHit()
	case shortcutCloseTab:
		state.closeActivePaneOrTab()
	case shortcutNextTab:
		if state.cycleWindowTabs(1) {
			result.Focus = state.activeTopTabFocusHit()
		}
	case shortcutPreviousTab:
		if state.cycleWindowTabs(-1) {
			result.Focus = state.activeTopTabFocusHit()
		}
	case shortcutPanePrevious:
		state.Click(hitPanePrevious)
	case shortcutPaneNext:
		state.Click(hitPaneNext)
	case shortcutPaneSplitHorizontal:
		state.Click(hitPaneSplitHorizontal)
	case shortcutPaneSplitVertical:
		state.Click(hitPaneSplitVertical)
	case shortcutPaneClose:
		state.Click(hitPaneClose)
	case shortcutSFTP:
		if hasSession {
			state.Click(hitSFTPOpen)
		}
	case shortcutZoomIn:
		state.zoomTerminal(1, false)
	case shortcutZoomOut:
		state.zoomTerminal(-1, false)
	case shortcutZoomReset:
		state.zoomTerminal(0, true)
	case shortcutHistory:
		if hasSession && session.term != nil {
			state.openHistory(session)
			result.Focus = hitHistorySearch
		}
	case shortcutClearScreen:
		if hasSession && session.term != nil {
			session.term.clearScreen(state.messages)
		}
	case shortcutCompletion:
		state.openCompletions()
	case shortcutAssistant:
		state.toggleAIPanel()
		// Opened, the keyboard goes to the message box; closed, back to the terminal.
		if state.aiPanelVisible() {
			result.Focus = hitAIInput
		} else {
			result.Focus = state.activeTerminalFocusHit()
		}
	}
	return result
}

func encodeTerminalPaste(value string, terminal *terminal) []byte {
	return term.EncodePaste(value, terminal.screen.Modes().BracketedPaste)
}

// pasteIntoTerminal sends the system clipboard to a live shell as a paste.
func (state *State) pasteIntoTerminal(session *Session) {
	if session.term == nil || session.term.shell == nil {
		return
	}
	value, err := state.options.ClipboardText()
	if err == nil && value != "" {
		if state.modal == modalTerminalInput || state.guardTerminalText(session, app.TextEvent{Kind: app.TextPaste, Text: value}) {
			return
		}
		state.typed(session, encodeTerminalPaste(value, session.term))
		return
	}
	// A clipboard with a picture and no words is not an empty one on a server that
	// takes pictures; some systems also answer a text request on it with an error.
	if state.pasteClipboardImage(session) {
		return
	}
	if err == nil || state.clipboardHoldsImage() {
		// Nothing travels as text, but a program that reads pictures from the
		// clipboard itself (OpenCode, for one) waits for a paste with no content
		// to do so, as it gets from iTerm2, Ghostty and kitty. Without bracketed
		// paste an empty paste is no event at all, so nothing is sent.
		if session.term.screen.Modes().BracketedPaste {
			state.typed(session, encodeTerminalPaste("", session.term))
		}
		return
	}
	state.status = fmt.Sprintf(state.messages.StatusClipboardReadFailedFmt, err)
}

// clipboardHoldsImage reports whether the system clipboard has a picture, for a
// text request that failed on it: some systems answer that with an error.
func (state *State) clipboardHoldsImage() bool {
	_, err := state.options.ClipboardImagePNG()
	return err == nil || errors.Is(err, app.ErrTooLarge)
}
