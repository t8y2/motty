package workbench

import (
	"slices"

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
func (state *State) foldMergedHosts(saved savedState, merges []hostMerge) savedState {
	state.merged = mergeAliases(merges)
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
	state.moveMergedSecrets(merges)
	return saved
}

// moveMergedSecrets gives the host that stayed the password and one-time code of
// a host that was folded into it, unless it has its own. It reads the vault only
// when there is something to move, and does nothing while the vault is not open.
func (state *State) moveMergedSecrets(merges []hostMerge) {
	if state.secrets.vault == nil {
		return
	}
	var changes []vault.Change
	for _, merge := range merges {
		kept := state.resolveMerged(merge.dropped.CatalogID)
		for _, part := range []string{hostPasswordPart, hostOTPPart} {
			value, found, err := state.secrets.vault.Get(hostSecretName(merge.dropped.CatalogID, part))
			if err != nil || !found {
				continue
			}
			if _, has, err := state.secrets.vault.Get(hostSecretName(kept, part)); err != nil || has {
				continue
			}
			changes = append(changes, vault.Change{Name: hostSecretName(kept, part), Value: value})
		}
	}
	if len(changes) > 0 {
		_ = state.secrets.vault.Apply(changes)
	}
}

func (catalog *hostCatalog) hostByCatalogID(id string) (Host, bool) {
	for _, host := range catalog.hosts {
		if host.CatalogID == id {
			return host, true
		}
	}
	return Host{}, false
}
