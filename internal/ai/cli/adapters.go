package cli

import (
	"context"
	"encoding/json"
	"errors"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"os"
	"path/filepath"
	"sort"
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

const (
	openCodeV1 = "v1"
	openCodeV2 = "v2"

	openCodeProbeAgent = "motty_probe"
	openCodeProbeAllow = "motty_probe_allow"
)

// OpenCode 2 removed --pure, replaced the V1 permission map with ordered
// permission rules, and routes every command through a shared background server
// unless --standalone is passed. The V2 profile keeps scoped tool permissions
// and project-config isolation through native V2 config and a private server.
// It cannot match V1's --pure suppression of user-installed global plugins:
// OpenCode 2 has no equivalent, so those plugins still load.
var openCodeV2Adapter = adapter{
	program:  "opencode",
	flavor:   openCodeV2,
	required: []string{"--standalone"},
	checks: []capabilityCheck{
		{label: "run --help", args: []string{"run", "--help"}, validate: validateOpenCodeV2Run},
		{
			label:    "debug agents",
			args:     []string{"--standalone", "debug", "agents"},
			env:      openCodeV2Env(openCodeV2ProbeConfig()),
			isolate:  true,
			validate: validateOpenCodeV2Permissions,
		},
	},
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
		program: "opencode", flavor: openCodeV1, required: []string{"--pure"},
		checks: []capabilityCheck{{
			label:    "debug config --pure",
			args:     []string{"debug", "config", "--pure"},
			env:      []string{"OPENCODE_DISABLE_PROJECT_CONFIG=1", "OPENCODE_DB=:memory:", `OPENCODE_CONFIG_CONTENT={"permission":"deny"}`},
			validate: validateOpenCodePermission,
		}},
		alternate: &openCodeV2Adapter,
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
	store.KindQoderCLI: {
		program: "qoder",
		required: []string{"--print", "--output-format", "--permission-mode", "--allowed-tools", "--mcp-config",
			"--strict-mcp-config", "--setting-sources", "--settings", "--no-session-persistence", "--tools",
			"--attachment", "--model"},
		checks: []capabilityCheck{{
			label:    "plugins list --json",
			args:     []string{"--setting-sources", "user", "plugins", "list", "--json"},
			validate: validateQoderPlugins,
		}},
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

// OpenCode 2 replaced --pure with --standalone and moved run's required output
// and isolation switches into the run subcommand.
func validateOpenCodeV2Run(raw []byte) checkFailure {
	flags := helpFlags(raw)
	for _, flag := range []string{"--format", "--agent", "--file", "--title", "--thinking"} {
		if !flags[flag] {
			return checkFailure{reason: "missing_capability", detail: "run " + flag}
		}
	}
	return checkFailure{}
}

// validateOpenCodeV2Permissions proves the installed V2 build applies the
// ordered permission config that scopes its agent. The resolved agent is read
// from debug agents because OpenCode 2's debug config lists sources, not the
// merged result.
func validateOpenCodeV2Permissions(raw []byte) checkFailure {
	var agents []struct {
		ID          string                   `json:"id"`
		Permissions []openCodePermissionRule `json:"permissions"`
	}
	if json.Unmarshal(raw, &agents) != nil {
		return checkFailure{reason: "invalid_output"}
	}
	var rules []openCodePermissionRule
	found := false
	for _, agent := range agents {
		if agent.ID == openCodeProbeAgent {
			rules, found = agent.Permissions, true
			break
		}
	}
	if !found {
		return checkFailure{reason: "not_applied", detail: "agent=" + openCodeProbeAgent}
	}
	for _, action := range []string{"read", "edit", "glob", "grep", "shell", "subagent",
		"skill", "question", "webfetch", "websearch", "external_directory"} {
		if effect := openCodePermissionEffect(rules, action, "*"); effect != "deny" {
			return checkFailure{reason: "not_applied", detail: action + "=deny"}
		}
	}
	if effect := openCodePermissionEffect(rules, openCodeProbeAllow, "*"); effect != "allow" {
		return checkFailure{reason: "not_applied", detail: openCodeProbeAllow + "=allow"}
	}
	return checkFailure{}
}

type openCodePermissionRule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// openCodePermissionEffect applies OpenCode's ordered rules: the last rule whose
// action and resource match decides, and no match defaults to ask.
func openCodePermissionEffect(rules []openCodePermissionRule, action, resource string) string {
	effect := "ask"
	for _, rule := range rules {
		if openCodeWildcard(rule.Action, action) && openCodeWildcard(rule.Resource, resource) {
			effect = rule.Effect
		}
	}
	return effect
}

// openCodeWildcard implements the permission pattern language: * matches any
// run of characters including '/', ? matches one, everything else is literal.
func openCodeWildcard(pattern, value string) bool {
	p, v := 0, 0
	star, mark := -1, 0
	for v < len(value) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == value[v]):
			p++
			v++
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, v
			p++
		case star >= 0:
			p = star + 1
			mark++
			v = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

func openCodeV2ProbeConfig() map[string]any {
	deny := map[string]any{"action": "*", "resource": "*", "effect": "deny"}
	return map[string]any{
		"permissions": []any{deny},
		"agents": map[string]any{openCodeProbeAgent: map[string]any{
			"description": "Motty capability probe",
			"mode":        "primary",
			"permissions": []any{
				deny,
				map[string]any{"action": openCodeProbeAllow, "resource": "*", "effect": "allow"},
			},
		}},
	}
}

// openCodeV2ForeignServers reads the merged servers from OpenCode 2's config
// sources. A source may carry native mcp.servers or the V1 mcp map; both are
// normalized later by OpenCode, but the source listing keeps the raw shape.
func openCodeV2ForeignServers(raw []byte) ([]string, bool) {
	var sources []struct {
		Info struct {
			MCP json.RawMessage `json:"mcp"`
		} `json:"info"`
	}
	if json.Unmarshal(raw, &sources) != nil {
		return nil, false
	}
	seen := make(map[string]bool)
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, source := range sources {
		if len(source.Info.MCP) == 0 {
			continue
		}
		var mcp map[string]json.RawMessage
		if json.Unmarshal(source.Info.MCP, &mcp) != nil {
			continue
		}
		if servers, ok := mcp["servers"]; ok {
			var native map[string]json.RawMessage
			if json.Unmarshal(servers, &native) == nil {
				for name := range native {
					add(name)
				}
				continue
			}
		}
		for name, entry := range mcp {
			var keyed struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(entry, &keyed) == nil && keyed.Type != "" {
				add(name)
			}
		}
	}
	sort.Strings(names)
	return names, true
}

// validateQoderPlugins proves the installed build lists installed plugins in
// the JSON shape used to disable them for the scoped session.
func validateQoderPlugins(raw []byte) checkFailure {
	if _, ok := qoderPluginIDs(raw); !ok {
		return checkFailure{reason: "invalid_output"}
	}
	return checkFailure{}
}

// qoderPluginIDs reads the installed plugin IDs the inline settings disable
// with enabledPlugins[id]=false. A listed entry without an ID cannot be
// disabled, so the whole list is rejected rather than left enabled.
func qoderPluginIDs(raw []byte) ([]string, bool) {
	var plugins []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &plugins) != nil {
		return nil, false
	}
	ids := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		if plugin.ID == "" {
			return nil, false
		}
		ids = append(ids, plugin.ID)
	}
	return ids, true
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

// OpenCode 2 ignores the V1-only isolation variables. Project plugins and
// Claude compatibility discovery are still suppressible; --standalone keeps the
// private server from inheriting a running background server's config.
func openCodeV2Env(config any) []string {
	return []string{
		"OPENCODE_CONFIG_CONTENT=" + jsonString(config), "OPENCODE_DB=:memory:", "OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_DISABLE_AUTOUPDATE=1",
	}
}

// foreignQoderPlugins lists the user-scope plugins the session must disable.
// Project and local scopes are excluded by the same --setting-sources user used
// for the run, so only user plugins need an explicit false entry.
func foreignQoderPlugins(parent context.Context, c command, dir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	c.dir = dir
	c.args = append(c.args, "--setting-sources", "user", "plugins", "list", "--json")
	raw, err := capture(ctx, c, 4<<20)
	if err != nil {
		return nil, compatibilityFailure(adapters[store.KindQoderCLI].program, c, "plugins list --json", "unavailable", "")
	}
	ids, ok := qoderPluginIDs(raw)
	if !ok {
		return nil, compatibilityFailure(adapters[store.KindQoderCLI].program, c, "plugins list --json", "invalid_output", "")
	}
	return ids, nil
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
		if c.flavor == openCodeV2 {
			c.args = append(c.args, "--standalone", "debug", "config")
			c.env = append(c.env, "OPENCODE_CONFIG_CONTENT={}", "OPENCODE_DB=:memory:", "OPENCODE_DISABLE_PROJECT_CONFIG=1",
				"OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_DISABLE_AUTOUPDATE=1")
			raw, err := capture(ctx, c, 4<<20)
			if err != nil {
				return nil, compatibilityFailure(adapters[kind].program, c, "debug config", "unavailable", "")
			}
			names, ok := openCodeV2ForeignServers(raw)
			if !ok {
				return nil, compatibilityFailure(adapters[kind].program, c, "debug config", "invalid_output", "")
			}
			return names, nil
		}
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
		if c.flavor == openCodeV2 {
			permissions := []any{map[string]any{"action": "*", "resource": "*", "effect": "deny"}}
			for _, n := range names {
				permissions = append(permissions, map[string]any{"action": server + "_" + n, "resource": "*", "effect": "allow"})
			}
			servers := map[string]any{
				server: map[string]any{"type": "remote", "url": url, "headers": map[string]string{"Authorization": "Bearer " + token}, "oauth": false, "codemode": false},
			}
			for _, n := range foreign {
				if n != server {
					// A disabled remote entry needs no credential or inherited
					// command, and replaces any same-named server in lower config.
					servers[n] = map[string]any{"type": "remote", "url": "http://127.0.0.1:1/", "disabled": true}
				}
			}
			config := map[string]any{
				"permissions": []any{map[string]any{"action": "*", "resource": "*", "effect": "deny"}},
				"agents": map[string]any{server: map[string]any{
					"description": "Motty scoped assistant",
					"mode":        "primary",
					"permissions": permissions,
				}},
				"mcp": map[string]any{"servers": servers},
			}
			c.env = append(c.env, openCodeV2Env(config)...)
			// The private server keeps a running shared daemon from supplying
			// its own config and database; --title skips the title agent's
			// extra model call.
			c.args = append(c.args, "--standalone", "run", "--format", "json", "--agent", server, "--title", "motty", "--thinking")
			if req.Model != "" && req.Model != "default" {
				c.args = append(c.args, "--model", req.Model)
			}
			for _, file := range images {
				c.args = append(c.args, "--file="+file)
			}
			return c, nil
		}
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
	case store.KindQoderCLI:
		disabledPlugins := make(map[string]bool, len(foreign))
		for _, id := range foreign {
			disabledPlugins[id] = false
		}
		settings, err := writePrivate(dir, "settings.json", map[string]any{"disableAllHooks": true, "enabledPlugins": disabledPlugins})
		if err != nil {
			return command{}, err
		}
		mcpConfig, err := writePrivate(dir, "mcp.json", map[string]any{"mcpServers": map[string]any{
			server: map[string]any{"type": "http", "url": url, "headers": map[string]string{"Authorization": "Bearer " + token}},
		}})
		if err != nil {
			return command{}, err
		}
		c.args = append(c.args, "--print", "--output-format", "stream-json",
			"--permission-mode", "dont_ask", "--no-session-persistence",
			"--setting-sources", "user", "--settings", settings,
			"--strict-mcp-config", "--mcp-config", mcpConfig, "--tools", "")
		for _, n := range names {
			c.args = append(c.args, "--allowed-tools", "mcp__"+server+"__"+n)
		}
		if req.Model != "" && req.Model != "default" {
			c.args = append(c.args, "--model", req.Model)
		}
		for _, file := range images {
			c.args = append(c.args, "--attachment="+file)
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
