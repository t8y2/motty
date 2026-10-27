package workbench

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

// terminalMenuHits is the place of the controls this file paints.
var terminalMenuHits = tree.Feature("terminal-menu")

// Two more rows of the menu, outside the range the window's other menus use.
var (
	hitTerminalMenuSave    = tree.ID(terminalMenuHits, "terminal-menu-save")
	hitTerminalMenuSearch  = tree.ID(terminalMenuHits, "terminal-menu-search")
	hitTerminalMenuHistory = tree.ID(terminalMenuHits, "terminal-menu-history")
)

// maxMenuActions bounds what the menu offers for the text under the pointer.
const maxMenuActions = 4

// hitTerminalMenuAction is the row of one detected action.
var hitTerminalMenuAction = [maxMenuActions]ui.ID{
	tree.ID(terminalMenuHits, "terminal-menu-action-0"), tree.ID(terminalMenuHits, "terminal-menu-action-1"),
	tree.ID(terminalMenuHits, "terminal-menu-action-2"), tree.ID(terminalMenuHits, "terminal-menu-action-3"),
}

// maxSearchQuery bounds what a web search is sent.
const maxSearchQuery = 300

// terminalMenu is the right-click menu of a terminal: where it was asked for and
// which session it acts on. Closed, it costs nothing.
type terminalMenu struct {
	open    bool
	session uint32
	at      ui.Point // window coordinates of the press that opened it
	pointer ui.Point // window coordinates of the latest pointer event
	// found is what can be done with the text the menu was opened on.
	found [maxMenuActions]detected
	count int
}

// terminalMenuPointer closes an open menu on a press of any button outside it:
// the backdrop swallows the press, and only a primary one is also a click.
func (state *State) terminalMenuPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if !state.terminalMenu.open || event.Target != hitTerminalMenuDismiss || event.Kind != ui.PointerDown || event.Button == ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	state.terminalMenu.open = false
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) openTerminalMenu(session *Session) {
	state.terminalMenu = terminalMenu{open: true, session: session.ID, at: state.terminalMenu.pointer, pointer: state.terminalMenu.pointer}
}

// offerActionsAt lets the open menu offer what can be done with the text at a
// cell: connect to an address, show a path in the file browser, unpack an archive.
func (state *State) offerActionsAt(session *Session, position term.Position) {
	menu := &state.terminalMenu
	user := ""
	if !session.isLocal() {
		user = session.host.User
	}
	for _, item := range classifyToken(session.term.screen.TokenAt(position), user) {
		if menu.count == maxMenuActions {
			break
		}
		if !state.actionAvailable(session, item) {
			continue
		}
		menu.found[menu.count] = item
		menu.count++
	}
}

// actionAvailable is whether an action can be taken from this session now.
func (state *State) actionAvailable(session *Session, item detected) bool {
	switch item.kind {
	case actionFiles:
		return session.isLocal() || session.conn != nil && session.hasServer()
	case actionExtract:
		return session.term.shell != nil && session.host.System != sshconn.SystemWindows && !(session.isLocal() && runtime.GOOS == "windows")
	}
	return true
}

// maxActionLabel is how many characters of a row the menu shows before it cuts.
const maxActionLabel = 30

// actionLabel is a menu row for a detected action, in the current language. The
// text it names is shortened, in the middle, until the whole row fits the menu.
func (state *State) actionLabel(item detected) string {
	m := state.messages.Tools
	format := m.ActionCopyFmt
	switch item.kind {
	case actionSSH:
		format = m.ActionSSHFmt
	case actionBrowser:
		format = m.ActionBrowserFmt
	case actionFiles:
		format = m.ActionFilesFmt
	case actionExtract:
		format = m.ActionExtractFmt
	}
	label := []rune(item.label)
	if over := len([]rune(fmt.Sprintf(format, item.label))) - maxActionLabel; over > 0 && len(label) > over+1 {
		keep := len(label) - over - 1 // one rune goes to the ellipsis
		head := (keep + 1) / 2
		label = append(append(label[:head:head], '…'), label[len(label)-(keep-head):]...)
	}
	return fmt.Sprintf(format, string(label))
}

// runAction takes the action the user chose from the menu.
func (state *State) runAction(session *Session, item detected) {
	switch item.kind {
	case actionSSH:
		parsed, err := parseConnection(item.target)
		if err != nil {
			state.status = fmt.Sprintf(state.messages.ErrBadTargetFmt, item.target)
			return
		}
		state.connectParsed(parsed)
	case actionBrowser:
		state.openExternal(item.target)
	case actionCopy:
		if err := state.options.SetClipboardText(item.target); err != nil {
			state.status = err.Error()
		}
	case actionFiles:
		state.showInFiles(session, item.target)
	case actionExtract:
		// Typed, not run: the user reads the line and presses Enter.
		state.typed(session, []byte(item.target))
	}
}

// showInFiles opens the file panel on the folder that holds a path.
func (state *State) showInFiles(session *Session, target string) {
	dir := path.Dir(target)
	if strings.HasSuffix(target, "/") {
		dir = path.Clean(target)
	}
	state.Click(hitSFTPOpen)
	view := session.files
	switch {
	case view == nil:
	case view.local:
		state.listLocal(view, filepath.FromSlash(dir))
	default:
		state.openSFTPDirectory(view, dir)
	}
}

// terminalMenuSession is the session an open menu acts on, if it is still there.
func (state *State) terminalMenuSession() (*Session, bool) {
	if !state.terminalMenu.open || state.activeSection != sectionTerminal || state.modal != modalNone {
		return nil, false
	}
	session, ok := state.sessionByIDPointer(state.terminalMenu.session)
	return session, ok && session.term != nil
}

// terminalMenuSplits reports whether the session is one pane of a split tab, so
// closing it takes away a pane rather than the whole tab.
func (state *State) terminalMenuSplits(session *Session) bool {
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	return ok && len(state.deck.Panes(workspaceID, nil)) > 1
}

func (state *State) paintTerminalMenu(canvas *ui.Canvas, viewport ui.Rect) {
	session, ok := state.terminalMenuSession()
	if !ok {
		state.terminalMenu.open = false
		return
	}
	messages := state.messages
	live := session.term.shell != nil
	hasSelection := session.term.selection.selected
	// One keycap per key of the bound chord. The slices are fixed-size scratch,
	// so painting the menu allocates nothing.
	var keys [shortcutActionCount][5]string
	caps := func(action shortcutAction) []string {
		return state.shortcuts.chords[action].keys(keys[action][:0])
	}
	icon := func(kind uint8) ui.Icon { return lucideIcons[kind] }
	var rows menuRows
	for index := 0; index < state.terminalMenu.count; index++ {
		found := state.terminalMenu.found[index]
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuAction[index], Label: state.actionLabel(found), Icon: icon(actionIcon(found.kind))})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuCopy, Label: messages.MenuCopy, Icon: icon(iconCopy), Keys: caps(shortcutCopy), Disabled: !hasSelection})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuPaste, Label: messages.MenuPaste, Icon: icon(iconPaste), Keys: caps(shortcutPaste), Disabled: !live})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSelectAll, Label: messages.MenuSelectAll, Icon: icon(iconSelectAll)})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuClear, Label: messages.MenuClearScrollback, Icon: icon(iconEraser), Disabled: session.term.screen.Scrollback() == 0})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSave, Label: messages.Tools.MenuSaveOutput, Icon: icon(iconFileDown)})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSplitRight, Label: messages.PaneSplitHorizontal, Icon: icon(iconColumns2), Keys: caps(shortcutPaneSplitHorizontal)})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSplitDown, Label: messages.PaneSplitVertical, Icon: icon(iconRows2), Keys: caps(shortcutPaneSplitVertical)})
	// A tab of one pane closes as a tab, which this menu does not offer: closing
	// its only pane would look like the same thing under a narrower name.
	if state.terminalMenuSplits(session) {
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuClosePane, Label: messages.PaneClose, Icon: icon(iconClose), Keys: caps(shortcutPaneClose)})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuFiles, Label: messages.SFTPFiles, Icon: icon(iconFolderOpen), Keys: caps(shortcutSFTP), Disabled: !session.isLocal() && (session.conn == nil || !session.hasServer())})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuTransfer, Label: messages.Tools.XYMenu, Icon: icon(iconTransfer), Disabled: !live || state.transfer.phase != transferNone})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuAsk, Label: messages.AIPanel.MenuAskSelection, Icon: icon(iconBot), Disabled: !hasSelection})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuExplain, Label: messages.AIPanel.MenuExplainOutput, Icon: icon(iconSparkles)})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuHistory, Label: messages.History.Title, Icon: icon(iconHistory), Keys: caps(shortcutHistory), Disabled: !state.historyAvailable(session)})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSearch, Label: messages.Tools.MenuSearchSelection, Icon: icon(iconSearch), Disabled: !hasSelection})
	const switchWidth = float32(32)
	if _, _, monitor := state.remoteStatsToggle(session); monitor {
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuStats, Label: messages.MenuResourceMonitor, Icon: icon(iconActivity), TrailingWidth: switchWidth})
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuTools, Label: messages.Tools.RemoteMenu, Icon: icon(iconWrench)})
	}
	if state.canReconnect(session) {
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuReconnect, Label: messages.SessionReconnect, Icon: icon(iconRefresh)})
	}

	width, height := rows.size(canvas, state.contextMenuTheme())
	at := state.terminalMenu.at
	bounds := ui.Rect{
		X:     min(at.X, max(viewport.Width-width-6, 0)),
		Y:     min(at.Y, max(viewport.Height-height-6, 0)),
		Width: width, Height: height,
	}
	// Anything outside the menu closes it, and the press does nothing else: the
	// menu declares its own backdrop from Viewport.
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitTerminalMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitTerminalMenuDismiss),
		RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	// The row is the click target; the switch in its slot only shows the state,
	// so it is painted without a hit of its own.
	rows.paint(&menu, func(_ components.ContextMenuItem, slot ui.Rect) {
		var track flex.Box
		f := state.paneChrome.Begin(canvas, state.Theme(), slot)
		f.Row().Fill().Items(flex.Center).Children(func() {
			track = f.Slot().Size(switchWidth, 18)
		})
		f.End()
		components.BeginSwitch(canvas, state.switchTheme(), ui.ID{}, track.Rect(), session.stats != nil).Small().Paint()
	})
	menu.End()
}

// actionIcon is the icon of the menu row of a detected action.
func actionIcon(kind actionKind) uint8 {
	switch kind {
	case actionSSH:
		return iconPlug
	case actionBrowser:
		return iconGlobe
	case actionFiles:
		return iconFolderOpen
	case actionExtract:
		return iconArchive
	}
	return iconCopy
}

// clickTerminalMenu runs a menu choice. A press anywhere else only closes the menu.
func (state *State) clickTerminalMenu(id ui.ID) bool {
	if !state.terminalMenu.open {
		return false
	}
	session, ok := state.terminalMenuSession()
	state.terminalMenu.open = false
	if !ok {
		return true
	}
	for index := 0; index < state.terminalMenu.count; index++ {
		if id == hitTerminalMenuAction[index] {
			state.runAction(session, state.terminalMenu.found[index])
			return true
		}
	}
	switch id {
	case hitTerminalMenuCopy:
		if selection := session.term.selection; selection.selected {
			if err := state.options.SetClipboardText(session.term.screen.SelectedText(selection.anchor, selection.focus)); err != nil {
				state.status = err.Error()
			}
		}
	case hitTerminalMenuPaste:
		state.pasteIntoTerminal(session)
	case hitTerminalMenuSelectAll:
		cols, rows := session.term.screen.Size()
		session.term.selection.selectRange(term.Position{}, term.Position{Row: rows - 1, Column: cols - 1})
	case hitTerminalMenuClear:
		session.term.screen.ClearScrollback()
	case hitTerminalMenuSave:
		state.saveTerminalOutput(session)
	case hitTerminalMenuSearch:
		state.searchSelection(session)
	case hitTerminalMenuHistory:
		state.openHistory(session)
	case hitTerminalMenuSplitRight:
		state.Click(hitPaneSplitHorizontal)
	case hitTerminalMenuSplitDown:
		state.Click(hitPaneSplitVertical)
	case hitTerminalMenuClosePane:
		// The session the menu was opened on, not whichever pane has the
		// keyboard: a right click does not move focus.
		state.closeSession(session.ID)
	case hitTerminalMenuFiles:
		state.Click(hitSFTPOpen)
	case hitTerminalMenuAsk:
		state.aiAskAboutSelection(session)
	case hitTerminalMenuExplain:
		state.aiExplainOutput(session)
	case hitTerminalMenuTransfer:
		state.openXYSetup(session)
	case hitTerminalMenuTools:
		state.openRemoteTools(session)
	case hitTerminalMenuStats:
		state.Click(hitRemoteStats)
	case hitTerminalMenuReconnect:
		state.reconnect(session)
	}
	return true
}
