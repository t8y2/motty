package workbench

import (
	"slices"
	"strings"

	"motty/internal/vault"
)

// mergeAliases is, for each dropped host of merges, the ID of the host that
// stays, following a host that was itself folded into another.
func mergeAliases(merges []hostMerge) map[string]string {
	if len(merges) == 0 {
		return nil
	}
	aliases := make(map[string]string, len(merges))
	for _, merge := range merges {
		aliases[merge.dropped.CatalogID] = merge.kept.CatalogID
	}
	for dropped, kept := range aliases {
		for steps := 0; steps < len(aliases); steps++ {
			next, again := aliases[kept]
			if !again {
				break
			}
			kept = next
		}
		aliases[dropped] = kept
	}
	return aliases
}

// resolveMerged is the ID a reference to the host with id now means.
func (catalog *hostCatalog) resolveMerged(id string) string {
	if kept, ok := catalog.merged[id]; ok {
		return kept
	}
	return id
}

// foldMergedHosts brings everything that referred to a host that mergeSameHosts
// dropped over to the host that stayed: the routes and workspaces of saved, the
// hosts an agent may use, the tabs open in this window and the saved password.
// It returns saved with those references replaced; the slices it changes are
// copies, so what the caller loaded is untouched.
func (state *State) foldMergedHosts(saved savedState, merges []hostMerge) (savedState, error) {
	merges = slices.Clone(merges)
	slices.SortFunc(merges, func(a, b hostMerge) int {
		return strings.Compare(a.dropped.CatalogID, b.dropped.CatalogID)
	})
	state.merged = mergeAliases(merges)
	if err := state.moveMergedSecrets(merges); err != nil {
		state.merged = nil
		return saved, err
	}
	state.mergedHosts = true

	saved.Proxies = slices.Clone(saved.Proxies)
	for index := range saved.Proxies {
		saved.Proxies[index].TargetID = state.resolveMerged(saved.Proxies[index].TargetID)
		saved.Proxies[index].JumpID = state.resolveMerged(saved.Proxies[index].JumpID)
	}

	saved.WorkspaceTemplates = slices.Clone(saved.WorkspaceTemplates)
	for index, template := range saved.WorkspaceTemplates {
		template.HostIDs = slices.Clone(template.HostIDs)
		for position, id := range template.HostIDs {
			template.HostIDs[position] = state.resolveMerged(id)
		}
		// A workspace that held both hosts would now hold one twice, which a
		// workspace refuses; it keeps the ids it had rather than turning invalid.
		if _, err := normalizeWorkspaceTemplate(template); err == nil {
			saved.WorkspaceTemplates[index] = template
		}
	}

	for dropped, kept := range state.merged {
		if state.agentHosts[dropped] {
			delete(state.agentHosts, dropped)
			if state.agentHosts == nil {
				state.agentHosts = make(map[string]bool)
			}
			state.agentHosts[kept] = true
		}
	}

	for _, merge := range merges {
		kept, ok := state.hostByCatalogID(state.resolveMerged(merge.dropped.CatalogID))
		if !ok {
			continue
		}
		for index := range state.sessions {
			if session := &state.sessions[index]; session.HostID == merge.dropped.ID && merge.dropped.ID != 0 {
				session.HostID, session.host = kept.ID, kept
			}
		}
	}
	return saved, nil
}

// moveMergedSecrets carries passwords, VNC system accounts and one-time codes
// to the surviving host without replacing its own secrets. VNC merges fail if
// their accounts cannot be carried over. Other protocols keep merging while
// the vault is unavailable, as they did before.
func (state *State) moveMergedSecrets(merges []hostMerge) error {
	hasVNC := false
	for _, merge := range merges {
		if merge.dropped.Protocol == protocolVNC {
			hasVNC = true
			break
		}
	}
	if state.secrets.vault == nil {
		if hasVNC {
			return vault.ErrUnavailable
		}
		return nil
	}
	var changes []vault.Change
	var names []string
	if hasVNC {
		var err error
		names, err = state.secrets.vault.Names()
		if err != nil {
			return err
		}
	}
	for _, merge := range merges {
		kept := state.resolveMerged(merge.dropped.CatalogID)
		parts := []string{hostPasswordPart, hostOTPPart}
		if merge.dropped.Protocol == protocolVNC {
			prefix := hostSecretName(merge.dropped.CatalogID, "vnc-system-password:")
			for _, name := range names {
				if account, ok := strings.CutPrefix(name, prefix); ok {
					parts = append(parts, vncSystemPasswordPart(account))
				}
			}
		}
		for _, part := range parts {
			value, found, err := state.secrets.vault.Get(hostSecretName(merge.dropped.CatalogID, part))
			if err != nil {
				if merge.dropped.Protocol == protocolVNC {
					return err
				}
				continue
			}
			if !found {
				continue
			}
			changes = append(changes, vault.Change{Name: hostSecretName(kept, part), Value: value})
		}
	}
	if len(changes) > 0 {
		if _, err := state.secrets.vault.ApplyMissingWithCommit(changes, func(int) error { return nil }); err != nil && hasVNC {
			return err
		}
	}
	return nil
}

func (catalog *hostCatalog) hostByCatalogID(id string) (Host, bool) {
	for _, host := range catalog.hosts {
		if host.CatalogID == id {
			return host, true
		}
	}
	return Host{}, false
}
