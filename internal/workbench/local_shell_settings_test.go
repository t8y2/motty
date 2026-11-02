package workbench

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestLocalShellSettingVisibilityAndPosition(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		state := newTestState(t, testOptions(t))
		state.setLanguage(language)
		state.Click(hitSettings)
		state.selectSettingsCategory(settingsTerminal)
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 1400, Height: 1800})
		hit, visible := hitByID(&canvas, hitSettingsLocalShell)
		if visible != (runtime.GOOS == "windows") {
			t.Fatalf("local shell visible=%v on %s", visible, runtime.GOOS)
		}
		if visible {
			notify, _ := hitByID(&canvas, hitSettingsNotifyToggle)
			if hit.Role != ui.RoleCombobox || hit.Bounds.Y <= notify.Bounds.Y {
				t.Fatalf("shell select is not below the notification row: %+v", hit)
			}
		}
		searchSettings(t, state, state.messages.SettingsLocalShell)
		want := 0
		if runtime.GOOS == "windows" {
			want = 1
		}
		if state.settings.count != want {
			t.Fatalf("shell search returned %d results, want %d", state.settings.count, want)
		}
	}
}

func TestMissingSavedLocalShellReportsStartupFailure(t *testing.T) {
	options := testOptions(t)
	missing := filepath.Join(t.TempDir(), "removed-shell")
	if err := saveState(options.StatePath, savedState{LocalShell: missing}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.Click(hitLocalTerminal)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.shellFailed || session.err == nil || !strings.Contains(session.err.Error(), "removed-shell") {
		t.Fatalf("a missing configured shell did not report failure: %+v", session)
	}
}
