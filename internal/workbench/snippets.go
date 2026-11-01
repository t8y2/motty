package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// Snippets are commands the user keeps to send to a terminal later: from the
// command palette or a shortcut, as if typed. They live in the state file, and
// are managed on their own page of the sidebar.

type snippet struct {
	Name    string
	Command string
	// Run presses Enter after sending, so the command runs at once.
	Run bool
	// Shortcut sends the snippet from the terminal; the zero chord is none. It is
	// kept on this device only: the modifier that is free differs by platform.
	Shortcut shortcutChord

	// cells is what the page's table shows of it, made by refreshSnippets.
	cells [3]string
}

const (
	// maxSnippets bounds the list a damaged state file could carry, not a
	// working set: users migrating from other tools arrive with hundreds of
	// commands (#211), and a list of this size costs the page's table and
	// the command palette nothing noticeable.
	maxSnippets       = 512
	maxSnippetName    = 80
	maxSnippetCommand = 1000
)

// snippetsHits is the place of the controls this file paints.
var snippetsHits = tree.Feature("snippets")

var (
	hitSnippetAdd         = tree.ID(snippetsHits, "snippet-add")
	hitSnippetEdit        = tree.ID(snippetsHits, "snippet-edit")
	hitSnippetSend        = tree.ID(snippetsHits, "snippet-send")
	hitSnippetRemove      = tree.ID(snippetsHits, "snippet-remove")
	hitSnippetName        = tree.ID(snippetsHits, "snippet-name")
	hitSnippetCommand     = tree.ID(snippetsHits, "snippet-command")
	hitSnippetRun         = tree.ID(snippetsHits, "snippet-run")
	hitSnippetShortcut    = tree.ID(snippetsHits, "snippet-shortcut")
	hitSnippetShortcutOff = tree.ID(snippetsHits, "snippet-shortcut-clear")
	hitSnippetSubmit      = tree.ID(snippetsHits, "snippet-submit")
	hitSnippetCancel      = tree.ID(snippetsHits, "snippet-cancel")
	hitSnippetClose       = tree.ID(snippetsHits, "snippet-close")
	hitSnippetDismiss     = tree.ID(snippetsHits, "snippet-dismiss")
)

var (
	hitSnippetRowList  = tree.Sub(snippetsHits, tree.Name("snippet-row"))  // + snippet index
	hitSnippetSendList = tree.Sub(snippetsHits, tree.Name("snippet-send")) // the command palette's entries
	hitSnippetPickList = tree.Sub(snippetsHits, tree.Name("snippet-pick")) // + session ID: the terminals to send to
)

type snippetsUI struct {
	list []snippet

	// The dialog: which snippet it edits (-1 adds one) and the fields.
	editing     int
	name        text.Editor
	command     text.Editor
	run         bool
	shortcut    shortcutChord
	capturing   bool // the next chord pressed becomes shortcut
	dialogError string

	// The page: the selected row, its layout and the count under the table.
	selected  int
	layout    flex.View
	countText string
	// sendAt is where the menu of terminals opens from the Send button.
	sendAt ui.Point
}

// cleanSnippetText makes one line of what was typed: a snippet is sent as
// keystrokes, so a control character in it would be a key the user never meant.
func cleanSnippetText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = strings.ToValidUTF8(value[:limit], "")
	}
	return value
}

// snippetFor is the snippet a chord sends, or -1; skip is one that does not count
// (the one being edited).
func (state *State) snippetFor(chord shortcutChord, skip int) int {
	if !chord.valid() {
		return -1
	}
	for index := range state.snippets.list {
		if index != skip && state.snippets.list[index].Shortcut == chord {
			return index
		}
	}
	return -1
}

// snippetNamed is the name of the snippet a chord sends, or "".
func (state *State) snippetNamed(chord shortcutChord) string {
	if index := state.snippetFor(chord, -1); index >= 0 {
		return state.snippets.list[index].Name
	}
	return ""
}

// chordOwner says what already uses a chord: a built-in action, or a snippet
// other than the one at skip. It is "" when the chord is free.
func (state *State) chordOwner(chord shortcutChord, skip int) string {
	if action := state.shortcuts.actionFor(chord); action != shortcutNone {
		return state.shortcutActionLabel(action)
	}
	if index := state.snippetFor(chord, skip); index >= 0 {
		return state.snippets.list[index].Name
	}
	return ""
}

// dropSnippetChordsTaken unbinds the snippets whose chord a built-in action now
// uses, which is what resetting the shortcuts to their defaults can cause.
func (state *State) dropSnippetChordsTaken() {
	for index := range state.snippets.list {
		if chord := state.snippets.list[index].Shortcut; state.shortcuts.actionFor(chord) != shortcutNone {
			state.snippets.list[index].Shortcut = shortcutChord{}
		}
	}
	state.refreshSnippets()
}

func (state *State) openSnippetDialog(index int) {
	form := &state.snippets
	form.editing = index
	form.name, form.command = text.Editor{}, text.Editor{}
	form.run = true
	form.shortcut, form.capturing = shortcutChord{}, false
	form.dialogError = ""
	if index >= 0 && index < len(form.list) {
		form.name.Insert(form.list[index].Name)
		form.command.Insert(form.list[index].Command)
		form.run = form.list[index].Run
		form.shortcut = form.list[index].Shortcut
	}
	state.modal = modalSnippet
}

func (state *State) clickSnippetDialog(id ui.ID) {
	form := &state.snippets
	switch id {
	case hitSnippetSubmit:
		state.submitSnippet()
	case hitSnippetCancel, hitSnippetClose, hitSnippetDismiss:
		state.closeModal()
	case hitSnippetRun:
		form.run = !form.run
	case hitSnippetShortcut:
		form.capturing = !form.capturing
		form.dialogError = ""
	case hitSnippetShortcutOff:
		form.shortcut, form.capturing = shortcutChord{}, false
		form.dialogError = ""
	}
}

// captureSnippetShortcut takes the chord pressed while the dialog waits for one.
// ok is false for a key that is no chord at all, which is refused the same way
// as a chord a snippet may not use.
func (state *State) captureSnippetShortcut(event ui.KeyEvent, chord shortcutChord, ok bool) ui.EventResult {
	form, m := &state.snippets, state.messages
	result := ui.EventResult{Handled: true, Repaint: true}
	if event.Repeat {
		return ui.EventResult{Handled: true}
	}
	if !ok || !chord.validForSnippet() {
		form.dialogError = m.Tools.SnippetShortcutInvalid
		return result
	}
	if owner := state.chordOwner(chord, form.editing); owner != "" {
		form.dialogError = fmt.Sprintf(m.ShortcutsConflictFmt, owner)
		return result
	}
	form.shortcut, form.capturing = chord, false
	form.dialogError = ""
	return result
}

func (state *State) submitSnippet() {
	form, m := &state.snippets, state.messages.Tools
	entry := snippet{
		Name:     cleanSnippetText(form.name.String(), maxSnippetName),
		Command:  cleanSnippetText(form.command.String(), maxSnippetCommand),
		Run:      form.run,
		Shortcut: form.shortcut,
	}
	switch {
	case entry.Name == "":
		form.dialogError = m.SnippetErrorName
		return
	case entry.Command == "":
		form.dialogError = m.SnippetErrorCommand
		return
	}
	if form.editing >= 0 && form.editing < len(form.list) {
		form.list[form.editing] = entry
		form.selected = form.editing
	} else if len(form.list) >= maxSnippets {
		form.dialogError = m.SnippetErrorTooMany
		return
	} else {
		form.list = append(form.list, entry)
		form.selected = len(form.list) - 1
	}
	state.refreshSnippets()
	state.closeModal()
	state.save()
}

func (state *State) deleteSnippet(index int) {
	list := state.snippets.list
	if index < 0 || index >= len(list) {
		return
	}
	state.snippets.list = append(list[:index:index], list[index+1:]...)
	state.refreshSnippets()
	state.status = fmt.Sprintf(state.messages.Tools.StatusSnippetRemovedFmt, list[index].Name)
	state.save()
}

// sendSnippet types a snippet into the active terminal, and into the other panes
// of its tab while the tab is broadcasting.
func (state *State) sendSnippet(index int) bool {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil {
		state.showActionWarning(state.messages.Tools.SnippetsTitle, state.messages.Tools.StatusSnippetNoTerminal)
		return false
	}
	return state.sendSnippetTo(index, session)
}

// sendSnippetTo types a snippet into session, and into the other panes of its tab
// while the tab is broadcasting.
func (state *State) sendSnippetTo(index int, session *Session) bool {
	m := state.messages.Tools
	if index < 0 || index >= len(state.snippets.list) {
		return false
	}
	entry := state.snippets.list[index]
	input := make([]byte, 0, len(entry.Command)+1)
	input = append(input, entry.Command...)
	if entry.Run {
		input = append(input, '\r')
	}
	if !state.typed(session, input) {
		state.showActionWarning(m.SnippetsTitle, m.StatusSnippetNotConnected)
		return false
	}
	state.status = fmt.Sprintf(m.StatusSnippetSentFmt, entry.Name, session.Title)
	return true
}

// sendSnippetFromPage sends a snippet picked on the Snippets page. The page has
// no terminal of its own, so with one terminal open that is the target; with
// several, the menu of terminals opens at `at` and the user says which. The
// terminal it went to is shown; when nothing can be sent the page stays, with the
// reason in a global notice.
func (state *State) sendSnippetFromPage(index int, at ui.Point) {
	if index < 0 || index >= len(state.snippets.list) {
		return
	}
	var only *Session
	terminals := 0
	for i := range state.sessions {
		if state.sessions[i].term != nil {
			only = &state.sessions[i]
			terminals++
		}
	}
	switch {
	case terminals == 0:
		state.showActionWarning(state.messages.Tools.SnippetsTitle, state.messages.Tools.StatusSnippetNoTerminal)
	case terminals == 1:
		state.sendSnippetPicked(index, only)
	default:
		state.snippetMenu = snippetMenu{open: true, pick: true, index: index, at: at}
	}
}

// sendSnippetPicked sends to session and shows its terminal.
func (state *State) sendSnippetPicked(index int, session *Session) {
	if state.sendSnippetTo(index, session) {
		state.ActivatePane(session.ID)
		state.activeSection = sectionTerminal
	}
}

// sendSnippetOn is called while painting a snippet row: when this paint is the
// event pass of a double click on it, the snippet is sent. Consuming the press
// keeps its release from clicking whatever the terminal put under the pointer.
func (state *State) sendSnippetOn(canvas *ui.Canvas, id ui.ID, index int) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != ui.PointerDown || event.Button != ui.PointerButtonPrimary || event.ClickCount != 2 || state.modal != modalNone || state.activeSection != sectionSnippets {
		return
	}
	state.snippets.selected = index
	state.sendSnippetFromPage(index, ui.Point{X: event.X, Y: event.Y})
	canvas.Consume()
}

func (state *State) paintSnippetDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages.Tools, state.Theme()
	form := &state.snippets
	title := m.SnippetDialogAddTitle
	if form.editing >= 0 {
		title = m.SnippetDialogEditTitle
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitSnippetDismiss, CloseID: hitSnippetClose, InitialFocusID: hitSnippetName,
		ContentSize: ui.Size{Width: 560}, FitBody: true, BodyHeight: withError(204, form.dialogError), // two fields, a box and the shortcut
	})
	layout := dialog.Begin(title, "")
	entries := [...]struct {
		id          ui.ID
		label       string
		placeholder string
		value       string
	}{
		{hitSnippetName, m.SnippetName, m.SnippetNamePlaceholder, form.name.String()},
		{hitSnippetCommand, m.SnippetCommand, m.SnippetCommandPlaceholder, form.command.String()},
	}
	var fields [len(entries)]flex.Box
	var run, shortcutButtons flex.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		for index := range fields {
			fields[index] = f.Slot().Height(62).MarginSides(0, 0, 6, 0)
		}
		run = state.declareDialogCheckbox(f, m.SnippetRun, 7, 0)
		f.Row().Height(32).Items(flex.Center).MarginSides(12, 0, 0, 2).Children(func() {
			f.Text(m.SnippetShortcut).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(state.pal.text).LineHeight(20).Grow(1)
			shortcutButtons = f.Slot().Size(240, 32).Shrink(0)
		})
		if form.dialogError != "" {
			f.Text(form.dialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).LineHeight(18).MarginSides(15, 0, 0, 0)
		}
	})
	f.End()
	for index, entry := range entries {
		content := components.BeginField(canvas, theme, fields[index].Rect()).Paint(entry.label, "", "")
		focused := canvas.Focused() == entry.id
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID: entry.id, Label: entry.label, Value: entry.value, Placeholder: entry.placeholder,
			Focused: focused, PlatformEditing: focused && entry.value != "", Invalid: form.dialogError != "",
		})
	}
	state.paintDialogCheckbox(canvas, hitSnippetRun, m.SnippetRun, form.run, run)
	label := m.SnippetShortcutNone
	switch {
	case form.capturing:
		label = m.SnippetShortcutRecording
	case form.shortcut.valid():
		label = form.shortcut.label()
	}
	buttons := [...]components.ButtonAction{
		{ID: hitSnippetShortcut, Label: label, Variant: components.ButtonOutline},
		{ID: hitSnippetShortcutOff, Label: m.SnippetShortcutClear, Variant: components.ButtonGhost, Disabled: !form.shortcut.valid() && !form.capturing},
	}
	components.BeginButtonRow(canvas, theme, shortcutButtons.Rect()).Paint(buttons[:]...)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitSnippetCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitSnippetSubmit, Label: m.SnippetSave},
	)
	dialog.End()
}
