package workbench

import (
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestCustomApplicationSeedsCurrentTerminalFromEveryMode(t *testing.T) {
	for _, mode := range []appearanceMode{appearancePinnedDark, appearancePinnedLight, appearanceFollowTerminal} {
		t.Run(mode.saved(), func(t *testing.T) {
			state := newTestState(t, freshOptions(t))
			state.chooseAppearanceMode(mode)
			state.setTerminalScheme("dracula")
			// 兼容旧版已进入过自定义、却只有默认基底的配置。
			state.applicationTheme.Base = "dark"
			state.chooseAppearanceMode(appearanceCustom)
			source, _ := state.schemeByID("dracula")
			want := applicationPaletteForScheme(*source)
			if rgb8(state.pal.workspace) != rgb8(want.workspace) || rgb8(state.pal.shell) != rgb8(want.shell) {
				t.Fatalf("custom palette %v/%v, want terminal %v/%v", state.pal.workspace, state.pal.shell, want.workspace, want.shell)
			}
			if state.activeScheme() != "dracula" {
				t.Fatal("entering custom changed terminal theme")
			}
		})
	}
}

func TestCustomApplicationOverridesStayIndependentOfTerminal(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setTerminalScheme("dracula")
	state.openThemeEditor(true, false)
	state.clickThemeEditor(hitThemeApply)
	sourceID := state.activeScheme()
	state.chooseAppearanceMode(appearanceCustom)
	background := gfx.RGB(45, 65, 85)
	state.setApplicationColor("workspace", background)
	opacity := 35
	state.applicationTheme.Opacity = &opacity
	state.setTerminalScheme("nord")
	if state.pal.workspace != background {
		t.Fatal("terminal selection overwrote custom interface")
	}
	state.setTerminalScheme(sourceID)
	state.openThemeEditor(false, false)
	state.themeEditor.colors[0] = gfx.RGB(80, 40, 60)
	state.themeEditor.opacity.Value = 80
	state.clickThemeEditor(hitThemeApply)
	if state.pal.workspace != background {
		t.Fatal("editing terminal overwrote custom interface")
	}
	state.chooseAppearanceMode(appearanceFollowTerminal)
	if state.pal.workspace != gfx.RGB(80, 40, 60) || state.applicationOpacity() != 80 {
		t.Fatal("follow did not use edited terminal theme")
	}
	state.chooseAppearanceMode(appearanceCustom)
	if state.pal.workspace != background {
		t.Fatal("returning to custom lost saved override")
	}
	state.save()
	restored := newTestState(t, options)
	if restored.pal.workspace != background || len(restored.applicationTheme.BaseColors) == 0 || restored.applicationOpacity() != 35 {
		t.Fatal("custom base and overrides did not survive restart")
	}
}

func TestCustomApplicationSeedKeepsExistingOverrides(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setTerminalScheme("nord")
	color := gfx.RGB(80, 40, 60)
	state.applicationTheme.setColor("shell", color)
	state.chooseAppearanceMode(appearanceCustom)
	scheme, _ := state.schemeByID("nord")
	if state.pal.shell != color || rgb8(state.pal.workspace) != scheme.background {
		t.Fatal("migration did not retain overrides above terminal base")
	}
}

func TestCustomApplicationRestoresLegacyTerminalBase(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setTerminalScheme("nord")
	state.chooseAppearanceMode(appearanceCustom)
	state.applicationTheme.BaseColors = nil
	color := gfx.RGB(80, 40, 60)
	state.applicationTheme.setColor("shell", color)
	state.save()
	restored := newTestState(t, options)
	scheme, _ := restored.schemeByID("nord")
	if restored.pal.shell != color || rgb8(restored.pal.workspace) != scheme.background {
		t.Fatal("legacy custom settings did not inherit terminal colors and preserve overrides")
	}
}
