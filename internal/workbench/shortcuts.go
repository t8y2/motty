package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
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

// shortcutChord is a modifier set and one key: a printable one in Key, or one of
// the named keys in Named. Exactly one of the two is set.
type shortcutChord struct {
	Modifiers ui.Modifiers
	Key       byte
	Named     ui.Key
}

// namedShortcutKeys are the keys without text a chord may use, with the name
// the label shows, the keycap a menu shows, and the name the settings file
// keeps. Unbound keys remain available to the terminal program.
var namedShortcutKeys = [...]struct {
	key    ui.Key
	label  string
	keycap string
	saved  string
}{
	{ui.KeyTab, "Tab", "Tab", "tab"},
	{ui.KeyPageUp, "PageUp", "PgUp", "page_up"},
	{ui.KeyPageDown, "PageDown", "PgDn", "page_down"},
	{ui.KeyLeft, "Left", "←", "left"},
	{ui.KeyRight, "Right", "→", "right"},
	{ui.KeyEnter, "Enter", "↵", "enter"},
	{ui.KeyEscape, "Esc", "Esc", "escape"},
	{ui.KeyUp, "Up", "↑", "up"},
	{ui.KeyDown, "Down", "↓", "down"},
	{ui.KeyHome, "Home", "Home", "home"},
	{ui.KeyEnd, "End", "End", "end"},
	{ui.KeyBackspace, "Backspace", "⌫", "backspace"},
	{ui.KeyDelete, "Delete", "Del", "delete"},
	{ui.KeyInsert, "Insert", "Ins", "insert"},
	{ui.KeyF1, "F1", "F1", "f1"},
	{ui.KeyF2, "F2", "F2", "f2"},
	{ui.KeyF3, "F3", "F3", "f3"},
	{ui.KeyF4, "F4", "F4", "f4"},
	{ui.KeyF5, "F5", "F5", "f5"},
	{ui.KeyF6, "F6", "F6", "f6"},
	{ui.KeyF7, "F7", "F7", "f7"},
	{ui.KeyF8, "F8", "F8", "f8"},
	{ui.KeyF9, "F9", "F9", "f9"},
	{ui.KeyF10, "F10", "F10", "f10"},
	{ui.KeyF11, "F11", "F11", "f11"},
	{ui.KeyF12, "F12", "F12", "f12"},
}

func namedShortcutKey(key ui.Key) (int, bool) {
	for index := range namedShortcutKeys {
		if namedShortcutKeys[index].key == key {
			return index, true
		}
	}
	return 0, false
}

// savedKey is the chord's key as the settings file writes it: the character, or
// the name of a named key. An unbound action has no key at all, which is how the
// file says to leave it without a chord instead of restoring its default. Older
// builds read a name as no key at all and keep the default for that action.
func (chord shortcutChord) savedKey() string {
	if !chord.valid() {
		return ""
	}
	if index, ok := namedShortcutKey(chord.Named); ok {
		return namedShortcutKeys[index].saved
	}
	return string([]byte{chord.Key})
}

// chordFromSaved is the inverse of savedKey.
func chordFromSaved(modifiers ui.Modifiers, key string) (shortcutChord, bool) {
	for index := range namedShortcutKeys {
		if namedShortcutKeys[index].saved == key {
			return shortcutChord{Modifiers: modifiers, Named: namedShortcutKeys[index].key}, true
		}
	}
	if len(key) != 1 {
		return shortcutChord{}, false
	}
	character := key[0]
	if character >= 'A' && character <= 'Z' {
		character += 'a' - 'A'
	}
	return shortcutChord{Modifiers: modifiers, Key: character}, true
}

type shortcutMap struct {
	chords [shortcutActionCount]shortcutChord
	labels [shortcutActionCount]string
}

func defaultShortcuts() shortcutMap {
	primary := defaultShortcutModifier()
	var shortcuts shortcutMap
	shortcuts.chords[shortcutCommandPalette] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 'p'}
	shortcuts.chords[shortcutNewWindow] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 'n'}
	shortcuts.chords[shortcutFind] = shortcutChord{Modifiers: primary, Key: 'f'}
	shortcuts.chords[shortcutCopy] = shortcutChord{Modifiers: primary, Key: 'c'}
	shortcuts.chords[shortcutPaste] = shortcutChord{Modifiers: primary, Key: 'v'}
	shortcuts.chords[shortcutNewTab] = shortcutChord{Modifiers: primary, Key: 't'}
	shortcuts.chords[shortcutLocalTerminal] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 't'}
	shortcuts.chords[shortcutCloseTab] = shortcutChord{Modifiers: primary, Key: 'w'}
	// Control+Tab is how browsers, VS Code and Windows Terminal switch tabs on
	// every platform, the Mac included, where Command+Tab is the system's.
	shortcuts.chords[shortcutNextTab] = shortcutChord{Modifiers: ui.ModifierControl, Named: ui.KeyTab}
	shortcuts.chords[shortcutPreviousTab] = shortcutChord{Modifiers: ui.ModifierControl | ui.ModifierShift, Named: ui.KeyTab}
	// One binding covers the whole digit row: its modifiers plus 1 through 9.
	// The stored digit is the one the user pressed, and any digit matches.
	shortcuts.chords[shortcutTabNumber] = shortcutChord{Modifiers: primary, Key: '1'}
	shortcuts.chords[shortcutPanePrevious] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'j'}
	shortcuts.chords[shortcutPaneNext] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'k'}
	shortcuts.chords[shortcutPaneSplitHorizontal] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'h'}
	shortcuts.chords[shortcutPaneSplitVertical] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'v'}
	shortcuts.chords[shortcutPaneClose] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'w'}
	shortcuts.chords[shortcutFilesPath] = shortcutChord{Modifiers: primary, Key: 'l'}
	shortcuts.chords[shortcutSFTP] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 's'}
	shortcuts.chords[shortcutAssistant] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 'a'}
	shortcuts.chords[shortcutZoomIn] = shortcutChord{Modifiers: primary, Key: '='}
	shortcuts.chords[shortcutZoomOut] = shortcutChord{Modifiers: primary, Key: '-'}
	shortcuts.chords[shortcutZoomReset] = shortcutChord{Modifiers: primary, Key: '0'}
	shortcuts.chords[shortcutHistory] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 'h'}
	shortcuts.chords[shortcutClearScreen] = shortcutChord{Modifiers: primary, Key: 'k'}
	shortcuts.chords[shortcutCompletion] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 'j'}
	shortcuts.refreshLabels()
	return shortcuts
}

func (shortcuts *shortcutMap) refreshLabels() {
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		shortcuts.labels[action] = shortcuts.chords[action].labelFor(action)
	}
}

func (chord shortcutChord) valid() bool {
	if chord.Modifiers&^(ui.ModifierShift|ui.ModifierControl|ui.ModifierAlt|ui.ModifierMeta) != 0 {
		return false
	}
	if chord.Named != ui.KeyUnknown {
		_, named := namedShortcutKey(chord.Named)
		return named && chord.Key == 0
	}
	return chord.Key >= ' ' && chord.Key <= '~' && !(chord.Key >= 'A' && chord.Key <= 'Z')
}

func (chord shortcutChord) label() string {
	if !chord.valid() {
		return "—"
	}
	var label strings.Builder
	if chord.Modifiers&ui.ModifierMeta != 0 {
		label.WriteString("Cmd+")
	}
	if chord.Modifiers&ui.ModifierControl != 0 {
		label.WriteString("Ctrl+")
	}
	if chord.Modifiers&ui.ModifierAlt != 0 {
		label.WriteString("Alt+")
	}
	if chord.Modifiers&ui.ModifierShift != 0 {
		label.WriteString("Shift+")
	}
	if index, ok := namedShortcutKey(chord.Named); ok {
		label.WriteString(namedShortcutKeys[index].label)
		return label.String()
	}
	key := chord.Key
	if key == ' ' {
		label.WriteString("Space")
		return label.String()
	}
	if key >= 'a' && key <= 'z' {
		key -= 'a' - 'A'
	}
	label.WriteByte(key)
	return label.String()
}

// labelFor is the settings label of a chord: the digit row reads as the whole
// range it covers, so one row explains all nine keys.
func (chord shortcutChord) labelFor(action shortcutAction) string {
	if action == shortcutTabNumber && chord.valid() && tabNumberChord(chord) {
		return strings.TrimSuffix(chord.label(), string(chord.Key)) + "1…9"
	}
	return chord.label()
}

// keycapText is the text of each key code, made once: converting a rune to a
// string allocates, and keycaps are painted on every restored page and menu.
var keycapText = func() [256]string {
	var table [256]string
	for code := range table {
		table[code] = string(rune(code))
	}
	return table
}()

// keys splits a chord into the keycaps a menu shows for it: one per modifier,
// then the key. Where the platform has symbols for the modifiers it uses them.
func (chord shortcutChord) keys(dst []string) []string {
	if !chord.valid() {
		return dst
	}
	if chord.Modifiers&ui.ModifierControl != 0 {
		dst = append(dst, modifierKeycap(ui.ModifierControl))
	}
	if chord.Modifiers&ui.ModifierAlt != 0 {
		dst = append(dst, modifierKeycap(ui.ModifierAlt))
	}
	if chord.Modifiers&ui.ModifierShift != 0 {
		dst = append(dst, modifierKeycap(ui.ModifierShift))
	}
	if chord.Modifiers&ui.ModifierMeta != 0 {
		dst = append(dst, modifierKeycap(ui.ModifierMeta))
	}
	if index, ok := namedShortcutKey(chord.Named); ok {
		return append(dst, namedShortcutKeys[index].keycap)
	}
	key := chord.Key
	if key == ' ' {
		return append(dst, "Space")
	}
	if key >= 'a' && key <= 'z' {
		key -= 'a' - 'A'
	}
	return append(dst, keycapText[key])
}

// validForSnippet is valid with one more rule. Control alone is how a terminal
// reads its own keys (Ctrl+R searches history, Ctrl+D ends input), so where
// Command is not the modifier a snippet chord needs Shift or Alt as well.
func (chord shortcutChord) validForSnippet() bool {
	printable := chord.Key >= 'a' && chord.Key <= 'z' || chord.Key >= '0' && chord.Key <= '9' || chord.Key == '=' || chord.Key == '-'
	return chord.valid() && printable && chord.Named == ui.KeyUnknown &&
		chord.Modifiers&(ui.ModifierControl|ui.ModifierMeta) != 0 &&
		chord.Modifiers&(ui.ModifierMeta|ui.ModifierShift|ui.ModifierAlt) != 0
}

func shortcutChordFromEvent(event ui.KeyEvent) (shortcutChord, bool) {
	if event.Key != ui.KeyUnknown {
		if _, named := namedShortcutKey(event.Key); named || event.Key == ui.KeySpace {
			return shortcutChordFromKey(event)
		}
	}
	if len(event.Text) != 1 {
		return shortcutChord{}, false
	}
	key := event.Text[0]
	if key >= 'A' && key <= 'Z' {
		key += 'a' - 'A'
	}
	chord := shortcutChord{Modifiers: event.Modifiers, Key: key}
	return chord, chord.valid()
}

// shortcutChordFromKey is the chord of a key without text, one of the named
// keys a chord may use. Printable chords come through shortcutChordFromEvent.
func shortcutChordFromKey(event ui.KeyEvent) (shortcutChord, bool) {
	if event.Key == ui.KeySpace {
		return shortcutChord{Modifiers: event.Modifiers, Key: ' '}, true
	}
	chord := shortcutChord{Modifiers: event.Modifiers, Named: event.Key}
	return chord, event.Key != ui.KeyUnknown && chord.valid()
}

func (shortcuts *shortcutMap) actionFor(chord shortcutChord) shortcutAction {
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		if shortcuts.chords[action] == chord {
			return action
		}
	}
	// A digit row is one action: the same modifiers and any digit from 1 to 9.
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		if action == shortcutTabNumber && tabNumberFamily(shortcuts.chords[action], chord) {
			return action
		}
	}
	return shortcutNone
}

// tabNumberFamily reports whether chord is a digit the tab-number binding
// covers: the same modifiers and a digit from 1 to 9.
func tabNumberFamily(bound, chord shortcutChord) bool {
	return bound.Modifiers == chord.Modifiers && tabNumberChord(bound) && tabNumberChord(chord)
}

// tabNumberChord reports whether a chord can be the tab-number binding: a
// plain digit from 1 to 9, never a named key.
func tabNumberChord(chord shortcutChord) bool {
	return chord.Named == ui.KeyUnknown && chord.Key >= '1' && chord.Key <= '9'
}

func (shortcuts *shortcutMap) bind(action shortcutAction, chord shortcutChord) (shortcutAction, bool) {
	if action <= shortcutNone || action >= shortcutActionCount || !chord.valid() {
		return shortcutNone, false
	}
	if action == shortcutTabNumber && !tabNumberChord(chord) {
		return shortcutNone, false
	}
	if conflict := shortcuts.actionFor(chord); conflict != shortcutNone && conflict != action {
		return conflict, false
	}
	shortcuts.chords[action] = chord
	shortcuts.labels[action] = chord.labelFor(action)
	return shortcutNone, true
}

// unbind takes the chord away from an action, which then reads "—" in the
// settings and runs nothing until it is given one again or the defaults return.
func (shortcuts *shortcutMap) unbind(action shortcutAction) bool {
	if action <= shortcutNone || action >= shortcutActionCount {
		return false
	}
	shortcuts.chords[action] = shortcutChord{}
	shortcuts.labels[action] = shortcuts.chords[action].labelFor(action)
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
	defer state.afterEvent(&outcome)
	if result, handled := state.desktopFullscreenKey(event); handled {
		return result
	}
	chord, ok := shortcutChordFromEvent(event)
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
func (state *State) shortcut(event ui.KeyEvent, chord shortcutChord) ui.EventResult {
	if event.Release {
		// Swallow the release of every press this routing consumed, a snippet
		// chord included, so a terminal that asked for releases is not sent the
		// end of a press it never saw.
		consumed := state.shortcuts.actionFor(chord) != shortcutNone ||
			state.activeSection == sectionTerminal && state.snippetFor(chord, -1) >= 0
		return ui.EventResult{Handled: consumed}
	}
	if state.modal == modalShortcutCapture {
		return state.captureShortcut(event, chord)
	}
	if chord.Key == '+' && state.shortcuts.actionFor(chord) == shortcutNone && state.snippetNamed(chord) == "" {
		// Preserve the default zoom-in alias without collapsing a custom + binding.
		alias := shortcutChord{Modifiers: chord.Modifiers &^ ui.ModifierShift, Key: '='}
		if state.shortcuts.actionFor(alias) == shortcutZoomIn {
			chord = alias
		}
	}
	if login := state.ai.login; login != nil && state.ai.cfg.dialog == aiDialogCLILogin {
		action := state.shortcuts.actionFor(chord)
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
	if result, handled := state.aiPanelShortcut(event); handled {
		return result
	}
	action := state.shortcuts.actionFor(chord)
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
	if action == shortcutCopy && chord.Modifiers == ui.ModifierControl && chord.Key == 'c' {
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
func (state *State) tabNumberShortcut(chord shortcutChord, event ui.KeyEvent) ui.EventResult {
	index := int(chord.Key - '1')
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
func (state *State) captureShortcut(event ui.KeyEvent, chord shortcutChord) ui.EventResult {
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
	chord, ok := shortcutChordFromEvent(event)
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
	if err != nil {
		state.status = fmt.Sprintf(state.messages.StatusClipboardReadFailedFmt, err)
	}
}
