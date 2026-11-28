package workbench

import (
	"motty/internal/i18n"
	"testing"
)

func TestLegacyFollowModeMigratesToSharedSettings(t *testing.T) {
	for _, base := range []string{"dark", "light"} {
		t.Run(base, func(t *testing.T) {
			state := newTestState(t, freshOptions(t))
			saved := state.snapshot()
			saved.Theme = "terminal"
			saved.ApplicationTheme = &applicationTheme{FollowBase: base, FollowScheme: "dracula"}
			state.restoreSettings(saved)
			current := state.snapshot()
			if !current.AppearanceFollowTerminal || current.Theme == "terminal" || state.activeScheme() != "dracula" {
				t.Fatalf("migration: mode=%q follow=%v scheme=%q", current.Theme, current.AppearanceFollowTerminal, state.activeScheme())
			}
			if base == "light" && current.TerminalSchemeLight != "dracula" || base == "dark" && current.TerminalSchemeDark != "dracula" {
				t.Fatal("legacy choice was not written to terminal settings")
			}
			if current.ApplicationTheme != nil && current.ApplicationTheme.FollowScheme != "" {
				t.Fatal("legacy device-local source survived migration")
			}
			state.save()
			restored := newTestState(t, state.options)
			if !restored.appearanceFollowTerminal || restored.activeScheme() != "dracula" {
				t.Fatal("migrated choice did not survive restart")
			}
		})
	}
}

func TestFollowTerminalChoiceIsSharedBetweenWindows(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	pair.first.appearanceFollowTerminal = true
	pair.first.setTerminalScheme("dracula")
	pair.first.save()
	if !pair.second.appearanceFollowTerminal || pair.second.terminalSchemes[appearanceDark] != "dracula" {
		t.Fatal("follow choice did not reach the other window")
	}
	pair.second.appearanceFollowTerminal = false
	pair.second.chooseAppearanceMode(appearancePinnedDark)
	if pair.second.activeScheme() != "dracula" {
		t.Fatal("disabling follow reverted the shared terminal choice")
	}
}

func TestThemeEditorCatalogFollowsLanguage(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		state.setLanguage(language)
		state.openThemeEditor(true, true)
		if state.themeEditor.name.Text() != state.messages.Themes.MyTheme || state.messages.Themes.NameRequired == "" {
			t.Fatal("theme editor did not use the selected catalog")
		}
	}
}
