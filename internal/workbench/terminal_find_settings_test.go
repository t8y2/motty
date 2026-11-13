//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestTerminalFindSettingsSharedControlsAndPersistence(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("needle\r\n")
	state.Wake()
	openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")

	view, err := headless.Open(app.Config{
		Width: 1100, Height: 800, Tree: Tree(), Paint: state.Paint,
		OnResize: state.Resize, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if input := state.TerminalFindPreferencesInput(context); input != nil {
				return input
			}
			if input := state.TerminalFindInput(context); input != nil {
				return input
			}
			if editor := state.EditorFor(context.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	hit := func(id gfx.ID) gfx.Rect {
		t.Helper()
		for _, h := range view.Snapshot().Hits {
			if h.ID == id && !h.Block && h.Bounds.Width > 0 && h.Bounds.Height > 0 {
				return h.Bounds
			}
		}
		t.Fatalf("missing hit %v", id)
		return gfx.Rect{}
	}
	click := func(id gfx.ID) {
		t.Helper()
		b := hit(id)
		must(view.Click(b.X+b.Width/2, b.Y+b.Height/2))
		must(view.Rest())
	}
	screenshot := func(name string) {
		t.Helper()
		if dir := os.Getenv("MOTTY_FIND_SCREENSHOTS"); dir != "" {
			must(view.Screenshot(filepath.Join(dir, name+".png")))
		}
	}
	must(view.Rest())
	field, gear := hit(hitTerminalFindField), hit(hitTerminalFindSettings)
	if gear.X < field.X+field.Width || gear.Y < field.Y || gear.Y+gear.Height > field.Y+field.Height {
		t.Fatalf("settings button is not in the field's trailing slot: field=%+v gear=%+v", field, gear)
	}
	click(hitTerminalFindSettings)
	if !state.findSettingsUI.open {
		t.Fatal("gear did not open find settings")
	}
	screenshot("find-popover")
	click(hitFindForeground)
	click(hitFindColorHex)
	must(view.Keys("Cmd+A"))
	must(view.Type("#663399"))
	must(view.Keys("Enter"))
	must(view.Rest())
	if state.terminalFindPrefs.Foreground != "#663399" || !state.findSettingsUI.open {
		t.Fatalf("foreground edit failed: %+v", state.terminalFindPrefs)
	}

	slider := hit(hitFindSettingsOpacity)
	must(view.Driver().Press(slider.X+slider.Width/2, slider.Y+slider.Height/2, gfx.PointerButtonPrimary))
	must(view.Driver().Move(slider.X-20, slider.Y+slider.Height/2))
	must(view.Driver().Release(slider.X-20, slider.Y+slider.Height/2, gfx.PointerButtonPrimary))
	must(view.Rest())
	if state.terminalFindPrefs.Opacity != 0 {
		t.Fatalf("slider drag ended at %d", state.terminalFindPrefs.Opacity)
	}
	click(hitFindSettingsOrigin)
	click(hitFindOriginItems.At(1))
	if state.terminalFindPrefs.Origin != findSessionBottom {
		t.Fatalf("origin: %s", state.terminalFindPrefs.Origin)
	}
	must(view.Keys("Escape"))
	must(view.Rest())
	if state.findSettingsUI.open || view.Snapshot().Focused != hitTerminalFindField {
		t.Fatalf("dismissing settings: open=%t focus=%v want=%v pending=%v", state.findSettingsUI.open, view.Snapshot().Focused, hitTerminalFindField, state.findSettingsUI.focus)
	}

	click(hitWindowSettings)
	click(hitSettingsCategoryList.At(int(settingsTerminal)))
	click(hitSettingsSearch)
	must(view.Type(state.messages.FindSettings.Title))
	must(view.Rest())
	click(hitSettingsResultList.At(0))
	screenshot("find-settings-page")
	click(hitFindBackground)
	click(hitFindColorHex)
	must(view.Keys("Cmd+A"))
	must(view.Type("#CCDDEE"))
	click(hitFindColorApply)
	want := terminalFindPreferences{Foreground: "#663399", Background: "#CCDDEE", Opacity: 0, Origin: findSessionBottom}
	if state.terminalFindPrefs != want {
		t.Fatalf("settings page: got %+v, want %+v", state.terminalFindPrefs, want)
	}
	saved, _, err := loadState(state.options.StatePath)
	must(err)
	if got := saved.TerminalFind.normalized(); got != want {
		t.Fatalf("saved preferences: got %+v, want %+v", got, want)
	}
	click(hitFindSettingsReset)
	if state.terminalFindPrefs != defaultTerminalFindPreferences {
		t.Fatalf("restore defaults: %+v", state.terminalFindPrefs)
	}
}
