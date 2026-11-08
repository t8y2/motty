package workbench

import (
	"errors"
	"fmt"
	"strings"

	layoutcore "motty/internal/workspace"
)

var ErrWorkspaceLaunchSave = errors.New("workbench: could not save workspace launch")

// workspaceLaunchIdentity is the metadata saved atomically with a new
// workspace. Zero fields mean an ordinary tab with no saved template identity.
type workspaceLaunchIdentity struct {
	managedLayout   layoutcore.LayoutSpec
	temporary       bool
	temporaryLayout layoutcore.LayoutSpec
}

// launchWorkspace opens an ordinary workspace tab.
func (state *State) launchWorkspace(plan WorkspaceLaunchPlan, title string) (layoutcore.WorkspaceID, []Session, error) {
	return state.launchWorkspaceWithIdentity(plan, title, workspaceLaunchIdentity{})
}

// launchWorkspaceWithIdentity creates one tab with every pane and Session
// binding in place before any connection starts. Its template identity is part
// of the same final save, so a crash cannot restore a half-classified tab.
// Structural failures leave the window unchanged and never dial; once dialing
// begins, each Session owns its own outcome.
func (state *State) launchWorkspaceWithIdentity(
	plan WorkspaceLaunchPlan,
	title string,
	identity workspaceLaunchIdentity,
) (layoutcore.WorkspaceID, []Session, error) {
	if err := validateWorkspaceLaunchPlan(plan); err != nil {
		return 0, nil, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = plan.Hosts[0].Label
	}

	previousBroadcast := state.broadcast
	previousWorkspace := state.deck.ActiveWorkspace()
	previousSessionID := state.activeSessionID
	previousSection := state.activeSection
	previousNextSessionID := state.nextSessionID
	previousPauseSave := state.pauseSave
	state.pauseSave++

	workspaceID, firstPane, err := state.deck.Create(title)
	if err != nil {
		state.pauseSave = previousPauseSave
		return 0, nil, err
	}
	paneIDs := make([]layoutcore.PaneID, 1, len(plan.Hosts))
	paneIDs[0] = firstPane
	for len(paneIDs) < len(plan.Hosts) {
		paneID, splitErr := state.deck.Split(workspaceID, paneIDs[len(paneIDs)-1], layoutcore.Horizontal)
		if splitErr != nil {
			state.rollbackWorkspaceLaunch(workspaceID, nil, previousWorkspace, previousSessionID, previousSection, previousNextSessionID, previousBroadcast, previousPauseSave)
			return 0, nil, splitErr
		}
		paneIDs = append(paneIDs, paneID)
	}
	if err := state.deck.SetLayout(workspaceID, paneIDs, plan.Layout); err != nil {
		state.rollbackWorkspaceLaunch(workspaceID, nil, previousWorkspace, previousSessionID, previousSection, previousNextSessionID, previousBroadcast, previousPauseSave)
		return 0, nil, err
	}

	sessionIDs := make([]uint32, 0, len(plan.Hosts))
	for index, host := range plan.Hosts {
		session := state.bindWorkspaceSession(workspaceID, paneIDs[index], host, state.sessionAddress(host))
		if session == nil {
			state.rollbackWorkspaceLaunch(workspaceID, sessionIDs, previousWorkspace, previousSessionID, previousSection, previousNextSessionID, previousBroadcast, previousPauseSave)
			return 0, nil, layoutcore.ErrPaneFull
		}
		sessionIDs = append(sessionIDs, session.ID)
	}

	state.clearBroadcast()
	if identity.managedLayout.Kind != 0 {
		state.managedWorkspaceLayouts[workspaceID] = identity.managedLayout
	}
	if identity.temporary {
		layout := identity.temporaryLayout
		if layout.Kind == 0 {
			layout = identity.managedLayout
		}
		state.temporaryWorkspaceLayouts[workspaceID] = layout
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	state.refreshWorkspaceViews()
	state.rebuildTabOptions()
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	state.pauseSave = previousPauseSave
	if !state.saveResult() {
		state.rollbackWorkspaceLaunch(workspaceID, sessionIDs, previousWorkspace, previousSessionID, previousSection, previousNextSessionID, previousBroadcast, previousPauseSave)
		return 0, nil, fmt.Errorf("save workspace: %w", ErrWorkspaceLaunchSave)
	}

	for index, sessionID := range sessionIDs {
		session, ok := state.sessionByIDPointer(sessionID)
		if !ok {
			return workspaceID, nil, layoutcore.ErrSessionNotFound
		}
		state.startWorkspaceSession(session, plan.Hosts[index])
	}

	sessions := make([]Session, len(sessionIDs))
	for index, sessionID := range sessionIDs {
		session, _ := state.sessionByID(sessionID)
		sessions[index] = session
	}
	return workspaceID, sessions, nil
}

func validateWorkspaceLaunchPlan(plan WorkspaceLaunchPlan) error {
	if len(plan.Hosts) == 0 {
		return ErrWorkspaceLaunchEmpty
	}
	if len(plan.Hosts) > layoutcore.MaxPanes {
		return ErrWorkspaceLaunchFull
	}
	if plan.Layout.Validate(len(plan.Hosts)) != nil {
		return ErrWorkspaceTemplateLayout
	}
	return nil
}

// rollbackWorkspaceLaunch removes only structure created by this launch. It is
// called before dialing, so the Sessions have no transport or log row to close.
func (state *State) rollbackWorkspaceLaunch(
	workspaceID layoutcore.WorkspaceID,
	sessionIDs []uint32,
	previousWorkspace layoutcore.WorkspaceID,
	previousSessionID uint32,
	previousSection uint8,
	previousNextSessionID uint32,
	previousBroadcast layoutcore.WorkspaceID,
	previousPauseSave int,
) {
	if len(sessionIDs) > 0 {
		remove := make(map[uint32]struct{}, len(sessionIDs))
		for _, sessionID := range sessionIDs {
			remove[sessionID] = struct{}{}
		}
		kept := state.sessions[:0]
		for _, session := range state.sessions {
			if _, drop := remove[session.ID]; !drop {
				kept = append(kept, session)
			}
		}
		clear(state.sessions[len(kept):])
		state.sessions = kept
	}
	_, _ = state.deck.RemoveWorkspace(workspaceID, nil)
	delete(state.temporaryWorkspaceLayouts, workspaceID)
	delete(state.managedWorkspaceLayouts, workspaceID)
	state.broadcast = previousBroadcast
	if previousWorkspace != 0 {
		state.deck.ActivateWorkspace(previousWorkspace)
	}
	state.nextSessionID = previousNextSessionID
	state.activeSessionID = previousSessionID
	state.activeSection = previousSection
	state.refreshWorkspaceViews()
	state.rebuildTabOptions()
	state.pauseSave = previousPauseSave
}
