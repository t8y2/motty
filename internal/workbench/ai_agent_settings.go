package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/gate"
	"motty/internal/ai/store"
	"motty/internal/i18n"
)

// The settings of the MCP server that works without the window (`motty
// mcp-serve`, see agent_mcp.go): what agents may do, which hosts they have, the
// client configuration that starts it, and what they did.

var (
	hitAISetAgentObserve       = tree.ID(aiSettingsHits, "ai-set-agent-observe")
	hitAISetAgentRun           = tree.ID(aiSettingsHits, "ai-set-agent-run")
	hitAISetAgentAllowAll      = tree.ID(aiSettingsHits, "ai-set-agent-allow-all")
	hitAISetAgentCopy          = tree.ID(aiSettingsHits, "ai-set-agent-copy")
	hitAISetAgentClear         = tree.ID(aiSettingsHits, "ai-set-agent-clear")
	hitAISetAgentSelect        = tree.ID(aiSettingsHits, "ai-set-agent-select")
	hitAISetAgentSelectDismiss = tree.ID(aiSettingsHits, "ai-set-agent-select-dismiss")
	hitAISetAgentClients       = tree.Sub(aiSettingsHits, tree.Name("ai-set-agent-client"))
)

// agentAuditShown is how many of the latest calls the page lists.
const agentAuditShown = 5

func (state *State) aiAgentModeOptions() [3]components.SegmentOption {
	m := state.messages.AISettings
	return [...]components.SegmentOption{
		{ID: hitAISetAgentObserve, Label: m.AgentModeObserve},
		{ID: hitAISetAgentRun, Label: m.AgentModeRun},
		{ID: hitAISetAgentAllowAll, Label: m.MCPModeAllowAll},
	}
}

// aiAgentClick handles the controls of the group.
func (state *State) aiAgentClick(id ui.ID) bool {
	switch id {
	case hitAISetAgentObserve:
		state.aiPut(agentModeSetting, AgentModeReadOnly)
	case hitAISetAgentRun:
		state.aiPut(agentModeSetting, AgentModeCommands)
	case hitAISetAgentAllowAll:
		if state.ai.settings.agentMode != gate.ModeAllowAll {
			state.ai.cfg.dialog = aiDialogAgentAllowAll
		}
	case hitAISetAgentCopy:
		state.aiCopyText(hitAISetAgentCopy, state.aiAgentPreview())
	case hitAISetAgentSelect:
		state.ai.cfg.agentSelectOpen = !state.ai.cfg.agentSelectOpen
		state.ai.cfg.mcpSelectOpen = false
	case hitAISetAgentSelectDismiss:
		state.ai.cfg.agentSelectOpen = false
	case hitAISetAgentClear:
		if db, err := state.aiStore(); err == nil && db.ClearAudit() == nil {
			state.aiWrote()
		}
	default:
		if index, ok := id.In(hitAISetAgentClients); ok && index >= 0 && index < int(aiMCPClientCount) {
			state.aiChooseMCPClient(aiMCPClient(index), true)
			state.ai.cfg.agentSelectOpen = false
			return true
		}
		return false
	}
	return true
}

// agentExecutable is the program a client has to start.
func (state *State) agentExecutable() string {
	path, err := state.options.Executable()
	if err != nil {
		return "motty"
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path
}

// agentHostLabels are the names of the hosts open to agents, in catalog order.
func (state *State) agentHostLabels() []string {
	var labels []string
	for _, host := range state.agentHostList() {
		labels = append(labels, host.Label)
	}
	return labels
}

// agentSummary caches the texts the page shows about agents, which are made of
// data that changes rarely, so that painting an unchanged page allocates nothing.
type agentSummary struct {
	hostsHash  uint64
	hostsCount int
	hostsText  string

	lines    []string
	language i18n.Language
}

// agentHostsText lists the hosts open to agents, or says there are none. It is
// rebuilt only when the hosts that are open, or their names, are not the ones it
// was made from.
func (state *State) agentHostsText() string {
	const offset, prime = 14695981039346656037, 1099511628211
	hash, count := uint64(offset), 0
	for index := range state.hosts {
		host := &state.hosts[index]
		if !host.isSSH() || !state.agentHosts[host.CatalogID] {
			continue
		}
		count++
		for i := 0; i < len(host.Label); i++ {
			hash = (hash ^ uint64(host.Label[i])) * prime
		}
		hash = (hash ^ 0xff) * prime
	}
	if count == 0 {
		return state.messages.AISettings.AgentHostsNoneDesc
	}
	cache := &state.ai.agentSummary
	if cache.hostsCount != count || cache.hostsHash != hash || cache.hostsText == "" {
		cache.hostsHash, cache.hostsCount = hash, count
		cache.hostsText = strings.Join(state.agentHostLabels(), ", ")
	}
	return cache.hostsText
}

// agentAuditLines are the latest calls as the page shows them, in the language
// of the window; a reload of the log or a change of language rebuilds them.
func (state *State) agentAuditLines() []string {
	cache := &state.ai.agentSummary
	entries := state.ai.agentAudit
	if len(entries) == 0 {
		return nil
	}
	if cache.lines == nil || cache.language != state.language {
		cache.lines = cache.lines[:0]
		for _, entry := range entries[:min(len(entries), agentAuditShown)] {
			cache.lines = append(cache.lines, agentAuditLine(entry, state.messages.AISettings))
		}
		cache.language = state.language
	}
	return cache.lines
}

func (state *State) paintAIAgents(page *settingsPage) {
	pal, m := state.pal, state.messages.AISettings
	selected := hitAISetAgentObserve
	switch state.ai.settings.agentMode {
	case gate.ModeAsk:
		selected = hitAISetAgentRun
	case gate.ModeAllowAll:
		selected = hitAISetAgentAllowAll
	}
	modes := state.aiAgentModeOptions()
	hosts := state.agentHostsText()
	entries := state.ai.agentAudit
	activity := m.AgentActivityNone
	if len(entries) > 0 {
		activity = m.AgentActivityDesc
	}
	page.group(m.AgentsTitle, func() {
		page.f.Column().Pad(18).Children(func() {
			page.f.Wrap(m.AgentsDescription).FontSize(metrics.FontSizeS).LineHeight(18).Color(pal.muted)
		})
		page.mcpFrame(m.AgentMode, m.AgentModeDesc, pal.muted, func() {
			width := min(components.SegmentedControlWidth(page.f.Canvas(), state.Theme(), modes[:]), max(0, page.width-36))
			page.segmentSlot(width, modes[:], selected)
		})
		if state.ai.settings.agentMode == gate.ModeAllowAll {
			page.mcpAccessWarning(m.AgentAllowAllWarning)
		}
		page.mcpFrame(m.AgentHosts, hosts, pal.muted, func() {})
		page.separator()
		state.paintMCPConfigBlock(page, aiMCPConfigView{
			title: m.AgentConfig, description: m.AgentConfigDesc,
			client: state.ai.cfg.agentClient, selectKind: controlAgentSelect,
			copyID: hitAISetAgentCopy, ready: true,
			preview: state.aiAgentPreview(),
			lines:   &state.ai.cfg.agentPreviewLines,
		})
		page.mcpFrame(m.AgentActivity, activity, pal.muted, func() {
			page.button(hitAISetAgentClear, m.AgentActivityClear, aiButtonOutline, len(entries) == 0)
		})
		if len(entries) == 0 {
			return
		}
		page.separator()
		f := page.f
		f.Column().Pad(18).Gap(4).Children(func() {
			for _, line := range state.agentAuditLines() {
				f.Wrap(line).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(16)
			}
		})
	})
}

// Paths are resolved when settings opens; painting only reuses the program and
// the rendered configuration until the client choice changes.
func (state *State) aiAgentPreview() string {
	cfg := &state.ai.cfg
	if cfg.agentPreviewConfig == "" || cfg.agentPreviewClient != cfg.agentClient {
		program := cfg.agentProgram
		if program == "" {
			program = "motty"
		}
		cfg.agentPreviewClient = cfg.agentClient
		cfg.agentPreviewConfig = aiAgentConfigForDataDir(cfg.agentClient, program, cfg.agentDataDir)
	}
	return cfg.agentPreviewConfig
}

// agentAuditLine is one call of the activity list: when, which host, which tool,
// the command or path, and how it ended.
func agentAuditLine(entry store.AuditEntry, m i18n.AISettingsMessages) string {
	outcome := m.AgentOutcomeOK
	switch entry.Outcome {
	case store.AuditFailed:
		outcome = m.AgentOutcomeFailed
	case store.AuditRefused:
		outcome = m.AgentOutcomeRefused
	}
	what := entry.Tool
	if entry.Summary != "" {
		what += " " + entry.Summary
	}
	host := entry.HostLabel
	if host == "" {
		host = "-"
	}
	return fmt.Sprintf("%s  %s  %s — %s", entry.At.Local().Format(time.DateTime), host, what, outcome)
}

// aiCopyText puts text on the clipboard and remembers which button did, so the
// button can say it worked.
func (state *State) aiCopyText(target ui.ID, value string) {
	cfg := &state.ai.cfg
	cfg.copyTarget = target
	if err := state.options.SetClipboardText(value); err != nil {
		cfg.copied = aiCopyFailed
		return
	}
	cfg.copied = aiCopyDone
}

// aiAgentConfig is what a client needs to start `motty mcp-serve` itself: the
// program and its one argument. There is no address and no token. The forms
// follow each client's own way of naming a program to start; clients whose
// stdio form is not documented get the common mcpServers entry.
func aiAgentConfig(client aiMCPClient, executable string) string {
	return aiAgentConfigForDataDir(client, executable, "")
}

func aiAgentConfigForDataDir(client aiMCPClient, executable, dataDir string) string {
	const name = "motty-hosts"
	args := []string{"mcp-serve"}
	if dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}
	words := []string{shellWord(executable)}
	for _, argument := range args {
		words = append(words, shellWord(argument))
	}
	commandLine := strings.Join(words, " ")
	arguments, _ := json.Marshal(args)
	switch client {
	case aiMCPClaude:
		return "claude mcp add " + name + " -- " + commandLine
	case aiMCPCodeBuddy:
		return "codebuddy mcp add --scope local -- " + name + " " + commandLine
	case aiMCPCodex:
		// JSON string escaping is also valid for TOML basic strings.
		command, _ := json.Marshal(executable)
		return fmt.Sprintf("[mcp_servers.%s]\ncommand = %s\nargs = %s", name, command, arguments)
	case aiMCPDeepSeek:
		command, _ := json.Marshal(executable)
		return fmt.Sprintf("- insert:\n    - id: mcp-%s\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: %s\n        transport: stdio\n        command: %s\n        args: %s", name, name, command, arguments)
	}
	server := map[string]any{"command": executable, "args": args}
	root := "mcpServers"
	switch client {
	case aiMCPZCode, aiMCPTrae, aiMCPQoder, aiMCPWorkBuddy:
		server["type"] = "stdio"
	case aiMCPVSCode:
		root = "servers"
		server["type"] = "stdio"
	case aiMCPOpenCode:
		root = "mcp"
		server = map[string]any{"type": "local", "command": append([]string{executable}, args...), "enabled": true}
	case aiMCPCherry:
		server["type"], server["name"] = "stdio", name
	}
	var config any = map[string]any{root: map[string]any{name: server}}
	if client == aiMCPZCode {
		config = map[string]any{"mcp": map[string]any{"servers": map[string]any{name: server}}}
	}
	value, _ := json.MarshalIndent(config, "", "  ")
	return string(value)
}

var plainShellWord = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,\\-]+$`)

// shellWord quotes a path for the command line of the user's shell, so a path
// with a space survives: double quotes where the shell is Windows', single
// quotes elsewhere.
func shellWord(word string) string {
	if plainShellWord.MatchString(word) {
		return word
	}
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(word, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// executableOf is Options.Executable's default.
func executableOf() (string, error) { return os.Executable() }
