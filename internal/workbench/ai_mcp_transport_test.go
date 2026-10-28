package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/ai/gate"
)

func TestMCPTransportClientsCopyIndependentlyAndSurviveRestart(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.Executable = func() (string, error) { return "/opt/motty/motty", nil }
	})
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	state.Click(hitAISetMCPEnabled)
	state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "transport-test-token"
	before := world.changed
	state.Click(aiMCPClientID(aiMCPCodex))
	state.Click(aiAgentClientID(aiMCPCursor))
	if world.changed != before {
		t.Fatal("an export preference reconfigured MCP permissions")
	}
	state.Click(hitAISetMCPCopy)
	if got := world.clip[len(world.clip)-1]; !strings.HasPrefix(got, "[mcp_servers.motty]") || !strings.Contains(got, "transport-test-token") || strings.Contains(got, "mcp-serve") {
		t.Fatalf("HTTP copy used the other transport: %s", got)
	}
	state.Click(hitAISetAgentCopy)
	if got := world.clip[len(world.clip)-1]; !strings.HasPrefix(got, "{") || !strings.Contains(got, "motty-hosts") || !strings.Contains(got, "mcp-serve") || strings.Contains(got, "transport-test-token") || strings.Contains(got, "http://") {
		t.Fatalf("stdio copy used the other transport: %s", got)
	}
	if state.ai.cfg.agentPreviewConfig != world.clip[len(world.clip)-1] {
		t.Fatal("the stdio preview differs from its copy")
	}
	again := world.open(t)
	again.selectSettingsCategory(settingsMCP)
	if again.ai.cfg.mcpClient != aiMCPCodex || again.ai.cfg.agentClient != aiMCPCursor {
		t.Fatal("independent export choices were not restored")
	}
	state.Click(hitAISetMCPEnabled) // HTTP is disabled; stdio configuration still works.
	state.Click(hitAISetAgentCopy)
	if !strings.Contains(world.clip[len(world.clip)-1], "mcp-serve") {
		t.Fatal("HTTP disabled the independent configuration copy")
	}
}

func TestAgentAllowAllWaitsForConfirmationAndKeepsHTTPIndependent(t *testing.T) {
	for _, initial := range []string{AgentModeReadOnly, AgentModeCommands} {
		t.Run(initial, func(t *testing.T) {
			world := newSettingsWorld(t, newLister())
			state, db := world.state, world.db(t)
			state.selectSettingsCategory(settingsMCP)
			if err := db.SetSetting(agentModeSetting, initial); err != nil {
				t.Fatal(err)
			}
			state.reloadAI()
			before := state.ai.settings.agentMode
			check := func(mode gate.Mode, setting string) {
				t.Helper()
				if state.ai.settings.agentMode != mode || settingValue(t, db, agentModeSetting) != setting || state.ai.settings.mcpMode != gate.ModeObserve {
					t.Fatal("stdio permission or independent HTTP permission changed unexpectedly")
				}
			}
			state.Click(hitAISetAgentAllowAll)
			check(before, initial)
			if state.ai.cfg.dialog != aiDialogAgentAllowAll {
				t.Fatal("full access did not ask for consent")
			}
			state.Click(hitAISetConfirmCancel)
			check(before, initial)
			state.Click(hitAISetAgentAllowAll)
			state.Click(hitAISetConfirmAction)
			check(gate.ModeAllowAll, AgentModeAllowAll)
			if again := world.open(t); again.ai.settings.agentMode != gate.ModeAllowAll {
				t.Fatal("confirmed access did not survive restart")
			}
			state.Click(hitAISetAgentObserve)
			check(gate.ModeObserve, AgentModeReadOnly)
			state.Click(hitAISetAgentAllowAll)
			if state.ai.cfg.dialog != aiDialogAgentAllowAll {
				t.Fatal("re-enabling full access skipped consent")
			}
		})
	}
}

func TestAgentClientSelectKeyboardAndInvalidPreferences(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1000, Height: 2400})
	if hit, ok := hitByID(&canvas, hitAISetAgentSelect); !ok || hit.Role != ui.RoleCombobox || hit.Value != "Claude Code" {
		t.Fatal("stdio has no visible client selector")
	}
	result := state.Key(ui.KeyEvent{Target: hitAISetAgentSelect, Key: ui.KeyDown})
	if !result.Handled || !state.ai.cfg.agentSelectOpen || state.ai.cfg.mcpSelectOpen {
		t.Fatal("the stdio selector did not open independently")
	}
	state.Paint(&canvas, ui.Size{Width: 1000, Height: 2400})
	result = state.Key(ui.KeyEvent{Target: aiAgentClientID(aiMCPClaude), Key: ui.KeyDown})
	if result.Focus != aiAgentClientID(aiMCPCursor) {
		t.Fatal("stdio arrow navigation selected an HTTP item")
	}
	state.Click(result.Focus)
	if state.ai.cfg.agentSelectOpen || state.ai.cfg.agentClient != aiMCPCursor || state.ai.cfg.mcpClient != aiMCPClaude {
		t.Fatal("stdio selection affected HTTP")
	}
	for _, key := range []string{aiAgentClientSetting, agentModeSetting} {
		if err := world.db(t).SetSetting(key, "future-unknown-value"); err != nil {
			t.Fatal(err)
		}
	}
	state.reloadAI()
	if state.ai.cfg.agentClient != aiMCPClaude || state.ai.settings.agentMode != gate.ModeObserve {
		t.Fatal("unknown preferences did not use safe defaults")
	}
}
