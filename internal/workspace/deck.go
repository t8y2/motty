package workspace

import (
	"errors"
	"fmt"
	"slices"
)

// WorkspaceID identifies one workspace tab. Zero is invalid.
type WorkspaceID uint32

// PaneID identifies one leaf in a workspace split tree. Zero is invalid.
type PaneID uint32

// Deck limits keep accidental splits and corrupted persistence bounded.
const (
	MaxWorkspaces       = 32
	MaxPanes            = 8
	MinPaneRatio        = float32(0.15)
	MaxPaneRatio        = float32(0.85)
	DefaultContextWidth = float32(340)
	MinContextWidth     = float32(280)
	MaxContextWidth     = float32(480)
)

// ContextKind identifies the one shared panel attached to a workspace.
type ContextKind uint8

const (
	ContextNone ContextKind = iota
	ContextSFTP
	ContextAI
	ContextMonitor
	ContextSessionDetails
)

// ContextSide is the edge at which a workspace's context panel is docked.
// The zero value preserves the original left-docked layout.
type ContextSide uint8

const (
	ContextLeft ContextSide = iota
	ContextRight
)

// ContextState is workspace-owned panel presentation state. Session-specific
// content remains with the session selected by ActivePane.
type ContextState struct {
	Kind  ContextKind
	Open  bool
	Width float32
	Side  ContextSide
}

var (
	ErrWorkspaceNotFound = errors.New("workspace: workspace not found")
	ErrPaneNotFound      = errors.New("workspace: pane not found")
	ErrPaneFull          = errors.New("workspace: pane already has a session")
	ErrWorkspaceFull     = errors.New("workspace: workspace limit reached")
	ErrPaneLimit         = errors.New("workspace: pane limit reached")
)

// Pane describes one workspace leaf and its optional session binding.
type Pane struct {
	ID        PaneID
	SessionID SessionID
}

// WorkspaceInfo is the stable metadata needed to paint workspace tabs.
type WorkspaceInfo struct {
	ID         WorkspaceID
	Title      string
	ActivePane PaneID
	PaneCount  int
}

type workspaceRecord struct {
	id         WorkspaceID
	title      string
	layout     Layout
	panes      []Pane
	activePane PaneID
	context    ContextState
	revision   uint64
}

// Deck owns workspace tabs, their independent split trees, and pane/session
// bindings. It is synchronous, starts no background work, and uses flat slices
// plus compact layout slots rather than pointer-linked trees.
type Deck struct {
	workspaces      []workspaceRecord
	activeWorkspace WorkspaceID
	nextWorkspace   WorkspaceID
	nextPane        PaneID
}

// Create appends a workspace containing one empty pane and activates it.
func (deck *Deck) Create(title string) (WorkspaceID, PaneID, error) {
	if len(deck.workspaces) >= MaxWorkspaces {
		return 0, 0, ErrWorkspaceFull
	}
	workspaceID := deck.allocateWorkspaceID()
	paneID := deck.allocatePaneID()
	layout, err := New(SessionID(paneID))
	if err != nil {
		return 0, 0, err
	}
	deck.workspaces = append(deck.workspaces, workspaceRecord{
		id: workspaceID, title: title, layout: layout,
		panes: []Pane{{ID: paneID}}, activePane: paneID, context: normalizeContext(ContextState{}), revision: 1,
	})
	deck.activeWorkspace = workspaceID
	return workspaceID, paneID, nil
}

// Workspaces appends tab metadata in display order.
func (deck *Deck) Workspaces(dst []WorkspaceInfo) []WorkspaceInfo {
	for index := range deck.workspaces {
		workspace := &deck.workspaces[index]
		dst = append(dst, WorkspaceInfo{
			ID: workspace.id, Title: workspace.title,
			ActivePane: workspace.activePane, PaneCount: len(workspace.panes),
		})
	}
	return dst
}

// ActiveWorkspace returns the selected workspace tab, or zero when empty.
func (deck *Deck) ActiveWorkspace() WorkspaceID { return deck.activeWorkspace }

// ActivateWorkspace selects an existing workspace without changing its panes.
func (deck *Deck) ActivateWorkspace(id WorkspaceID) bool {
	if _, ok := deck.workspace(id); !ok {
		return false
	}
	deck.activeWorkspace = id
	return true
}

// Rename changes a workspace title.
func (deck *Deck) Rename(id WorkspaceID, title string) error {
	workspace, ok := deck.workspace(id)
	if !ok {
		return ErrWorkspaceNotFound
	}
	workspace.title = title
	return nil
}

// RemoveWorkspace removes a tab and returns the sessions it owned. The next
// tab at the same index wins; removing the last tab selects its predecessor.
func (deck *Deck) RemoveWorkspace(id WorkspaceID, sessions []SessionID) ([]SessionID, error) {
	index := deck.workspaceIndex(id)
	if index < 0 {
		return sessions, ErrWorkspaceNotFound
	}
	for _, pane := range deck.workspaces[index].panes {
		if pane.SessionID != 0 {
			sessions = append(sessions, pane.SessionID)
		}
	}
	deck.workspaces = slices.Delete(deck.workspaces, index, index+1)
	if deck.activeWorkspace == id {
		if len(deck.workspaces) == 0 {
			deck.activeWorkspace = 0
		} else {
			if index == len(deck.workspaces) {
				index--
			}
			deck.activeWorkspace = deck.workspaces[index].id
		}
	}
	return sessions, nil
}

// Panes appends the workspace's panes in visual tree order.
func (deck *Deck) Panes(workspaceID WorkspaceID, dst []Pane) []Pane {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return dst
	}
	order := workspace.layout.Sessions(nil)
	for _, item := range order {
		if pane, ok := workspace.pane(PaneID(item)); ok {
			dst = append(dst, *pane)
		}
	}
	return dst
}

// ActivePane returns the selected pane in a workspace.
func (deck *Deck) ActivePane(workspaceID WorkspaceID) PaneID {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return 0
	}
	return workspace.activePane
}

// Revision changes whenever pane geometry, binding, or active-pane metadata
// changes. Paint caches can skip split-tree traversal while it stays equal.
func (deck *Deck) Revision(workspaceID WorkspaceID) (uint64, bool) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return 0, false
	}
	return workspace.revision, true
}

// Context returns one workspace's shared context-panel state.
func (deck *Deck) Context(workspaceID WorkspaceID) (ContextState, bool) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return ContextState{}, false
	}
	return normalizeContext(workspace.context), true
}

// SetContext updates presentation state without touching panes or sessions.
func (deck *Deck) SetContext(workspaceID WorkspaceID, context ContextState) error {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	if err := validateContext(context); err != nil {
		return err
	}
	workspace.context = normalizeContext(context)
	return nil
}

// ActivatePane makes exactly one existing pane the workspace target.
func (deck *Deck) ActivatePane(workspaceID WorkspaceID, paneID PaneID) bool {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return false
	}
	if _, ok := workspace.pane(paneID); !ok {
		return false
	}
	if workspace.activePane != paneID {
		workspace.activePane = paneID
		workspace.revision++
	}
	deck.activeWorkspace = workspaceID
	return true
}

// PaneForSession finds the unique pane displaying sessionID.
func (deck *Deck) PaneForSession(sessionID SessionID) (WorkspaceID, PaneID, bool) {
	if sessionID == 0 {
		return 0, 0, false
	}
	for workspaceIndex := range deck.workspaces {
		workspace := &deck.workspaces[workspaceIndex]
		for paneIndex := range workspace.panes {
			if workspace.panes[paneIndex].SessionID == sessionID {
				return workspace.id, workspace.panes[paneIndex].ID, true
			}
		}
	}
	return 0, 0, false
}

// Split creates an empty pane after target and makes it active.
func (deck *Deck) Split(workspaceID WorkspaceID, target PaneID, axis Axis) (PaneID, error) {
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
	paneID := deck.allocatePaneID()
	if err := workspace.layout.Split(SessionID(target), SessionID(paneID), axis, 0.5, After); err != nil {
		return 0, err
	}
	workspace.panes = append(workspace.panes, Pane{ID: paneID})
	workspace.activePane = paneID
	workspace.revision++
	deck.activeWorkspace = workspaceID
	return paneID, nil
}

// Move places pane beside target inside one workspace while preserving both
// stable pane IDs and their session bindings.
func (deck *Deck) Move(workspaceID WorkspaceID, paneID, target PaneID, axis Axis, ratio float32, placement Placement) error {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	if _, ok := workspace.pane(paneID); !ok {
		return ErrPaneNotFound
	}
	if _, ok := workspace.pane(target); !ok {
		return ErrPaneNotFound
	}
	ratio = min(max(ratio, MinPaneRatio), MaxPaneRatio)
	if err := workspace.layout.Move(SessionID(paneID), SessionID(target), axis, ratio, placement); err != nil {
		return err
	}
	workspace.activePane = paneID
	workspace.revision++
	deck.activeWorkspace = workspaceID
	return nil
}

// ClosePane removes one leaf and collapses its parent split. It returns the
// detached session, if any. Closing the final pane leaves an empty workspace.
func (deck *Deck) ClosePane(workspaceID WorkspaceID, paneID PaneID) (SessionID, error) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return 0, ErrWorkspaceNotFound
	}
	index := workspace.paneIndex(paneID)
	if index < 0 {
		return 0, ErrPaneNotFound
	}
	visual := workspace.layout.Sessions(nil)
	visualIndex := 0
	for at, item := range visual {
		if PaneID(item) == paneID {
			visualIndex = at
			break
		}
	}
	sessionID := workspace.panes[index].SessionID
	if err := workspace.layout.Remove(SessionID(paneID)); err != nil {
		return 0, err
	}
	workspace.panes = slices.Delete(workspace.panes, index, index+1)
	workspace.revision++
	if len(workspace.panes) == 0 {
		workspace.activePane = 0
		return sessionID, nil
	}
	next := visualIndex + 1
	if next >= len(visual) {
		next = visualIndex - 1
	}
	workspace.activePane = PaneID(visual[next])
	return sessionID, nil
}

// EnsurePane creates the first empty pane of an empty workspace.
func (deck *Deck) EnsurePane(workspaceID WorkspaceID) (PaneID, error) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return 0, ErrWorkspaceNotFound
	}
	if workspace.activePane != 0 {
		return workspace.activePane, nil
	}
	paneID := deck.allocatePaneID()
	if err := workspace.layout.Add(SessionID(paneID)); err != nil {
		return 0, err
	}
	workspace.panes = append(workspace.panes, Pane{ID: paneID})
	workspace.activePane = paneID
	workspace.revision++
	return paneID, nil
}

// Bind installs a session in an empty pane. A session can appear in only one
// pane across the complete deck.
func (deck *Deck) Bind(workspaceID WorkspaceID, paneID PaneID, sessionID SessionID) error {
	if sessionID == 0 {
		return ErrInvalidSession
	}
	for workspaceIndex := range deck.workspaces {
		for paneIndex := range deck.workspaces[workspaceIndex].panes {
			if deck.workspaces[workspaceIndex].panes[paneIndex].SessionID == sessionID {
				return ErrSessionExists
			}
		}
	}
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	pane, ok := workspace.pane(paneID)
	if !ok {
		return ErrPaneNotFound
	}
	if pane.SessionID != 0 {
		return ErrPaneFull
	}
	pane.SessionID = sessionID
	workspace.activePane = paneID
	workspace.revision++
	deck.activeWorkspace = workspaceID
	return nil
}

// Unbind clears a pane without removing it from the split tree.
func (deck *Deck) Unbind(workspaceID WorkspaceID, paneID PaneID) (SessionID, error) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return 0, ErrWorkspaceNotFound
	}
	pane, ok := workspace.pane(paneID)
	if !ok {
		return 0, ErrPaneNotFound
	}
	sessionID := pane.SessionID
	pane.SessionID = 0
	workspace.revision++
	return sessionID, nil
}

// Pane returns one pane binding.
func (deck *Deck) Pane(workspaceID WorkspaceID, paneID PaneID) (Pane, bool) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return Pane{}, false
	}
	pane, ok := workspace.pane(paneID)
	if !ok {
		return Pane{}, false
	}
	return *pane, true
}

// Layout returns the workspace split tree for read-only traversal.
func (deck *Deck) Layout(workspaceID WorkspaceID) (*Layout, bool) {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return nil, false
	}
	return &workspace.layout, true
}

// SetRatio constrains one split to the product-safe range.
func (deck *Deck) SetRatio(workspaceID WorkspaceID, split NodeID, ratio float32) error {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	ratio = min(max(ratio, MinPaneRatio), MaxPaneRatio)
	if err := workspace.layout.SetRatio(split, ratio); err != nil {
		return err
	}
	workspace.revision++
	return nil
}

// Validate checks every workspace tree, pane binding, active ID, and global
// session uniqueness.
func (deck *Deck) Validate() error {
	if len(deck.workspaces) > MaxWorkspaces {
		return fmt.Errorf("%w: %d workspaces", ErrInvalidLayout, len(deck.workspaces))
	}
	workspaceIDs := make(map[WorkspaceID]struct{}, len(deck.workspaces))
	paneIDs := make(map[PaneID]struct{})
	sessionIDs := make(map[SessionID]struct{})
	activeFound := deck.activeWorkspace == 0 && len(deck.workspaces) == 0
	for index := range deck.workspaces {
		workspace := &deck.workspaces[index]
		if workspace.id == 0 || len(workspace.panes) > MaxPanes {
			return ErrInvalidLayout
		}
		if _, exists := workspaceIDs[workspace.id]; exists {
			return ErrInvalidLayout
		}
		workspaceIDs[workspace.id] = struct{}{}
		if workspace.id == deck.activeWorkspace {
			activeFound = true
		}
		if err := workspace.layout.Validate(); err != nil {
			return err
		}
		order := workspace.layout.Sessions(nil)
		if len(order) != len(workspace.panes) {
			return ErrInvalidLayout
		}
		activePaneFound := workspace.activePane == 0 && len(workspace.panes) == 0
		for _, pane := range workspace.panes {
			if pane.ID == 0 {
				return ErrInvalidLayout
			}
			if _, exists := paneIDs[pane.ID]; exists {
				return ErrInvalidLayout
			}
			paneIDs[pane.ID] = struct{}{}
			if pane.ID == workspace.activePane {
				activePaneFound = true
			}
			if pane.SessionID != 0 {
				if _, exists := sessionIDs[pane.SessionID]; exists {
					return ErrInvalidLayout
				}
				sessionIDs[pane.SessionID] = struct{}{}
			}
		}
		for _, item := range order {
			if _, ok := workspace.pane(PaneID(item)); !ok {
				return ErrInvalidLayout
			}
		}
		if !activePaneFound {
			return ErrInvalidLayout
		}
		if err := validateContext(workspace.context); err != nil {
			return err
		}
	}
	if !activeFound {
		return ErrInvalidLayout
	}
	return nil
}

func normalizeContext(context ContextState) ContextState {
	if context.Width == 0 {
		context.Width = DefaultContextWidth
	}
	return context
}

func validateContext(context ContextState) error {
	if context.Kind > ContextSessionDetails || context.Side > ContextRight || context.Open && context.Kind == ContextNone ||
		context.Width != context.Width || context.Width != 0 && (context.Width < MinContextWidth || context.Width > MaxContextWidth) {
		return ErrInvalidLayout
	}
	return nil
}

func (deck *Deck) workspace(id WorkspaceID) (*workspaceRecord, bool) {
	index := deck.workspaceIndex(id)
	if index < 0 {
		return nil, false
	}
	return &deck.workspaces[index], true
}

func (deck *Deck) workspaceIndex(id WorkspaceID) int {
	for index := range deck.workspaces {
		if deck.workspaces[index].id == id {
			return index
		}
	}
	return -1
}

func (workspace *workspaceRecord) pane(id PaneID) (*Pane, bool) {
	index := workspace.paneIndex(id)
	if index < 0 {
		return nil, false
	}
	return &workspace.panes[index], true
}

func (workspace *workspaceRecord) paneIndex(id PaneID) int {
	for index := range workspace.panes {
		if workspace.panes[index].ID == id {
			return index
		}
	}
	return -1
}

func (deck *Deck) allocateWorkspaceID() WorkspaceID {
	deck.nextWorkspace++
	if deck.nextWorkspace == 0 {
		deck.nextWorkspace++
	}
	return deck.nextWorkspace
}

func (deck *Deck) allocatePaneID() PaneID {
	deck.nextPane++
	if deck.nextPane == 0 {
		deck.nextPane++
	}
	return deck.nextPane
}
