package workbench

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
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
		{aiMCPPi, []string{"mcpServers", "motty"}, "url", ""},
	} {
		t.Run(aiMCPClientNames[tc.client], func(t *testing.T) {
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
			// Pi's default exposure hides MCP tools behind codemode scripts.
			if tc.client == aiMCPPi && (config["exposure"] != "direct" || config["timeout"] != float64(900)) {
				t.Fatalf("Pi tools are not declared directly: %+v", config)
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

// The preview's header names what the block holds, so it has to be the format
// the client reads: a file body labeled as another format sends people to the
// wrong editor.
func TestMCPPreviewHeaderMatchesThePayload(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	m := world.state.messages.AISettings
	for index, name := range aiMCPClientNames {
		client := aiMCPClient(index)
		payload := aiMCPConfig(client, "http://127.0.0.1:4455/mcp", "test-secret")
		switch format := aiMCPPreviewFormat(client, m); format {
		case m.MCPCommand:
			if !strings.HasPrefix(payload, "claude mcp add ") && !strings.HasPrefix(payload, "codebuddy mcp add ") {
				t.Fatalf("%s: a command is expected, got %q", name, payload)
			}
		case "JSON":
			if !json.Valid([]byte(payload)) {
				t.Fatalf("%s: JSON is expected, got %q", name, payload)
			}
		case "TOML":
			if !strings.HasPrefix(payload, "[mcp_servers.motty]") {
				t.Fatalf("%s: TOML is expected, got %q", name, payload)
			}
		case "YAML":
			if !strings.HasPrefix(payload, "- insert:") {
				t.Fatalf("%s: YAML is expected, got %q", name, payload)
			}
		default:
			t.Fatalf("%s: unnamed preview format %q", name, format)
		}
	}
}

func TestMCPClientSelectAndCompleteCatalog(t *testing.T) {
	want := []string{"Claude Code", "Cursor", "CodeBuddy Code CLI", "ZCode", "TRAE", "VS Code", "Windsurf", "Codex", "DeepSeek Harness", "OpenCode", "Cherry Studio", "Qoder", "WorkBuddy", "Pi Coding Agent"}
	if !reflect.DeepEqual(aiMCPClientNames[:], want) {
		t.Fatalf("clients = %v", aiMCPClientNames)
	}
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	state.Click(hitAISetMCPEnabled)
	state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "secret-value"
	for i, name := range aiMCPClientNames {
		chooseMCPClient(t, state, false, aiMCPClient(i))
		canvas, _ := world.paint()
		found := false
		for _, hit := range canvas.Hits() {
			if hit.Role == gfx.RoleCombobox && hit.Label == state.messages.AISettings.MCPConfig && hit.Value == name && !hit.Expanded {
				found = true
			}
		}
		if !found {
			t.Fatalf("selected client %s is not visible", name)
		}
		state.Click(hitAISetMCPCopy)
		if got := world.clip[len(world.clip)-1]; got != aiMCPConfig(aiMCPClient(i), state.aiMCPURL(), state.ai.mcpToken) {
			t.Fatalf("wrong clipboard for %s", name)
		}
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
		for _, width := range []float32{570, 600, 720, 1000, 1400} {
			for index := range aiMCPClientNames {
				chooseMCPClient(t, state, false, aiMCPClient(index))
				var canvas gfx.Canvas
				// Both transport sections may be tall when narrow code blocks wrap.
				state.Paint(&canvas, gfx.Size{Width: width, Height: 6000})
				var copyToken, reset gfx.Hit
				for _, id := range []gfx.ID{hitAISetMCPCopy, hitAISetMCPCopyURL, hitAISetMCPCopyToken, hitAISetMCPReset, hitAISetAgentCopy} {
					hit, ok := hitByID(&canvas, id)
					if !ok || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > width || hit.Bounds.Width <= 0 {
						t.Fatalf("width %v: %s outside window: %+v", width, tree.Text(id), hit)
					}
					if id == hitAISetMCPCopyToken {
						copyToken = hit
					}
					if id == hitAISetMCPReset {
						reset = hit
					}
				}
				// The stdio access level is a page Choice, painted after the HTTP one.
				if hit, ok := settingsChoiceIn(&canvas, state.messages.AISettings.MCPModeAllowAll); !ok || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > width || hit.Bounds.Width <= 0 {
					t.Fatalf("width %v: stdio access level outside window: %+v", width, hit)
				}
				// Narrow pages stack the token actions instead of clipping one.
				if width <= 600 && copyToken.Bounds.Y+copyToken.Bounds.Height > reset.Bounds.Y {
					t.Fatalf("width %v: token actions are still one row: copy=%+v reset=%+v", width, copyToken.Bounds, reset.Bounds)
				}
			}
		}
	}
}
