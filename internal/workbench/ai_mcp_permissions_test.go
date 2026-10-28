package workbench

import (
	"strings"
	"testing"
	"unicode"

	"github.com/vivaui/viva/ui"
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

func TestMCPConsentForgetsSaveErrorsWhenCanceled(t *testing.T) {
	for _, agent := range []bool{false, true} {
		world := newSettingsWorld(t, newLister())
		state := world.state
		state.selectSettingsCategory(settingsMCP)
		allow := hitAISetMCPAllowAll
		if agent {
			allow = hitAISetAgentAllowAll
		}
		state.Click(allow)
		if err := world.db(t).Close(); err != nil {
			t.Fatal(err)
		}
		state.Click(hitAISetConfirmAction)
		if state.ai.cfg.notice == "" {
			t.Fatal("the failed save did not report its error")
		}
		state.Click(hitAISetConfirmCancel)
		state.Click(allow)
		if state.ai.cfg.dialog == aiDialogNone || state.ai.cfg.notice != "" {
			t.Fatal("the new confirmation retained the canceled save error")
		}
	}
}

func TestMCPPermissionsFollowTheSharedHubAcrossOpenWindows(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	a, b := world.state, world.open(t)
	a.selectSettingsCategory(settingsMCP)
	b.selectSettingsCategory(settingsMCP)
	hub := NewAIHub(world.options.AIStorePath)
	t.Cleanup(hub.Close)
	hub.Add(a, nil)
	hub.Add(b, nil)
	reconfigure := func() {
		t.Helper()
		if err := hub.Reconfigure(); err != nil {
			t.Fatal(err)
		}
	}
	a.options.AISettingsChanged, b.options.AISettingsChanged = reconfigure, reconfigure
	check := func(enabled bool, http, agent gate.Mode) {
		t.Helper()
		a.Wake()
		b.Wake()
		for _, state := range []*State{a, b} {
			if state.ai.settings.mcpEnabled != enabled || state.ai.settings.mcpMode != http || state.ai.settings.agentMode != agent {
				t.Fatalf("stale window: enabled=%t HTTP=%v stdio=%v", state.ai.settings.mcpEnabled, state.ai.settings.mcpMode, state.ai.settings.agentMode)
			}
		}
		if enabled && hub.gate().Mode != http {
			t.Fatal("the published permission differs from the running service")
		}
	}
	a.Click(hitAISetMCPAsk)
	a.Click(hitAISetMCPEnabled)
	check(true, gate.ModeAsk, gate.ModeObserve)
	a.Click(hitAISetMCPAllowAll)
	a.Click(hitAISetConfirmAction)
	check(true, gate.ModeAllowAll, gate.ModeObserve)
	// The other window can immediately revoke access; subsequent unrelated
	// settings changes must not write an earlier access level back.
	b.Click(hitAISetMCPObserve)
	check(true, gate.ModeObserve, gate.ModeObserve)
	a.Click(hitAISetAgentAllowAll)
	a.Click(hitAISetConfirmAction)
	check(true, gate.ModeObserve, gate.ModeAllowAll)
	b.Click(hitAISetMCPEnabled)
	check(false, gate.ModeObserve, gate.ModeAllowAll)
	a.Click(hitAISetMCPAllowAll)
	a.Click(hitAISetConfirmAction)
	check(false, gate.ModeAllowAll, gate.ModeAllowAll)
	b.Click(hitAISetAgentObserve)
	check(false, gate.ModeAllowAll, gate.ModeObserve)
	b.Click(hitAISetMCPEnabled)
	check(true, gate.ModeAllowAll, gate.ModeObserve)
	c := world.open(t)
	hub.Add(c, nil)
	c.Wake()
	if c.ai.settings.mcpMode != gate.ModeAllowAll || c.aiMCPURL() != hub.Address() {
		t.Fatal("a newly added window did not inherit the current service")
	}
}

func TestMCPFullAccessWarningsStayVisibleWhileHTTPIsOffAndAfterRestart(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	state.Click(hitAISetMCPAllowAll)
	state.Click(hitAISetConfirmAction)
	state.Click(hitAISetAgentAllowAll)
	state.Click(hitAISetConfirmAction)
	state = world.open(t)
	state.selectSettingsCategory(settingsMCP)
	if state.ai.settings.mcpEnabled {
		t.Fatal("the test should leave HTTP disabled")
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1100, Height: 6000})
	var rendered strings.Builder
	for _, text := range canvas.Texts() {
		if text.Style.Color == state.pal.danger {
			rendered.WriteString(text.Value)
		}
	}
	withoutSpace := func(value string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, value)
	}
	for _, warning := range []string{state.messages.AISettings.MCPAllowAllWarning, state.messages.AISettings.AgentAllowAllWarning} {
		if !strings.Contains(withoutSpace(rendered.String()), withoutSpace(warning)) {
			t.Fatalf("full access has no persistent risk warning: %s", warning)
		}
	}
}
