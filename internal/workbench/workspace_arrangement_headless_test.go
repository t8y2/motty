//go:build darwin || windows || linux

package workbench

import (
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func workspaceEditorView(t *testing.T, state *State) *headless.Session {
	t.Helper()
	var input app.TextInput
	view, err := headless.Open(app.Config{Title: "Workspace layout", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			if input.Editor = state.EditorFor(context.ID); input.Editor != nil {
				return &input
			}
			return nil
		}}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

func workspaceEditorClick(t *testing.T, view *headless.Session, id gfx.ID) {
	t.Helper()
	hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
	if !ok || hit.Disabled || hit.Block {
		t.Fatalf("control %v is unavailable: %+v", id, hit)
	}
	if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func workspaceEditorDrag(t *testing.T, view *headless.Session, from, to gfx.ID) {
	t.Helper()
	source, ok := hitByIDSnapshot(view.Snapshot().Hits, from)
	if !ok {
		t.Fatalf("drag source %v missing", from)
	}
	target, ok := hitByIDSnapshot(view.Snapshot().Hits, to)
	if !ok {
		t.Fatalf("drag target %v missing", to)
	}
	x, y := source.Bounds.X+source.Bounds.Width/2, source.Bounds.Y+source.Bounds.Height/2
	tx, ty := target.Bounds.X+target.Bounds.Width/2, target.Bounds.Y+target.Bounds.Height/2
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(tx, ty); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(tx, ty); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceLayoutDragAutoSavesWithoutReconnectAndEscapeReturnsFocus(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	before := paneSessionIDs(state.deck.Panes(workspace, nil))
	active, dials := state.activeSessionID, len(world.configs)
	view := workspaceEditorView(t, state)
	workspaceEditorClick(t, view, hitWorkspaceHeaderLayout)
	workspaceEditorClick(t, view, hitWorkspaceHeaderPresets.At(1))
	workspaceEditorDrag(t, view, hitWorkspaceLayoutCards.At(0), hitWorkspaceLayoutCards.At(2))
	if got := paneSessionIDs(state.deck.Panes(workspace, nil)); !slices.Equal(got, []uint32{before[1], before[2], before[0]}) {
		t.Fatalf("applied order = %v", got)
	}
	if state.activeSessionID != active || len(world.configs) != dials || !isGrid(state.managedWorkspaceLayouts[workspace], 2) {
		t.Fatal("layout apply changed sessions or dialed again")
	}
	if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if view.Snapshot().Focused != state.activeTerminalFocusHit() {
		t.Fatalf("closing the editor did not restore terminal focus: %v", view.Snapshot().Focused)
	}
}

func TestWorkspaceTemplateDragAutoSavesThroughWindow(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1, 2), 2)
	state.templates = []WorkspaceTemplate{template}
	view := workspaceEditorView(t, state)
	workspaceEditorClick(t, view, hitWorkspaceTemplatePresets.At(0))
	if !isGrid(state.templates[0].Layout, 1) {
		t.Fatal("template preset did not auto-save")
	}
	workspaceEditorDrag(t, view, hitWorkspaceTemplateCards.At(0), hitWorkspaceTemplateCards.At(2))
	want := []string{template.HostIDs[1], template.HostIDs[2], template.HostIDs[0]}
	if !slices.Equal(state.templates[0].HostIDs, want) {
		t.Fatalf("auto-saved template = %+v", state.templates[0])
	}
	loaded, _, err := loadState(state.options.StatePath)
	if err != nil || len(loaded.WorkspaceTemplates) != 1 || !slices.Equal(loaded.WorkspaceTemplates[0].HostIDs, want) {
		t.Fatalf("persisted drag = %+v, %v", loaded.WorkspaceTemplates, err)
	}
	if len(world.configs) != 0 {
		t.Fatal("editing a template opened connections")
	}
	workspaceEditorClick(t, view, hitWorkspaceTemplateOpen.At(0))
	if len(world.configs) != 3 || world.configs[0].Host != "staging.example.com" {
		t.Fatal("auto-saved template could not be opened directly")
	}
}

func TestWorkspaceTemplateDragDoesNotOverwriteNewerOrder(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1, 2), 2)
	state.templates = []WorkspaceTemplate{template}
	view := workspaceEditorView(t, state)
	source, _ := hitByIDSnapshot(view.Snapshot().Hits, hitWorkspaceTemplateCards.At(0))
	target, _ := hitByIDSnapshot(view.Snapshot().Hits, hitWorkspaceTemplateCards.At(1))
	if err := view.Press(source.Bounds.X+20, source.Bounds.Y+20); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(target.Bounds.X+20, target.Bounds.Y+20); err != nil {
		t.Fatal(err)
	}
	want := []string{template.HostIDs[2], template.HostIDs[0], template.HostIDs[1]}
	state.templates[0].HostIDs = slices.Clone(want)
	state.save()
	if err := view.Release(target.Bounds.X+20, target.Bounds.Y+20); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(state.templates[0].HostIDs, want) {
		t.Fatal("a stale drag overwrote a newer template order")
	}
}

func TestWorkspaceRestoredTemporaryHeaderSaveThroughWindow(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	launchTemporaryWorkspaceWithColumns(t, first, 2, 0, 1)
	state := newTestState(t, options)
	view := workspaceEditorView(t, state)
	workspaceEditorClick(t, view, hitWorkspaceHeaderSave)
	if view.Snapshot().Focused != hitWorkspaceSaveName {
		t.Fatal("Save as template did not focus its name field")
	}
	if err := view.Type("Reopened workspace"); err != nil {
		t.Fatal(err)
	}
	workspaceEditorClick(t, view, hitWorkspaceSaveSubmit)
	if len(state.templates) != 1 || state.templates[0].Name != "Reopened workspace" || !isGrid(state.templates[0].Layout, 2) {
		t.Fatalf("saved restored workspace = %+v", state.templates)
	}
	if len(world.configs) != 2 {
		t.Fatal("restoring or saving a workspace started new connections")
	}
}

func TestWorkspaceDraftDragPreservesItsOrderOnSaveAndLaunch(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.openWorkspaceDraft(workspaceDraftFixed)
	state.workspaceDraft.name.Insert("API")
	state.Click(workspaceDraftHostID(0))
	state.Click(workspaceDraftHostID(1))
	view := workspaceEditorView(t, state)
	if err := view.Wait(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	workspaceEditorDrag(t, view, hitWorkspaceDraftCards.At(0), hitWorkspaceDraftCards.At(1))
	want := []string{state.hosts[1].CatalogID, state.hosts[0].CatalogID}
	if !slices.Equal(state.workspaceDraftSelectedCatalogIDs(), want) {
		t.Fatal("draft drag did not reorder the selected hosts")
	}
	workspaceEditorClick(t, view, hitWorkspaceDraftSubmit)
	if len(state.templates) != 1 || !slices.Equal(state.templates[0].HostIDs, want) {
		t.Fatalf("saved order = %+v", state.templates)
	}
	workspaceEditorClick(t, view, hitWorkspaceTemplateOpen.At(0))
	if len(world.configs) != 2 || world.configs[0].Host != "staging.example.com" || world.configs[1].Host != "prod.example.com" {
		t.Fatalf("launch discarded drag order: %+v", world.configs)
	}
}

func TestWorkspaceLocalTerminalDragSaveAndOpenThroughWindow(t *testing.T) {
	skipWithoutUnixShell(t)
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.localShell = "/bin/sh"
	state.activeSection = sectionWorkspaces
	state.openWorkspaceDraft(workspaceDraftFixed)
	state.workspaceDraft.name.Insert("Local + SSH")
	view := workspaceEditorView(t, state)
	workspaceEditorClick(t, view, hitWorkspaceDraftHostList.At(0))
	workspaceEditorClick(t, view, workspaceDraftHostID(0))
	workspaceEditorDrag(t, view, hitWorkspaceDraftCards.At(0), hitWorkspaceDraftCards.At(1))
	workspaceEditorClick(t, view, hitWorkspaceDraftSubmit)
	want := []string{state.hosts[0].CatalogID, workspaceLocalTerminalRef}
	if len(state.templates) != 1 || !slices.Equal(state.templates[0].HostIDs, want) {
		t.Fatalf("mixed template order = %+v", state.templates)
	}
	workspaceEditorClick(t, view, hitWorkspaceTemplateOpen.At(0))
	if len(state.sessions) != 2 || state.sessions[0].isLocal() || !state.sessions[1].isLocal() || len(world.conns) != 1 {
		t.Fatal("opening the template lost the local terminal or its pane order")
	}
}

func TestWorkspaceCardDragCancelsOutsideAndEscapeKeepsSavedEdits(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1, 2), 2)
	state.templates = []WorkspaceTemplate{template}
	view := workspaceEditorView(t, state)
	source, _ := hitByIDSnapshot(view.Snapshot().Hits, hitWorkspaceTemplateCards.At(0))
	x, y := source.Bounds.X+20, source.Bounds.Y+20
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(10, 10); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(10, 10); err != nil {
		t.Fatal(err)
	}
	if !state.templateUnchanged(template) {
		t.Fatal("an outside drop changed the saved template")
	}
	workspaceEditorDrag(t, view, hitWorkspaceTemplateCards.At(0), hitWorkspaceTemplateCards.At(2))
	saved := slices.Clone(state.templates[0].HostIDs)
	source, _ = hitByIDSnapshot(view.Snapshot().Hits, hitWorkspaceTemplateCards.At(0))
	target, _ := hitByIDSnapshot(view.Snapshot().Hits, hitWorkspaceTemplateCards.At(1))
	if err := view.Press(source.Bounds.X+20, source.Bounds.Y+20); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(target.Bounds.X+20, target.Bounds.Y+20); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(target.Bounds.X+20, target.Bounds.Y+20); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(state.templates[0].HostIDs, saved) || state.workspaceCardDrag.active {
		t.Fatal("Escape during drag changed a completed edit")
	}
	if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(state.templates[0].HostIDs, saved) {
		t.Fatal("Escape rolled back an auto-saved edit")
	}
}

func TestWorkspaceCardKeyboardReorderKeepsTheMovedHostFocused(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1, 2), 2)
	state.templates = []WorkspaceTemplate{template}
	view := workspaceEditorView(t, state)
	workspaceEditorClick(t, view, hitWorkspaceTemplateCards.At(0))
	if err := view.Driver().Key(gfx.KeyRight, gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	want := []string{template.HostIDs[1], template.HostIDs[0], template.HostIDs[2]}
	if !slices.Equal(state.templates[0].HostIDs, want) || view.Snapshot().Focused != hitWorkspaceTemplateCards.At(1) {
		t.Fatalf("keyboard reorder = %v, focus %v", state.templates[0].HostIDs, view.Snapshot().Focused)
	}
}
