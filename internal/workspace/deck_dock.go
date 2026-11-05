package workspace

import "errors"

// ErrDockSelf means a workspace was asked to dock into itself.
var ErrDockSelf = errors.New("workspace: a workspace cannot dock into itself")

// MoveWorkspace moves a tab so that it ends up at index among the tabs; an index
// past the last is the last. The active tab stays active.
func (deck *Deck) MoveWorkspace(id WorkspaceID, index int) error {
	from := deck.workspaceIndex(id)
	if from < 0 {
		return ErrWorkspaceNotFound
	}
	index = min(max(index, 0), len(deck.workspaces)-1)
	if index == from {
		return nil
	}
	moved := deck.workspaces[from]
	if index < from {
		copy(deck.workspaces[index+1:from+1], deck.workspaces[index:from])
	} else {
		copy(deck.workspaces[from:index], deck.workspaces[from+1:index+1])
	}
	deck.workspaces[index] = moved
	return nil
}

// Insert creates an empty pane beside target, on the side placement names, and
// makes it active. Split always puts the new pane after its target; Insert is
// for a pane that has to go before it, or take a ratio.
func (deck *Deck) Insert(workspaceID WorkspaceID, target PaneID, axis Axis, ratio float32, placement Placement) (PaneID, error) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return 0, ErrWorkspaceNotFound
	}
	if len(workspace.panes) >= MaxPanes {
		return 0, ErrPaneLimit
	}
	if _, ok := workspace.pane(target); !ok {
		return 0, ErrPaneNotFound
	}
	ratio = min(max(ratio, MinPaneRatio), MaxPaneRatio)
	paneID := deck.allocatePaneID()
	if err := workspace.layout.Split(SessionID(target), SessionID(paneID), axis, ratio, placement); err != nil {
		return 0, err
	}
	workspace.panes = append(workspace.panes, Pane{ID: paneID})
	workspace.activePane = paneID
	workspace.revision++
	deck.activeWorkspace = workspaceID
	return paneID, nil
}

// CanDock reports whether src's panes fit beside dst's.
func (deck *Deck) CanDock(dst, src WorkspaceID) bool {
	if dst == src {
		return false
	}
	to, toOK := deck.workspace(dst)
	from, fromOK := deck.workspace(src)
	return toOK && fromOK && len(from.panes) > 0 && len(to.panes)+len(from.panes) <= MaxPanes
}

// Dock moves every pane of src into dst beside target and removes the emptied
// src tab. The first pane goes on the side placement names of target along axis;
// the others follow it in the order src showed them, each beside the one before,
// so a docked tab with several panes arrives as one row or column rather than
// with its old arrangement. Sessions keep their bindings and the first docked
// pane becomes active. It returns the docked sessions.
func (deck *Deck) Dock(dst, src WorkspaceID, target PaneID, axis Axis, placement Placement) ([]SessionID, error) {
	if dst == src {
		return nil, ErrDockSelf
	}
	to, toOK := deck.workspace(dst)
	_, fromOK := deck.workspace(src)
	if !toOK || !fromOK {
		return nil, ErrWorkspaceNotFound
	}
	if _, ok := to.pane(target); !ok {
		return nil, ErrPaneNotFound
	}
	if !deck.CanDock(dst, src) {
		return nil, ErrPaneLimit
	}
	panes := deck.Panes(src, nil)
	// Unbound first: a session can be in one pane of the deck only.
	sessions := make([]SessionID, len(panes))
	for index, pane := range panes {
		sessions[index] = pane.SessionID
		if pane.SessionID != 0 {
			_, _ = deck.Unbind(src, pane.ID)
		}
	}
	first := PaneID(0)
	previous := target
	for index := range panes {
		side := After
		if index == 0 {
			side = placement
		}
		paneID, err := deck.Insert(dst, previous, axis, 0.5, side)
		if err != nil {
			return sessions[:index], err
		}
		if index == 0 {
			first = paneID
		}
		if sessions[index] != 0 {
			if err := deck.Bind(dst, paneID, sessions[index]); err != nil {
				return sessions[:index], err
			}
		}
		previous = paneID
	}
	if _, err := deck.RemoveWorkspace(src, nil); err != nil {
		return sessions, err
	}
	deck.ActivateWorkspace(dst)
	deck.ActivatePane(dst, first)
	moved := sessions[:0:0]
	for _, id := range sessions {
		if id != 0 {
			moved = append(moved, id)
		}
	}
	return moved, nil
}

// ErrUndockOnly means the pane is the last one of its tab, so there is no tab
// left behind to take it out of.
var ErrUndockOnly = errors.New("workspace: the only pane of a tab cannot be undocked")

// Undock takes one pane out of its tab and makes it a tab of its own at index
// among the tabs (past the last is the last), titled title. It is the reverse of
// Dock for a single pane: the session stays bound to the pane, now in the new
// tab, and the tab it left keeps its other panes. The new tab becomes active.
func (deck *Deck) Undock(workspaceID WorkspaceID, paneID PaneID, title string, index int) (WorkspaceID, error) {
	source, ok := deck.workspace(workspaceID)
	if !ok {
		return 0, ErrWorkspaceNotFound
	}
	pane, ok := source.pane(paneID)
	if !ok {
		return 0, ErrPaneNotFound
	}
	if len(source.panes) < 2 {
		return 0, ErrUndockOnly
	}
	if len(deck.workspaces) >= MaxWorkspaces {
		return 0, ErrWorkspaceFull
	}
	sessionID := pane.SessionID
	if sessionID != 0 {
		_, _ = deck.Unbind(workspaceID, paneID)
	}
	if _, err := deck.ClosePane(workspaceID, paneID); err != nil {
		if sessionID != 0 {
			_ = deck.Bind(workspaceID, paneID, sessionID)
		}
		return 0, err
	}
	id, newPane, err := deck.Create(title)
	if err != nil {
		return 0, err
	}
	if sessionID != 0 {
		if err := deck.Bind(id, newPane, sessionID); err != nil {
			return id, err
		}
	}
	if err := deck.MoveWorkspace(id, index); err != nil {
		return id, err
	}
	deck.ActivateWorkspace(id)
	return id, nil
}
