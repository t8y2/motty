package workbench

import (
	"fmt"
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
	// Lines sends a multi-line command one line at a time, each line submitted
	// with Enter before the next goes out, as the paste review's line-by-line
	// choice does; Run then decides only the last line. Without it the lines go
	// as one paste.
	Lines bool
	// Shortcut sends the snippet from the terminal; the zero chord is none. It is
	// kept on this device only: the modifier that is free differs by platform.
	Shortcut keymap.Chord

	// cells is what the page's table shows of it, made by refreshSnippets.
	cells [4]string
	// inlineDraft keeps unvalidated edits made in the Snippets page's inline
	// editor so switching selection or leaving the page does not discard them.
	inlineDraft *snippetInlineDraft
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
	hitSnippetAdd    = tree.ID(snippetsHits, "snippet-add")
	hitSnippetSend   = tree.ID(snippetsHits, "snippet-send")
	hitSnippetRemove = tree.ID(snippetsHits, "snippet-remove")
	// The delete confirmation's buttons.
	hitSnippetDeleteDismiss = tree.ID(snippetsHits, "snippet-delete-dismiss")
	hitSnippetDeleteClose   = tree.ID(snippetsHits, "snippet-delete-close")
	hitSnippetDeleteCancel  = tree.ID(snippetsHits, "snippet-delete-cancel")
	hitSnippetDeleteConfirm = tree.ID(snippetsHits, "snippet-delete-confirm")
	// hitTerminalSnippets is the terminal toolbar's button that lists the
	// snippets to send to that terminal.
	hitTerminalSnippets = tree.ID(snippetsHits, "terminal-snippets")

	// Tree panel controls on the Snippets page.
	hitSnippetTreeToggle         = tree.ID(snippetsHits, "snippet-tree-toggle")
	hitSnippetTreeSearchToggle   = tree.ID(snippetsHits, "snippet-tree-search-toggle")
	hitSnippetTreeSearch         = tree.ID(snippetsHits, "snippet-tree-search")
	hitSnippetTreeExpandAll      = tree.ID(snippetsHits, "snippet-tree-expand-all")
	hitSnippetTreeScroll         = tree.ID(snippetsHits, "snippet-tree-scroll")
	hitSnippetFolderMenuDismiss  = tree.ID(snippetsHits, "snippet-folder-menu-dismiss")
	hitSnippetFolderMenuAdd      = tree.ID(snippetsHits, "snippet-folder-menu-add")
	hitSnippetFolderMenuNewChild = tree.ID(snippetsHits, "snippet-folder-menu-new-child")
	hitSnippetFolderMenuRename   = tree.ID(snippetsHits, "snippet-folder-menu-rename")
	hitSnippetFolderMenuDelete   = tree.ID(snippetsHits, "snippet-folder-menu-delete")
	hitSnippetBlankMenuDismiss   = tree.ID(snippetsHits, "snippet-blank-menu-dismiss")
	hitSnippetBlankNewGroup      = tree.ID(snippetsHits, "snippet-blank-new-group")
	hitSnippetBlankNewSnippet    = tree.ID(snippetsHits, "snippet-blank-new-snippet")

	// Inline editor controls on the Snippets page.
	hitSnippetInlineName         = tree.ID(snippetsHits, "snippet-inline-name")
	hitSnippetInlineCommand      = tree.ID(snippetsHits, "snippet-inline-command")
	hitSnippetInlineGroup        = tree.ID(snippetsHits, "snippet-inline-group")
	hitSnippetInlineGroupTrigger = tree.ID(snippetsHits, "snippet-inline-group-trigger")
	hitSnippetInlineGroupClear   = tree.ID(snippetsHits, "snippet-inline-group-clear")
	hitSnippetInlineGroupDismiss = tree.ID(snippetsHits, "snippet-inline-group-dismiss")
	hitSnippetInlineGroupScroll  = tree.ID(snippetsHits, "snippet-inline-group-scroll")
	snippetInlineGroupScope      = tree.ID(snippetsHits, "snippet-inline-group-scope")
	hitSnippetInlineRun          = tree.ID(snippetsHits, "snippet-inline-run")
	hitSnippetInlineLines        = tree.ID(snippetsHits, "snippet-inline-lines")
	hitSnippetInlineShortcut     = tree.ID(snippetsHits, "snippet-inline-shortcut")
	hitSnippetInlineShortcutOff  = tree.ID(snippetsHits, "snippet-inline-shortcut-clear")
)

var (
	hitSnippetSendList           = tree.Sub(snippetsHits, tree.Name("snippet-send")) // the command palette's entries
	hitSnippetPickList           = tree.Sub(snippetsHits, tree.Name("snippet-pick")) // + session ID: the terminals to send to
	hitSnippetFolderMore         = tree.Sub(snippetsHits, tree.Name("snippet-folder-more"))
	hitSnippetInlineGroupChoices = tree.Sub(snippetsHits, tree.Name("snippet-inline-group-option"))
)

type snippetsUI struct {
	list []snippet

	// The page's group state: the groups there are, how many snippets each
	// names, and the list indices visible under the active filter/search.
	customGroups []string
	groups       []string
	groupCounts  map[string]int
	hasUngrouped bool
	visible      []int
	filter       snippetFilter

	// The page: the selected snippet and the count under the tree.
	selected  int
	countText string
	// sendAt is where the menu of terminals opens from the Send button.
	sendAt gfx.Point
	// deleting is the snippet index plus one the delete confirmation asks about;
	// zero means no confirmation is open.
	deleting int

	// Directory-tree browser state on the Snippets page.
	tree                ui.TreeState
	treeWidth           float32
	treeCollapsed       bool
	treeDrawer          bool
	treeSplitter        ui.SplitterState
	treeSearchOpen      bool
	treeSearching       bool
	treeSearch          text.Editor
	treeSearchField     ui.Field
	treeSearchBound     bool
	treeFocus           gfx.ID
	treeRows            []snippetTreeEntry
	treeCollapsedGroups map[string]bool
	treeFolderTokens    map[string]int
	selectedFolder      string
	treeC               ui.C
	treeNode            func(int) ui.TreeNode
	treeLabel           func(int) string
	treeContent         func(int)
	treeKey             func(int) int
	treeCanDrop         func(int, int, ui.TreeDrop) bool
	treeAfterLayout     func(*ui.DrawContext)
	drawInlineFields    func(*ui.DrawContext)
	folderMenu          snippetFolderMenu
	blankMenu           snippetBlankMenu

	// Inline editor state on the Snippets page.
	inline snippetInlineEditor
}

// snippetTreeEntry is one visible row in the Snippets directory tree: either a
// group folder or a snippet leaf.
type snippetTreeEntry struct {
	folder    bool
	group     string
	groupKey  string
	label     string
	countText string
	depth     int
	snippet   int // index into snippetsUI.list, or -1 for folders
}

// snippetFolderMenu is the context menu for a folder row in the Snippets tree.
type snippetFolderMenu struct {
	open  bool
	group string
	at    gfx.Point
}

// snippetBlankMenu is the context menu opened by right-clicking the blank area
// of the Snippets directory tree.
type snippetBlankMenu struct {
	open bool
	at   gfx.Point
}

// snippetInlineDraft preserves unvalidated inline editor text for an existing
// snippet across selection switches and page changes.
type snippetInlineDraft struct {
	name    string
	command string
	error   string
}

// snippetNewDraft preserves an unsaved new-snippet draft when the user switches
// to view another snippet before finishing it.
type snippetNewDraft struct {
	name    string
	command string
	snap    snippet
	error   string
}

// snippetInlineEditor holds the Snippets page's right-pane inline editor state.
type snippetInlineEditor struct {
	draft       bool
	savedDraft  *snippetNewDraft
	loadedValid bool
	loadedIndex int
	loadedSnap  snippet
	name        text.Editor
	command     text.Editor
	nameForm    ui.FormFieldState
	nameField   ui.Field
	nameBound   bool
	commandArea dialogArea
	groupPicker components.SingleSelectState
	groupSlot   ui.Box
	capturing   bool
	error       string
	breadcrumb  string
	shortcutLbl string
	dirty       bool
	saveTimer   *time.Timer
	saveDelay   time.Duration
	saveSeq     uint64
	lastFocus   gfx.ID
}

// resetSnippetPreview resets transient previews/menus after the list changes.
func (form *snippetsUI) resetSnippetPreview() {
	form.inline.capturing = false
	form.inline.error = ""
	form.folderMenu.open = false
	form.blankMenu.open = false
}

// syncRowSelection keeps the tree's selection aligned with form.selected.
func (form *snippetsUI) syncRowSelection() {
	for i, row := range form.treeRows {
		if !row.folder && row.snippet == form.selected {
			form.tree.Select(i)
			return
		}
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
	state.flushInlineSnippetSave()
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
	group := form.list[index].Group
	for target := index + direction; target >= 0 && target < len(form.list); target += direction {
		if form.filter.matches(&form.list[target]) && sameSnippetGroup(form.list[target].Group, group) {
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
	form.resetSnippetPreview()
	state.refreshSnippets()
	state.save()
	return true
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
	if entry.Lines && strings.Contains(command, "\n") {
		if !state.startPasteQueue(session, splitPasteLines(command), entry.Run) {
			state.showActionWarning(m.SnippetsTitle, m.StatusSnippetNotConnected)
			return false
		}
		state.status = fmt.Sprintf(m.StatusSnippetSentFmt, entry.Name, session.Title)
		return true
	}
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
	state.flushInlineSnippetSave()
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
		state.setActiveSection(sectionTerminal)
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
