//go:build darwin || windows || linux

package workbench

import (
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestWorkspaceTemplateEditThroughWindowDriver(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		state.hosts[0].CatalogID, "host_missing", state.hosts[1].CatalogID,
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	var input app.TextInput
	view, err := headless.Open(app.Config{
		Title: "Workspace template edit", Width: 1280, Height: 800,
		Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			input.Editor = state.EditorFor(context.ID)
			if input.Editor == nil {
				return nil
			}
			return &input
		},
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok || hit.Block || hit.Disabled {
			t.Fatalf("control %v unavailable: %+v", id, hit)
		}
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}

	click(hitWorkspaceTemplateMore.At(0))
	click(hitWorkspaceTemplateMenuEdit)
	if err := view.Type("Critical"); err != nil {
		t.Fatal(err)
	}
	click(hitWorkspaceDraftRemoveItem.At(1))
	click(workspaceDraftHostID(2))
	click(hitWorkspaceDraftPresets.At(2))
	for i := 0; i < 80 && view.Snapshot().Focused != hitWorkspaceDraftCards.At(1); i++ {
		if err := view.Driver().Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
	}
	if got := view.Snapshot().Focused; got != hitWorkspaceDraftCards.At(1) {
		t.Fatalf("cannot focus card for reordering: %v", got)
	}
	if err := view.Driver().Key(gfx.KeyLeft, gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	click(hitWorkspaceDraftSubmit)
	want := []string{state.hosts[1].CatalogID, state.hosts[0].CatalogID, state.hosts[2].CatalogID}
	if state.modal != modalNone || len(state.templates) != 1 || state.templates[0].ID != template.ID ||
		state.templates[0].Name != "Critical" || !isGrid(state.templates[0].Layout, 3) ||
		!slices.Equal(state.templates[0].HostIDs, want) || len(world.configs) != 0 {
		t.Fatalf("edited templates=%+v modal=%d dials=%d", state.templates, state.modal, len(world.configs))
	}
	loaded, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK || !reflect.DeepEqual(loaded.WorkspaceTemplates, state.templates) {
		t.Fatalf("reloaded templates=%+v report=%+v err=%v", loaded.WorkspaceTemplates, report, err)
	}
}

func TestWorkspaceTemplateManagementThroughKeyboard(t *testing.T) {
	for _, test := range []struct {
		name string
		key  gfx.Key
	}{{"Enter", gfx.KeyEnter}, {"Space", gfx.KeySpace}} {
		t.Run(test.name, func(t *testing.T) {
			state := templateManagementState(t, 1)
			original := slices.Clone(state.templates[0].HostIDs)
			var input app.TextInput
			view, err := headless.Open(app.Config{
				Title: "Workspace template", Width: MinimumWindowWidth, Height: MinimumWindowHeight,
				Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
				OnScroll: state.Scroll, OnResize: state.Resize,
				TextInput: func(context app.TextInputContext) *app.TextInput {
					input.Editor = state.EditorFor(context.ID)
					if input.Editor == nil {
						return nil
					}
					return &input
				},
			}, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			press := func(key gfx.Key) {
				t.Helper()
				if err := view.Driver().Key(key, 0); err != nil {
					t.Fatal(err)
				}
			}
			focus := func(id gfx.ID) {
				t.Helper()
				var visited []gfx.ID
				for i := 0; i < 80 && view.Snapshot().Focused != id; i++ {
					visited = append(visited, view.Snapshot().Focused)
					press(gfx.KeyTab)
				}
				if got := view.Snapshot().Focused; got != id {
					t.Fatalf("cannot focus %v, got %v; visited %v; preview=%+v scroll=%g", id, got, visited, state.workspaceDraft.previewBounds, state.workspaceDraft.previewScroll)
				}
			}

			focus(hitWorkspaceTemplateMore.At(0))
			press(gfx.KeyEnter)
			if got := view.Snapshot().Focused; got != hitWorkspaceTemplateMenuEdit {
				t.Fatalf("menu focus = %v", got)
			}
			press(gfx.KeyEnter)
			if state.modal != modalWorkspaceDraft || view.Snapshot().Focused != hitWorkspaceDraftName {
				t.Fatalf("edit modal=%d focus=%v", state.modal, view.Snapshot().Focused)
			}
			focus(hitWorkspaceDraftPreviewScroll)
			press(gfx.KeyPageDown)
			if state.workspaceDraft.previewScroll == 0 {
				t.Fatal("keyboard could not scroll the template preview")
			}
			focus(hitWorkspaceDraftRemoveItem.At(1))
			press(test.key)
			if got := state.workspaceDraft.order; !slices.Equal(got, []string{original[0], original[2]}) {
				t.Fatalf("remove using %s left %v", test.name, got)
			}
			press(gfx.KeyEscape)
			if state.modal != modalNone || view.Snapshot().Focused != hitWorkspaceTemplateMore.At(0) {
				t.Fatalf("edit escape modal=%d focus=%v", state.modal, view.Snapshot().Focused)
			}
			press(gfx.KeySpace)
			press(gfx.KeyDown)
			press(gfx.KeyEnter)
			if state.modal != modalWorkspaceTemplateDelete || view.Snapshot().Focused != hitWorkspaceTemplateDeleteCancel {
				t.Fatalf("delete modal=%d focus=%v", state.modal, view.Snapshot().Focused)
			}
			press(gfx.KeyTab)
			press(gfx.KeyEnter)
			if state.modal != modalNone || len(state.templates) != 0 || view.Snapshot().Focused != hitWorkspaceNew {
				t.Fatalf("delete modal=%d templates=%v focus=%v", state.modal, state.templates, view.Snapshot().Focused)
			}
		})
	}
}
