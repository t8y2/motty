package workbench

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestMCPClientTabsCopyAndKeyboard(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	state.Click(hitAISetMCPEnabled)
	state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "test-secret_abc-123"
	world.paint()
	state.Click(hitAISetMCPCopyURL)
	if got := world.clip[len(world.clip)-1]; got != "http://127.0.0.1:4455/mcp" {
		t.Fatalf("URL copy = %q", got)
	}
	state.Click(hitAISetMCPCopyToken)
	if got := world.clip[len(world.clip)-1]; got != state.ai.mcpToken {
		t.Fatalf("token copy = %q", got)
	}
	result := state.Key(ui.KeyEvent{Key: ui.KeyRight, Target: hitAISetMCPClaude})
	if !result.Handled || result.Focus != hitAISetMCPCursor || state.ai.cfg.mcpClient != aiMCPCursor {
		t.Fatalf("arrow did not select Cursor: %+v", result)
	}
	state.Click(hitAISetMCPCopy)
	var cursor struct {
		Servers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(world.clip[len(world.clip)-1]), &cursor); err != nil {
		t.Fatal(err)
	}
	if server := cursor.Servers["motty"]; server.URL != state.aiMCPURL() || server.Headers["Authorization"] != "Bearer "+state.ai.mcpToken {
		t.Fatalf("Cursor configuration = %+v", server)
	}
	state.Key(ui.KeyEvent{Key: ui.KeyEnd, Target: hitAISetMCPCursor})
	if state.ai.cfg.mcpClient != aiMCPCodex || state.ai.cfg.copied != aiCopyIdle {
		t.Fatal("End did not select Codex and reset copy feedback")
	}
	state.Click(hitAISetMCPCopy)
	want := "[mcp_servers.motty]\nurl = \"http://127.0.0.1:4455/mcp\"\nhttp_headers = { Authorization = \"Bearer test-secret_abc-123\" }"
	if got := world.clip[len(world.clip)-1]; got != want {
		t.Fatalf("Codex configuration = %q", got)
	}
	canvas, texts := world.paint()
	for _, value := range texts {
		if strings.Contains(value, state.ai.mcpToken) {
			t.Fatal("preview exposed the token")
		}
	}
	for _, check := range []struct {
		id    ui.ID
		label string
	}{
		{hitAISetMCPCopy, state.messages.AISettings.MCPCopied},
		{hitAISetMCPCopyURL, state.messages.AISettings.MCPCopyURL},
		{hitAISetMCPCopyToken, state.messages.AISettings.MCPCopyToken},
	} {
		if hit, ok := hitByID(canvas, check.id); !ok || hit.Label != check.label {
			t.Fatalf("copy feedback belongs to the wrong button: %+v", hit)
		}
	}
	state.options.SetClipboardText = func(string) error { return errors.New("clipboard unavailable") }
	state.Click(hitAISetMCPCopyURL)
	if state.ai.cfg.copied != aiCopyFailed || state.ai.cfg.copyTarget != hitAISetMCPCopyURL {
		t.Fatal("clipboard failure was not reported")
	}
}

func TestMCPCopyWaitsForLiveCredentials(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "old-token"
	checkDisabled := func() {
		t.Helper()
		canvas, _ := world.paint()
		for _, id := range []ui.ID{hitAISetMCPCopy, hitAISetMCPCopyURL, hitAISetMCPCopyToken} {
			if hit, ok := hitByID(canvas, id); !ok || !hit.Disabled {
				t.Fatalf("copy must be disabled: %+v", hit)
			}
			state.Click(id)
		}
		if len(world.clip) != 0 {
			t.Fatal("copied stale credentials")
		}
	}
	checkDisabled() // Disabling the service precedes the asynchronous endpoint update.
	state.Click(hitAISetMCPEnabled)
	state.Click(hitAISetMCPReset)
	checkDisabled() // No old token while the hub is issuing a replacement.
	state.setMCPEndpoint("http://127.0.0.1:4455/mcp", "new-token")
	pumpSettings(t, state, "replacement token", func() bool { return state.ai.mcpToken == "new-token" })
	state.Click(hitAISetMCPCopyToken)
	if len(world.clip) != 1 || world.clip[0] != "new-token" {
		t.Fatal("did not copy the replacement token")
	}
}

func TestMCPConfigurationLayout(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		world := newSettingsWorld(t, newLister())
		state := world.state
		state.setLanguage(language)
		state.selectSettingsCategory(settingsMCP)
		state.Click(hitAISetMCPEnabled)
		state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "test-secret"
		for _, width := range []float32{720, 1000, 1400} {
			for _, tab := range aiMCPClientTabs() {
				state.Click(tab.ID)
				var canvas ui.Canvas
				state.Paint(&canvas, ui.Size{Width: width, Height: 1600})
				for _, id := range []ui.ID{hitAISetMCPClaude, hitAISetMCPCursor, hitAISetMCPCodex, hitAISetMCPCopy, hitAISetMCPCopyURL, hitAISetMCPCopyToken, hitAISetMCPReset} {
					hit, ok := hitByID(&canvas, id)
					if !ok || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > width || hit.Bounds.Width <= 0 {
						t.Fatalf("width %v: control outside window: %+v", width, hit)
					}
				}
				key := ui.KeyRight
				if state.ai.cfg.mcpTabsVertical {
					key = ui.KeyDown
				}
				if result := state.Key(ui.KeyEvent{Key: key, Target: hitAISetMCPClaude}); result.Focus != hitAISetMCPCursor {
					t.Fatalf("width %v: keyboard orientation did not follow tabs: %+v", width, result)
				}
			}
		}
	}
}
