package workbench

import layoutcore "motty/internal/workspace"

const defaultPaneRatio = float32(0.5)

// FractionBounds describes a rectangle within the terminal content area. Every
// component is in the inclusive zero-to-one coordinate space; it is independent
// of pixels, DPI, and Viva geometry types.
type FractionBounds struct {
	X      float32
	Y      float32
	Width  float32
	Height float32
}

// Pane is one workspace leaf and its deterministic fractional bounds. A zero
// SessionID is an empty pane waiting for a host selection.
type Pane struct {
	ID        layoutcore.PaneID
	SessionID uint32
	Bounds    FractionBounds
	Active    bool
}

// PaneSplit is one divider and the full bounds it divides. NodeID is the stable
// event identity accepted by SetPaneRatio while the split remains live.
type PaneSplit struct {
	NodeID layoutcore.NodeID
	Axis   layoutcore.Axis
	Ratio  float32
	Bounds FractionBounds
}

// Panes appends panes from the active workspace in visual order.
func (state *State) Panes(dst []Pane) []Pane {
	workspaceID, layout := state.activeWorkspaceLayout()
	if layout == nil || layout.Root() == 0 {
		return dst
	}
	return state.appendWorkspacePanes(dst, workspaceID, layout, layout.Root(), FractionBounds{Width: 1, Height: 1})
}

// PaneSplits appends active-workspace divider targets in deterministic order.
func (state *State) PaneSplits(dst []PaneSplit) []PaneSplit {
	_, layout := state.activeWorkspaceLayout()
	if layout == nil || layout.Root() == 0 {
		return dst
	}
	return state.appendWorkspaceSplits(dst, layout, layout.Root(), FractionBounds{Width: 1, Height: 1})
}

// SplitPane moves an existing session pane beside another existing session.
// New empty panes are created by splitActivePane.
func (state *State) SplitPane(target, session uint32, axis layoutcore.Axis, placement layoutcore.Placement) error {
	targetWorkspace, targetPane, targetOK := state.deck.PaneForSession(layoutcore.SessionID(target))
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(session))
	if !targetOK {
		return layoutcore.ErrTargetNotFound
	}
	if !ok {
		return layoutcore.ErrSessionNotFound
	}
	if targetWorkspace != workspaceID {
		return layoutcore.ErrInvalidPlacement
	}
	if err := state.deck.Move(workspaceID, paneID, targetPane, axis, defaultPaneRatio, placement); err != nil {
		return err
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	return nil
}

// MovePane moves an existing session pane while preserving its Pane ID.
func (state *State) MovePane(session, target uint32, axis layoutcore.Axis, ratio float32, placement layoutcore.Placement) error {
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(session))
	targetWorkspace, targetPane, targetOK := state.deck.PaneForSession(layoutcore.SessionID(target))
	if !ok {
		return layoutcore.ErrSessionNotFound
	}
	if !targetOK {
		return layoutcore.ErrTargetNotFound
	}
	if workspaceID != targetWorkspace {
		return layoutcore.ErrInvalidPlacement
	}
	if err := state.deck.Move(workspaceID, paneID, targetPane, axis, ratio, placement); err != nil {
		return err
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	return nil
}

// SetPaneRatio updates one active-workspace divider without rebuilding it.
func (state *State) SetPaneRatio(split layoutcore.NodeID, ratio float32) error {
	workspaceID := state.deck.ActiveWorkspace()
	if workspaceID == 0 {
		return layoutcore.ErrInvalidNode
	}
	var bounds FractionBounds
	cached := state.workspaceViewCacheID == workspaceID
	if cached {
		cached = false
		for index := range state.paneSplits {
			if state.paneSplits[index].NodeID == split {
				bounds = state.paneSplits[index].Bounds
				cached = true
				break
			}
		}
	}
	if err := state.deck.SetRatio(workspaceID, split, ratio); err != nil {
		return err
	}
	if !cached {
		return nil
	}
	layout, ok := state.deck.Layout(workspaceID)
	if !ok || !state.updateCachedPaneSubtree(layout, split, bounds) {
		return nil
	}
	if revision, ok := state.deck.Revision(workspaceID); ok {
		state.workspaceViewRevision = revision
	}
	return nil
}

func (state *State) updateCachedPaneSubtree(layout *layoutcore.Layout, nodeID layoutcore.NodeID, bounds FractionBounds) bool {
	node, ok := layout.Node(nodeID)
	if !ok {
		return false
	}
	if node.Kind == layoutcore.KindLeaf {
		paneID := layoutcore.PaneID(node.Session)
		for index := range state.panes {
			if state.panes[index].ID == paneID {
				state.panes[index].Bounds = bounds
				return true
			}
		}
		return false
	}
	for index := range state.paneSplits {
		if state.paneSplits[index].NodeID == nodeID {
			state.paneSplits[index].Axis = node.Axis
			state.paneSplits[index].Ratio = node.Ratio
			state.paneSplits[index].Bounds = bounds
			first, second := splitFractionBounds(bounds, node.Axis, node.Ratio)
			return state.updateCachedPaneSubtree(layout, node.First, first) &&
				state.updateCachedPaneSubtree(layout, node.Second, second)
		}
	}
	return false
}

// ActivatePane makes the pane containing session the workspace target.
func (state *State) ActivatePane(session uint32) bool {
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(session))
	if !ok || !state.activatePane(workspaceID, paneID) {
		return false
	}
	return true
}

func (state *State) activatePane(workspaceID layoutcore.WorkspaceID, paneID layoutcore.PaneID) bool {
	previous := state.deck.ActivePane(workspaceID)
	if !state.deck.ActivatePane(workspaceID, paneID) {
		return false
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	if previous != paneID && state.viewport.Width > 0 && state.viewport.Height > 0 {
		state.fitWorkspaceTerminals(workspaceID)
		state.fitSFTP(state.viewport)
	}
	return true
}

func (state *State) activateWorkspace(id layoutcore.WorkspaceID) bool {
	if !state.deck.ActivateWorkspace(id) {
		return false
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	return true
}

func (state *State) syncActiveSession() {
	workspaceID := state.deck.ActiveWorkspace()
	paneID := state.deck.ActivePane(workspaceID)
	pane, ok := state.deck.Pane(workspaceID, paneID)
	if !ok {
		state.activeSessionID = 0
		return
	}
	state.activeSessionID = uint32(pane.SessionID)
	state.ensureContextSFTP()
}

// ensureContextSFTP opens the file list for the session the files panel follows
// when the panel is up but has no list yet: after a restart that reconnected the
// session, or after moving to a session that was connected without it.
func (state *State) ensureContextSFTP() {
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || context.Kind != layoutcore.ContextSFTP {
		return
	}
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.files == nil {
		state.openSFTP(session) // a session that is not connected is left alone
	}
}

func (state *State) activeWorkspaceLayout() (layoutcore.WorkspaceID, *layoutcore.Layout) {
	state.ensureWorkspaceDeck()
	workspaceID := state.deck.ActiveWorkspace()
	if workspaceID == 0 {
		return 0, nil
	}
	layout, ok := state.deck.Layout(workspaceID)
	if !ok {
		return 0, nil
	}
	return workspaceID, layout
}

// ensureWorkspaceDeck gives sessions that have no pane (a saved deck that is
// missing or does not restore, direct Session construction) one workspace.
// Normal connection creation binds sessions eagerly.
func (state *State) ensureWorkspaceDeck() {
	if len(state.sessions) == 0 {
		return
	}
	if state.deck.ActiveWorkspace() == 0 {
		workspaceID, paneID, err := state.deck.Create(state.sessions[0].Title)
		if err == nil {
			_ = state.deck.Bind(workspaceID, paneID, layoutcore.SessionID(state.sessions[0].ID))
			state.initializeSFTPPanel(workspaceID, false)
		}
	}
	wantedSession := state.activeSessionID
	for index := range state.sessions {
		sessionID := layoutcore.SessionID(state.sessions[index].ID)
		if _, _, ok := state.deck.PaneForSession(sessionID); ok {
			continue
		}
		workspaceID := state.deck.ActiveWorkspace()
		target := state.deck.ActivePane(workspaceID)
		paneID, err := state.deck.Split(workspaceID, target, layoutcore.Horizontal)
		if err != nil {
			continue
		}
		_ = state.deck.Bind(workspaceID, paneID, sessionID)
	}
	if wantedSession != 0 {
		if workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(wantedSession)); ok {
			state.deck.ActivatePane(workspaceID, paneID)
		}
	}
	state.syncActiveSession()
}

func (state *State) appendWorkspacePanes(dst []Pane, workspaceID layoutcore.WorkspaceID, layout *layoutcore.Layout, id layoutcore.NodeID, bounds FractionBounds) []Pane {
	node, ok := layout.Node(id)
	if !ok {
		return dst
	}
	if node.Kind == layoutcore.KindLeaf {
		paneID := layoutcore.PaneID(node.Session)
		pane, _ := state.deck.Pane(workspaceID, paneID)
		return append(dst, Pane{
			ID: paneID, SessionID: uint32(pane.SessionID), Bounds: bounds,
			Active: paneID == state.deck.ActivePane(workspaceID),
		})
	}
	first, second := splitFractionBounds(bounds, node.Axis, node.Ratio)
	dst = state.appendWorkspacePanes(dst, workspaceID, layout, node.First, first)
	return state.appendWorkspacePanes(dst, workspaceID, layout, node.Second, second)
}

func (state *State) appendWorkspaceSplits(dst []PaneSplit, layout *layoutcore.Layout, id layoutcore.NodeID, bounds FractionBounds) []PaneSplit {
	node, ok := layout.Node(id)
	if !ok || node.Kind == layoutcore.KindLeaf {
		return dst
	}
	dst = append(dst, PaneSplit{NodeID: id, Axis: node.Axis, Ratio: node.Ratio, Bounds: bounds})
	first, second := splitFractionBounds(bounds, node.Axis, node.Ratio)
	dst = state.appendWorkspaceSplits(dst, layout, node.First, first)
	return state.appendWorkspaceSplits(dst, layout, node.Second, second)
}

func splitFractionBounds(bounds FractionBounds, axis layoutcore.Axis, ratio float32) (FractionBounds, FractionBounds) {
	first := bounds
	second := bounds
	if axis == layoutcore.Horizontal {
		first.Width *= ratio
		second.X += first.Width
		second.Width -= first.Width
	} else {
		first.Height *= ratio
		second.Y += first.Height
		second.Height -= first.Height
	}
	return first, second
}

func (state *State) savedWorkspaceDeck() layoutcore.DeckDTO {
	dto := state.deck.DTO()
	persistedIDs := make(map[uint32]layoutcore.SessionID, len(state.sessions))
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.containerCommand == "" {
			persistedIDs[session.ID] = layoutcore.SessionID(len(persistedIDs) + 1)
		}
	}
	retained := dto.Workspaces[:0]
	for workspaceIndex := range dto.Workspaces {
		workspace := dto.Workspaces[workspaceIndex]
		hadTransient := false
		var firstPersistent layoutcore.PaneID
		for paneIndex := range workspace.Panes {
			pane := &workspace.Panes[paneIndex]
			if pane.SessionID == 0 {
				continue
			}
			persisted := persistedIDs[uint32(pane.SessionID)]
			if persisted == 0 {
				hadTransient = true
			} else if firstPersistent == 0 {
				firstPersistent = pane.ID
			}
			pane.SessionID = persisted
		}
		if hadTransient && firstPersistent == 0 {
			continue
		}
		if hadTransient {
			for _, pane := range workspace.Panes {
				if pane.ID == workspace.ActivePane && pane.SessionID == 0 {
					workspace.ActivePane = firstPersistent
				}
			}
		}
		retained = append(retained, workspace)
	}
	dto.Workspaces = retained
	activePresent := false
	for _, workspace := range dto.Workspaces {
		if workspace.ID == dto.ActiveWorkspace {
			activePresent = true
			break
		}
	}
	if !activePresent {
		dto.ActiveWorkspace = 0
		if len(dto.Workspaces) > 0 {
			dto.ActiveWorkspace = dto.Workspaces[0].ID
		}
	}
	return dto
}

func (state *State) restoreWorkspaceDeck(saved *layoutcore.DeckDTO, sourceSessions []uint32) {
	if saved != nil {
		dto := *saved
		dto.Workspaces = append([]layoutcore.WorkspaceDTO(nil), saved.Workspaces...)
		for workspaceIndex := range dto.Workspaces {
			workspace := &dto.Workspaces[workspaceIndex]
			workspace.Layout.Nodes = append([]layoutcore.NodeDTO(nil), workspace.Layout.Nodes...)
			workspace.Panes = append([]layoutcore.PaneDTO(nil), workspace.Panes...)
			for paneIndex := range workspace.Panes {
				pane := &workspace.Panes[paneIndex]
				if pane.SessionID == 0 {
					continue
				}
				index := int(pane.SessionID) - 1
				if index < 0 || index >= len(sourceSessions) {
					pane.SessionID = 0
					continue
				}
				pane.SessionID = layoutcore.SessionID(sourceSessions[index])
			}
		}
		if deck, err := layoutcore.RestoreDeck(dto); err == nil {
			state.deck = deck
			for _, workspace := range state.deck.Workspaces(nil) {
				panes := state.deck.Panes(workspace.ID, nil)
				if len(panes) == 1 {
					if session, ok := state.sessionByIDPointer(uint32(panes[0].SessionID)); ok {
						state.renameSession(session, workspace.Title)
					}
				}
				state.initializeSFTPPanel(workspace.ID, true)
			}
			state.syncActiveSession()
			return
		}
	}
	state.deck = layoutcore.Deck{}
	state.ensureWorkspaceDeck()
}
