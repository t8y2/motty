package cli

import (
	"context"
	"encoding/json"
	"errors"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Disable complete non-Motty execution channels while retaining the CLI's own
// authentication and provider routing. No credential-bearing config is copied.
var codexControls = []string{
	`features.goals=false`,
	`features.view_image=false`, `tools.experimental_request_user_input.enabled=false`,
	`features.shell_tool=false`, `features.apply_patch_freeform=false`, `features.js_repl=false`,
	`features.multi_agent=false`, `features.collab=false`, `features.multi_agent_v2=false`,
	`features.apps=false`, `features.connectors=false`, `features.plugins=false`, `features.plugin_hooks=false`,
	`features.hooks=false`, `features.codex_hooks=false`, `features.browser_use=false`, `features.computer_use=false`,
	`features.memory_tool=false`, `features.imagegenext=false`, `features.request_permissions=false`,
	`features.code_mode=false`, `web_search="disabled"`, `notify=[]`, `hooks={}`,
}

var adapters = map[string]adapter{
	store.KindClaudeCLI: {
		program: "claude",
		required: []string{"--settings", "--setting-sources", "--strict-mcp-config", "--tools", "--disable-slash-commands",
			"--include-partial-messages", "--no-session-persistence", "--print", "--output-format", "--mcp-config",
			"--permission-mode", "--allowedTools"},
	},
	store.KindCodexCLI: {
		program: "codex", required: []string{"--ephemeral", "--json", "--sandbox"},
		help:   []string{"exec", "--help"},
		checks: []capabilityCheck{{label: "features list", args: withOverrides([]string{"features", "list"}, codexControls), validate: validateCodexControls}},
	},
	store.KindOpenCodeCLI: {
		program: "opencode", required: []string{"--pure"},
		checks: []capabilityCheck{{
			label:    "debug config --pure",
			args:     []string{"debug", "config", "--pure"},
			env:      []string{"OPENCODE_DISABLE_PROJECT_CONFIG=1", "OPENCODE_DB=:memory:", `OPENCODE_CONFIG_CONTENT={"permission":"deny"}`},
			validate: validateOpenCodePermission,
		}},
	},
	// Pi's built-in read, bash, edit, and write tools run with the permissions of
	// its own process, and its user-level MCP servers would bypass Motty's gate.
	// Both are removed; only the built-in MCP extension stays, pointed at Motty's
	// server by the private agent directory.
	store.KindPiCLI: {
		program: "pi",
		required: []string{"--print", "--mode", "--no-builtin-tools", "--no-extensions", "--extension",
			"--no-skills", "--no-prompt-templates", "--no-context-files", "--no-approve", "--no-session"},
		// Printing help builds a session and loads resources, so the same
		// isolation flags precede it: no installed extension runs from probe.
		help: []string{"--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--no-approve", "--offline", "--help"},
	},
}

// Check the installed binary's effective feature state, not a version allowlist.
// A feature the CLI still lists must be off under our overrides; a richer or
// reworded table is tolerated, and a feature the CLI no longer has cannot be
// switched on, so it is inert rather than unsafe.
func validateCodexControls(raw []byte) checkFailure {
	controlled := make(map[string]bool)
	for _, override := range codexControls {
		if name, ok := strings.CutPrefix(override, "features."); ok {
			name, value, _ := strings.Cut(name, "=")
			if value == "false" {
				controlled[name] = true
			}
		}
	}
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || headerRow(fields) {
			continue
		}
		if len(fields) < 3 || seen[fields[0]] {
			return checkFailure{reason: "invalid_output"}
		}
		name, value := fields[0], fields[len(fields)-1]
		switch value {
		case "true", "false":
		default:
			return checkFailure{reason: "invalid_output"}
		}
		seen[name] = true
		if controlled[name] && value != "false" {
			return checkFailure{reason: "not_applied", detail: "features." + name + "=false"}
		}
	}
	return checkFailure{}
}

// headerRow is a human-readable header, not a feature: "Feature Stage Enabled".
func headerRow(fields []string) bool {
	switch strings.ToLower(fields[0]) {
	case "feature", "features", "name":
		return true
	}
	return false
}

func validateOpenCodePermission(raw []byte) checkFailure {
	if !json.Valid(raw) {
		return checkFailure{reason: "invalid_output"}
	}
	if !openCodeConfigDenied(raw) {
		return checkFailure{reason: "not_applied", detail: "permission=deny"}
	}
	return checkFailure{}
}

func withOverrides(args, overrides []string) []string {
	for _, v := range overrides {
		args = append(args, "-c", v)
	}
	return args
}
func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }
func writePrivate(dir, name string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		return "", errors.New("cli: runtime_config_failed")
	}
	return path, nil
}
func openCodeEnv(config any) []string {
	return []string{
		"OPENCODE_CONFIG_CONTENT=" + jsonString(config), "OPENCODE_DB=:memory:", "OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS=1", "OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD=1", "OPENCODE_DISABLE_DEFAULT_PLUGINS=1", "OPENCODE_PERMISSION=", "OPENCODE_PURE=1",
	}
}
func foreignMCP(parent context.Context, kind string, c command, dir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	c.dir = dir
	var names []string
	switch kind {
	case store.KindClaudeCLI:
		settings, err := writePrivate(dir, "probe-settings.json", map[string]any{"disableAllHooks": true})
		if err != nil {
			return nil, err
		}
		c.args = append(c.args, "--settings", settings, "plugin", "list", "--json")
		raw, err := capture(ctx, c, 4<<20)
		if err != nil {
			return nil, compatibilityFailure(adapters[kind].program, c, "plugin list --json", "unavailable", "")
		}
		var plugins []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &plugins) != nil {
			return nil, compatibilityFailure(adapters[kind].program, c, "plugin list --json", "invalid_output", "")
		}
		for _, plugin := range plugins {
			if plugin.ID != "" {
				names = append(names, plugin.ID)
			}
		}
	case store.KindCodexCLI:
		c.args = withOverrides(append(c.args, "mcp", "list", "--json"), codexControls)
		raw, err := capture(ctx, c, 4<<20)
		if err != nil {
			return nil, compatibilityFailure(adapters[kind].program, c, "mcp list --json", "unavailable", "")
		}
		var servers []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &servers) != nil {
			return nil, compatibilityFailure(adapters[kind].program, c, "mcp list --json", "invalid_output", "")
		}
		for _, s := range servers {
			if s.Name != "" {
				names = append(names, s.Name)
			}
		}
	case store.KindOpenCodeCLI:
		c.args = append(c.args, "debug", "config", "--pure")
		c.env = openCodeEnv(map[string]any{"permission": "deny"})
		raw, err := capture(ctx, c, 4<<20)
		if err != nil {
			return nil, compatibilityFailure(adapters[kind].program, c, "debug config --pure", "unavailable", "")
		}
		var config struct {
			MCP map[string]json.RawMessage `json:"mcp"`
		}
		if json.Unmarshal(raw, &config) != nil {
			return nil, compatibilityFailure(adapters[kind].program, c, "debug config --pure", "invalid_output", "")
		}
		for name := range config.MCP {
			names = append(names, name)
		}
	}
	return names, nil
}
func buildCommand(kind string, c command, req llm.Request, dir, url, token string, foreign []string, server string) (command, error) {
	c.args = append([]string{}, c.args...)
	c.dir = dir
	names := make([]string, 0, len(req.Tools))
	for _, s := range req.Tools {
		names = append(names, s.Name)
	}
	images, err := writeImages(dir, conversationImages(req.Messages))
	if err != nil {
		return command{}, err
	}
	switch kind {
	case store.KindClaudeCLI:
		disabledPlugins := make(map[string]bool)
		c.env = append(c.env, "ENABLE_TOOL_SEARCH=false", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1")
		for _, plugin := range foreign {
			disabledPlugins[plugin] = false
		}
		settings, err := writePrivate(dir, "settings.json", map[string]any{"disableAllHooks": true, "enabledPlugins": disabledPlugins})
		if err != nil {
			return command{}, err
		}
		allowed := make([]string, 0, len(names))
		for _, n := range names {
			allowed = append(allowed, "mcp__"+server+"__"+n)
		}
		config, err := writePrivate(dir, "mcp.json", map[string]any{"mcpServers": map[string]any{server: map[string]any{"type": "http", "url": url, "headers": map[string]string{"Authorization": "Bearer " + token}}}})
		if err != nil {
			return command{}, err
		}
		c.args = append(c.args, "--print", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--no-session-persistence", "--setting-sources", "user", "--settings", settings, "--disable-slash-commands", "--strict-mcp-config", "--mcp-config", config, "--tools", "", "--permission-mode", "dontAsk")
		if len(allowed) > 0 {
			c.args = append(c.args, "--allowedTools", strings.Join(allowed, ","))
		}
		if req.Model != "" && req.Model != "default" {
			c.args = append(c.args, "--model", req.Model)
		}
	case store.KindCodexCLI:
		// Root tables merge with user layers. Explicitly disable each inherited
		// server rather than assuming mcp_servers={} replaces the lower table.
		entries := make([]string, 0, len(foreign)+1)
		for _, n := range foreign {
			entries = append(entries, strconv.Quote(n)+"={enabled=false}")
		}
		entries = append(entries, strconv.Quote(server)+"={url="+strconv.Quote(url)+`,bearer_token_env_var="MOTTY_CLI_TOKEN",required=true,default_tools_approval_mode="approve",enabled_tools=`+jsonString(names)+",tool_timeout_sec=900}")
		c.args = append(c.args, "exec", "--json", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only")
		c.args = withOverrides(c.args, append(append([]string{}, codexControls...), "mcp_servers={"+strings.Join(entries, ",")+"}"))
		// --image takes any number of values, so each is joined to its flag and
		// cannot swallow the prompt placeholder that ends the arguments.
		for _, file := range images {
			c.args = append(c.args, "--image="+file)
		}
		c.env = append(c.env, "MOTTY_CLI_TOKEN="+token)
		if req.Model != "" && req.Model != "default" {
			c.args = append(c.args, "--model", req.Model)
		}
		c.args = append(c.args, "-")
	case store.KindOpenCodeCLI:
		mcps := map[string]any{}
		for _, n := range foreign {
			mcps[n] = map[string]any{"enabled": false}
		}
		mcps[server] = map[string]any{"type": "remote", "url": url, "headers": map[string]string{"Authorization": "Bearer " + token}, "enabled": true, "timeout": 900000}
		permission := map[string]string{"*": "deny"}
		tools := map[string]bool{}
		for _, n := range []string{"bash", "read", "write", "edit", "glob", "grep", "list", "webfetch", "websearch", "codesearch", "task", "skill", "todowrite", "todoread", "question"} {
			tools[n] = false
		}
		for _, n := range names {
			permission[server+"_"+n] = "allow"
			tools[server+"_"+n] = true
		}
		config := map[string]any{"permission": "deny", "mcp": mcps, "agent": map[string]any{server: map[string]any{"description": "Motty scoped assistant", "mode": "primary", "permission": permission, "tools": tools}}}
		c.env = append(c.env, openCodeEnv(config)...)
		c.args = append(c.args, "run", "--format", "json", "--pure", "--agent", server)
		if req.Model != "" && req.Model != "default" {
			c.args = append(c.args, "--model", req.Model)
		}
		for _, file := range images {
			c.args = append(c.args, "--file="+file)
		}
	case store.KindPiCLI:
		agent, err := piAgentDir(dir)
		if err != nil {
			return command{}, err
		}
		if err = writePiMCP(agent, server, url, token); err != nil {
			return command{}, err
		}
		c.env = append(c.env,
			"PI_CODING_AGENT_DIR="+agent, "PI_SKIP_VERSION_CHECK=1", "PI_TELEMETRY=0", "PI_OFFLINE=1",
		)
		c.args = append(c.args, "--print", "--mode", "json",
			"--no-extensions", "--extension", "builtin:mcp", "--no-builtin-tools",
			"--no-skills", "--no-prompt-templates", "--no-context-files", "--no-approve", "--no-session")
		for _, file := range images {
			c.args = append(c.args, "@"+file)
		}
		if req.Model != "" && req.Model != "default" {
			c.args = append(c.args, "--model", req.Model)
		}
	default:
		return command{}, errors.New("cli: unsupported_provider")
	}
	return c, nil
}
