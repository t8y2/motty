package workbench

import (
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestWorkspaceAutoSaveTemplateLayoutAndOrder(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1, 2), 2)
	state.templates = []WorkspaceTemplate{template}
	state.Click(hitWorkspaceTemplatePresets.At(0))
	if !isGrid(state.templates[0].Layout, 1) {
		t.Fatal("choosing a template layout did not save it immediately")
	}
	state.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsTemplate, template: template.ID, index: 0}, 2)
	want := []string{template.HostIDs[1], template.HostIDs[2], template.HostIDs[0]}
	if !slices.Equal(state.templates[0].HostIDs, want) {
		t.Fatalf("reordered template = %v", state.templates[0].HostIDs)
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	loaded, _, err := loadState(state.options.StatePath)
	if err != nil || len(loaded.WorkspaceTemplates) != 1 || !isGrid(loaded.WorkspaceTemplates[0].Layout, 1) || !slices.Equal(loaded.WorkspaceTemplates[0].HostIDs, want) {
		t.Fatalf("auto-saved template = %+v, error = %v", loaded.WorkspaceTemplates, err)
	}
	if len(world.configs) != 0 {
		t.Fatal("template edits started connections")
	}
	state.Click(hitWorkspaceTemplateOpen.At(0))
	if len(world.configs) != 3 || world.configs[0].Host != state.hosts[1].Address {
		t.Fatal("auto-saved template could not be opened directly")
	}
}

func TestWorkspaceAutoSaveRuntimeLayoutKeepsEditingAndPersists(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	before := paneSessionIDs(state.deck.Panes(workspace, nil))
	active, dials := state.activeSessionID, len(world.configs)
	state.Click(hitWorkspaceHeaderLayout)
	state.Click(hitWorkspaceHeaderPresets.At(1))
	if !isGrid(state.managedWorkspaceLayouts[workspace], 2) || state.workspaceHeaderMenu != workspaceHeaderMenuLayout {
		t.Fatal("runtime layout did not apply immediately and keep the editor open")
	}
	state.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsRuntime, index: 0}, 2)
	if got := paneSessionIDs(state.deck.Panes(workspace, nil)); !slices.Equal(got, []uint32{before[1], before[2], before[0]}) {
		t.Fatalf("runtime order = %v", got)
	}
	state.Click(hitWorkspaceHeaderPresets.At(0))
	if state.workspaceLayoutDraft.err != "" || !isGrid(state.managedWorkspaceLayouts[workspace], 1) {
		t.Fatal("a second edit was rejected as a stale layout")
	}
	state.Click(hitWorkspaceHeaderLayoutDismiss)
	loaded, _, err := loadState(state.options.StatePath)
	want := state.savedWorkspaceDeck()
	if err != nil || !reflect.DeepEqual(loaded.Deck, &want) {
		t.Fatalf("runtime layout was not persisted: %v", err)
	}
	if state.activeSessionID != active || len(world.configs) != dials {
		t.Fatal("layout auto-save changed the active session or reconnected")
	}
}

func TestWorkspaceHasOneCreationEntryAndNoLayoutConfirmation(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	for _, populated := range []bool{false, true} {
		if populated {
			state.templates = []WorkspaceTemplate{mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1), 2)}
		}
		canvas, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900})
		panelHit(t, canvas, hitWorkspaceNew)
		if slices.Contains(texts, "New template") {
			t.Fatal("the separate New template button is still visible")
		}
	}
	state.Click(hitWorkspaceTemplatePresets.At(0))
	_, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if slices.Contains(texts, "Save changes") || slices.Contains(texts, "Cancel") {
		t.Fatal("template layout still requires Save or Cancel")
	}
	launchTemporaryWorkspace(t, state, 0, 1)
	state.Click(hitWorkspaceHeaderLayout)
	_, texts = paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if slices.Contains(texts, "Apply layout") || slices.Contains(texts, "Cancel") {
		t.Fatal("runtime layout still requires Apply or Cancel")
	}
}
