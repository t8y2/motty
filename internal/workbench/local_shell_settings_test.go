package workbench

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

func TestLocalShellSettingVisibilityAndPosition(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		state := newTestState(t, testOptions(t))
		state.setLanguage(language)
		state.Click(hitSettings)
		state.selectSettingsCategory(settingsTerminal)
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
		if _, visible := hitByID(&canvas, hitSettingsLocalShell); !visible {
			// The terminal page has grown past this canvas, so reach the row
			// the way a user does; both rows sit together at the page's end.
			state.scrollSettings(gfx.ScrollEvent{DeltaY: state.settingsContent, Precise: true})
			canvas.Reset()
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
		}
		hit, visible := hitByID(&canvas, hitSettingsLocalShell)
		if !visible {
			t.Fatal("the local shell setting is not visible")
		}
		if visible {
			notify, _ := hitByID(&canvas, hitSettingsNotifyToggle)
			if hit.Role != gfx.RoleCombobox || hit.Bounds.Y <= notify.Bounds.Y {
				t.Fatalf("shell select is not below the notification row: %+v", hit)
			}
		}
		searchSettings(t, state, state.messages.SettingsLocalShell)
		if state.settings.count != 1 {
			t.Fatalf("shell search returned %d results, want 1", state.settings.count)
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
