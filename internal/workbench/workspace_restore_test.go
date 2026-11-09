package workbench

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func TestWorkspaceRestoredActiveTabKeepsHeaderAndLayout(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, first, 0, 1, 2)
	first.Click(hitWorkspaceHeaderLayout)
	first.Click(hitWorkspaceHeaderPresets.At(1))
	first.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsRuntime, index: 0}, 2)
	first.Click(hitWorkspaceHeaderLayoutDismiss)
	before := paneHostAddresses(first, workspace)
	dials := len(world.configs)
	second := newTestState(t, options)
	if !second.workspaceManaged(workspace) || !isGrid(second.managedWorkspaceLayouts[workspace], 2) || second.deck.ActiveWorkspace() != workspace {
		t.Fatal("restoring the active workspace lost its header identity or columns")
	}
	if !slices.Equal(paneHostAddresses(second, workspace), before) || len(world.configs) != dials {
		t.Fatal("restoring changed pane order or dialed connections")
	}
	canvas, _ := paintAt(second, gfx.Size{Width: 960, Height: 600})
	panelHit(t, canvas, hitWorkspaceHeaderBack)
	panelHit(t, canvas, hitWorkspaceHeaderSave)
	panelHit(t, canvas, hitWorkspaceHeaderLayout)
	if hit := panelHit(t, canvas, hitWorkspaceHeaderBroadcast); !hit.Disabled || second.broadcast != 0 {
		t.Fatal("restored, disconnected workspace can broadcast")
	}
	if _, ok := second.temporaryWorkspaceLayouts[workspace]; !ok {
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
	if _, temporary := second.temporaryWorkspaceLayouts[workspace]; temporary {
		t.Fatal("a saved workspace was restored as temporary")
	}
}

func TestWorkspaceTemporaryHeaderSavesCurrentLayoutAsTemplate(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	state.Click(hitWorkspaceHeaderLayout)
	state.Click(hitWorkspaceHeaderPresets.At(1))
	state.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsRuntime, index: 0}, 2)
	state.Click(hitWorkspaceHeaderLayoutDismiss)
	canvas, _ := paintAt(state, gfx.Size{Width: 960, Height: 600})
	panelHit(t, canvas, hitWorkspaceHeaderSave)
	state.Click(hitWorkspaceHeaderSave)
	if state.modal != modalWorkspaceSave || !isGrid(state.workspaceSave.layout, 2) {
		t.Fatal("header action did not open the current workspace's save dialog")
	}
	state.workspaceSave.name.Insert("Current layout")
	state.Click(hitWorkspaceSaveSubmit)
	want := workspaceCatalogIDs(state, 1, 2, 0)
	if len(state.templates) != 1 || !isGrid(state.templates[0].Layout, 2) || !slices.Equal(state.templates[0].HostIDs, want) || len(world.configs) != 3 {
		t.Fatal("header save lost the current layout or reconnected its sessions")
	}
	state.deck.ActivateWorkspace(workspace)
	state.activeSection = sectionTerminal
	canvas, _ = paintAt(state, gfx.Size{Width: 960, Height: 600})
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
	canvas, _ := paintAt(second, gfx.Size{Width: 960, Height: 600})
	panelHit(t, canvas, hitWorkspaceHeaderBack)
}

func TestWorkspaceRestoreIgnoresUnknownOrInvalidLayoutMetadata(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1)
	state.restoreWorkspaceLayouts([]savedWorkspaceLayout{
		{ID: workspace, Layout: layoutcore.GridSpec(0), Temporary: true},
		{ID: workspace, Layout: layoutcore.GridSpec(4), Temporary: true},
		{ID: 0, Layout: layoutcore.GridSpec(1)},
		{ID: workspace + 100, Layout: layoutcore.GridSpec(1)},
	})
	if state.workspaceManaged(workspace) || len(state.managedWorkspaceLayouts) != 0 || len(state.temporaryWorkspaceLayouts) != 0 {
		t.Fatal("invalid metadata was attached to a restored workspace")
	}
}

// A file from before layouts existed holds only a grid width for each workspace.
// Reading it and writing it back must work, or the save that follows every edit
// fails for as long as the file is stored.
func TestLegacyWorkspaceLayoutEntryRoundTripsAndDoesNotBlockSaving(t *testing.T) {
	const legacy = `{"workspace_layouts":[{"id":2,"columns":3,"temporary":true}]}`
	saved, err := projectState([]byte(legacy))
	if err != nil {
		t.Fatalf("a stored legacy layout entry cannot be read back: %v", err)
	}
	var again savedState
	if err := json.Unmarshal(saved, &again); err != nil || len(again.WorkspaceLayouts) != 1 || again.WorkspaceLayouts[0].Columns != 3 {
		t.Fatalf("the entry did not survive: %s (%v)", saved, err)
	}
}
