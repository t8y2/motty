package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/app"
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
	if count == 0 {
		tray.tooltip = messages.TrayTooltipEmpty
	} else {
		tray.tooltip = fmt.Sprintf(messages.TrayTooltipFmt, count)
	}
	return tray.tooltip, true
}

// WantsToStay reports whether a window with sessions should hide rather than
// close: closing it would end connections the user may still be using.
func (state *State) WantsToStay() bool { return len(state.sessions) > 0 }

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
// them), then a new local terminal, the windows, and Quit. Choosing a session
// brings its window forward at that session.
func (tray *Tray) Menu() []app.MenuItem {
	if len(tray.members) == 0 {
		return nil
	}
	messages := tray.members[0].state.messages
	items := make([]app.MenuItem, 0, 8+tray.SessionCount())
	if count := tray.SessionCount(); count == 0 {
		items = append(items, app.MenuItem{Title: messages.TrayEmpty, Disabled: true})
	} else {
		items = append(items, app.MenuItem{Title: fmt.Sprintf("%s · %d", messages.TrayTitle, count), Disabled: true})
		items = tray.appendSessions(items)
	}
	return append(items,
		app.MenuItem{Separator: true},
		app.MenuItem{Title: messages.TrayLocalTerminal, OnSelect: tray.openLocalTerminal},
		app.MenuItem{Title: messages.TrayShowWindow, OnSelect: tray.ShowWindow},
		app.MenuItem{Title: messages.TrayCheckUpdates, OnSelect: tray.checkForUpdates, Disabled: tray.checkingForUpdates()},
		app.MenuItem{Separator: true},
		app.MenuItem{Title: messages.TrayQuit, OnSelect: tray.quit},
	)
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
				items = append(items, app.MenuItem{Title: fmt.Sprintf("%s (%d)", workspace.Title, len(sessions)), Submenu: sessions})
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
