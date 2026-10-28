package workbench

import (
	"slices"
	"sync"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// Windows of one process that let a tab move between them. A Group is created
// once and given to every window's Options; each window joins it when it is made
// and leaves when it closes. All windows run on one event-loop thread, so a
// window may call another's methods from its own pointer handler.

// Group is the windows of a process.
type Group struct {
	mu      sync.Mutex
	members []*State
	// frame is where the process's windows were last seen. It lives here, not
	// in one window, so any window's save writes the newest place rather than
	// the one it read at start.
	frame    ui.Rect
	hasFrame bool
}

// setWindowFrame remembers where the windows are, for the next save.
func (group *Group) setWindowFrame(frame ui.Rect) {
	group.mu.Lock()
	defer group.mu.Unlock()
	group.frame, group.hasFrame = frame, true
}

// windowFrame is the newest frame any window reported.
func (group *Group) windowFrame() (ui.Rect, bool) {
	group.mu.Lock()
	defer group.mu.Unlock()
	return group.frame, group.hasFrame
}

// NewGroup returns an empty group.
func NewGroup() *Group { return &Group{} }

// sessionCount is how many sessions are open in all the windows.
func (group *Group) sessionCount() int {
	group.mu.Lock()
	defer group.mu.Unlock()
	count := 0
	for _, member := range group.members {
		count += len(member.sessions)
	}
	return count
}

func (group *Group) join(state *State) {
	group.mu.Lock()
	defer group.mu.Unlock()
	group.members = append(group.members, state)
}

func (group *Group) leave(state *State) {
	group.mu.Lock()
	defer group.mu.Unlock()
	for index, member := range group.members {
		if member == state {
			group.members = slices.Delete(group.members, index, index+1)
			return
		}
	}
}

// windowAt is the window other than state whose content area holds the screen
// point, if any. Hidden and closed windows hold nothing.
func (group *Group) windowAt(state *State, at ui.Point) *State {
	group.mu.Lock()
	members := append([]*State(nil), group.members...)
	group.mu.Unlock()
	for _, member := range members {
		if member == state || member.closed || member.frame == nil || (member.windowVisible != nil && !member.windowVisible()) {
			continue
		}
		if frame, ok := member.frame(); ok && pointInRect(at.X, at.Y, frame) {
			return member
		}
	}
	return nil
}

// SetWindow tells the state where its window is: frame is the content area in
// screen coordinates (app.Window.Frame) and visible says the window is on
// screen. Without it the window cannot be a drop target for another window's
// tab. Call it once the window exists.
func (state *State) SetWindow(frame func() (ui.Rect, bool), visible func() bool) {
	state.frame, state.windowVisible = frame, visible
}

// tabPayload is a tab on its way from one window to another: its panes' layout
// and the live sessions in them. Nothing in it is closed or copied; the sessions
// are the ones the user was working in.
type tabPayload struct {
	title     string
	dto       layoutcore.WorkspaceDTO
	sessions  []Session
	transfers []transferEntry
}

// detachWorkspace takes a tab out of this window without ending its sessions.
func (state *State) detachWorkspace(id layoutcore.WorkspaceID) (*tabPayload, bool) {
	dto, ok := state.deck.ExportWorkspace(id)
	if !ok {
		return nil, false
	}
	// A transfer in progress belongs to this window's dialog and cannot follow.
	if state.transfer.phase != transferNone {
		for _, pane := range dto.Panes {
			if uint32(pane.SessionID) == state.transfer.sessionID {
				state.status = state.messages.Tools.TabMoveBusy
				return nil, false
			}
		}
	}
	payload := &tabPayload{title: dto.Title, dto: dto}
	for _, pane := range dto.Panes {
		if pane.SessionID == 0 {
			continue
		}
		for index := range state.sessions {
			if state.sessions[index].ID == uint32(pane.SessionID) {
				payload.sessions = append(payload.sessions, state.sessions[index])
			}
		}
	}
	for index := range payload.sessions {
		session := &payload.sessions[index]
		payload.transfers = state.detachTransfers(session.files, payload.transfers)
		// What belongs to this window rather than to the session ends here: the
		// reconnect timer, the port forwards that were opened through it, the
		// files open in an outside editor, and a dialog about it.
		state.cancelReconnect(session)
		state.stopSessionForwards(session.ID, false)
		state.dropExternalEdits(session.ID)
		if state.modalSession == session.ID {
			state.closeModal()
		}
		if state.broadcast == id {
			state.broadcast = 0
		}
	}
	_, _ = state.deck.RemoveWorkspace(id, nil)
	kept := state.sessions[:0]
	for _, session := range state.sessions {
		if !payloadHolds(payload, session.ID) {
			kept = append(kept, session)
		}
	}
	for index := len(kept); index < len(state.sessions); index++ {
		state.sessions[index] = Session{}
	}
	state.sessions = kept
	state.syncActiveSession()
	if state.deck.ActiveWorkspace() == 0 {
		state.activeSection = sectionHosts
		state.activeSessionID = 0
	}
	state.rebuildTabOptions()
	state.refreshWorkspaceViews()
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	state.save()
	return payload, true
}

func payloadHolds(payload *tabPayload, id uint32) bool {
	for _, session := range payload.sessions {
		if session.ID == id {
			return true
		}
	}
	return false
}

// attachPayload puts a tab from another window here: as a tab at drop's place
// among the tabs, or docked beside a pane. It reports whether it did; on false
// nothing here changed.
func (state *State) attachPayload(payload *tabPayload, drop tabDrop) bool {
	if payload == nil || len(payload.sessions) == 0 && len(payload.dto.Panes) == 0 {
		return false
	}
	dockInto := state.deck.ActiveWorkspace()
	docking := drop.kind == dropDock && dockInto != 0
	if docking && len(state.deck.Panes(dockInto, nil))+len(payload.dto.Panes) > layoutcore.MaxPanes {
		return false
	}
	// The sessions get this window's identifiers.
	renamed := make(map[layoutcore.SessionID]layoutcore.SessionID, len(payload.sessions))
	added := make([]uint32, 0, len(payload.sessions))
	for _, session := range payload.sessions {
		old := session.ID
		session.ID = state.nextSessionID
		state.nextSessionID++
		renamed[layoutcore.SessionID(old)] = layoutcore.SessionID(session.ID)
		session.paneSlot = 0
		if session.relay == nil {
			session.relay = newNotifyRelay(state.options.Invalidate)
		} else {
			session.relay.set(state.options.Invalidate)
		}
		if session.HostID != localHostID {
			session.HostID = 0
			if host, ok := state.hostByCatalogID(session.host.CatalogID); ok && session.host.CatalogID != "" {
				session.HostID = host.ID
			}
		}
		session.Title = state.uniqueSessionTitle(session.Title, 0)
		state.sessions = append(state.sessions, session)
		moved := &state.sessions[len(state.sessions)-1]
		moved.paneSlot = state.claimPaneSlot()
		if moved.term != nil {
			// What the terminal kept of the window it left: its row cache is made
			// for that window's look, and its callbacks and text handlers name
			// that window and the session's old number.
			moved.term.cache = rowCache{}
			moved.term.input.OnEvent = nil
			moved.term.find.input.OnEvent = nil
			state.configureTerminal(moved)
		}
		added = append(added, session.ID)
	}
	index := len(state.workspaceTabs)
	if drop.kind == dropReorder {
		index = drop.boundary
	}
	id, err := state.deck.Import(payload.dto, renamed, index)
	if err != nil {
		keep := len(state.sessions) - len(added)
		clear(state.sessions[keep:])
		state.sessions = state.sessions[:keep]
		state.nextSessionID -= uint32(len(added))
		return false
	}
	if docking {
		if _, err := state.deck.Dock(dockInto, id, drop.pane, drop.edge.axis(), drop.edge.placement()); err != nil {
			_, _ = state.deck.RemoveWorkspace(id, nil)
			state.deck.ActivateWorkspace(dockInto)
			keep := len(state.sessions) - len(added)
			clear(state.sessions[keep:])
			state.sessions = state.sessions[:keep]
			state.nextSessionID -= uint32(len(added))
			return false
		}
		id = dockInto
	}
	for _, sessionID := range added {
		if moved, ok := state.sessionByIDPointer(sessionID); ok {
			state.renameSession(moved, moved.Title)
		}
	}
	state.adoptTransfers(payload.transfers)
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	state.rebuildTabOptions()
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.save()
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
	return true
}

// hostByCatalogID finds a saved host by its stable identifier.
func (state *State) hostByCatalogID(catalogID string) (Host, bool) {
	if catalogID == "" {
		return Host{}, false
	}
	for _, host := range state.hosts {
		if host.CatalogID == catalogID {
			return host, true
		}
	}
	return Host{}, false
}
