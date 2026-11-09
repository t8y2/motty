//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

// The opt-in captures use fake hosts and shells, never the user's saved data.
func TestWorkspaceDesignCaptures(t *testing.T) {
	dir := os.Getenv("MOTTY_WORKSPACE_SCREENSHOTS")
	if dir == "" {
		t.Skip("set MOTTY_WORKSPACE_SCREENSHOTS for rendered workspace captures")
	}
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, light := range []bool{false, true} {
			for _, width := range []int{1280, 960, 680, 420} {
				name := fmt.Sprintf("%s-light%t-%d", language, light, width)
				t.Run(name, func(t *testing.T) {
					world, options := newWorld(t)
					state := newTestState(t, options)
					state.setLanguage(language)
					if light {
						state.setAppearance(appearanceLight)
					}
					state.templates = []WorkspaceTemplate{
						mustWorkspaceTemplate(t, "Production API", workspaceCatalogIDs(state, 0, 1, 2), 2),
						mustWorkspaceTemplate(t, "Staging", workspaceCatalogIDs(state, 1), 1),
					}
					state.activeSection = sectionWorkspaces
					capture := func(surface string) {
						t.Helper()
						var input app.TextInput
						height := 800
						if width == MinimumWindowWidth {
							height = MinimumWindowHeight
						}
						view, err := headless.Open(app.Config{Title: "Motty", Width: width, Height: height,
							Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
							TextInput: func(context app.TextInputContext) *app.TextInput {
								if context.Terminal {
									return state.TerminalInput(context)
								}
								if input.Editor = state.EditorFor(context.ID); input.Editor == nil {
									return nil
								}
								return &input
							},
							OnResize: state.Resize}, nil, headless.Options{Scale: 1.25})
						if err != nil {
							t.Fatal(err)
						}
						defer view.Close()
						if err := view.Wait(time.Second); err != nil {
							t.Fatal(err)
						}
						if err := view.Screenshot(filepath.Join(dir, name+"-"+surface+".png")); err != nil {
							t.Fatal(err)
						}
					}
					capture("library")
					state.openWorkspaceDraftEdit(state.templates[0].ID)
					capture("edit")
					state.closeModal()
					state.openWorkspaceTemplateDelete(state.templates[0].ID)
					capture("delete")
					state.closeModal()
					state.moveWorkspaceTemplateCard(state.templates[0].ID, 0, 1)
					capture("library-reordered")
					state.openWorkspaceDraft(workspaceDraftFixed)
					state.workspaceDraft.name.Insert("Production API")
					state.Click(workspaceDraftHostID(0))
					state.Click(workspaceDraftHostID(1))
					state.Click(workspaceDraftHostID(2))
					capture("draft")
					state.closeModal()
					launchTemporaryWorkspace(t, state, 0, 1, 2)
					capture("connecting")
					for _, connection := range world.conns {
						finishWorkspaceConnection(t, state, connection)
						connection.shell(t).say("$ hostname\r\napi.internal\r\n$ ")
					}
					state.Wake()
					capture("panes")
					state.Click(hitWorkspaceHeaderLayout)
					capture("layout")
					state.Click(hitWorkspaceHeaderLayoutDismiss)
					state.toggleBroadcast()
					capture("broadcast")
					state.Click(hitWorkspacePaneTools)
					capture("tools")
					state.save()
					state = newTestState(t, options)
					state.setLanguage(language)
					if light {
						state.setAppearance(appearanceLight)
					}
					capture("restored")
				})
			}
		}
	}
}

func TestWorkspacePaneToolsThroughWindowDriver(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	launchTemporaryWorkspace(t, state, 0, 1, 2)
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	view, err := headless.Open(app.Config{Title: "Workspace actions", Width: 960, Height: 600,
		Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnResize: state.Resize}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok || hit.Disabled || hit.Block {
			t.Fatalf("action %d is unavailable: %+v", id, hit)
		}
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	trigger := hitWorkspacePaneTools
	click(trigger)
	if state.workspaceHeaderMenu != workspaceHeaderMenuTools {
		t.Fatal("embedded terminal menu did not open")
	}
	if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.workspaceHeaderMenu != workspaceHeaderMenuNone || view.Snapshot().Focused != state.activeTerminalFocusHit() {
		t.Fatal("Escape did not return input to the active terminal")
	}
	click(trigger)
	index := -1
	for i := 0; i < state.workspacePaneTools.count; i++ {
		if state.workspacePaneTools.items[i].id == hitPaneSplitVertical {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("split action missing")
	}
	before := len(state.Panes(nil))
	click(hitWorkspacePaneToolList.At(index))
	if len(state.Panes(nil)) != before+1 || state.workspaceHeaderMenu != workspaceHeaderMenuNone {
		t.Fatal("menu split did not reach the active session")
	}
}
