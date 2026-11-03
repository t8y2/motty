package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

func TestWorkspaceRestoredActiveTabKeepsHeaderAndLayout(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, first, 0, 1, 2)
	first.Click(hitWorkspaceHeaderLayout)
	first.Click(hitWorkspaceHeaderColumnsList.At(2))
	first.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsRuntime, index: 0}, 2)
	first.Click(hitWorkspaceHeaderLayoutDismiss)
	before := paneHostAddresses(first, workspace)
	dials := len(world.configs)
	second := newTestState(t, options)
	if !second.workspaceManaged(workspace) || second.managedWorkspaceColumns[workspace] != 2 || second.deck.ActiveWorkspace() != workspace {
		t.Fatal("restoring the active workspace lost its header identity or columns")
	}
	if !slices.Equal(paneHostAddresses(second, workspace), before) || len(world.configs) != dials {
		t.Fatal("restoring changed pane order or dialed connections")
	}
	canvas, _ := paintAt(second, ui.Size{Width: 960, Height: 600})
	panelHit(t, canvas, hitWorkspaceHeaderBack)
	panelHit(t, canvas, hitWorkspaceHeaderSave)
	panelHit(t, canvas, hitWorkspaceHeaderLayout)
	if hit := panelHit(t, canvas, hitWorkspaceHeaderBroadcast); !hit.Disabled || second.broadcast != 0 {
		t.Fatal("restored, disconnected workspace can broadcast")
	}
	if _, ok := second.temporaryWorkspaceColumns[workspace]; !ok {
		t.Fatal("restored temporary workspace cannot be saved as a template")
	}
}

func TestWorkspaceRestoredTemplateAndOrdinaryTabsKeepTheirKinds(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	template := mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(first, 0), 1)
	first.templates = []WorkspaceTemplate{template}
	first.activeSection = sectionWorkspaces
	first.selectedWorkspaceTemplateID = template.ID
	first.applySelectedWorkspaceTemplate()
	workspace := first.deck.ActiveWorkspace()
	first.createHostSession(first.hosts[0], first.sessionAddress(first.hosts[0]))
	ordinary := first.deck.ActiveWorkspace()
	if _, err := first.deck.Split(ordinary, first.deck.ActivePane(ordinary), layoutcore.Horizontal); err != nil {
		t.Fatal(err)
	}
	first.save()
	second := newTestState(t, options)
	if !second.workspaceManaged(workspace) || second.workspaceManaged(ordinary) {
		t.Fatal("single-terminal workspace and ordinary tab were not distinguished on restore")
	}
	if _, temporary := second.temporaryWorkspaceColumns[workspace]; temporary {
		t.Fatal("a saved workspace was restored as temporary")
	}
}

func TestWorkspaceTemporaryHeaderSavesCurrentLayoutAsTemplate(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	state.Click(hitWorkspaceHeaderLayout)
	state.Click(hitWorkspaceHeaderColumnsList.At(2))
	state.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsRuntime, index: 0}, 2)
	state.Click(hitWorkspaceHeaderLayoutDismiss)
	canvas, _ := paintAt(state, ui.Size{Width: 960, Height: 600})
	panelHit(t, canvas, hitWorkspaceHeaderSave)
	state.Click(hitWorkspaceHeaderSave)
	if state.modal != modalWorkspaceSave || state.workspaceSave.columns != 2 {
		t.Fatal("header action did not open the current workspace's save dialog")
	}
	state.workspaceSave.name.Insert("Current layout")
	state.Click(hitWorkspaceSaveSubmit)
	want := workspaceCatalogIDs(state, 1, 2, 0)
	if len(state.templates) != 1 || state.templates[0].Columns != 2 || !slices.Equal(state.templates[0].HostIDs, want) || len(world.configs) != 3 {
		t.Fatal("header save lost the current layout or reconnected its sessions")
	}
	state.deck.ActivateWorkspace(workspace)
	state.activeSection = sectionTerminal
	canvas, _ = paintAt(state, ui.Size{Width: 960, Height: 600})
	if _, exists := hitByID(canvas, hitWorkspaceHeaderSave); exists {
		t.Fatal("saved workspace still has the temporary-workspace action")
	}
}

func TestWorkspaceLegacySplitTabRestoresNavigation(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, first, 0, 1)
	// This is the previous persisted shape: a deck and sessions without any
	// workspace identity. Do not infer identity from a localized tab title.
	legacy := savedState{Hosts: first.snapshot().Hosts, Sessions: first.snapshot().Sessions, Workspace: "terminal", Deck: first.snapshot().Deck}
	if err := saveState(options.StatePath, legacy); err != nil {
		t.Fatal(err)
	}
	second := newTestState(t, options)
	if !second.workspaceManaged(workspace) {
		t.Fatal("legacy multi-terminal tab still has no workspace navigation")
	}
	canvas, _ := paintAt(second, ui.Size{Width: 960, Height: 600})
	panelHit(t, canvas, hitWorkspaceHeaderBack)
}

func TestWorkspaceRestoreIgnoresUnknownOrInvalidLayoutMetadata(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1)
	state.restoreWorkspaceLayouts([]savedWorkspaceLayout{
		{ID: workspace, Columns: 0, Temporary: true},
		{ID: workspace, Columns: 4, Temporary: true},
		{ID: 0, Columns: 1},
		{ID: workspace + 100, Columns: 1},
	})
	if state.workspaceManaged(workspace) || len(state.managedWorkspaceColumns) != 0 || len(state.temporaryWorkspaceColumns) != 0 {
		t.Fatal("invalid metadata was attached to a restored workspace")
	}
}
