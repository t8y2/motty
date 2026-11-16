package workbench

import (
	"path/filepath"
	"slices"
	"strings"

	"motty/internal/localshell"
)

// A space keeps this reference outside the catalog ID namespace. Bare, it
// means the default shell on whichever computer opens the template, never a
// saved host. With the shell suffix it names one terminal of this computer,
// as in "local terminal:wsl:Ubuntu-22.04".
const workspaceLocalTerminalRef = "local terminal"

// workspaceLocalShellRefPrefix follows the local reference with the shell a
// pane runs: a localshell choice path, an absolute program or "wsl:" and a
// distribution.
const workspaceLocalShellRefPrefix = workspaceLocalTerminalRef + ":"

// workspaceLocalShellRef is the shell a reference names, if it is a local
// shell reference rather than the default-shell sentinel or a catalog id.
func workspaceLocalShellRef(id string) (string, bool) {
	shell, ok := strings.CutPrefix(id, workspaceLocalShellRefPrefix)
	return shell, ok && validLocalShell(shell)
}

// workspaceLocalShellReference is the workspace reference that reopens shell.
func workspaceLocalShellReference(shell string) string {
	return workspaceLocalShellRefPrefix + shell
}

func (state *State) workspaceLocalHost() Host {
	host := localHost(state.messages.LocalTerminalTitle)
	host.CatalogID = workspaceLocalTerminalRef
	host.Icon = hostIconTerminal
	return host
}

// workspaceLocalShellHost is the workspace's copy of a shell choice, so a pane
// can run one terminal among the ones this computer has instead of only the
// default.
func (state *State) workspaceLocalShellHost(shell localshell.Choice) Host {
	host := state.workspaceLocalHost()
	host.Label = shell.Name
	host.localShell = shell.Path
	host.CatalogID = workspaceLocalShellReference(shell.Path)
	return host
}

// workspaceLocalShellHostByRef rebuilds the host a saved reference names.
// A shell this computer still detects keeps its discovered display name; one
// that is away falls back to the program path's own, so templates render
// before discovery has run.
func (state *State) workspaceLocalShellHostByRef(shell string) Host {
	for _, choice := range state.localShellMenu.choices {
		if strings.EqualFold(choice.Path, shell) {
			return state.workspaceLocalShellHost(choice)
		}
	}
	name := filepath.Base(shell)
	if distro, wsl := localshell.WSLDistro(shell); wsl {
		name = localshell.WSLName(distro)
	}
	return state.workspaceLocalShellHost(localshell.Choice{Name: name, Path: shell})
}

func (state *State) workspaceHosts() []Host {
	if !state.localShellMenu.loaded {
		state.refreshLocalShells()
	}
	// The default terminal is the row the sentinel already stands for, so it is
	// not offered twice, like the new-tab picker keeps a lone shell unlisted.
	// A shell hidden from the pickers is left out here too.
	defaultIndex := state.localShellSelected()
	hosts := make([]Host, 0, len(state.hosts)+len(state.localShellMenu.choices)+1)
	hosts = append(hosts, state.workspaceLocalHost())
	for index, choice := range state.localShellMenu.choices {
		if choice.Path == "" || index == defaultIndex || state.shellHidden(choice) {
			continue
		}
		hosts = append(hosts, state.workspaceLocalShellHost(choice))
	}
	return append(hosts, state.hosts...)
}

// workspaceLaunchCatalog is what template launches resolve references
// against: the draft's choices, plus every reference the template names that
// the draft's filter left out. A shell hidden from the pickers, or one that
// has since become the default, still launches what was saved — the filter
// shapes what the user picks from, never what a saved template resolves to.
func (state *State) workspaceLaunchCatalog(hostIDs []string) []Host {
	catalog := state.workspaceHosts()
	for _, id := range hostIDs {
		if slices.ContainsFunc(catalog, func(host Host) bool { return host.CatalogID == id }) {
			continue
		}
		if host, ok := state.workspaceHostByID(id); ok {
			catalog = append(catalog, host)
		}
	}
	return catalog
}

func (state *State) workspaceHostByID(id string) (Host, bool) {
	if id == workspaceLocalTerminalRef {
		return state.workspaceLocalHost(), true
	}
	if shell, ok := workspaceLocalShellRef(id); ok {
		return state.workspaceLocalShellHostByRef(shell), true
	}
	return state.hostByCatalogID(id)
}

func workspaceHostReference(host Host) string {
	if host.ID == localHostID {
		if host.localShell != "" {
			return workspaceLocalShellReference(host.localShell)
		}
		return workspaceLocalTerminalRef
	}
	return host.CatalogID
}

func workspaceHostEligible(host Host) bool {
	if host.ID == localHostID {
		if host.CatalogID == workspaceLocalTerminalRef {
			return true
		}
		_, shell := workspaceLocalShellRef(host.CatalogID)
		return shell
	}
	return host.isSSH() && host.CatalogID != "" && validCatalogID(host.CatalogID)
}
