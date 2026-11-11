//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/gate"
	"motty/internal/i18n"
)

func TestMCPAllowAllConsentThroughNativeInput(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 1.5, 2} {
			t.Run(fmt.Sprintf("%s-%.1f", language, scale), func(t *testing.T) {
				world := newSettingsWorld(t, newLister())
				state, db := world.state, world.db(t)
				state.setLanguage(language)
				state.setAppearance(appearanceLight)
				state.selectSettingsCategory(settingsMCP)
				view, err := headless.Open(state.WindowConfig(app.Config{Title: "MCP permissions", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				// The access level is a page Choice: click its segment by its
				// words. The first "Allow all" is the HTTP level.
				clickChoice := func(label string, first bool) {
					t.Helper()
					view.Rest()
					hit, ok := settingsChoiceInHits(view.Snapshot().Hits, label, !first)
					if !ok {
						t.Fatalf("missing access choice %q", label)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				allowAll := state.messages.AISettings.MCPModeAllowAll
				clickButton := func(label string) {
					t.Helper()
					view.Rest()
					hit, ok := snapshotButton(view.Snapshot(), label)
					if !ok {
						t.Fatalf("missing button %q", label)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				checkUnselected := func() {
					t.Helper()
					if state.ai.settings.mcpMode != gate.ModeAsk || settingValue(t, db, "ai.mcp.mode") != "ask" {
						t.Fatal("access changed before confirmation")
					}
					for _, hit := range view.Snapshot().Hits {
						if hit.Label == allowAll && hit.Role == gfx.RoleRadio && hit.Selected {
							t.Fatal("unconfirmed option looks selected")
						}
					}
				}
				shot := func(name string) {
					t.Helper()
					if dir := os.Getenv("MOTTY_MCP_PERMISSION_SCREENSHOTS"); dir != "" && scale == 1 {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("%s-%s.png", language, name))); err != nil {
							t.Fatal(err)
						}
					}
				}
				clickChoice(state.messages.AISettings.MCPModeAsk, false)
				if err := view.Keys("right"); err != nil {
					t.Fatal(err)
				}
				checkUnselected()
				cancel, ok := snapshotButton(view.Snapshot(), state.messages.Cancel)
				if state.ai.cfg.dialog != aiDialogMCPAllowAll || !ok || view.Snapshot().Focused != cancel.ID {
					t.Fatal("keyboard selection did not focus the safe action")
				}
				view.Rest()
				shot("confirm")
				if err := view.Keys("esc"); err != nil {
					t.Fatal(err)
				}
				checkUnselected()
				if state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("Escape did not cancel")
				}
				clickChoice(allowAll, true)
				checkUnselected()
				if err := view.Keys("enter"); err != nil {
					t.Fatal(err)
				}
				checkUnselected() // Enter on the initial Cancel button must cancel.
				if state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("the default button did not cancel")
				}
				clickChoice(allowAll, true)
				clickButton(state.messages.AISettings.MCPAllowAllConfirm)
				if state.ai.cfg.dialog != aiDialogNone || state.ai.settings.mcpMode != gate.ModeAllowAll {
					t.Fatal("confirmation did not select full access")
				}
				view.Rest()
				shot("enabled")
				state.setAppearance(appearanceDark)
				if err := view.Resize(1024, 760); err != nil {
					t.Fatal(err)
				}
				view.Rest()
				for _, label := range []string{state.messages.AISettings.MCPModeObserve, state.messages.AISettings.MCPModeAsk, state.messages.AISettings.MCPModeAllowAll} {
					found := false
					for _, text := range view.Snapshot().Texts {
						found = found || text.Value == label
					}
					if !found {
						t.Fatalf("a narrow settings page clipped the access option %q", label)
					}
				}
				shot("enabled-dark")
				clickChoice(state.messages.AISettings.MCPModeObserve, false)
				if state.ai.settings.mcpMode != gate.ModeObserve {
					t.Fatal("could not revoke access")
				}
			})
		}
	}
}
