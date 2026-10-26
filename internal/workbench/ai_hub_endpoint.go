package workbench

import (
	"strings"

	"motty/internal/ai/mcp"
)

// setMCPEndpoint tells a window where external agents can reach it, for the
// settings page to show. The hub knows the server's URL; the window keeps
// host:port and adds the scheme and path itself, so the URL is cut down to that
// here. It is safe from any goroutine.
func (state *State) setMCPEndpoint(url, token string) {
	addr := strings.TrimSuffix(strings.TrimPrefix(url, "http://"), mcp.Path)
	state.aiPost(func(s *State) {
		if s.ai.mcpAddr != addr || s.ai.mcpToken != token {
			s.ai.cfg.copied = aiCopyIdle
		}
		s.ai.mcpAddr, s.ai.mcpToken = addr, token
	})
}
