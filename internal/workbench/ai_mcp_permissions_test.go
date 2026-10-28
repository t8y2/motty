package workbench

import (
	"testing"

	"motty/internal/ai/gate"
)

func TestMCPAllowAllIsInactiveUntilConfirmedAndCanBeRevoked(t *testing.T) {
	for _, initial := range []struct {
		name string
		mode gate.Mode
	}{
		{"observe", gate.ModeObserve}, {"ask", gate.ModeAsk},
	} {
		t.Run(initial.name, func(t *testing.T) {
			world := newSettingsWorld(t, newLister())
			state, db := world.state, world.db(t)
			state.selectSettingsCategory(settingsMCP)
			if err := db.SetSetting("ai.mcp.mode", initial.name); err != nil {
				t.Fatal(err)
			}
			state.reloadAI()
			hub := NewAIHub(state.options.AIStorePath)
			t.Cleanup(hub.Close)
			state.options.AISettingsChanged = func() {
				world.changed++
				if err := hub.Reconfigure(); err != nil {
					t.Fatal(err)
				}
			}
			state.Click(hitAISetMCPEnabled)
			before := world.changed
			check := func(mode gate.Mode, saved string) {
				t.Helper()
				if state.ai.settings.mcpMode != mode || hub.gate().Mode != mode || settingValue(t, db, "ai.mcp.mode") != saved {
					t.Fatalf("UI mode=%v, server mode=%v, saved=%q", state.ai.settings.mcpMode, hub.gate().Mode, settingValue(t, db, "ai.mcp.mode"))
				}
			}
			state.Click(hitAISetMCPAllowAll)
			check(initial.mode, initial.name)
			if state.ai.cfg.dialog != aiDialogMCPAllowAll || world.changed != before {
				t.Fatal("selecting allow-all did not wait for consent")
			}
			state.Click(hitAISetConfirmCancel)
			check(initial.mode, initial.name)
			if state.ai.cfg.dialog != aiDialogNone || world.changed != before {
				t.Fatal("cancel changed the permission")
			}
			state.Click(hitAISetMCPAllowAll)
			state.Click(hitAISetConfirmAction)
			check(gate.ModeAllowAll, "allow-all")
			if state.ai.cfg.dialog != aiDialogNone || world.changed != before+1 {
				t.Fatal("confirmation did not commit exactly once")
			}
			if again := world.open(t); again.ai.settings.mcpMode != gate.ModeAllowAll {
				t.Fatal("a new window lost the confirmed permission")
			}
			state.Click(hitAISetMCPAllowAll)
			if state.ai.cfg.dialog != aiDialogNone {
				t.Fatal("clicking the selected option asked again")
			}
			state.Click(hitAISetMCPAsk)
			check(gate.ModeAsk, "ask")
			state.Click(hitAISetMCPObserve)
			check(gate.ModeObserve, "observe")
			state.Click(hitAISetMCPAllowAll)
			if state.ai.cfg.dialog != aiDialogMCPAllowAll {
				t.Fatal("enabling full access again bypassed confirmation")
			}
		})
	}
}

func TestMCPUnknownAccessSettingStaysReadOnly(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	if world.state.ai.settings.mcpMode != gate.ModeObserve {
		t.Fatal("a missing setting is not read-only")
	}
	for _, value := range []string{"", "all", "auto", "ALLOW-ALL", "future-mode"} {
		if err := world.db(t).SetSetting("ai.mcp.mode", value); err != nil {
			t.Fatal(err)
		}
		world.state.reloadAI()
		_, mode, _, _, err := NewAIHub(world.options.AIStorePath).readSettings()
		if err != nil || mode != gate.ModeObserve || world.state.ai.settings.mcpMode != gate.ModeObserve {
			t.Fatalf("unknown setting %q escalated access: mode=%v err=%v", value, mode, err)
		}
	}
}

func TestMCPAllowAllSaveFailureDoesNotSelectIt(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	state.Click(hitAISetMCPAsk)
	state.Click(hitAISetMCPAllowAll)
	before := world.changed
	if err := world.db(t).Close(); err != nil {
		t.Fatal(err)
	}
	state.Click(hitAISetConfirmAction)
	if state.ai.settings.mcpMode != gate.ModeAsk || state.ai.cfg.dialog != aiDialogMCPAllowAll || state.ai.cfg.notice == "" || world.changed != before {
		t.Fatal("failed storage committed or dismissed the permission change")
	}
}
