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
	for index, name := range aiMCPClientNames {
		client := aiMCPClient(index)
		t.Run(name, func(t *testing.T) {
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
	if got := state.agentHostsText(); got != m.AgentHostsNoneDesc {
		t.Fatalf("empty hosts description = %q", got)
	}
	if !contains(texts, m.AgentsTitle) || !contains(texts, m.AgentActivityNone) {
		t.Fatalf("the empty group says %v", texts)
	}
	clickSettingsChoice(t, state, m.AgentModeRun)
	if settingValue(t, db, agentModeSetting) != AgentModeCommands || state.ai.settings.agentMode != gate.ModeAsk {
		t.Fatal("running commands was not stored")
	}
	clickSettingsChoice(t, state, m.AgentModeObserve)
	if settingValue(t, db, agentModeSetting) != AgentModeReadOnly || state.ai.settings.agentMode != gate.ModeObserve {
		t.Fatal("read only was not stored")
	}

	state.Click(hitAISetAgentCopy)
	if got := world.clip[len(world.clip)-1]; !strings.HasPrefix(got, "claude mcp add motty-hosts -- /opt/motty/motty mcp-serve --data-dir ") {
		t.Fatalf("copied %q", got)
	}
	chooseMCPClient(t, state, true, aiMCPCodex)
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
	for index, name := range aiMCPClientNames {
		t.Run(name, func(t *testing.T) {
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

func TestAgentHostModeChoicesBulkUpdateHostsAndPersist(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.restoreSettings(savedState{Hosts: []savedHost{
		{ID: "web", Label: "Web", Address: "web.example.com", User: "ops", Port: 22},
		{ID: "db", Label: "DB", Address: "db.example.com", User: "ops", Port: 22},
		{ID: "desk", Label: "Desk", Address: "desk.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
	}})
	state.selectSettingsCategory(settingsMCP)
	m := state.messages.AISettings

	clickSettingsChoiceInRow(t, state, m.AgentHosts, m.AgentHostsAll)
	if state.agentHostMode != agentHostModeAll || !state.agentHosts["web"] || !state.agentHosts["db"] || state.agentHosts["desk"] {
		t.Fatalf("allow all did not open every SSH host: mode %v, hosts %v", state.agentHostMode, state.agentHosts)
	}
	catalog, err := readAgentCatalog(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.agentHostList(); len(got) != 2 || got[0].CatalogID != "web" || got[1].CatalogID != "db" {
		t.Fatalf("the independent service read %+v after allow all", got)
	}
	all := New(state.options)
	if all.agentHostMode != agentHostModeAll || !all.agentHosts["web"] || !all.agentHosts["db"] || all.agentHosts["desk"] {
		t.Fatalf("the allow-all choice did not survive a restart: mode %v, hosts %v", all.agentHostMode, all.agentHosts)
	}
	// Close before the test changes the store again: a state writes its own
	// snapshot back on the way out.
	all.Close()

	clickSettingsChoiceInRow(t, state, m.AgentHosts, m.AgentHostsNone)
	if state.agentHostMode != agentHostModeNone || len(state.agentHosts) != 0 {
		t.Fatalf("allow none left hosts open: mode %v, hosts %v", state.agentHostMode, state.agentHosts)
	}
	catalog, err = readAgentCatalog(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.agentHostList(); len(got) != 0 {
		t.Fatalf("the independent service read %+v after allow none", got)
	}

	state.setAgentAccess(state.hosts[0], true)
	clickSettingsChoiceInRow(t, state, m.AgentHosts, m.AgentHostsFollow)
	if state.agentHostMode != agentHostModeFollow || !state.agentHosts["web"] || state.agentHosts["db"] {
		t.Fatalf("following host settings changed their choices: mode %v, hosts %v", state.agentHostMode, state.agentHosts)
	}
	again := New(state.options)
	defer again.Close()
	if again.agentHostMode != agentHostModeFollow || !again.agentHosts["web"] || again.agentHosts["db"] {
		t.Fatalf("the follow choice did not survive a restart: mode %v, hosts %v", again.agentHostMode, again.agentHosts)
	}
	if got := agentHostModeFromSaved("a-future-mode"); got != agentHostModeFollow {
		t.Fatalf("an unknown mode became %v instead of the compatible per-host behavior", got)
	}
}

func TestAgentHostModeControlsNewHostDefaultsAndIndividualOverrides(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	for _, test := range []struct {
		mode agentHostMode
		want bool
	}{
		{mode: agentHostModeNone, want: false},
		{mode: agentHostModeFollow, want: false},
		{mode: agentHostModeAll, want: true},
	} {
		state.setAgentHostMode(test.mode)
		state.openHostDialog()
		if state.editor.agentAccess != test.want {
			t.Errorf("new host in mode %v starts with agent access %v", test.mode, state.editor.agentAccess)
		}
		state.Click(hitDialogCancel)
		state.openCopyHostID(host.ID)
		if state.editor.agentAccess != test.want {
			t.Errorf("copied host in mode %v starts with agent access %v", test.mode, state.editor.agentAccess)
		}
		state.Click(hitDialogCancel)
	}

	state.setAgentHostMode(agentHostModeAll)
	state.openHostDialog()
	typeInto(state, hitDialogLabel, "New host")
	typeInto(state, hitDialogAddress, "new.example.com")
	typeInto(state, hitDialogUser, "ops")
	state.Click(hitDialogSubmit)
	created := state.hosts[len(state.hosts)-1]
	if state.agentHostMode != agentHostModeAll || !state.agentHosts[created.CatalogID] {
		t.Fatalf("a host created with the default was not opened: mode %v, access %v", state.agentHostMode, state.agentHosts[created.CatalogID])
	}

	state.openEditHostID(created.ID)
	state.Click(hitDialogAgentAccess)
	state.Click(hitDialogSubmit)
	if state.agentHostMode != agentHostModeFollow || state.agentHosts[created.CatalogID] || !state.agentHosts[host.CatalogID] {
		t.Fatalf("an individual exception did not switch to per-host settings: mode %v, hosts %v", state.agentHostMode, state.agentHosts)
	}
}

func TestEffectiveAgentHostModeDescribesTheSavedPermissions(t *testing.T) {
	ssh1 := Host{ID: 1, CatalogID: "web", Address: "web.example.com", Port: 22}
	ssh2 := Host{ID: 2, CatalogID: "db", Address: "db.example.com", Port: 22}
	desktop := Host{ID: 3, CatalogID: "desk", Address: "desk.example.com", Port: 5900, Protocol: protocolVNC}
	for _, test := range []struct {
		name string
		mode agentHostMode
		open []string
		want agentHostMode
	}{
		{name: "all matches every SSH host", mode: agentHostModeAll, open: []string{"web", "db"}, want: agentHostModeAll},
		{name: "a desktop host is not part of the bulk choice", mode: agentHostModeAll, open: []string{"web", "db", "desk"}, want: agentHostModeAll},
		{name: "an SSH host closed while stored as all is drift", mode: agentHostModeAll, open: []string{"web"}, want: agentHostModeFollow},
		{name: "none matches closed hosts", mode: agentHostModeNone, want: agentHostModeNone},
		{name: "a desktop host in the list is not drift from none", mode: agentHostModeNone, open: []string{"desk"}, want: agentHostModeNone},
		{name: "an SSH host opened while stored as none is drift", mode: agentHostModeNone, open: []string{"db"}, want: agentHostModeFollow},
		{name: "follow is always itself", mode: agentHostModeFollow, open: []string{"web"}, want: agentHostModeFollow},
	} {
		catalog := hostCatalog{hosts: []Host{ssh1, ssh2, desktop}, agentHostMode: test.mode, agentHosts: agentHostSet(test.open)}
		if got := catalog.effectiveAgentHostMode(); got != test.want {
			t.Errorf("%s: effective mode %v, want %v", test.name, got, test.want)
		}
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
