package workbench

import (
	"fmt"
	"strings"

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
	shortcutCloseTab
	shortcutPanePrevious
	shortcutPaneNext
	shortcutPaneSplitHorizontal
	shortcutPaneSplitVertical
	shortcutPaneClose
	shortcutSFTP
	shortcutAssistant
	shortcutZoomIn
	shortcutZoomOut
	shortcutZoomReset
	shortcutHistory
	shortcutActionCount
)

type shortcutChord struct {
	Modifiers ui.Modifiers
	Key       byte
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
	shortcuts.chords[shortcutCloseTab] = shortcutChord{Modifiers: primary, Key: 'w'}
	shortcuts.chords[shortcutPanePrevious] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'j'}
	shortcuts.chords[shortcutPaneNext] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'k'}
	shortcuts.chords[shortcutPaneSplitHorizontal] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'h'}
	shortcuts.chords[shortcutPaneSplitVertical] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'v'}
	shortcuts.chords[shortcutPaneClose] = shortcutChord{Modifiers: primary | ui.ModifierAlt, Key: 'w'}
	shortcuts.chords[shortcutSFTP] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 's'}
	shortcuts.chords[shortcutAssistant] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 'a'}
	shortcuts.chords[shortcutZoomIn] = shortcutChord{Modifiers: primary, Key: '='}
	shortcuts.chords[shortcutZoomOut] = shortcutChord{Modifiers: primary, Key: '-'}
	shortcuts.chords[shortcutZoomReset] = shortcutChord{Modifiers: primary, Key: '0'}
	shortcuts.chords[shortcutHistory] = shortcutChord{Modifiers: primary | ui.ModifierShift, Key: 'h'}
	shortcuts.refreshLabels()
	return shortcuts
}

func (shortcuts *shortcutMap) refreshLabels() {
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		shortcuts.labels[action] = shortcuts.chords[action].label()
	}
}

func (chord shortcutChord) valid() bool {
	return chord.Modifiers&(ui.ModifierControl|ui.ModifierMeta) != 0 &&
		chord.Modifiers&^(ui.ModifierShift|ui.ModifierControl|ui.ModifierAlt|ui.ModifierMeta) == 0 &&
		(chord.Key >= 'a' && chord.Key <= 'z' || chord.Key >= '0' && chord.Key <= '9' || chord.Key == '=' || chord.Key == '-')
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
	key := chord.Key
	if key >= 'a' && key <= 'z' {
		key -= 'a' - 'A'
	}
	label.WriteByte(key)
	return label.String()
}

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
	key := chord.Key
	if key >= 'a' && key <= 'z' {
		key -= 'a' - 'A'
	}
	return append(dst, string(rune(key)))
}

func shortcutChordFromEvent(event ui.KeyEvent) (shortcutChord, bool) {
	if len(event.Text) != 1 {
		return shortcutChord{}, false
	}
	key := event.Text[0]
	if key >= 'A' && key <= 'Z' {
		key += 'a' - 'A'
	}
	modifiers := event.Modifiers
	if key == '+' {
		// Command+Shift+= types a plus: it is the same zoom-in chord.
		key, modifiers = '=', modifiers&^ui.ModifierShift
	}
	chord := shortcutChord{Modifiers: modifiers, Key: key}
	return chord, chord.valid()
}

func (shortcuts *shortcutMap) actionFor(chord shortcutChord) shortcutAction {
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		if shortcuts.chords[action] == chord {
			return action
		}
	}
	return shortcutNone
}

func (shortcuts *shortcutMap) bind(action shortcutAction, chord shortcutChord) (shortcutAction, bool) {
	if action <= shortcutNone || action >= shortcutActionCount || !chord.valid() {
		return shortcutNone, false
	}
	if conflict := shortcuts.actionFor(chord); conflict != shortcutNone && conflict != action {
		return conflict, false
	}
	shortcuts.chords[action] = chord
	shortcuts.labels[action] = chord.label()
	return shortcutNone, true
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
	case shortcutCloseTab:
		return "close_tab"
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

// Shortcut handles an application-owned modified printable chord. It is wired
// to app.Config.OnShortcut so ordinary pages can own shortcuts without moving
// text insertion, dead keys, or IME composition out of the native text path.
func (state *State) Shortcut(event ui.KeyEvent) (outcome ui.EventResult) {
	defer state.afterEvent(&outcome)
	chord, ok := shortcutChordFromEvent(event)
	if !ok {
		return ui.EventResult{}
	}
	if state.modal == modalShortcutCapture {
		if event.Repeat {
			return ui.EventResult{Handled: true}
		}
		conflict, bound := state.shortcuts.bind(state.shortcutCapture, chord)
		if !bound {
			if conflict != shortcutNone {
				state.shortcutError = fmt.Sprintf(state.messages.ShortcutsConflictFmt, state.shortcutActionLabel(conflict))
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
	if state.modal != modalNone {
		return ui.EventResult{}
	}
	if _, view := state.focusedDesktop(event.Target); view != nil && (isEscapeChord(event) || !view.released) {
		// The keyboard belongs to the remote desktop: none of Motty's shortcuts run.
		return state.desktopShortcut(view, event)
	}
	if result, handled := state.aiPanelShortcut(event); handled {
		return result
	}
	action := state.shortcuts.actionFor(chord)
	if action == shortcutNone {
		return ui.EventResult{}
	}
	if event.Repeat {
		return ui.EventResult{Handled: true}
	}
	return state.executeShortcut(action)
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
	case shortcutCloseTab:
		state.closeActivePaneOrTab()
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
	if err != nil {
		state.status = fmt.Sprintf(state.messages.StatusClipboardReadFailedFmt, err)
		return
	}
	if value != "" {
		state.typed(session, encodeTerminalPaste(value, session.term))
	}
}
