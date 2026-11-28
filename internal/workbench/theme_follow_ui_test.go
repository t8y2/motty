package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestCustomThemeFollowBackgroundThroughEditor(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setLanguage(i18n.Chinese)
	state.setTerminalScheme("dracula")
	state.chooseAppearanceMode(appearanceFollowTerminal)
	state.openThemeEditor(true, false)
	state.clickThemeEditor(hitThemeSave)
	state.openThemeEditorFor(state.managedSchemes[0].id, false, false)
	view, err := headless.Open(state.WindowConfig(app.Config{Title: "Theme", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(30 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel("背景"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	color := gfx.RGB(72, 30, 45)
	state.themeEditor.pickers[0].SetDraftValue(components.ColorPickerValue{}.WithColor(color))
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Hit(state.messages.ColorPickerApply); ok {
		t.Fatal("picker still requires confirmation")
	}
	clickThemeThroughPopover(t, view, "保存并应用")
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	id := state.activeScheme()
	if state.pal.workspace != color || len(state.managedSchemes) != 1 {
		t.Fatalf("after apply: scheme=%q background=%v count=%d", id, state.pal.workspace, len(state.managedSchemes))
	}
	state.openSettings()
	state.selectSettingsCategory(settingsAppearance)
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []appearanceMode{appearanceCustom, appearanceFollowTerminal} {
		if err := view.ClickLabel(state.settingsAppearanceLabel(mode)); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if _, ok := state.schemeByID(id); !ok {
			t.Fatal("saved theme disappeared")
		}
		found := false
		for _, index := range state.schemeQuickPicks() {
			candidate, _, _ := state.schemeAt(index)
			found = found || candidate == id
		}
		if !found {
			t.Fatal("saved custom theme disappeared from cards")
		}

	}
	if state.activeScheme() != id || state.pal.workspace != color {
		t.Fatalf("after toggling: scheme=%q background=%v; want %q", state.activeScheme(), state.pal.workspace, id)
	}
	found := false
	for _, index := range state.schemeQuickPicks() {
		candidate, _, _ := state.schemeAt(index)
		found = found || candidate == id
	}
	if !found {
		t.Fatal("saved custom theme disappeared from cards")
	}
	restored := newTestState(t, state.options)
	if _, ok := restored.schemeByID(id); !ok {
		t.Fatal("saved theme missing after restart")
	}
}

func TestCustomThemeLiveColorCancelKeepsSavedTheme(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setLanguage(i18n.Chinese)
	state.openThemeEditor(true, false)
	original := state.themeEditor.colors[0]
	view, err := headless.Open(state.WindowConfig(app.Config{Title: "Theme", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(30 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel("背景"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.messages.ColorPickerSV); err != nil {
		t.Fatal(err)
	}
	if state.themeEditor.colors[0] == original {
		t.Fatal("picking color did not update draft immediately")
	}
	clickThemeThroughPopover(t, view, "文字")
	if state.themeEditor.colorIndex != 1 {
		t.Fatal("clicking another swatch did not switch picker")
	}
	clickThemeThroughPopover(t, view, state.messages.Cancel)
	if state.modal != modalNone || len(state.managedSchemes) != 0 {
		t.Fatal("cancel saved the draft")
	}
}

// 用真实坐标点击弹层外控件，验证同一次鼠标点击会关闭色盘并执行目标动作。
func clickThemeThroughPopover(t *testing.T, view *headless.Session, label string) {
	t.Helper()
	hits := view.Snapshot().Hits
	for i := len(hits) - 1; i >= 0; i-- {
		if hits[i].Label != label {
			continue
		}
		r := hits[i].Bounds
		if err := view.Click(r.X+r.Width/2, r.Y+r.Height/2); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("missing control %q", label)
}
