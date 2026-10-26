package workbench

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestMCPClientConfigurationSchemas(t *testing.T) {
	endpoint, token := "http://127.0.0.1:49845/mcp", "test_secret-123"
	for _, tc := range []struct {
		client            aiMCPClient
		path              []string
		urlKey, transport string
	}{
		{aiMCPCursor, []string{"mcpServers", "motty"}, "url", ""},
		{aiMCPZCode, []string{"mcp", "servers", "motty"}, "url", "http"},
		{aiMCPTrae, []string{"mcpServers", "motty"}, "url", "http"},
		{aiMCPVSCode, []string{"servers", "motty"}, "url", "http"},
		{aiMCPWindsurf, []string{"mcpServers", "motty"}, "serverUrl", ""},
		{aiMCPOpenCode, []string{"mcp", "motty"}, "url", "remote"},
		{aiMCPCherry, []string{"mcpServers", "motty"}, "baseUrl", "streamableHttp"},
		{aiMCPQoder, []string{"mcpServers", "motty"}, "url", "http"},
		{aiMCPWorkBuddy, []string{"mcpServers", "motty"}, "url", "http"},
	} {
		t.Run(aiMCPClientOptions()[tc.client].Label, func(t *testing.T) {
			var config map[string]any
			if err := json.Unmarshal([]byte(aiMCPConfig(tc.client, endpoint, token)), &config); err != nil {
				t.Fatal(err)
			}
			for _, key := range tc.path {
				child, ok := config[key].(map[string]any)
				if !ok {
					t.Fatalf("missing object %s: %+v", key, config)
				}
				config = child
			}
			if config[tc.urlKey] != endpoint {
				t.Fatalf("wrong URL field: %+v", config)
			}
			if tc.transport != "" && config["type"] != tc.transport {
				t.Fatalf("wrong transport: %+v", config)
			}
			if !reflect.DeepEqual(config["headers"], map[string]any{"Authorization": "Bearer " + token}) {
				t.Fatalf("wrong authentication: %+v", config)
			}
			if tc.client == aiMCPOpenCode && (config["enabled"] != true || config["oauth"] != false) {
				t.Fatal("OpenCode must use static authorization")
			}
		})
	}
	if got := aiMCPConfig(aiMCPCodeBuddy, endpoint, token); got != `codebuddy mcp add --scope local --transport http --header "Authorization: Bearer test_secret-123" -- motty http://127.0.0.1:49845/mcp` {
		t.Fatalf("CodeBuddy command = %s", got)
	}
	want := "- insert:\n    - id: mcp-motty\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: motty\n        transport: streamable-http\n        url: \"http://127.0.0.1:49845/mcp\"\n        headers:\n          Authorization: \"Bearer test_secret-123\""
	if got := aiMCPConfig(aiMCPDeepSeek, endpoint, token); got != want {
		t.Fatalf("DeepSeek patch = %s", got)
	}
}

func TestMCPClientSelectAndCompleteCatalog(t *testing.T) {
	want := []string{"Claude Code", "Cursor", "CodeBuddy Code CLI", "ZCode", "TRAE", "VS Code", "Windsurf", "Codex", "DeepSeek Harness", "OpenCode", "Cherry Studio", "Qoder", "WorkBuddy"}
	tabs := aiMCPClientOptions()
	var got []string
	for _, tab := range tabs {
		got = append(got, tab.Label)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("clients = %v", got)
	}
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	state.Click(hitAISetMCPEnabled)
	state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "secret-value"
	for i, tab := range tabs {
		state.Click(tab.ID)
		canvas, _ := world.paint()
		if hit, ok := hitByID(canvas, hitAISetMCPSelect); !ok || hit.Role != ui.RoleCombobox || hit.Value != tab.Label || hit.Expanded {
			t.Fatalf("selected client %s is not visible", tab.Label)
		}
		state.Click(hitAISetMCPCopy)
		if got := world.clip[len(world.clip)-1]; got != aiMCPConfig(aiMCPClient(i), state.aiMCPURL(), state.ai.mcpToken) {
			t.Fatalf("wrong clipboard for %s", tab.Label)
		}
	}
	state.Click(hitAISetMCPSelect)
	canvas, _ := world.paint()
	for _, option := range tabs {
		if hit, ok := hitByID(canvas, option.ID); !ok || hit.Role != ui.RoleOption {
			t.Fatalf("missing client %s", option.Label)
		}
	}
	result := state.Key(ui.KeyEvent{Target: hitAISetMCPSelect, Key: ui.KeyTab})
	if !result.Handled || !result.ContinueTraversal || state.ai.cfg.mcpSelectOpen {
		t.Fatal("Tab must close the dropdown and continue traversal")
	}
}

func TestMCPClientSelectCopyAndKeyboard(t *testing.T) {
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
	result := state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: hitAISetMCPSelect})
	if !result.Handled || !state.ai.cfg.mcpSelectOpen {
		t.Fatalf("arrow did not open the selector: %+v", result)
	}
	world.paint()
	result = state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: hitAISetMCPClaude})
	if result.Focus != hitAISetMCPCursor {
		t.Fatalf("Down did not focus Cursor: %+v", result)
	}
	state.Click(result.Focus)
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
	state.Click(hitAISetMCPSelect)
	world.paint()
	result = state.Key(ui.KeyEvent{Key: ui.KeyEnd, Target: hitAISetMCPCursor})
	state.Click(result.Focus)
	if state.ai.cfg.mcpClient != aiMCPWorkBuddy || state.ai.cfg.copied != aiCopyIdle {
		t.Fatal("End did not select WorkBuddy and reset copy feedback")
	}
	state.Click(hitAISetMCPCopy)
	state.Click(hitAISetMCPCodex)
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

func TestMCPClientSelectShortWindow(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	paint := func() *ui.Canvas {
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 900, Height: 520})
		return &canvas
	}
	paint()
	state.settingsScroll = 10000
	paint()
	state.Click(hitAISetMCPSelect)
	canvas := paint()
	surface := state.popups.surface
	if surface.Y < windowBarHeight || surface.Y+surface.Height > 520 || surface.Height <= 0 {
		t.Fatalf("popup outside window: %+v", surface)
	}
	if _, ok := hitByID(canvas, hitMenuScrollBar); !ok {
		t.Fatal("short list needs a scrollbar")
	}
	result := state.Key(ui.KeyEvent{Target: hitAISetMCPSelect, Key: ui.KeyEnd})
	last := aiMCPClientID(aiMCPWorkBuddy)
	if !result.Handled || result.Focus != last {
		t.Fatalf("End: %+v", result)
	}
	if hit, ok := hitByID(paint(), last); !ok || intersectRect(hit.Bounds, surface).Height <= 0 {
		t.Fatal("last client did not scroll into view")
	}
	before := state.settingsScroll
	state.Scroll(ui.ScrollEvent{X: 20, Y: 20, DeltaY: 100})
	if state.settingsScroll != before {
		t.Fatal("dropdown must not scroll the background")
	}
	state.Click(last)
	paint()
	if state.ai.cfg.mcpSelectOpen || state.ai.cfg.mcpClient != aiMCPWorkBuddy {
		t.Fatal("choice did not close and update the select")
	}
	if result := state.Key(ui.KeyEvent{Target: hitAISetMCPSelect, Text: "z"}); !result.Handled || state.ai.cfg.mcpClient != aiMCPZCode {
		t.Fatal("closed select typeahead failed")
	}
	state.Click(hitAISetMCPSelect)
	paint()
	state.Click(hitAISetMCPSelectDismiss)
	if state.ai.cfg.mcpSelectOpen {
		t.Fatal("outside dismissal did not close the select")
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
			for _, tab := range aiMCPClientOptions() {
				state.Click(tab.ID)
				var canvas ui.Canvas
				state.Paint(&canvas, ui.Size{Width: width, Height: 1600})
				for _, id := range []ui.ID{hitAISetMCPSelect, hitAISetMCPCopy, hitAISetMCPCopyURL, hitAISetMCPCopyToken, hitAISetMCPReset} {
					hit, ok := hitByID(&canvas, id)
					if !ok || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > width || hit.Bounds.Width <= 0 {
						t.Fatalf("width %v: control outside window: %+v", width, hit)
					}
				}
			}
		}
	}
}
