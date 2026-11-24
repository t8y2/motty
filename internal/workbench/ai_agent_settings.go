package workbench

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/gate"
	"motty/internal/ai/store"
	"motty/internal/i18n"
)

// The settings of the MCP server that works without the window (`motty
// mcp-serve`, see agent_mcp.go): what agents may do, which hosts they have, the
// client configuration that starts it, and what they did.

var (
	hitAISetAgentCopy      = tree.ID(aiSettingsHits, "ai-set-agent-copy")
	hitAISetAgentClear     = tree.ID(aiSettingsHits, "ai-set-agent-clear")
	hitAISetAgentHostsView = tree.ID(aiSettingsHits, "ai-set-agent-hosts-view")

	hitAISetAgentTransferAdd    = tree.ID(aiSettingsHits, "ai-set-agent-transfer-add")
	hitAISetAgentTransferRemove = func() (ids [maxAgentTransferRoots]gfx.ID) {
		for i := range ids {
			ids[i] = tree.ID(aiSettingsHits, "ai-set-agent-transfer-remove-"+strconv.Itoa(i))
		}
		return
	}()
)

// maxAgentTransferRoots bounds the folder list so every row has a fixed control
// identity.
const maxAgentTransferRoots = 8

// agentAuditShown is how many of the latest calls the page lists.
const agentAuditShown = 5

var agentHostModeChoices = [...]agentHostMode{agentHostModeNone, agentHostModeFollow, agentHostModeAll}

func (state *State) agentHostModeLabel(mode agentHostMode) string {
	switch mode {
	case agentHostModeNone:
		return state.messages.AISettings.AgentHostsNone
	case agentHostModeAll:
		return state.messages.AISettings.AgentHostsAll
	}
	return state.messages.AISettings.AgentHostsFollow
}

// applyAgentHostMode stores a bulk choice and persists it. A refused save puts
// the mode and the per-host permissions back, so the page, the running server
// and the file keep describing the same access.
func (state *State) applyAgentHostMode(mode agentHostMode) {
	previousMode, previousHosts := state.agentHostMode, maps.Clone(state.agentHosts)
	state.setAgentHostMode(mode)
	if err := state.saveWithResult(); err != nil {
		state.agentHostMode, state.agentHosts = previousMode, previousHosts
		state.showActionFailure(state.messages.AISettings.AgentsTitle, err, state.messages.ActionFeedback.SaveFailed)
	}
}

// aiAgentClick handles the controls of the group that are not its mode choice.
func (state *State) aiAgentClick(id gfx.ID) bool {
	switch id {
	case hitAISetAgentCopy:
		state.aiCopyText(hitAISetAgentCopy, state.aiAgentPreview())
	case hitAISetAgentClear:
		if db, err := state.aiStore(); err == nil && db.ClearAudit() == nil {
			state.aiWrote()
		}
	case hitAISetAgentHostsView:
		state.openAgentHostsView()
	case hitAISetAgentTransferAdd:
		state.addAgentTransferRoot()
	default:
		for index, remove := range hitAISetAgentTransferRemove {
			if id == remove {
				state.removeAgentTransferRoot(index)
				return true
			}
		}
		return false
	}
	return true
}

// addAgentTransferRoot lets the user pick a local folder agents may transfer
// files within. A canceled picker changes nothing.
func (state *State) addAgentTransferRoot() {
	roots := state.ai.agentTransferRoots
	if len(roots) >= maxAgentTransferRoots {
		return
	}
	directory, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: state.messages.AISettings.AgentTransferChoose})
	if err != nil || directory == "" {
		return
	}
	directory = filepath.Clean(directory)
	if slices.Contains(roots, directory) {
		return
	}
	state.putAgentTransferRoots(append(slices.Clone(roots), directory))
}

func (state *State) removeAgentTransferRoot(index int) {
	roots := state.ai.agentTransferRoots
	if index < 0 || index >= len(roots) {
		return
	}
	state.putAgentTransferRoots(slices.Delete(slices.Clone(roots), index, index+1))
}

func (state *State) putAgentTransferRoots(roots []string) {
	value := ""
	if len(roots) > 0 {
		encoded, _ := json.Marshal(roots)
		value = string(encoded)
	}
	state.aiPut(agentTransferRootsSetting, value)
}

// applyAIAgentMode stores the stdio server's access level the choice already
// wrote. Full access is asked for, not written: the confirmation stores it.
func (state *State) applyAIAgentMode(mode gate.Mode) {
	switch mode {
	case gate.ModeAllowAll:
		if state.ai.settings.agentMode != gate.ModeAllowAll {
			state.ai.cfg.dialog = aiDialogAgentAllowAll
		}
	case gate.ModeAsk:
		state.aiPut(agentModeSetting, AgentModeCommands)
	default:
		state.aiPut(agentModeSetting, AgentModeReadOnly)
	}
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
// The labels are cached separately so a large catalog never becomes one giant
// wrapping text run.
func (state *State) agentHostLabels() []string {
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
	cache := &state.ai.agentSummary
	if cache.hostsCount != count || cache.hostsHash != hash {
		cache.hostsHash, cache.hostsCount = hash, count
		cache.hosts = make([]string, 0, count)
		for index := range state.hosts {
			host := &state.hosts[index]
			if host.isSSH() && state.agentHosts[host.CatalogID] {
				cache.hosts = append(cache.hosts, host.Label)
			}
		}
	}
	return cache.hosts
}

// agentSummary caches the texts the page shows about agents, which are made of
// data that changes rarely, so that painting an unchanged page allocates nothing.
type agentSummary struct {
	hostsHash  uint64
	hostsCount int
	hosts      []string

	lines    []string
	language i18n.Language
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
	c, pal, m := page.c, state.pal, state.messages.AISettings
	entries := state.ai.agentAudit
	activity := m.AgentActivityNone
	if len(entries) > 0 {
		activity = m.AgentActivityDesc
	}
	page.group(m.AgentsTitle, func() {
		ui.Column(c).Pad(18).Children(func() {
			ui.Wrap(c, m.AgentsDescription).FontSize(metrics.FontSizeS).LineHeight(18).Color(pal.muted)
		})
		mode := state.ai.settings.agentMode
		settingsChoiceRow(page, m.AgentMode, m.AgentModeDesc, &mode, aiAgentModeChoices[:], state.aiAgentModeLabel,
			func() { state.applyAIAgentMode(mode) }, nil)
		if state.ai.settings.agentMode == gate.ModeAllowAll {
			page.mcpAccessWarning(m.AgentAllowAllWarning)
		}
		choices := agentHostModeChoices[:]
		controlWidth := max(min(float32(300), max(float32(190), page.width*0.40)), settingsChoiceWidth(page, choices, state.agentHostModeLabel))
		controlWidth = min(controlWidth, max(page.width-60, 0))
		page.mcpAgentHostsFrame(controlWidth)
		state.paintAgentTransferRoots(page)
		page.separator()
		state.paintMCPConfigBlock(page, aiMCPConfigView{
			title: m.AgentConfig, description: m.AgentConfigDesc,
			client: &state.ai.cfg.agentClient, agent: true,
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
		ui.Column(c).Pad(18).Gap(4).Children(func() {
			for _, line := range state.agentAuditLines() {
				ui.Wrap(c, line).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(16)
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
func (state *State) aiCopyText(target gfx.ID, value string) {
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

// paintAgentTransferRoots is the list of local folders agents may transfer files
// within. The rows are offered under any access level so the setting can be
// found, but the service uses them only under full access.
func (state *State) paintAgentTransferRoots(page *settingsPage) {
	c, pal, m := page.c, state.pal, state.messages.AISettings
	roots := state.ai.agentTransferRoots
	description := m.AgentTransferDesc
	if len(roots) == 0 {
		description = m.AgentTransferNoneDesc
	}
	page.mcpFrame(m.AgentTransfer, description, pal.muted, func() {
		page.button(hitAISetAgentTransferAdd, m.AgentTransferAdd, aiButtonOutline, len(roots) >= maxAgentTransferRoots)
	})
	for index, root := range roots[:min(len(roots), maxAgentTransferRoots)] {
		page.separator()
		ui.Row(c).FillWidth().PadSides(8, 18, 8, 18).Gap(12).Items(ui.Center).Children(func() {
			ui.Wrap(c, root).FontSize(metrics.FontSizeS).LineHeight(16).Color(pal.text).Grow(1)
			page.button(hitAISetAgentTransferRemove[index], m.AgentTransferRemove, aiButtonOutline, false).Shrink(0)
		})
	}
}
