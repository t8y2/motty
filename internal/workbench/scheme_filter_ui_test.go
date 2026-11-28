package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestSchemeCategoryButtonsFilterDrawer(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setLanguage(i18n.Chinese)
	scheme := state.resolvedScheme("dracula")
	scheme.id, scheme.name = "", "我的紫色副本"
	id, err := state.saveCustomScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}
	state.openSettings()
	state.selectSettingsCategory(settingsTerminal)
	state.openSchemeBrowser()
	view, err := headless.Open(state.WindowConfig(app.Config{Title: "Schemes", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, category := range []schemeCategory{schemeFilterBuiltin, schemeFilterCommunityDark, schemeFilterCommunityLight, schemeFilterMine, schemeFilterAll} {
		if err := view.ClickLabel(state.schemeCategoryLabel(category)); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if state.scheme.category != category {
			t.Fatalf("filter=%v, want %v", state.scheme.category, category)
		}
		if category == schemeFilterMine || category == schemeFilterAll {
			if _, ok := view.Hit("我的紫色副本"); !ok {
				t.Fatal("my theme is not visible at top")
			}
		}
	}
	if shots := os.Getenv("MOTTY_THEME_SCREENSHOTS"); shots != "" {
		if err := view.Screenshot(filepath.Join(shots, "theme-category-filter.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.ClickLabel("我的"); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.messages.Tools.SchemeSearchPlaceholder); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("紫色"); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel("我的紫色副本"); err != nil {
		t.Fatal(err)
	}
	if state.activeScheme() != id {
		t.Fatal("filtered theme was not applied")
	}
}
