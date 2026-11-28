package workbench

import (
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestApplicationThemeFollowsEditedCopyWhenEnabled(t *testing.T) {
	for _, previouslyFollowed := range []bool{false, true} {
		name := "first activation"
		if previouslyFollowed {
			name = "reactivation"
		}
		t.Run(name, func(t *testing.T) {
			options := freshOptions(t)
			state := newTestState(t, options)
			state.chooseAppearanceMode(appearancePinnedDark)
			state.setTerminalScheme("dracula")
			if previouslyFollowed {
				state.chooseAppearanceMode(appearanceFollowTerminal)
				state.chooseAppearanceMode(appearancePinnedDark)
			}
			state.openThemeEditor(true, false)
			state.clickThemeEditor(hitThemeSave)
			id := state.managedSchemes[0].id
			state.openThemeEditorFor(id, false, false)
			background := gfx.RGB(72, 30, 45)
			state.themeEditor.colors[0] = background
			state.clickThemeEditor(hitThemeApply)
			if state.modal != modalNone || state.activeScheme() != id {
				t.Fatalf("save and apply: modal=%v scheme=%q", state.modal, state.activeScheme())
			}
			state.chooseAppearanceMode(appearanceFollowTerminal)
			if state.activeScheme() != id || state.pal.workspace != background {
				t.Fatalf("followed %q background=%v; want %q background=%v", state.activeScheme(), state.pal.workspace, id, background)
			}
			state.save()
			restored := newTestState(t, options)
			if restored.activeScheme() != id || restored.pal.workspace != background {
				t.Fatalf("restored theme=%q background=%v", restored.activeScheme(), restored.pal.workspace)
			}
		})
	}
}
