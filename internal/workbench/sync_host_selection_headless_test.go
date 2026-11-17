//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestSelectedHostSettingsHeadless(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.localShellMenu.loaded = true
	state.setLanguage(i18n.Chinese)
	setSyncSelectionTestHosts(t, state,
		syncSelectionTestHost("Production", "Team"),
		syncSelectionTestHost("Staging", "Team/Sub"),
		syncSelectionTestHost("Local", "Private"))
	view, err := headless.Open(app.Config{Width: 1100, Height: 820, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click := func(id gfx.ID) {
		t.Helper()
		for offset := float32(0); offset <= state.settingsContent; offset += 120 {
			state.settingsScroll = offset
			if err := view.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			for _, hit := range view.Snapshot().Hits {
				if hit.ID == id && hit.Bounds.Height > 0 {
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					return
				}
			}
		}
		t.Fatal("selection control is unreachable", tree.Text(id))
	}
	click(hitSyncSelectedHosts)
	if !state.syncScope.hosts.OnlySelected {
		t.Fatal("native click did not enable host selection")
	}
	click(hitSyncGroupItems.At(state.hostGroupIndex("Team")))
	if !state.syncScope.hosts.hasHost("Production") || !state.syncScope.hosts.hasHost("Staging") || state.syncScope.hosts.hasHost("Local") {
		t.Fatal("native group click selected the wrong hosts")
	}
	click(hitSyncHostItems.At(1))
	if state.syncScope.hosts.hasHost("Staging") {
		t.Fatal("native host click did not deselect the host")
	}
	if directory := os.Getenv("MOTTY_SYNC_SELECTION_SCREENSHOTS"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for _, look := range []struct {
			name       string
			appearance appearance
		}{{"light", appearanceLight}, {"dark", appearanceDark}} {
			state.setAppearance(look.appearance)
			state.settingsScroll = state.settingsContent
			if err := view.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if err := view.Screenshot(filepath.Join(directory, "sync-selection-"+look.name+".png")); err != nil {
				t.Fatal(err)
			}
		}
	}
}
