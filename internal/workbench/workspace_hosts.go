package workbench

// A space keeps this reference outside the catalog ID namespace. It means the
// default shell on whichever computer opens the template, never a saved host.
const workspaceLocalTerminalRef = "local terminal"

func (state *State) workspaceLocalHost() Host {
	host := localHost(state.messages.LocalTerminalTitle)
	host.CatalogID = workspaceLocalTerminalRef
	host.Icon = hostIconTerminal
	return host
}

func (state *State) workspaceHosts() []Host {
	hosts := make([]Host, 0, len(state.hosts)+1)
	hosts = append(hosts, state.workspaceLocalHost())
	return append(hosts, state.hosts...)
}

func (state *State) workspaceHostByID(id string) (Host, bool) {
	if id == workspaceLocalTerminalRef {
		return state.workspaceLocalHost(), true
	}
	return state.hostByCatalogID(id)
}

func workspaceHostReference(host Host) string {
	if host.ID == localHostID {
		return workspaceLocalTerminalRef
	}
	return host.CatalogID
}

func workspaceHostEligible(host Host) bool {
	if host.ID == localHostID {
		return host.CatalogID == workspaceLocalTerminalRef
	}
	return host.isSSH() && host.CatalogID != "" && validCatalogID(host.CatalogID)
}
