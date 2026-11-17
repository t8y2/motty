package completion

import (
	"sort"
	"strings"
)

// pluginHosts names the commands that resolve subcommands from PATH using the
// "<host>-<name>" naming convention. Carapace discovers these at runtime by
// asking the host (docker --help, git --exec-path, gh extension list, and so
// on); the offline catalog cannot run anything, so the imported "<host>-<name>"
// root is also reachable as a subcommand of <host>.
//
// Only commands that document this convention belong here. A root such as
// "ssh-keygen" or "apt-get" is an independent program, not "ssh keygen".
var pluginHosts = map[string]bool{
	"cargo":   true,
	"docker":  true,
	"gh":      true,
	"git":     true,
	"helm":    true,
	"kubectl": true,
}

// PluginCommand is one subcommand a host resolves from PATH as
// "<host>-<Name>". Node is that command's root: it carries the plugin's own
// options and subcommands.
type PluginCommand struct {
	Name string
	Node *Node
}

// Plugins returns the subcommands host resolves from PATH in name order. A
// host without plugin roots returns nil. The slice belongs to the catalog and
// must not be modified.
func (c *Catalog) Plugins(host string) []PluginCommand {
	if c == nil {
		return nil
	}
	return c.plugins[host]
}

// buildPlugins groups roots named "<host>-<name>" under their host. The
// catalog is immutable once built, so the nodes are shared, not copied: the
// same root answers "docker-compose" and "docker compose".
func buildPlugins(roots []*Node) map[string][]PluginCommand {
	var plugins map[string][]PluginCommand
	for _, root := range roots {
		index := strings.IndexByte(root.Token, '-')
		if index <= 0 || index == len(root.Token)-1 {
			continue
		}
		host := root.Token[:index]
		if !pluginHosts[host] {
			continue
		}
		if plugins == nil {
			plugins = make(map[string][]PluginCommand, len(pluginHosts))
		}
		plugins[host] = append(plugins[host], PluginCommand{Name: root.Token[index+1:], Node: root})
	}
	// Import sorts roots, but a decoded cache is untrusted input; the merge in
	// the matcher needs every slice in name order to keep the list alphabetical.
	for _, commands := range plugins {
		sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	}
	return plugins
}

// pluginCommand resolves one plugin subcommand of host. The slices are tiny
// (the largest host has a few dozen roots), so a scan beats a second index.
func pluginCommand(catalog *Catalog, host, token string) *Node {
	for _, plugin := range catalog.Plugins(host) {
		if plugin.Name == token {
			return plugin.Node
		}
	}
	return nil
}
