package workbench

import (
	"encoding/json"
	"fmt"
)

type aiMCPClient uint8

const (
	aiMCPClaude aiMCPClient = iota
	aiMCPCursor
	aiMCPCodeBuddy
	aiMCPZCode
	aiMCPTrae
	aiMCPVSCode
	aiMCPWindsurf
	aiMCPCodex
	aiMCPDeepSeek
	aiMCPOpenCode
	aiMCPCherry
	aiMCPQoder
	aiMCPWorkBuddy
	aiMCPPi
	aiMCPClientCount
)

const (
	aiMCPClientSetting   = "ai.mcp.client"
	aiAgentClientSetting = "ai.mcp.stdio.client"
)

var aiMCPClientKeys = [...]string{"claude", "cursor", "codebuddy", "zcode", "trae", "vscode", "windsurf", "codex", "deepseek", "opencode", "cherry", "qoder", "workbuddy", "pi"}

func aiMCPClientFromKey(key string) aiMCPClient {
	for index, value := range aiMCPClientKeys {
		if key == value {
			return aiMCPClient(index)
		}
	}
	return aiMCPClaude
}

// Export preferences do not reconfigure either MCP service or its access level,
// but every open window shows the same stored choice.
func (state *State) aiChooseMCPClient(client aiMCPClient, agent bool) {
	if client >= aiMCPClientCount {
		return
	}
	key := aiMCPClientSetting
	if agent {
		key = aiAgentClientSetting
	}
	db, err := state.aiStore()
	if err == nil {
		err = db.SetSetting(key, aiMCPClientKeys[client])
	}
	if err != nil {
		state.ai.cfg.notice = state.aiScrub(err.Error(), "")
		return
	}
	if agent {
		state.ai.cfg.agentClient = client
	} else {
		state.ai.cfg.mcpClient = client
	}
	if state.options.Group != nil {
		state.options.Group.assistantChoicesChanged(state)
	}
}

// aiMCPClientNames name the clients the export select offers, in aiMCPClient order.
var aiMCPClientNames = [...]string{"Claude Code", "Cursor", "CodeBuddy Code CLI", "ZCode", "TRAE", "VS Code", "Windsurf", "Codex", "DeepSeek Harness", "OpenCode", "Cherry Studio", "Qoder", "WorkBuddy", "Pi Coding Agent"}

// aiMCPClients are the clients the export select offers.
var aiMCPClients = func() (clients [aiMCPClientCount]aiMCPClient) {
	for index := range clients {
		clients[index] = aiMCPClient(index)
	}
	return clients
}()

func aiMCPClientName(client aiMCPClient) string { return aiMCPClientNames[client] }

// The endpoint is a local HTTP URL; the server generates a base64url token.
// Both are shell-safe in the existing Claude Code command on Windows and Unix.
func aiMCPConfig(client aiMCPClient, endpoint, token string) string {
	switch client {
	case aiMCPClaude:
		return fmt.Sprintf(aiMCPClientFormat, endpoint, token)
	case aiMCPCodeBuddy:
		return fmt.Sprintf("codebuddy mcp add --scope local --transport http --header \"Authorization: Bearer %s\" -- motty %s", token, endpoint)
	case aiMCPCodex:
		// JSON string escaping is also valid for TOML basic strings.
		url, _ := json.Marshal(endpoint)
		auth, _ := json.Marshal("Bearer " + token)
		return fmt.Sprintf("[mcp_servers.motty]\nurl = %s\nhttp_headers = { Authorization = %s }", url, auth)
	case aiMCPDeepSeek:
		url, _ := json.Marshal(endpoint)
		auth, _ := json.Marshal("Bearer " + token)
		return fmt.Sprintf("- insert:\n    - id: mcp-motty\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: motty\n        transport: streamable-http\n        url: %s\n        headers:\n          Authorization: %s", url, auth)
	}
	server := map[string]any{"url": endpoint, "headers": map[string]string{"Authorization": "Bearer " + token}}
	root := "mcpServers"
	switch client {
	case aiMCPZCode, aiMCPTrae, aiMCPVSCode, aiMCPQoder, aiMCPWorkBuddy:
		server["type"] = "http"
	case aiMCPWindsurf:
		delete(server, "url")
		server["serverUrl"] = endpoint
	case aiMCPOpenCode:
		root = "mcp"
		server["type"], server["enabled"], server["oauth"] = "remote", true, false
	case aiMCPCherry:
		delete(server, "url")
		server["baseUrl"], server["type"], server["name"] = endpoint, "streamableHttp", "motty"
	case aiMCPPi:
		// Pi's default exposure keeps MCP tools out of the model's tool list and
		// reachable only from codemode scripts; direct exposure declares them.
		server["exposure"], server["timeout"] = "direct", 900
	}
	if client == aiMCPVSCode {
		root = "servers"
	}
	var config any = map[string]any{root: map[string]any{"motty": server}}
	if client == aiMCPZCode {
		config = map[string]any{"mcp": map[string]any{"servers": map[string]any{"motty": server}}}
	}
	value, _ := json.MarshalIndent(config, "", "  ")
	return string(value)
}
