//go:build darwin || windows || linux

package workbench

import (
	"errors"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestNavigationSwitchThroughVisibleControls(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	session, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		OnResize: state.Resize, OnScroll: state.Scroll,
	}, nil, headless.Options{Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	click := func(target gfx.ID) {
		t.Helper()
		for _, hit := range session.Snapshot().Hits {
			if hit.ID != target {
				continue
			}
			if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if _, err := session.Image(); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
				t.Fatal(err)
			}
			return
		}
		t.Fatalf("navigation control %v is not visible", target)
	}
	for range 3 {
		click(hitWorkspaces)
		if state.activeSection != sectionWorkspaces {
			t.Fatal("sidebar did not open workspaces")
		}
		click(hitHosts)
		if state.activeSection != sectionHosts {
			t.Fatal("sidebar did not return to hosts")
		}
		click(hitSettings)
		for _, category := range []settingsCategory{settingsGeneral, settingsTerminal, settingsConnection, settingsFiles, settingsShortcuts, settingsGeneral} {
			click(hitSettingsCategoryList.At(int(category)))
			if state.activeSection != sectionSettings || state.settingsCategory != category {
				t.Fatalf("settings click did not open category %v", category)
			}
			title := state.settingsCategoryName(category)
			found := false
			for _, label := range session.Snapshot().Labels {
				if label.Value == title {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("selected category %q was not painted", title)
			}
		}
	}
	if err := session.Driver().Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if state.activeSection != sectionSettings || state.settingsCategory != settingsGeneral {
		t.Fatal("settling navigation animations changed the selected page")
	}
}
