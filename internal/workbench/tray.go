package workbench

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"motty/internal/buildinfo"
	"motty/internal/update"
	layoutcore "motty/internal/workspace"
)

// The status-area presence of the application. Viva owns the icon and shows the
// system's own menu for it; what the menu lists, and what choosing an entry
// does to a workbench, is product behavior and lives here.

// trayWindow is the part of an app.Window a tray drives; *app.Window is one.
type trayWindow interface {
	Show()
	Hidden() bool
}

type trayMember struct {
	state  *State
	window trayWindow
}

// Tray lists every open session of every window in the menu of the status icon.
// It is used only on the event-loop thread, like the windows it reads.
type Tray struct {
	members []trayMember
	quit    func()
	tooltip string
	count   int
}

// NewTray returns a tray whose Quit entry calls quit.
func NewTray(quit func()) *Tray { return &Tray{quit: quit} }

// Add lists a window's sessions in the tray. window is how choosing one of them
// brings that window back.
func (tray *Tray) Add(state *State, window trayWindow) {
	tray.members = append(tray.members, trayMember{state: state, window: window})
}

// Remove stops listing a window's sessions, once the window has closed.
func (tray *Tray) Remove(state *State) {
	for index, member := range tray.members {
		if member.state == state {
			tray.members = slices.Delete(tray.members, index, index+1)
			return
		}
	}
}

// WindowCount is how many windows are listed, hidden ones included.
func (tray *Tray) WindowCount() int { return len(tray.members) }

// SessionCount is how many sessions are open in all windows.
func (tray *Tray) SessionCount() int {
	count := 0
	for _, member := range tray.members {
		count += len(member.state.sessions)
	}
	return count
}

// Tooltip is the text for the icon to show, and whether it changed since the
// last call, so the caller touches the system only when it has to.
func (tray *Tray) Tooltip() (text string, changed bool) {
	count := tray.SessionCount()
	if len(tray.members) == 0 || tray.tooltip != "" && count == tray.count {
		return tray.tooltip, false
	}
	tray.count = count
	messages := tray.members[0].state.messages
	version := update.Current
	if count == 0 {
		tray.tooltip = fmt.Sprintf(productText(messages.TrayTooltipEmpty), version)
	} else {
		tray.tooltip = fmt.Sprintf(productText(messages.TrayTooltipFmt), version, count)
	}
	return tray.tooltip, true
}

// productText puts this build's product name into an existing localized string.
// The catalogs stay unchanged, so switching builds never changes global i18n.
func productText(text string) string {
	if buildinfo.ProductName == "Motty" {
		return text
	}
	return strings.ReplaceAll(text, "Motty", buildinfo.ProductName)
}

// closeActionVisible reports whether closing a window can leave the application
// in the status area, so the choice of what it does is worth showing.
func (state *State) closeActionVisible() bool {
	return state.options.StatusArea != nil && state.options.StatusArea()
}

// StaysInTray reports whether closing the last window, or a window that has
// sessions, hides it into the status area instead of closing it.
func (state *State) StaysInTray() bool { return state.closeAction == closeToTray }

// WantsToStay reports whether a window with sessions should hide rather than
// close: closing it would end connections the user may still be using.
func (state *State) WantsToStay() bool { return len(state.sessions) > 0 }

// NoteHiddenToTray tells the user, once per device, that closing the window did
// not quit: the sessions stay connected and the status icon brings the window
// back. A window that vanishes into the status area looks the same as one that
// ended the program, so the first time it happens is explained rather than
// assumed. Later hides stay silent.
func (state *State) NoteHiddenToTray() {
	if state.trayHintShown {
		return
	}
	state.trayHintShown = true
	state.save()
	messages := state.messages
	if err := state.options.Notify(app.Notification{Title: productText(messages.TrayHintTitle), Body: productText(messages.TrayHintBody)}); err != nil {
		// The platform may have no notification service; the tray icon and the
		// setting are still there, so this is not reported over the window.
		return
	}
}

// ConfirmClose asks before a window that is about to quit takes its sessions
// with it, and runs allow only when the user agrees. It reports whether it took
// over; false means nothing is open and the caller may close at once. Asking a
// second time while the question is up closes, since the user insists.
func (state *State) ConfirmClose(allow func()) bool {
	if state.ConfirmDocumentClose(func() {
		if !state.ConfirmClose(allow) {
			allow()
		}
	}) {
		return true
	}
	if len(state.sessions) == 0 || state.closeAsked && state.modal == modalForm {
		return false
	}
	m := state.messages.Tools
	state.openForm(formDialog{
		title: m.CloseAppTitle, description: fmt.Sprintf(m.CloseAppDescriptionFmt, len(state.sessions)), submit: m.CloseAppConfirm, danger: true,
		run: func(*State, *formDialog) string {
			allow()
			return ""
		},
	})
	state.closeAsked = true
	return true
}

// RevealSession shows a session in this window: its workspace, and its pane
// within it. It reports whether the window has the session.
func (state *State) RevealSession(id uint32) bool {
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(id))
	if !ok || !state.activateWorkspace(workspaceID) {
		return false
	}
	state.activatePane(workspaceID, paneID)
	state.save()
	return true
}

// Menu is the menu the icon opens, built when it opens: how many sessions are
// open, the sessions of every window (a workspace with several is a submenu of
// them), the saved hosts and workspaces of a window, then a new local terminal,
// the windows, and Quit. Choosing a session brings its window forward at that
// session; choosing a saved host or workspace opens it in the window the user is
// working in. Every entry carries an id for the hosts that remember one: an
// entry keeps the id it was served with, and so stays choosable, while its title
// or state changes. Session and workspace numbers count within one window, so
// their entries carry the window's number too.
func (tray *Tray) Menu() []app.MenuItem {
	if len(tray.members) == 0 {
		return nil
	}
	member, _ := tray.visibleMember()
	messages := tray.members[0].state.messages
	count := tray.SessionCount()
	items := make([]app.MenuItem, 0, 10+count)
	heading := messages.TrayEmpty
	if count > 0 {
		heading = fmt.Sprintf("%s · %d", messages.TrayTitle, count)
	}
	items = append(items, app.MenuItem{ID: "sessions", Title: heading, Disabled: true})
	if count > 0 {
		items = tray.appendSessions(items)
	}
	if hosts := tray.hostItems(member); len(hosts) > 0 {
		items = append(items,
			app.MenuItem{Separator: true},
			app.MenuItem{ID: "hosts", Title: messages.NavHosts, Submenu: hosts},
		)
	}
	if workspaces := tray.workspaceItems(member); len(workspaces) > 0 {
		items = append(items, app.MenuItem{ID: "workspaces", Title: messages.NavWorkspaces, Submenu: workspaces})
	}
	return append(items,
		app.MenuItem{Separator: true},
		app.MenuItem{ID: "new-local-terminal", Title: messages.TrayLocalTerminal, OnSelect: tray.openLocalTerminal},
		app.MenuItem{ID: "show-window", Title: productText(messages.TrayShowWindow), OnSelect: tray.ShowWindow},
		app.MenuItem{ID: "check-updates", Title: messages.TrayCheckUpdates, OnSelect: tray.checkForUpdates, Disabled: tray.checkingForUpdates()},
		app.MenuItem{Separator: true},
		app.MenuItem{ID: "quit", Title: productText(messages.TrayQuit), OnSelect: tray.quit},
	)
}

// hostItems lists the saved hosts of a window under their groups: the hosts
// keep the order the hosts page shows, a group with hosts in it is a submenu
// named after the last part of its path, and a group left with nothing to list
// is dropped. The unnamed group's hosts sit at the top, as they do on the page.
func (tray *Tray) hostItems(member trayMember) []app.MenuItem {
	state := member.state
	if len(state.hosts) == 0 {
		return nil
	}
	order := make([]int, len(state.hosts))
	for index := range order {
		order[index] = index
	}
	state.sortHostIndexes(order)
	groups := newTrayHostGroups()
	for _, group := range state.hostGroups {
		groups.group(group.Name)
	}
	for _, index := range order {
		host := state.hosts[index]
		group := groups.group(strings.TrimSpace(host.Group))
		group.hosts = append(group.hosts, tray.hostItem(member, host))
	}
	return groups.root.items(member.state.windowID)
}

// hostItem is the entry for one saved host: its name, and opening it in a tab
// of the window the menu belongs to.
func (tray *Tray) hostItem(member trayMember, host Host) app.MenuItem {
	title := host.Label
	if title == "" {
		title = host.Address
	}
	catalogID := host.CatalogID
	return app.MenuItem{
		ID:       trayNamedID(member.state.windowID, "host", catalogID),
		Title:    title,
		OnSelect: func() { tray.openHost(member, catalogID) },
	}
}

// workspaceItems lists the saved workspace templates of a window, in the order
// the workspace library shows them.
func (tray *Tray) workspaceItems(member trayMember) []app.MenuItem {
	state := member.state
	if len(state.templates) == 0 {
		return nil
	}
	items := make([]app.MenuItem, 0, len(state.templates))
	for _, template := range state.templates {
		templateID := template.ID
		items = append(items, app.MenuItem{
			ID:       trayNamedID(state.windowID, "workspace-template", templateID),
			Title:    template.Name,
			OnSelect: func() { tray.openWorkspace(member, templateID) },
		})
	}
	return items
}

// trayHostGroups is the tree a window's saved hosts are filed in. The nodes
// are made in the order the hosts page lists their groups, so the menu keeps
// that order too.
type trayHostGroups struct {
	root  trayHostGroup
	nodes map[string]*trayHostGroup
}

type trayHostGroup struct {
	path     string
	hosts    []app.MenuItem
	children []*trayHostGroup
}

func newTrayHostGroups() *trayHostGroups {
	groups := &trayHostGroups{nodes: make(map[string]*trayHostGroup)}
	groups.nodes[""] = &groups.root
	return groups
}

// group returns the node for a group path, making the nodes above it first.
func (groups *trayHostGroups) group(path string) *trayHostGroup {
	if node, ok := groups.nodes[path]; ok {
		return node
	}
	parentPath := ""
	if at := strings.LastIndex(path, "/"); at >= 0 {
		parentPath = path[:at]
	}
	parent := groups.group(parentPath)
	node := &trayHostGroup{path: path}
	groups.nodes[path] = node
	parent.children = append(parent.children, node)
	return node
}

// items turns the tree into menu entries: a group's hosts come first, then its
// child groups. A group with no hosts anywhere below it makes no entry.
func (group *trayHostGroup) items(windowID int) []app.MenuItem {
	var items []app.MenuItem
	items = append(items, group.hosts...)
	for _, child := range group.children {
		submenu := child.items(windowID)
		if len(submenu) == 0 {
			continue
		}
		items = append(items, app.MenuItem{
			ID:      trayNamedID(windowID, "host-group", child.path),
			Title:   hostGroupLeaf(child.path),
			Submenu: submenu,
		})
	}
	return items
}

// trayEntryID names an entry that stands for one numbered thing in one window.
// Session and workspace numbers are counted within a window while this menu
// lists every window, so the window's number is part of the name. A host that
// keeps the ids it was served keeps the entry while its title or state
// changes.
func trayEntryID(windowID int, kind string, id uint32) string {
	return trayNamedID(windowID, kind, strconv.FormatUint(uint64(id), 10))
}

// trayNamedID names an entry that stands for one thing in one window by a
// string key, as trayEntryID does for a number: the window's number keeps two
// windows' equal keys apart, and the key names the entry while its title
// changes.
func trayNamedID(windowID int, kind, key string) string {
	return fmt.Sprintf("window:%d/%s:%s", windowID, kind, key)
}

// appendSessions lists the sessions of every window, in window order, then
// workspace order. A workspace with one session is a single entry; one with
// several is a submenu of them; one with none is not listed.
func (tray *Tray) appendSessions(items []app.MenuItem) []app.MenuItem {
	var workspaces []layoutcore.WorkspaceInfo
	var panes []layoutcore.Pane
	for _, member := range tray.members {
		state := member.state
		workspaces = state.deck.Workspaces(workspaces[:0])
		for _, workspace := range workspaces {
			panes = state.deck.Panes(workspace.ID, panes[:0])
			var sessions []app.MenuItem
			for _, pane := range panes {
				if session, ok := state.sessionByIDPointer(uint32(pane.SessionID)); pane.SessionID != 0 && ok {
					sessions = append(sessions, tray.sessionItem(member, session))
				}
			}
			switch len(sessions) {
			case 0:
			case 1:
				items = append(items, sessions[0])
			default:
				items = append(items, app.MenuItem{
					ID:      trayEntryID(member.state.windowID, "workspace", uint32(workspace.ID)),
					Title:   fmt.Sprintf("%s (%d)", workspace.Title, len(sessions)),
					Submenu: sessions,
				})
			}
		}
	}
	return items
}

// sessionItem is the entry for one session: its name and how it is doing.
func (tray *Tray) sessionItem(member trayMember, session *Session) app.MenuItem {
	label := session.Title
	if label == "" {
		label = session.Address
	}
	status, _ := member.state.sessionStatus(session)
	id := session.ID
	return app.MenuItem{
		ID:       trayEntryID(member.state.windowID, "session", session.ID),
		Title:    label + " · " + status,
		OnSelect: func() { tray.reveal(member, id) },
	}
}

// reveal brings a window forward at one of its sessions.
func (tray *Tray) reveal(member trayMember, id uint32) {
	if !member.state.RevealSession(id) {
		return
	}
	member.window.Show()
	member.state.options.Invalidate()
}

// openHost connects a saved host in a tab of its own, as the hosts page does,
// and brings the window forward: a menu chosen while the window is hidden
// should show the session it just opened.
func (tray *Tray) openHost(member trayMember, catalogID string) {
	host, ok := member.state.hostByCatalogID(catalogID)
	if !ok {
		return
	}
	member.state.pendingWorkspace, member.state.pendingPane = 0, 0
	member.state.newHostSession(host)
	member.window.Show()
	member.state.options.Invalidate()
}

// openWorkspace opens a saved workspace template, as the workspace library
// does, and brings the window forward. A launch that cannot resolve a host
// reports it over the window, which is shown either way.
func (tray *Tray) openWorkspace(member trayMember, templateID string) {
	state := member.state
	if _, _, ok := state.workspaceTemplateByID(templateID); !ok {
		return
	}
	state.selectedWorkspaceTemplateID = templateID
	state.applySelectedWorkspaceTemplate()
	member.window.Show()
	state.options.Invalidate()
}

// visibleMember is the window a new terminal or a request to show the
// application belongs to: one the user can see, or else the first.
func (tray *Tray) visibleMember() (trayMember, bool) {
	for _, member := range tray.members {
		if !member.window.Hidden() {
			return member, true
		}
	}
	if len(tray.members) == 0 {
		return trayMember{}, false
	}
	return tray.members[0], true
}

// openLocalTerminal opens a shell on this computer in a window the user can
// see, or else the first one, which it brings back.
func (tray *Tray) openLocalTerminal() {
	member, ok := tray.visibleMember()
	if !ok {
		return
	}
	member.state.openLocalTerminal()
	member.window.Show()
	member.state.options.Invalidate()
}

// ShowWindow brings the application back: the window the user can already see,
// or else the first one, which was hidden.
func (tray *Tray) ShowWindow() {
	if member, ok := tray.visibleMember(); ok {
		member.window.Show()
	}
}

// checkingForUpdates reports whether any window is already asking the feed, so
// the menu can disable the entry instead of queueing a second request.
func (tray *Tray) checkingForUpdates() bool {
	for _, member := range tray.members {
		if member.state.update.checking {
			return true
		}
	}
	return false
}

// checkForUpdates asks the feed from the window the user can see, bringing it
// forward first: a release is offered in that window's update dialog.
func (tray *Tray) checkForUpdates() {
	member, ok := tray.visibleMember()
	if !ok {
		return
	}
	member.window.Show()
	member.state.checkForUpdates(true)
}
