package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabsClose holds exactly the tabs covered by one close confirmation.
type tabsClose struct {
	keep   layoutcore.WorkspaceID
	ids    []layoutcore.WorkspaceID
	count  int
	locked int
	title  string
	// session is the one pane to close when a pane, not a tab, was asked for.
	session uint32
	// running names the sessions that are still connected; quiet is the "do not ask
	// again" box.
	running []string
	quiet   bool
}

// sessionRunning reports whether closing the session would end something live: a
// shell that has not ended.
func sessionRunning(session *Session) bool {
	return session != nil && session.term != nil && session.term.shell != nil && !session.term.ended
}

func (state *State) runningLabel(session *Session) string {
	if session.Address == "" || session.Address == session.Title {
		return session.Title
	}
	return session.Title + ": " + session.Address
}

// runningSessions names the live sessions in the given tabs.
func (state *State) runningSessions(ids []layoutcore.WorkspaceID) []string {
	if state.closeConfirmOff {
		return nil
	}
	var names []string
	var panes []layoutcore.Pane
	for _, id := range ids {
		panes = state.deck.Panes(id, panes[:0])
		for _, pane := range panes {
			if session, ok := state.sessionByIDPointer(uint32(pane.SessionID)); ok && sessionRunning(session) {
				names = append(names, state.runningLabel(session))
			}
		}
	}
	return names
}

// requestCloseSession closes one pane's session on the user's word, asking first
// when it is still running. A locked tab's only pane keeps the lock's question.
func (state *State) requestCloseSession(id uint32) {
	session, ok := state.sessionByIDPointer(id)
	if !ok {
		return
	}
	workspace, _, found := state.deck.PaneForSession(layoutcore.SessionID(id))
	if state.closeConfirmOff || !found || !sessionRunning(session) ||
		state.deck.Locked(workspace) && len(state.deck.Panes(workspace, nil)) == 1 {
		state.closeSession(id)
		return
	}
	state.tabsClose = tabsClose{session: id, count: 1, running: []string{state.runningLabel(session)}}
	state.modal = modalCloseTabs
}

// askCloseTabs confirms a batch or a protected tab; one unlocked tab closes
// immediately, just like its own close button.
func (state *State) askCloseTabs(keep layoutcore.WorkspaceID, pick func(position int) bool) {
	pending := tabsClose{keep: keep}
	for position, tab := range state.workspaceTabs {
		if pick(position) {
			pending.ids = append(pending.ids, tab.ID)
			pending.count++
			if tab.Locked {
				pending.locked++
				pending.title = state.tabLabel(tab)
			}
		}
	}
	pending.running = state.runningSessions(pending.ids)
	if pending.count < 2 && pending.locked == 0 && len(pending.running) == 0 {
		state.closeTabs(pending)
		return
	}
	state.tabsClose = pending
	state.modal = modalCloseTabs
}

func (state *State) askCloseLockedTab(id layoutcore.WorkspaceID) {
	index, ok := state.workspaceTab(id)
	if !ok {
		return
	}
	state.tabsClose = tabsClose{ids: []layoutcore.WorkspaceID{id}, count: 1, locked: 1, title: state.tabLabel(state.workspaceTabs[index])}
	state.modal = modalCloseTabs
}

func (state *State) tabMiddlePointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonMiddle || state.modal != modalNone || state.tabMenu.open {
		return gfx.EventResult{}, false
	}
	switch event.Target.Scope() {
	case hitTabList, hitTabCloseList:
		state.closeWorkspace(layoutcore.WorkspaceID(item(event.Target)))
	case fileTabList, fileTabCloseList:
		state.requestCloseSFTPTab(state.sftpWindows[uint32(item(event.Target))])
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) clickCloseTabsDialog(id gfx.ID) {
	switch id {
	case hitTabsCloseDismiss, hitTabsCloseClose, hitTabsCloseCancel:
		state.modal = modalNone
		state.tabsClose = tabsClose{}
	case hitTabsCloseQuiet:
		state.tabsClose.quiet = !state.tabsClose.quiet
	case hitTabsCloseConfirm:
		pending := state.tabsClose
		state.modal = modalNone
		state.tabsClose = tabsClose{}
		if pending.quiet && pending.locked == 0 {
			state.closeConfirmOff = true
			state.save()
		}
		if pending.session != 0 {
			state.closeSession(pending.session)
			return
		}
		state.closeTabs(pending)
	}
}

// closeTabsKey makes Enter on the confirm button confirm.
func (state *State) closeTabsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalCloseTabs || event.Key != gfx.KeyEnter || event.Target != hitTabsCloseConfirm || event.Repeat {
		return gfx.EventResult{}, false
	}
	state.clickCloseTabsDialog(hitTabsCloseConfirm)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) paintCloseTabsDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	messages := state.messages
	// A tab that asks because of its lock keeps that question; the running one
	// lists what would stop and offers to stop asking.
	asking := state.tabsClose.locked == 0 && len(state.tabsClose.running) > 0
	options := components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitTabsCloseDismiss, CloseID: hitTabsCloseClose, InitialFocusID: hitTabsCloseCancel,
		ContentSize: gfx.Size{Width: 520}, FitBody: true,
	}
	if asking {
		options.BodyHeight = float32(20*min(len(state.tabsClose.running), 6)) + 44
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, options)
	title := fmt.Sprintf(messages.CloseTabsTitleFmt, state.tabsClose.count)
	description := messages.CloseTabsDescription
	if state.tabsClose.locked > 0 {
		description = fmt.Sprintf(messages.CloseLockedTabsDescriptionFmt, state.tabsClose.locked)
		if state.tabsClose.count == 1 {
			title = messages.CloseLockedTabTitle
			description = fmt.Sprintf(messages.CloseLockedTabDescriptionFmt, state.tabsClose.title)
		}
	}
	if asking {
		title, description = messages.CloseRunningTitle, messages.CloseRunningDescription
	}
	layout := dialog.Begin(title, description)
	if asking {
		var quiet ui.Box
		f := state.settingsRow.Begin(canvas, state.Theme(), layout.Body)
		f.Column().Fill().Children(func() {
			for index, name := range state.tabsClose.running {
				if index == 6 {
					f.Text(fmt.Sprintf("+%d", len(state.tabsClose.running)-index)).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(20)
					break
				}
				f.Text(name).FontSize(metrics.FontSizeM).Color(state.pal.text).LineHeight(20)
			}
			f.Spacer().Grow(1)
			quiet = state.declareDialogCheckbox(f, messages.CloseRunningAsk, 0, 0)
		})
		f.End()
		state.paintDialogCheckbox(canvas, hitTabsCloseQuiet, messages.CloseRunningAsk, state.tabsClose.quiet, quiet)
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitTabsCloseCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitTabsCloseConfirm, Label: messages.CloseTabsConfirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}
