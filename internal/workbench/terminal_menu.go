package workbench

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

// terminalMenuHits is the place of the controls this file paints.
var terminalMenuHits = tree.Feature("terminal-menu")

// Two more rows of the menu, outside the range the window's other menus use.
var (
	hitTerminalMenuFolder  = tree.ID(terminalMenuHits, "terminal-menu-folder")
	hitTerminalMenuSave    = tree.ID(terminalMenuHits, "terminal-menu-save")
	hitTerminalMenuSearch  = tree.ID(terminalMenuHits, "terminal-menu-search")
	hitTerminalMenuHistory = tree.ID(terminalMenuHits, "terminal-menu-history")
	hitTerminalMenuRename  = tree.ID(terminalMenuHits, "terminal-menu-rename")
)

// maxMenuActions bounds what the menu offers for the text under the pointer.
const maxMenuActions = 4

// hitTerminalMenuAction is the row of one detected action.
var hitTerminalMenuAction = [maxMenuActions]gfx.ID{
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
	at      gfx.Point // window coordinates of the press that opened it
	pointer gfx.Point // window coordinates of the latest pointer event
	// found is what can be done with the text the menu was opened on.
	found [maxMenuActions]detected
	count int
}

func (state *State) openTerminalMenu(session *Session) {
	state.ghostMenu.open = false
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
	token := session.term.screen.TokenAt(position)
	found := classifyToken(token, user)
	if session.isLocal() && session.wslDistro() == "" && !offersPlace(found) {
		// A path in a local terminal is an entry of this computer, so the
		// system's file manager shows it where the text says it is; the file
		// panel stays the transfer surface it is, one menu row away.
		if target, ok := state.localRevealTarget(session, token); ok {
			label := pathLine.ReplaceAllString(token, "")
			found = append([]detected{{kind: actionReveal, target: target, label: label}}, found...)
		}
	}
	for _, item := range found {
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

// offersPlace reports whether the text already reads as an address to reach
// rather than as a path to look at.
func offersPlace(found []detected) bool {
	for _, item := range found {
		if item.kind == actionSSH || item.kind == actionBrowser {
			return true
		}
	}
	return false
}

// localRevealTarget resolves text under the pointer against the folder the
// local shell reported, falling back to where the shell started.
func (state *State) localRevealTarget(session *Session, token string) (string, bool) {
	home, _ := os.UserHomeDir()
	return localFilePath(token, localShellDirectory(session), home)
}

// actionAvailable is whether an action can be taken from this session now.
func (state *State) actionAvailable(session *Session, item detected) bool {
	switch item.kind {
	case actionFiles:
		// A local terminal's paths are this computer's, unless they are a WSL
		// distribution's: those are reached through the panel that maps them.
		return session.wslDistro() != "" || session.conn != nil && session.hasServer()
	case actionReveal:
		return session.isLocal() && session.wslDistro() == ""
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
	case actionReveal:
		format = m.ActionRevealFmt
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
	case actionReveal:
		m := state.messages.Tools
		state.revealLocalPath(item.target, m.ActionReveal, m.ActionRevealFailed, m.ActionRevealMissing)
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
	// Showing a folder only ever opens the panel: a panel that is already up
	// keeps the view it has, and the navigation below runs on it.
	state.showFilesPanel(session)
	view := session.files
	switch {
	case view == nil:
	case view.local:
		state.navigateLocalDirectory(view, filepath.FromSlash(dir))
	default:
		state.navigateSFTPDirectory(view, dir)
	}
}

// terminalMenuSession is the session an open menu acts on, if it is still there.
func (state *State) terminalMenuSession() (*Session, bool) {
	if !state.terminalMenu.open {
		return nil, false
	}
	return state.terminalMenuTarget()
}

// terminalMenuTarget is the session the menu was opened on, while the terminal is
// still the page: a choice is run once the menu has closed.
func (state *State) terminalMenuTarget() (*Session, bool) {
	if state.activeSection != sectionTerminal || state.modal != modalNone {
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

func (state *State) declareTerminalMenu(c ui.C) {
	if !state.terminalMenu.open {
		return
	}
	session, ok := state.terminalMenuSession()
	if !ok {
		state.terminalMenu.open = false
		return
	}
	messages := state.messages
	live := session.term.shell != nil
	hasSelection := session.term.selection.selected
	chord := func(action shortcutAction) keymap.Chord { return state.shortcuts.chords.Chord(action) }
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	var rows menuRows
	for index := 0; index < state.terminalMenu.count; index++ {
		found := state.terminalMenu.found[index]
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuAction[index], Label: state.actionLabel(found), Icon: icon(actionIcon(found.kind))})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuCopy, Label: messages.MenuCopy, Icon: icon(iconCopy), Disabled: !hasSelection})
	rows.chord(chord(shortcutCopy))
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuPaste, Label: messages.MenuPaste, Icon: icon(iconPaste), Disabled: !live})
	rows.chord(chord(shortcutPaste))
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSelectAll, Label: messages.MenuSelectAll, Icon: icon(iconSelectAll)})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuClearScreen, Label: messages.MenuClearScreen, Icon: icon(iconEraser), Disabled: session.term.screen.Modes().AltScreen})
	rows.chord(chord(shortcutClearScreen))
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuClear, Label: messages.MenuClearScrollback, Icon: icon(iconEraser), Disabled: session.term.screen.Scrollback() == 0})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSave, Label: messages.Tools.MenuSaveOutput, Icon: icon(iconSave)})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSplitRight, Label: messages.PaneSplitHorizontal, Icon: icon(iconColumns2)})
	rows.chord(chord(shortcutPaneSplitHorizontal))
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSplitDown, Label: messages.PaneSplitVertical, Icon: icon(iconRows2)})
	rows.chord(chord(shortcutPaneSplitVertical))
	// A tab of one pane closes as a tab, which this menu does not offer: closing
	// its only pane would look like the same thing under a narrower name.
	if state.terminalMenuSplits(session) {
		// The tab's own Rename names a one-terminal tab; in a split tab each
		// terminal needs a name of its own for the assistant to be told apart.
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuRename, Label: messages.MenuRenameTerminal, Icon: icon(iconPencil)})
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuClosePane, Label: messages.PaneClose, Icon: icon(iconClose)})
		rows.chord(chord(shortcutPaneClose))
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuFolder, Label: messages.ShortcutTerminalFolder, Icon: icon(iconFolderOpen), Disabled: !canOpenSessionFolder(session)})
	rows.chord(chord(shortcutTerminalFolder))
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuFiles, Label: messages.SFTPFiles, Icon: icon(iconFolderOpen), Kind: components.ContextMenuCheckbox, Checked: state.filesPanelShown(), Disabled: !session.isLocal() && (session.conn == nil || !session.hasServer())})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuTransfer, Label: messages.Tools.XYMenu, Icon: icon(iconTransfer), Disabled: !live || session.transferBusy()})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuAsk, Label: messages.AIPanel.MenuAskSelection, Icon: icon(iconBot), Disabled: !hasSelection})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuExplain, Label: messages.AIPanel.MenuExplainOutput, Icon: icon(iconSparkles)})
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuHistory, Label: messages.History.Title, Icon: icon(iconHistory), Disabled: !state.historyAvailable(session)})
	rows.chord(chord(shortcutHistory))
	rows.add(components.ContextMenuItem{ID: hitTerminalMenuSearch, Label: messages.Tools.MenuSearchSelection, Icon: icon(iconSearch), Disabled: !hasSelection})
	if _, _, monitor := state.remoteStatsToggle(session); monitor {
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuStats, Label: messages.MenuResourceMonitor, Icon: icon(iconGauge), Kind: components.ContextMenuCheckbox, Checked: session.stats != nil})
	}
	if state.remoteToolsOffered(session) {
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuTools, Label: messages.Tools.RemoteMenu, Icon: icon(iconProcess)})
	}
	reconnect, restart, disconnect := state.canReconnect(session), state.canReconnectLive(session), state.canDisconnect(session)
	if reconnect || restart || disconnect {
		rows.separate()
	}
	if reconnect {
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuReconnect, Label: messages.SessionReconnect, Icon: icon(iconRefresh)})
	}
	if restart {
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuRestart, Label: messages.MenuReconnect, Icon: icon(iconRefresh)})
	}
	if disconnect {
		rows.add(components.ContextMenuItem{ID: hitTerminalMenuDisconnect, Label: messages.MenuDisconnect, Icon: icon(iconPlug)})
	}

	state.declareRowsMenu(c, &state.terminalMenu.open, state.terminalMenu.at, hitTerminalMenuDismiss, &rows, state.chooseTerminalMenu)
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
	case actionReveal:
		return iconFolderOpen
	case actionExtract:
		return iconArchive
	}
	return iconCopy
}

// chooseTerminalMenu runs a choice of the menu, which closed with it.
func (state *State) chooseTerminalMenu(id gfx.ID) {
	session, ok := state.terminalMenuTarget()
	if !ok {
		return
	}
	for index := 0; index < state.terminalMenu.count; index++ {
		if id == hitTerminalMenuAction[index] {
			state.runAction(session, state.terminalMenu.found[index])
			return
		}
	}
	switch id {
	case hitTerminalMenuCopy:
		if selection := session.term.selection; selection.selected {
			if err := state.options.SetClipboardText(selection.text(session.term.screen)); err != nil {
				state.status = err.Error()
			}
		}
	case hitTerminalMenuPaste:
		state.pasteIntoTerminal(session)
	case hitTerminalMenuSelectAll:
		cols, rows := session.term.screen.Size()
		session.term.selection.selectRange(
			session.term.screen.PointAt(term.Position{}),
			session.term.screen.PointAt(term.Position{Row: rows - 1, Column: cols - 1}))
	case hitTerminalMenuClearScreen:
		session.term.clearScreen(state.messages)
	case hitTerminalMenuClear:
		session.term.screen.ClearScrollback()
		session.term.selection.trim(session.term.screen)
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
	case hitTerminalMenuRename:
		state.openTerminalRename(session)
	case hitTerminalMenuClosePane:
		// The session the menu was opened on, not whichever pane has the
		// keyboard: a right click does not move focus.
		state.requestCloseSession(session.ID)
	case hitTerminalMenuFolder:
		state.openSessionFolder(session)
	case hitTerminalMenuFiles:
		state.Click(hitSFTPOpen)
	case hitTerminalMenuAsk:
		state.aiAskAboutSelection(session)
	case hitTerminalMenuExplain:
		state.aiExplainOutput(session)
	case hitTerminalMenuTransfer:
		state.openXYSetup(session)
	case hitTerminalMenuTools:
		state.openProcessTools(session)
	case hitTerminalMenuStats:
		state.Click(hitRemoteStats)
	case hitTerminalMenuReconnect:
		state.reconnect(session)
	case hitTerminalMenuRestart:
		state.askSessionLink(session, false)
	case hitTerminalMenuDisconnect:
		state.askSessionLink(session, true)
	}
}
