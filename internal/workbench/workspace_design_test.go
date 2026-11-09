package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

func TestWorkspaceHeaderStatusNeverOverlapsActions(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, width := range []float32{420, 680, 1280} {
			world, options := newWorld(t)
			state := newTestState(t, options)
			state.setLanguage(language)
			launchTemporaryWorkspace(t, state, 0, 1)
			for _, connection := range world.conns {
				finishWorkspaceConnection(t, state, connection)
			}
			canvas, _ := paintAt(state, gfx.Size{Width: width, Height: 800})
			status := fmt.Sprintf(state.messages.WorkspaceHeaderConnectionFmt, 2, 2)
			for _, text := range canvas.Texts() {
				if text.Value != status {
					continue
				}
				for _, id := range []gfx.ID{hitWorkspaceHeaderSave, hitWorkspaceHeaderBroadcast, hitWorkspaceHeaderLayout} {
					button := panelHit(t, canvas, id).Bounds
					if text.Bounds.X < button.X+button.Width && text.Bounds.X+text.Bounds.Width > button.X &&
						text.Bounds.Y < button.Y+button.Height && text.Bounds.Y+text.Bounds.Height > button.Y {
						t.Errorf("%s width %g: connection status %+v overlaps action %+v", language, width, text.Bounds, button)
					}
				}
			}
		}
	}
}

func TestWorkspaceCompactLibraryKeepsHostTextInsideItsRow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	state.templates = []WorkspaceTemplate{mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0, 1), 2)}
	canvas, _ := paintAt(state, gfx.Size{Width: 420, Height: 800})
	row := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0)).Bounds
	for _, text := range canvas.Texts() {
		if text.Bounds.Y < row.Y || text.Bounds.X < row.X {
			continue
		}
		if text.Bounds.X+text.Bounds.Width > row.X+row.Width+0.5 {
			t.Errorf("compact template text exceeds row: %q %+v row %+v", text.Value, text.Bounds, row)
		}
	}
}

func TestWorkspaceDraftFitsAtTheMinimumWindow(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		state := newTestState(t, testOptions(t))
		state.setLanguage(language)
		state.openWorkspaceDraft(workspaceDraftFixed)
		canvas, _ := paintAt(state, gfx.Size{Width: MinimumWindowWidth, Height: MinimumWindowHeight})
		list := state.workspaceDraft.bounds
		if list.Height < 56 {
			t.Fatalf("%s: host list cannot display a full row: %+v", language, list)
		}
		submit := panelHit(t, canvas, hitWorkspaceDraftSubmit).Bounds
		if list.Y+list.Height >= submit.Y {
			t.Fatalf("host list covers footer: %+v, submit %+v", list, submit)
		}
		for _, id := range []gfx.ID{hitWorkspaceDraftName, hitWorkspaceDraftSearch, hitWorkspaceDraftPresets.At(0), hitWorkspaceDraftPresets.At(1), hitWorkspaceDraftPresets.At(2)} {
			box := panelHit(t, canvas, id).Bounds
			if box.X < 0 || box.X+box.Width > MinimumWindowWidth || box.Y < 0 || box.Y+box.Height > submit.Y {
				t.Fatalf("%s: draft control %d escapes body: %+v", language, id, box)
			}
		}
		result := state.Key(gfx.KeyEvent{Key: gfx.KeyRight, Target: hitWorkspaceDraftPresets.At(0)})
		if !result.Handled || result.Focus != hitWorkspaceDraftPresets.At(1) || !result.FocusVisible {
			t.Fatalf("columns cannot be reached with arrow keys: %+v", result)
		}
	}
}

func TestWorkspaceBroadcastReportsSwitchStateAndStops(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	launchTemporaryWorkspace(t, state, 0, 1)
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	state.Click(hitWorkspaceHeaderBroadcast)
	canvas, texts := paintAt(state, gfx.Size{Width: 960, Height: 600})
	hit := panelHit(t, canvas, hitWorkspaceHeaderBroadcast)
	if hit.Role != gfx.RoleSwitch || !hit.Checked || hit.Disabled || hit.Label != state.messages.Tools.BroadcastStop {
		t.Fatalf("broadcast switch has misleading semantics: %+v", hit)
	}
	if !hasText(texts, fmt.Sprintf(state.messages.WorkspaceHeaderBroadcastingFmt, 2)) {
		t.Fatal("broadcast scope is not visible")
	}
	state.Click(hitWorkspaceHeaderBroadcast)
	canvas, _ = paintAt(state, gfx.Size{Width: 960, Height: 600})
	if panelHit(t, canvas, hitWorkspaceHeaderBroadcast).Checked || state.broadcast != 0 {
		t.Fatal("broadcast did not stop")
	}
}

func TestWorkspacePaneToolsPreserveAvailabilityAndDismissForNewFocus(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	launchTemporaryWorkspace(t, state, 0, 1, 2)
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	canvas, _ := paintAt(state, gfx.Size{Width: 960, Height: 600})
	panelHit(t, canvas, hitWorkspacePaneTools)
	state.Click(hitWorkspacePaneTools)
	canvas, _ = paintAt(state, gfx.Size{Width: 960, Height: 600})
	if state.workspaceHeaderMenu != workspaceHeaderMenuTools {
		t.Fatal("terminal actions menu did not open")
	}
	index := -1
	for i := 0; i < state.workspacePaneTools.count; i++ {
		tool := state.workspacePaneTools.items[i]
		hit := panelHit(t, canvas, hitWorkspacePaneToolList.At(i))
		if hit.Disabled != tool.disabled {
			t.Fatalf("menu changed availability: %+v, tool %+v", hit, tool)
		}
		if tool.id == hitTerminalRecordingExport && !hit.Disabled {
			t.Fatal("menu enabled export without a recording")
		}
		if tool.id == hitPaneSplitHorizontal && !hit.Disabled {
			t.Fatal("menu offers a horizontal split that does not fit")
		}
		if tool.id == hitPaneSplitHorizontal {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("split is not reachable from terminal actions")
	}
	state.ActivatePane(state.sessions[0].ID)
	paintAt(state, gfx.Size{Width: 960, Height: 600})
	if state.workspaceHeaderMenu != workspaceHeaderMenuNone {
		t.Fatal("menu kept actions targeting a previous pane")
	}
	before := len(state.Panes(nil))
	state.Click(hitWorkspacePaneToolList.At(index))
	if len(state.Panes(nil)) != before {
		t.Fatal("a stale menu action changed the new pane")
	}
}

func TestWorkspacePaneToolsRejectDisabledAndInvalidActions(t *testing.T) {
	state := newTestState(t, testOptions(t))
	launchTemporaryWorkspace(t, state, 0, 1)
	before := len(state.Panes(nil))
	state.workspacePaneTools = workspacePaneTools{session: state.activeSessionID, count: 1}
	state.workspacePaneTools.items[0] = barTool{id: hitPaneClose, disabled: true}
	state.workspaceHeaderMenu = workspaceHeaderMenuTools
	state.activateWorkspacePaneTool(-1)
	state.activateWorkspacePaneTool(1)
	if state.workspaceHeaderMenu != workspaceHeaderMenuTools {
		t.Fatal("an invalid action changed the menu")
	}
	state.activateWorkspacePaneTool(0)
	if len(state.Panes(nil)) != before || state.workspaceHeaderMenu != workspaceHeaderMenuNone {
		t.Fatal("disabled close action changed the workspace")
	}
}

func TestWorkspaceStatusAndSelectedDetailStayReadable(t *testing.T) {
	rgb := func(color gfx.Color) [3]uint8 {
		return [3]uint8{uint8(color.Red*255 + .5), uint8(color.Green*255 + .5), uint8(color.Blue*255 + .5)}
	}
	for _, appearance := range []appearance{appearanceDark, appearanceLight} {
		state := newTestState(t, testOptions(t))
		state.setAppearance(appearance)
		pal := state.pal
		for _, amount := range []float32{0, .25, .5, .75, 1} {
			background := pal.workspace.Mix(pal.warning, .12*amount)
			ink := state.workspaceStatusInk(pal.warning)
			for _, color := range []gfx.Color{pal.warning, pal.muted} {
				ink = state.workspaceStatusInk(color)
				for _, hover := range []float32{0, .10} {
					if got := contrast(rgb(ink), rgb(background.Mix(ink, hover))); got < 4.5 {
						t.Errorf("%s broadcast contrast %.2f at progress %.2f hover %.2f", appearance, got, amount, hover)
					}
				}
			}
			background = pal.surface.Mix(pal.accentSoft, amount)
			ink = pal.muted.Mix(pal.text, .08*amount)
			if got := contrast(rgb(ink), rgb(background)); got < 4.5 {
				t.Errorf("%s selected host detail contrast %.2f at progress %.2f", appearance, got, amount)
			}
		}
		for _, color := range []gfx.Color{pal.success, pal.warning, pal.danger, pal.faint} {
			if got := contrast(rgb(state.workspaceStatusInk(color)), rgb(pal.workspace)); got < 4.5 {
				t.Errorf("%s status text contrast %.2f", appearance, got)
			}
		}
	}
}
