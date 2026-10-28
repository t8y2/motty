package workbench

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/store"
)

func TestAgentConfigurationStartsMottyMcpServeForEveryClient(t *testing.T) {
	const exe = "/Applications/Motty.app/Contents/MacOS/motty"
	for index, option := range aiMCPClientOptions() {
		client := aiMCPClient(index)
		t.Run(option.Label, func(t *testing.T) {
			config := aiAgentConfig(client, exe)
			if !strings.Contains(config, exe) || !strings.Contains(config, "mcp-serve") {
				t.Fatalf("does not start the program: %s", config)
			}
			if strings.Contains(strings.ToLower(config), "bearer") || strings.Contains(config, "http") {
				t.Fatalf("a program started by the client has no address or token: %s", config)
			}
			if strings.HasPrefix(config, "{") && !json.Valid([]byte(config)) {
				t.Fatalf("not JSON: %s", config)
			}
		})
	}
	if got := aiAgentConfig(aiMCPClaude, exe); got != "claude mcp add motty-hosts -- "+exe+" mcp-serve" {
		t.Fatalf("Claude Code = %s", got)
	}
	if got := aiAgentConfig(aiMCPCodex, exe); got != "[mcp_servers.motty-hosts]\ncommand = \""+exe+"\"\nargs = [\"mcp-serve\"]" {
		t.Fatalf("Codex = %s", got)
	}
	var vscode struct {
		Servers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(aiAgentConfig(aiMCPVSCode, exe)), &vscode); err != nil {
		t.Fatal(err)
	}
	if server := vscode.Servers["motty-hosts"]; server.Type != "stdio" || server.Command != exe || len(server.Args) != 1 || server.Args[0] != "mcp-serve" {
		t.Fatalf("VS Code = %+v", vscode)
	}
	var opencode struct {
		MCP map[string]struct {
			Type    string   `json:"type"`
			Command []string `json:"command"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(aiAgentConfig(aiMCPOpenCode, exe)), &opencode); err != nil {
		t.Fatal(err)
	}
	if server := opencode.MCP["motty-hosts"]; server.Type != "local" || len(server.Command) != 2 || server.Command[0] != exe {
		t.Fatalf("OpenCode = %+v", opencode)
	}
}

func TestAProgramPathWithASpaceSurvivesTheClientCommandLine(t *testing.T) {
	if got := shellWord("/usr/local/bin/motty"); got != "/usr/local/bin/motty" {
		t.Fatalf("a plain path was quoted: %s", got)
	}
	quoted := shellWord("/Users/a b/Motty.app/motty")
	if quoted == "/Users/a b/Motty.app/motty" || !strings.Contains(quoted, "a b") {
		t.Fatalf("a path with a space is not quoted: %s", quoted)
	}
	if got := aiAgentConfig(aiMCPClaude, "/Users/a b/motty"); !strings.Contains(got, quoted[:1]+"/Users/a b/motty") {
		t.Fatalf("the command does not quote the path: %s", got)
	}
}

func TestAgentSettingsStoreTheModeCopyTheConfigurationAndShowAndClearTheActivity(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.Executable = func() (string, error) { return "/opt/motty/motty", nil }
	})
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	db := world.db(t)
	m := state.messages.AISettings

	_, texts := world.paint()
	if !contains(texts, m.AgentsTitle) || !contains(texts, m.AgentHostsNoneDesc) || !contains(texts, m.AgentActivityNone) {
		t.Fatalf("the empty group says %v", texts)
	}
	state.Click(hitAISetAgentRun)
	if settingValue(t, db, agentModeSetting) != AgentModeCommands || state.ai.settings.agentMode != gate.ModeAsk {
		t.Fatal("running commands was not stored")
	}
	state.Click(hitAISetAgentObserve)
	if settingValue(t, db, agentModeSetting) != AgentModeReadOnly || state.ai.settings.agentMode != gate.ModeObserve {
		t.Fatal("read only was not stored")
	}

	state.Click(hitAISetAgentCopy)
	if got := world.clip[len(world.clip)-1]; !strings.HasPrefix(got, "claude mcp add motty-hosts -- /opt/motty/motty mcp-serve --data-dir ") {
		t.Fatalf("copied %q", got)
	}
	state.Click(aiAgentClientID(aiMCPCodex))
	state.Click(hitAISetAgentCopy)
	if got := world.clip[len(world.clip)-1]; !strings.HasPrefix(got, "[mcp_servers.motty-hosts]") {
		t.Fatalf("the chosen client was not used: %q", got)
	}

	if err := db.RecordAudit(store.AuditEntry{At: time.Now(), HostLabel: "web", Tool: "terminal_exec", Summary: "uptime", Outcome: store.AuditOK}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordAudit(store.AuditEntry{At: time.Now(), HostLabel: "db", Tool: "terminal_exec", Summary: "rm -rf /", Outcome: store.AuditRefused}); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	_, texts = world.paint()
	var lines []string
	for _, text := range texts {
		if strings.Contains(text, "terminal_exec") {
			lines = append(lines, text)
		}
	}
	if len(lines) != 2 || !strings.Contains(lines[0], "rm -rf /") || !strings.Contains(lines[0], m.AgentOutcomeRefused) ||
		!strings.Contains(lines[1], "uptime") || !strings.Contains(lines[1], m.AgentOutcomeOK) {
		t.Fatalf("the activity lists %q", lines)
	}

	state.Click(hitAISetAgentClear)
	if entries, _ := db.Audit(10); len(entries) != 0 {
		t.Fatalf("the activity was not cleared: %+v", entries)
	}
	if _, texts = world.paint(); contains(texts, m.AgentActivityNone) == false {
		t.Fatalf("the cleared page says %v", texts)
	}
}

func TestAgentConfigurationKeepsTheCurrentDataDirectoryForEveryClient(t *testing.T) {
	const executable = "/Applications/Motty.app/Contents/MacOS/motty"
	const directory = "/home/test/profile with spaces"
	for index, option := range aiMCPClientOptions() {
		t.Run(option.Label, func(t *testing.T) {
			config := aiAgentConfigForDataDir(aiMCPClient(index), executable, directory)
			if !strings.Contains(config, "--data-dir") || !strings.Contains(config, directory) {
				t.Fatalf("the service would read another profile: %s", config)
			}
			if strings.HasPrefix(config, "{") && !json.Valid([]byte(config)) {
				t.Fatalf("invalid configuration: %s", config)
			}
			if aiMCPClient(index) == aiMCPClaude && !strings.Contains(config, shellWord(directory)) {
				t.Fatal("the CLI profile path is not quoted")
			}
		})
	}
}

func TestOpeningAHostToAgentsIsAChoiceOfTheHostDialogAndACopyStartsClosed(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.selectedHostID = host.ID
	state.openEditHost()
	state.Click(hitDialogAdvanced)
	if state.editor.agentAccess {
		t.Fatal("a host starts closed to agents")
	}
	state.Click(hitDialogAgentAccess)
	state.Click(hitDialogSubmit)
	if !state.agentHosts[host.CatalogID] {
		t.Fatal("the switch was not kept")
	}
	if labels := state.agentHostLabels(); len(labels) != 1 || labels[0] != host.Label {
		t.Fatalf("hosts open to agents = %v", labels)
	}

	// It is there when the host is edited again, and the section shows it.
	state.selectedHostID = host.ID
	state.openEditHost()
	if !state.editor.agentAccess || !state.editor.advancedOpen {
		t.Fatalf("the open host's dialog: access %v, advanced %v", state.editor.agentAccess, state.editor.advancedOpen)
	}
	state.Click(hitDialogCancel)

	// A copy is a new host and starts closed, though its source is open.
	state.selectedHostID = host.ID
	state.openCopyHost()
	if state.editor.agentAccess {
		t.Fatal("a copy of an open host starts open")
	}
	state.Click(hitDialogSubmit)
	if labels := state.agentHostLabels(); len(labels) != 1 {
		t.Fatalf("copying opened the copy: %v", labels)
	}

	// It survives a restart, and turning it off closes the host.
	again := New(state.options)
	defer again.Close()
	if !again.agentHosts[host.CatalogID] {
		t.Fatal("the choice did not survive a restart")
	}
	state.selectedHostID = host.ID
	state.openEditHost()
	state.Click(hitDialogAgentAccess)
	state.Click(hitDialogSubmit)
	if len(state.agentHostLabels()) != 0 {
		t.Fatal("turning the switch off did not close the host")
	}
}

func TestADeletedHostDoesNotComeBackOpenToAgents(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.setAgentAccess(host, true)
	state.save()
	state.hosts = state.hosts[1:] // the host is deleted
	state.save()
	state.hosts = append([]Host{host}, state.hosts...) // and added again at the same address
	if got := state.snapshot().AgentHosts; len(got) != 0 {
		t.Fatalf("a host that was deleted and added again is open to agents: %v", got)
	}
}
