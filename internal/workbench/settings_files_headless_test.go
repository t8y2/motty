//go:build darwin || linux || windows

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

// TestFilesSettingsPreview renders review screenshots only when requested.
func TestFilesSettingsPreview(t *testing.T) {
	directory := os.Getenv("MOTTY_FILES_SETTINGS_SCREENSHOTS")
	if directory == "" {
		t.Skip("set MOTTY_FILES_SETTINGS_SCREENSHOTS to render the Files settings")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	state.chooseAppearanceMode(appearancePinnedDark)
	state.sftpDownloadDirectory = ""
	state.openSettings()
	state.selectSettingsCategory(settingsFiles)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Motty", Width: 1400, Height: 1800, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	save := func(name string) {
		t.Helper()
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name)
		if err := view.Screenshot(path); err != nil {
			t.Fatal(err)
		}
		t.Log(path)
	}
	save("files-settings-overview.png")
	if err := view.Driver().Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	save("files-settings-top.png")
	state.settingsScroll = max(0, state.settingsContent-state.settings.pageView)
	state.settings.pageMotion = gfx.Motion{}
	if err := view.Driver().Paint(); err != nil {
		t.Fatal(err)
	}
	save("files-settings-shared.png")
}
