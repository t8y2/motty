package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"strings"
	"testing"
)

func TestThemeCardContextActions(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	state.setTerminalScheme("nord")
	state.openSettings()
	state.selectSettingsCategory(settingsTerminal)
	scrollToSchemeSettings(state)
	view, err := headless.Open(state.WindowConfig(app.Config{Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll}), nil, headless.Options{})
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
	must(view.Rest())
	must(view.RightClickLabel("Dracula"))
	must(view.Rest())
	for _, hit := range view.Snapshot().Hits {
		if hit.Label == "编辑" || hit.Label == "删除" {
			t.Fatalf("built-in offers %s", hit.Label)
		}
	}
	must(view.ClickLabel("复制"))
	must(view.Rest())
	if state.activeScheme() != "nord" || state.themeEditor.draft.background != colorSchemes[1].background {
		t.Fatal("context action did not copy the targeted card independently of active theme")
	}
	state.themeEditor.name.SetText("Menu theme")
	must(view.ClickLabel("保存并应用"))
	must(view.Rest())
	id := state.activeScheme()
	if !strings.HasPrefix(id, customSchemePrefix) {
		t.Fatal("copy not saved")
	}
	must(view.RightClickLabel("Menu theme"))
	must(view.ClickLabel("编辑"))
	must(view.Rest())
	if state.themeEditor.draft.id != id {
		t.Fatal("edit targets wrong theme")
	}
	must(view.ClickLabel(state.messages.Cancel))
	must(view.Rest())
	must(view.RightClickLabel("Menu theme"))
	must(view.ClickLabel("删除"))
	must(view.Rest())
	if !state.themeEditor.deleting || state.themeEditor.draft.id != id {
		t.Fatal("delete targets wrong theme")
	}
	must(view.ClickLabel("删除"))
	must(view.Rest())
	if _, ok := state.schemeByID(id); ok {
		t.Fatal("theme not deleted")
	}
}
