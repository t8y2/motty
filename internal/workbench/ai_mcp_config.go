package workbench

import (
	"encoding/json"
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
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
	aiMCPClientCount
)

func aiMCPClientID(client aiMCPClient) ui.ID { return hitAISetMCPClients.At(int(client)) }

func aiAgentClientID(client aiMCPClient) ui.ID { return hitAISetAgentClients.At(int(client)) }

const (
	aiMCPClientSetting = "ai.mcp.client"
	aiAgentClientSetting = "ai.mcp.stdio.client"
)

var aiMCPClientKeys = [...]string{"claude", "cursor", "codebuddy", "zcode", "trae", "vscode", "windsurf", "codex", "deepseek", "opencode", "cherry", "qoder", "workbuddy"}

func aiMCPClientFromKey(key string) aiMCPClient {
	for index, value := range aiMCPClientKeys {
		if key == value {
			return aiMCPClient(index)
		}
	}
	return aiMCPClaude
}

// Export preferences do not reconfigure either MCP service or its access level.
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
}

func aiAgentClientOptions() [aiMCPClientCount]components.SelectItem {
	options := aiMCPClientOptions()
	for index := range options {
		options[index].ID = aiAgentClientID(aiMCPClient(index))
	}
	return options
}

func aiMCPClientOptions() [aiMCPClientCount]components.SelectItem {
	names := [...]string{"Claude Code", "Cursor", "CodeBuddy Code CLI", "ZCode", "TRAE", "VS Code", "Windsurf", "Codex", "DeepSeek Harness", "OpenCode", "Cherry Studio", "Qoder", "WorkBuddy"}
	var options [aiMCPClientCount]components.SelectItem
	for index, name := range names {
		options[index] = components.SelectItem{ID: aiMCPClientID(aiMCPClient(index)), Label: name}
	}
	return options
}

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
