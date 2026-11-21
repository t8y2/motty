package workbench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/configstore"
	"motty/internal/term"
)

// Snippets are commands the user keeps to send to a terminal later: from the
// command palette or a shortcut, as if typed. They live in the state file, and
// are managed on their own page of the sidebar.

type snippet struct {
	Name    string
	Command string
	// Group names the group the snippet is listed under; "" is none.
	Group string
	// Run presses Enter after sending, so the command runs at once.
	Run bool
	// Shortcut sends the snippet from the terminal; the zero chord is none. It is
	// kept on this device only: the modifier that is free differs by platform.
	Shortcut keymap.Chord

	// cells is what the page's table shows of it, made by refreshSnippets.
	cells [4]string
}

const (
	// maxSnippets bounds the list a damaged state file could carry, not a
	// working set: users migrating from other tools arrive with hundreds of
	// commands (#211), and a list of this size costs the page's table and
	// the command palette nothing noticeable.
	maxSnippets    = 512
	maxSnippetName = 80
	// maxSnippetCommand bounds one command. A snippet is one configstore record,
	// and the store refuses a payload over its MaxPayload; the editor accepts up
	// to this and submitSnippet refuses a command whose record would not fit
	// instead of cutting it. A long script or README must round-trip whole.
	maxSnippetCommand = configstore.MaxPayload
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
	// The delete confirmation's buttons.
	hitSnippetDeleteDismiss = tree.ID(snippetsHits, "snippet-delete-dismiss")
	hitSnippetDeleteClose   = tree.ID(snippetsHits, "snippet-delete-close")
	hitSnippetDeleteCancel  = tree.ID(snippetsHits, "snippet-delete-cancel")
	hitSnippetDeleteConfirm = tree.ID(snippetsHits, "snippet-delete-confirm")
	// hitTerminalSnippets is the terminal toolbar's button that lists the
	// snippets to send to that terminal.
	hitTerminalSnippets = tree.ID(snippetsHits, "terminal-snippets")
)

var (
	hitSnippetExpandList = tree.Sub(snippetsHits, tree.Name("snippet-expand")) // + snippet index
	hitSnippetSort       = tree.Sub(snippetsHits, tree.Name("snippet-sort"))   // + column index
	hitSnippetSendList   = tree.Sub(snippetsHits, tree.Name("snippet-send"))   // the command palette's entries
	hitSnippetPickList   = tree.Sub(snippetsHits, tree.Name("snippet-pick"))   // + session ID: the terminals to send to
	// hitSnippetHoverCard is the preview card the pointer rests on to keep open.
	hitSnippetHoverCard = tree.ID(snippetsHits, "snippet-hover-card")
)

type snippetsUI struct {
	list []snippet

	// The page's group filter: the groups there are, how many snippets each
	// names, the list indices it shows in the order it shows them, and the
	// integer choices its Select is over.
	groups        []string
	groupCounts   map[string]int
	hasUngrouped  bool
	visible       []int
	filter        snippetFilter
	filterChoice  int
	filterChoices []int

	// The dialog: which snippet it edits (-1 adds one) and the fields.
	editing     int
	name        text.Editor
	command     text.Editor
	commandArea dialogArea
	run         bool
	groupPicker components.SingleSelectState
	shortcut    keymap.Chord
	capturing   bool // the next chord pressed becomes shortcut
	dialogError string

	// nameForm and nameText are the name field's row and text, bound to name. The boxes are
	// the slots the dialog body paints the other controls in.
	nameForm       ui.FormFieldState
	nameText       ui.Field
	nameBound      bool
	dialogGroup    ui.Box
	dialogShortcut ui.Box

	// The page: the selected row and the count under the table.
	selected  int
	sort      components.TableSortState
	countText string
	// selectedRow is the place selected occupies in visible as of this paint,
	// so the first paint shows a choice the list has not been told yet.
	selectedRow int
	// rows is the page's list: its rows are the snippets the filter shows, and
	// are dragged or moved with Alt and the arrows while the page shows its
	// manual order. rowDecl, reorder and the row hooks are made once, so a paint
	// does not allocate them again.
	rows         ui.ListState
	rowDecl      func(ui.ListRow)
	reorder      func(rows []int, to int)
	handleDraw   func(*ui.DrawContext)
	detailAccent func(*ui.DrawContext)
	// detailHeight and detailLineHeight are the band the expanded command takes,
	// measured when the page declares the scroll area.
	detailHeight     float32
	detailLineHeight float32
	// sendAt is where the menu of terminals opens from the Send button.
	sendAt gfx.Point
	// deleting is the snippet index plus one the delete confirmation asks about;
	// zero means no confirmation is open.
	deleting int

	// expanded is the snippet index plus one whose command shows below its row;
	// zero hides every detail. detailLines and detailCache are the wrapped
	// command, reused across paints.
	expanded    int
	detailLines []string
	detailCache gfx.EllipsisCache

	// The command preview that opens after the pointer rests on a multi-line
	// row: hoverTarget and hoverSince time the rest, while hoverOpen, hoverRow
	// and hoverAnchor keep the card painted when the pointer moves onto it.
	hoverTarget gfx.ID
	hoverSince  time.Duration
	hoverOpen   bool
	hoverRow    int
	hoverAnchor ui.Box
	cardLines   []string
	cardCache   gfx.EllipsisCache
	// hoverCandidate and hoverCandidateAnchor are the row the pointer is on in
	// this paint, before the rest has opened the card. hoverName, hoverLineHeight
	// and hoverBody are what the popover shows.
	hoverCandidate       int
	hoverCandidateAnchor ui.Box
	hoverC               ui.C
	hoverName            string
	hoverLineHeight      float32
	hoverBody            func()
	// detailName and detailCommand name the expanded row for assistive
	// technology, without a closure per paint.
	detailName    string
	detailCommand string
}

// resetSnippetPreview closes the expanded row and the command preview after the
// list changed, so neither still points at a command the user no longer has.
func (form *snippetsUI) resetSnippetPreview() {
	form.expanded = 0
	form.hoverTarget, form.hoverOpen, form.hoverRow = gfx.ID{}, false, 0
	form.hoverAnchor = ui.Box{}
}

// syncRowSelection makes the list's keyboard row follow selected after the list
// or its order changed; a user's own choice is read back from the list instead.
func (form *snippetsUI) syncRowSelection() {
	row := slices.Index(form.visible, form.selected)
	form.rows.Select(row)
}

// prepareSnippetRows makes the list's callbacks once, so a paint reuses them.
func (form *snippetsUI) prepareSnippetRows(state *State) {
	if form.rowDecl != nil {
		return
	}
	form.rowDecl = state.declareSnippetRow
	form.reorder = func(rows []int, to int) { state.moveSnippetRows(rows, to) }
	form.handleDraw = state.paintSnippetHandle
	form.detailAccent = state.paintSnippetDetailAccent
	form.rows.NoTypeahead = true
	// Rows are named by their place, not by the snippet's index: reordering
	// changes an index's owner, and the list's Alt+arrow focus follows the place.
	form.rows.Key = nil
	form.rows.Label = func(i int) string {
		if i < 0 || i >= len(form.visible) {
			return ""
		}
		return form.list[form.visible[i]].Name
	}
}

// cleanSnippetText makes one line of what was typed: a snippet is sent as
// keystrokes, so a control character in it would be a key the user never meant.
// Surrounding space is not part of a name.
func cleanSnippetText(value string, limit int) string {
	return strings.TrimSpace(cleanSnippetLine(value, limit))
}

// sanitizeSnippetCommand makes what was typed safe to send: CRLF and lone CR
// become the newline the editor shows, and a control character is dropped
// because sending it would be a key the user never meant. It keeps the
// surrounding space: "cd " or "ssh " is sent to be finished by hand, and a
// leading space keeps a command out of the shell's history.
func sanitizeSnippetCommand(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(character rune) rune {
		if character < 0x20 && character != '\n' || character == 0x7f {
			return -1
		}
		return character
	}, value)
}

// cleanSnippetCommand is sanitizeSnippetCommand with a damaged or old state
// file's oversized command bounded to limit, so one bad record cannot make the
// editor or the next save carry an arbitrarily large string.
func cleanSnippetCommand(value string, limit int) string {
	value = sanitizeSnippetCommand(value)
	if limit > 0 && len(value) > limit {
		value = strings.ToValidUTF8(value[:limit], "")
	}
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return value
}

func cleanSnippetLine(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(value) > limit {
		value = strings.ToValidUTF8(value[:limit], "")
	}
	return value
}

// snippetFor is the snippet a chord sends, or -1; skip is one that does not count
// (the one being edited).
func (state *State) snippetFor(chord keymap.Chord, skip int) int {
	if !chord.Valid() {
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
func (state *State) snippetNamed(chord keymap.Chord) string {
	if index := state.snippetFor(chord, -1); index >= 0 {
		return state.snippets.list[index].Name
	}
	return ""
}

// chordOwner says what already uses a chord: a built-in action, or a snippet
// other than the one at skip. It is "" when the chord is free.
func (state *State) chordOwner(chord keymap.Chord, skip int) string {
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
			state.snippets.list[index].Shortcut = keymap.Chord{}
		}
	}
	state.refreshSnippets()
}

func (state *State) openSnippetDialog(index int) {
	form := &state.snippets
	form.editing = index
	form.name, form.command = text.Editor{}, text.Editor{}
	form.commandArea = dialogArea{}
	form.run = true
	group := ""
	form.shortcut, form.capturing = keymap.Chord{}, false
	form.dialogError = ""
	if index >= 0 && index < len(form.list) {
		form.name.Insert(form.list[index].Name)
		form.command.Insert(form.list[index].Command)
		form.run = form.list[index].Run
		form.shortcut = form.list[index].Shortcut
		group = form.list[index].Group
	}
	state.snippetGroupPicker(group)
	state.modal = modalSnippet
}

func (state *State) clickSnippetDialog(id gfx.ID) {
	form := &state.snippets
	if state.clickSnippetGroup(id) {
		return
	}
	switch id {
	case hitSnippetSubmit:
		state.submitSnippet()
	case hitSnippetCancel, hitSnippetClose:
		state.closeModal()
	case hitSnippetRun:
		form.run = !form.run
		form.groupPicker.Open = false
	case hitSnippetShortcut:
		form.capturing = !form.capturing
		form.dialogError = ""
	case hitSnippetShortcutOff:
		form.shortcut, form.capturing = keymap.Chord{}, false
		form.dialogError = ""
	case hitSnippetDialogGroups:
		state.openSnippetGroupsManagerFor(true)
	}
}

// captureSnippetShortcut takes the chord pressed while the dialog waits for one.
// ok is false for a key that is no chord at all, which is refused the same way
// as a chord a snippet may not use.
func (state *State) captureSnippetShortcut(event gfx.KeyEvent, chord keymap.Chord, ok bool) gfx.EventResult {
	form, m := &state.snippets, state.messages
	result := gfx.EventResult{Handled: true, Repaint: true}
	if event.Repeat {
		return gfx.EventResult{Handled: true}
	}
	if !ok || !validForSnippet(chord) {
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

// composingTarget returns the field whose input method still holds marked
// text. Saving then would persist the preedit verbatim, so the dialog waits for
// the input method to commit it; an empty range marks nothing to protect.
func (form *snippetsUI) composingTarget() gfx.ID {
	fields := [...]struct {
		id     gfx.ID
		editor *text.Editor
	}{{hitSnippetName, &form.name}, {hitSnippetCommand, &form.command}, {hitSnippetGroup, &form.groupPicker.Query}}
	for _, field := range fields {
		if marked, composing := field.editor.Composition(); composing && marked.Start != marked.End {
			return field.id
		}
	}
	return gfx.ID{}
}

func (form *snippetsUI) composing() bool {
	return form.composingTarget() != gfx.ID{}
}

// abandonOther ends the composition of the field the pointer is leaving. The
// input method drops its marked text when the text input moves; the editor has
// to drop it too, or the field the user left keeps composing and blocks every
// later save. Unmark keeps the text, which is what the user already sees.
func (form *snippetsUI) abandonOther(target gfx.ID) {
	switch target {
	case hitSnippetName, hitSnippetCommand, hitSnippetGroup:
	default:
		return
	}
	if target != hitSnippetName {
		form.name.Unmark()
	}
	if target != hitSnippetCommand {
		form.command.Unmark()
	}
	if target != hitSnippetGroup {
		form.groupPicker.Query.Unmark()
	}
}

// snippetOrder is the place the edited snippet occupies, or the place a new one
// would take. It is part of the record the save measures.
func (form *snippetsUI) snippetOrder() int {
	if form.editing >= 0 && form.editing < len(form.list) {
		return form.editing
	}
	return len(form.list)
}

func (state *State) submitSnippet() {
	form, m := &state.snippets, state.messages.Tools
	if form.composing() || !state.commitSnippetGroupQuery() {
		return
	}
	entry := snippet{
		Group:    cleanSnippetGroup(form.groupPicker.Value()),
		Name:     cleanSnippetText(form.name.String(), maxSnippetName),
		Command:  sanitizeSnippetCommand(form.command.String()),
		Run:      form.run,
		Shortcut: form.shortcut,
	}
	switch {
	case entry.Name == "":
		form.dialogError = m.SnippetErrorName
		return
	case strings.TrimSpace(entry.Command) == "":
		form.dialogError = m.SnippetErrorCommand
		return
	case len(entry.Command) > maxSnippetCommand || !snippetRecordFits(entry, form.snippetOrder()):
		form.dialogError = m.SnippetErrorTooLong
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
	form.showSnippet(form.selected)
	form.resetSnippetPreview()
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
	state.snippets.resetSnippetPreview()
	state.refreshSnippets()
	state.status = fmt.Sprintf(state.messages.Tools.StatusSnippetRemovedFmt, list[index].Name)
	state.save()
}

// openSnippetDelete asks before deleting a snippet: a saved command may be the
// only copy the user has.
func (state *State) openSnippetDelete(index int) {
	if index < 0 || index >= len(state.snippets.list) {
		return
	}
	state.snippets.deleting = index + 1
	state.modal = modalSnippetDelete
}

// clickSnippetDeleteDialog runs the delete confirmation.
func (state *State) clickSnippetDeleteDialog(id gfx.ID) {
	switch id {
	case hitSnippetDeleteDismiss, hitSnippetDeleteClose, hitSnippetDeleteCancel:
		state.snippets.deleting = 0
		state.closeModal()
	case hitSnippetDeleteConfirm:
		index := state.snippets.deleting - 1
		state.snippets.deleting = 0
		state.closeModal()
		state.deleteSnippet(index)
	}
}

func (state *State) snippetNeighbor(index, direction int) int {
	form := &state.snippets
	if index < 0 || index >= len(form.list) {
		return -1
	}
	for target := index + direction; target >= 0 && target < len(form.list); target += direction {
		if form.filter.matches(&form.list[target]) {
			return target
		}
	}
	return -1
}

func (state *State) moveSnippet(index, direction int) bool {
	target := state.snippetNeighbor(index, direction)
	if target < 0 {
		return false
	}
	form := &state.snippets
	form.list[index], form.list[target] = form.list[target], form.list[index]
	form.selected = target
	form.sort = components.TableSortState{}
	form.resetSnippetPreview()
	state.refreshSnippets()
	state.save()
	return true
}

// moveSnippetTo puts the snippet at source before position to in the manual
// order, as ListState.Reorder says: to is the row it goes before, len(list) for
// the end. It is the page's reorder callback.
func (state *State) moveSnippetTo(source, to int) bool {
	form := &state.snippets
	n := len(form.list)
	if source < 0 || source >= n || to < 0 || to > n || source == to ||
		form.sort.Direction != components.TableSortNone || !form.filter.all() {
		return false
	}
	entry := form.list[source]
	if source < to {
		to--
		if to == source {
			return false
		}
		copy(form.list[source:to], form.list[source+1:to+1])
	} else {
		copy(form.list[to+1:source+1], form.list[to:source])
	}
	form.list[to] = entry
	form.selected = to
	form.resetSnippetPreview()
	state.refreshSnippets()
	state.save()
	return true
}

// moveSnippetRows is the ListState.Reorder callback: it moves the rows the list
// carries, given by their place in visible, to before place to.
func (state *State) moveSnippetRows(rows []int, to int) {
	form := &state.snippets
	if len(rows) == 0 || form.sort.Direction != components.TableSortNone || !form.filter.all() {
		return
	}
	source := rows[0]
	if source < 0 || source >= len(form.visible) {
		return
	}
	state.moveSnippetTo(form.visible[source], to)
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

// sendResolvedSnippet types command into session, and into the other panes of its tab
// while the tab is broadcasting.
func (state *State) sendResolvedSnippet(entry snippet, command string, session *Session) bool {
	m := state.messages.Tools
	input := term.EncodePaste(command, session.term.screen.Modes().BracketedPaste && strings.Contains(command, "\n"))
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

// sendSnippetTo types a snippet into session, and into the other panes of its tab
// while the tab is broadcasting. If dynamic variables are present, it opens the
// variable input dialog instead.
func (state *State) sendSnippetTo(index int, session *Session) bool {
	if index < 0 || index >= len(state.snippets.list) {
		return false
	}
	entry := state.snippets.list[index]
	if snippetHasVariables(entry.Command) {
		state.openSnippetVariablesDialog(index, session.ID, false)
		return true
	}
	return state.sendResolvedSnippet(entry, entry.Command, session)
}

// sendSnippetFromPage sends a snippet picked on the Snippets page. The page has
// no terminal of its own, so with one terminal open that is the target; with
// several, the menu of terminals opens at `at` and the user says which. The
// terminal it went to is shown; when nothing can be sent the page stays, with the
// reason in a global notice.
func (state *State) sendSnippetFromPage(index int, at gfx.Point) {
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
		state.snippetMenu = snippetMenu{open: true, pick: true, index: index, name: state.snippets.list[index].Name, at: at}
	}
}

// sendSnippetPicked sends to session and shows its terminal.
func (state *State) sendSnippetPicked(index int, session *Session) {
	if index < 0 || index >= len(state.snippets.list) {
		return
	}
	entry := state.snippets.list[index]
	if snippetHasVariables(entry.Command) {
		state.openSnippetVariablesDialog(index, session.ID, true)
		return
	}
	if state.sendResolvedSnippet(entry, entry.Command, session) {
		state.ActivatePane(session.ID)
		state.activeSection = sectionTerminal
		// The page had the keyboard, and the page is what the click was on. The
		// terminal is now on screen, so ask the next paint for its keyboard;
		// otherwise the command arrives but typing goes nowhere until a click.
		state.returnKeyboardToTerminal()
	}
}

// sendSnippetOn is called while painting a snippet row: when this paint is the
// event pass of a double click on it, the snippet is sent. Consuming the press
// keeps its release from clicking whatever the terminal put under the pointer.
func (state *State) sendSnippetOn(canvas *gfx.Canvas, id gfx.ID, index int) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary || event.ClickCount != 2 || state.modal != modalNone || state.activeSection != sectionSnippets {
		return
	}
	state.snippets.selected = index
	state.sendSnippetFromPage(index, gfx.Point{X: event.X, Y: event.Y})
	canvas.Consume()
}

// declareSnippetDeleteDialog asks before the page deletes a snippet.
func (state *State) declareSnippetDeleteDialog(c ui.C) {
	m, form := state.messages.Tools, &state.snippets
	name := ""
	if form.deleting > 0 && form.deleting <= len(form.list) {
		name = form.list[form.deleting-1].Name
	}
	description := fmt.Sprintf(m.SnippetDeleteBodyFmt, name)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, description, 0)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SnippetDeleteTitle, Description: description,
		Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitSnippetDeleteDismiss, CloseID: hitSnippetDeleteClose,
		InitialFocusID: hitSnippetDeleteCancel,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitSnippetDeleteCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitSnippetDeleteConfirm, Label: m.SnippetDeleteConfirm, Variant: components.ButtonDestructive},
		)
	})
}

// declareSnippetDialog declares the snippet editor as a dialog of the workbench
// page.
func (state *State) declareSnippetDialog(c ui.C) {
	m := state.messages.Tools
	form := &state.snippets
	title := m.SnippetDialogAddTitle
	if form.editing >= 0 {
		title = m.SnippetDialogEditTitle
	}
	viewport := dialogViewport(c)
	// The name row, then the command box and the rows under it.
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 560, "", withError(282+dialogRowHeight(c.Theme()), form.dialogError))
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 560, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitSnippetClose, InitialFocusID: hitSnippetName,
	})
	dialog.Body(func() { state.declareSnippetDialogBody(c) })
	dialog.Footer(func() {
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickSnippetDialog(hitSnippetCancel) })
		}
		if dialogButton(c, m.SnippetSave, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickSnippetDialog(hitSnippetSubmit) })
		}
	})
}

// declareSnippetDialogBody lays out the snippet editor: name, command, group,
// the run box and the shortcut row, and paints the fields into their slots.
func (state *State) declareSnippetDialogBody(c ui.C) {
	form, m := &state.snippets, state.messages.Tools
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		if !form.nameBound {
			form.nameText.Bind(&form.name)
			form.nameBound = true
		}
		form.nameText.Invalid = form.dialogError != ""
		ui.FormField(c, &form.nameForm, m.SnippetName).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
			ui.TextFieldID(c, hitSnippetName, &form.nameText, m.SnippetName, m.SnippetNamePlaceholder)
		})
		ui.Column(c).Height(140).MarginSides(0, 0, 6, 0).Children(func() {
			form.commandArea.declare(c, hitSnippetCommand, &form.command, m.SnippetCommand, m.SnippetCommandPlaceholder, 140)
		})
		if len(form.groups) > 0 {
			ui.Row(c).Height(62).MarginSides(0, 0, 6, 0).Gap(8).Items(ui.Center).Children(func() {
				form.dialogGroup = ui.Slot(c).Grow(1).Height(62)
				groupsWidth := toolbarButtonWidth(c.Canvas(), m.SnippetGroupsManage, false)
				dialogButton(c, m.SnippetGroupsManage, components.ButtonGhost, false).
					Hit(hitSnippetDialogGroups, m.SnippetGroupsManage).Width(groupsWidth).Height(32)
			})
		} else {
			form.dialogGroup = ui.Slot(c).Height(62).MarginSides(0, 0, 6, 0)
		}
		state.declareDialogCheck(c, hitSnippetRun, m.SnippetRun, form.run, 7, 0)
		ui.Row(c).Height(32).Items(ui.Center).MarginSides(12, 0, 0, 2).Children(func() {
			ui.Text(c, m.SnippetShortcut).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text).LineHeight(20).Grow(1)
			label := m.SnippetShortcutNone
			switch {
			case form.capturing:
				label = m.SnippetShortcutRecording
			case form.shortcut.Valid():
				label = chordLabel(form.shortcut)
			}
			form.dialogShortcut = ui.Row(c).Size(240, 32).Shrink(0).Justify(ui.End).Gap(8)
			form.dialogShortcut.Children(func() {
				// The shortcut row's buttons keep the height they had when the
				// row was painted by hand.
				ui.Button(c, label).Outline().Height(28).Hit(hitSnippetShortcut, label)
				ui.Button(c, m.SnippetShortcutClear).Ghost().Height(28).Disabled(!form.shortcut.Valid() && !form.capturing).Hit(hitSnippetShortcutOff, m.SnippetShortcutClear)
			})
		})
		if form.dialogError != "" {
			ui.Text(c, form.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(18).MarginSides(15, 0, 0, 0)
		}
	})
	column.DrawOver(func(d *ui.DrawContext) { state.paintSnippetDialogFields(d.Canvas) })
}

// paintSnippetDialogFields paints the snippet editor's group picker into the
// slot the body laid out; the shortcut is page buttons.
func (state *State) paintSnippetDialogFields(canvas *gfx.Canvas) {
	form := &state.snippets
	state.paintSnippetGroupField(canvas, form.dialogGroup.Rect())
}
