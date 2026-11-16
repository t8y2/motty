package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"motty/internal/ai/store"
)

func piAgentPath(dir string) string { return filepath.Join(dir, "pi-agent") }

// Pi reads MCP servers from the same agent directory as its login and settings
// and offers no flag for a different file, so the whole directory is redirected.
// The private copy carries only Motty's server; the user's login and provider
// definitions are shared so a refreshed token lands in the file Pi and its other
// runs read next.
func piAgentDir(dir string) (string, error) {
	agent := piAgentPath(dir)
	if err := os.MkdirAll(agent, 0o700); err != nil {
		return "", errors.New("cli: runtime_config_failed")
	}
	if source := piUserAgentDir(); source != "" {
		for _, name := range []string{"auth.json", "models.json"} {
			if err := piShareFile(filepath.Join(source, name), filepath.Join(agent, name)); err != nil {
				return "", err
			}
		}
		if err := piModelSettings(filepath.Join(source, "settings.json"), filepath.Join(agent, "settings.json")); err != nil {
			return "", err
		}
	}
	return agent, nil
}

func piUserAgentDir() string {
	if dir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); dir != "" {
		return expandTilde(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// piShareFile links the user's file instead of copying it, so a token refresh
// during the run updates the credentials the next run reads. A copy is the
// fallback across volumes and on file systems that do not support links.
func piShareFile(source, target string) error {
	if _, err := os.Stat(source); err != nil {
		return nil
	}
	if err := os.Link(source, target); err == nil {
		return nil
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return errors.New("cli: runtime_config_failed")
	}
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		return errors.New("cli: runtime_config_failed")
	}
	return nil
}

// piModelSettings keeps the user's model choice and nothing else from its
// settings. Tool selection, project trust, and resource paths are settled by the
// command line, so no setting can open an execution channel or load a resource.
func piModelSettings(source, target string) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return nil
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(raw, &settings) != nil {
		return nil
	}
	kept := make(map[string]json.RawMessage)
	for _, key := range []string{"defaultProvider", "defaultModel", "defaultThinkingLevel", "modelThinkingLevels", "thinkingBudgets", "enabledModels"} {
		if value, ok := settings[key]; ok {
			kept[key] = value
		}
	}
	if len(kept) == 0 {
		return nil
	}
	value, err := json.Marshal(kept)
	if err != nil {
		return errors.New("cli: runtime_config_failed")
	}
	if err := os.WriteFile(target, value, 0o600); err != nil {
		return errors.New("cli: runtime_config_failed")
	}
	return nil
}

// writePiMCP registers Motty's server as the only one. Direct exposure declares
// each tool to the model, so calls arrive as tool calls rather than as codemode
// scripts; the timeout matches the MCP server's own call limit.
func writePiMCP(agent, server, url, token string) error {
	config := map[string]any{
		"autoEnableCodemode": false,
		"mcpServers": map[string]any{server: map[string]any{
			"url":      url,
			"headers":  map[string]string{"Authorization": "Bearer " + token},
			"exposure": "direct",
			"timeout":  900,
		}},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return errors.New("cli: runtime_config_failed")
	}
	if err := os.WriteFile(filepath.Join(agent, "mcp.json"), raw, 0o600); err != nil {
		return errors.New("cli: runtime_config_failed")
	}
	return nil
}

// verifyPiMCP proves the isolated directory reaches Motty's server and sees
// exactly the scoped tools. Pi starts no other server, so a listing entry that
// differs is a configuration the model would not be given.
func verifyPiMCP(ctx context.Context, c command, server string, want []string) error {
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	check := c
	check.args = append(append([]string{}, c.args...), "mcp", "list", "--json")
	check.input = ""
	out := &boundedWriter{limit: 4 << 20, strict: true, cancel: cancel}
	_, _ = runCommand(runCtx, check, out)
	if runCtx.Err() != nil {
		return runCtx.Err()
	}
	var report struct {
		Servers []struct {
			Name  string   `json:"name"`
			State string   `json:"state"`
			Tools []string `json:"tools"`
		} `json:"servers"`
	}
	if len(out.data) == 0 || json.Unmarshal(out.data, &report) != nil {
		return compatibilityFailure(adapters[store.KindPiCLI].program, check, "mcp list --json", "unavailable", "")
	}
	for _, entry := range report.Servers {
		if entry.Name != server {
			continue
		}
		if entry.State != "connected" {
			return compatibilityFailure(adapters[store.KindPiCLI].program, check, "mcp list --json", "not_applied", "state "+entry.State)
		}
		if !sameTools(entry.Tools, want) {
			return compatibilityFailure(adapters[store.KindPiCLI].program, check, "mcp list --json", "not_applied", "tools")
		}
		return nil
	}
	return compatibilityFailure(adapters[store.KindPiCLI].program, check, "mcp list --json", "not_applied", "server "+server)
}

func sameTools(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
