package workbench

import (
	"encoding/json"
	"fmt"

	"github.com/vivaui/viva/components"
)

type aiMCPClient uint8

const (
	aiMCPClaude aiMCPClient = iota
	aiMCPCursor
	aiMCPCodex
)

func aiMCPClientTabs() [3]components.TabOption {
	return [3]components.TabOption{
		{ID: hitAISetMCPClaude, Label: "Claude Code"},
		{ID: hitAISetMCPCursor, Label: "Cursor"},
		{ID: hitAISetMCPCodex, Label: "Codex"},
	}
}

// The endpoint is a local HTTP URL; the server generates a base64url token.
// Both are shell-safe in the existing Claude Code command on Windows and Unix.
func aiMCPConfig(client aiMCPClient, endpoint, token string) string {
	switch client {
	case aiMCPCursor:
		type server struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		}
		config := struct {
			Servers map[string]server `json:"mcpServers"`
		}{map[string]server{"motty": {endpoint, map[string]string{"Authorization": "Bearer " + token}}}}
		value, _ := json.MarshalIndent(config, "", "  ")
		return string(value)
	case aiMCPCodex:
		// JSON string escaping is also valid for TOML basic strings.
		url, _ := json.Marshal(endpoint)
		auth, _ := json.Marshal("Bearer " + token)
		return fmt.Sprintf("[mcp_servers.motty]\nurl = %s\nhttp_headers = { Authorization = %s }", url, auth)
	default:
		return fmt.Sprintf(aiMCPClientFormat, endpoint, token)
	}
}
