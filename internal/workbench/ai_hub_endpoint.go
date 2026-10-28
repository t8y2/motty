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
		// Every window uses the same database and HTTP service. Reload access
		// even when its endpoint did not change (or HTTP is disabled), so a
		// second window cannot display stale permissions. Do not open a store
		// just for publication: an unopened settings page loads it on demand.
		if s.ai.store == nil {
			return
		}
		enabled, _, enabledErr := s.ai.store.Setting("ai.mcp.enabled")
		mode, _, modeErr := s.ai.store.Setting("ai.mcp.mode")
		agentMode, _, agentErr := s.ai.store.Setting(agentModeSetting)
		if enabledErr == nil && modeErr == nil && agentErr == nil {
			s.ai.settings.mcpEnabled = enabled == "on"
			s.ai.settings.mcpMode = mcpModeFromSetting(mode)
			s.ai.settings.agentMode = mcpModeFromSetting(agentMode)
		}
	})
}
