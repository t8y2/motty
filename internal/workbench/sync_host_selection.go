package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
)

var (
	hitSyncSelectedHosts = tree.ID(backupUiHits, "sync-selected-hosts")
	hitSyncHostItems     = tree.Sub(backupUiHits, tree.Name("sync-host"))
	hitSyncGroupItems    = tree.Sub(backupUiHits, tree.Name("sync-host-group"))
)

// setSyncHostSelection writes the choice before scheduling an exchange. Failed
// saves restore the old choice, so an unsaved toggle cannot change what uploads.
func (state *State) setSyncHostSelection(selection syncHostSelection) {
	previous := state.syncScope.hosts
	state.syncScope.hosts = selection
	if err := state.saveWithResult(); err != nil {
		state.syncScope.hosts = previous
		return
	}
	state.sync.err, state.sync.retries, state.sync.runText = "", 0, ""
	state.scheduleSync()
}

func setSyncSelectionItem(items []string, value string, selected bool) []string {
	index := slices.Index(items, value)
	if selected && index < 0 && value != "" {
		items = append(items, value)
	} else if !selected && index >= 0 {
		items = slices.Delete(items, index, index+1)
	}
	return items
}

func (state *State) syncGroupSelected(path string) bool {
	selection := state.syncScope.hosts
	if !slices.Contains(selection.Groups, path) {
		return false
	}
	for _, host := range state.hosts {
		if hostGroupWithin(host.Group, path) && !selection.hasHost(host.CatalogID) {
			return false
		}
	}
	return true
}

// toggleSyncHostGroup snapshots current members, including subgroups. New
// hosts need an explicit selection, even if a group was previously checked.
func (state *State) toggleSyncHostGroup(path string) {
	selected := !state.syncGroupSelected(path)
	selection := state.syncScope.hosts.clone()
	for _, host := range state.hosts {
		if hostGroupWithin(host.Group, path) {
			selection.IDs = setSyncSelectionItem(selection.IDs, host.CatalogID, selected)
		}
	}
	selection.Groups = setSyncSelectionItem(selection.Groups, path, selected)
	for _, group := range state.hostGroups {
		if hostGroupWithin(group.Name, path) {
			selection.Groups = setSyncSelectionItem(selection.Groups, group.Name, selected)
		}
	}
	state.setSyncHostSelection(selection)
}

func (state *State) clickSyncHostSelection(id gfx.ID) bool {
	if !state.syncScope.allows(syncCategoryHosts) {
		return false
	}
	selection := state.syncScope.hosts.clone()
	if id == hitSyncSelectedHosts {
		selection.OnlySelected = !selection.OnlySelected
		state.setSyncHostSelection(selection)
		return true
	}
	if !selection.OnlySelected {
		return false
	}
	if index, ok := id.In(hitSyncHostItems); ok && index >= 0 && index < len(state.hosts) {
		host := state.hosts[index]
		selection.IDs = setSyncSelectionItem(selection.IDs, host.CatalogID, !selection.hasHost(host.CatalogID))
		state.setSyncHostSelection(selection)
		return true
	}
	if index, ok := id.In(hitSyncGroupItems); ok && index >= 0 && index < len(state.hostGroups) {
		state.toggleSyncHostGroup(state.hostGroups[index].Name)
		return true
	}
	return false
}

func (state *State) paintSyncHostSelection(page *settingsPage) {
	if !state.syncScope.allows(syncCategoryHosts) {
		return
	}
	m := state.messages.Tools
	selection := state.syncScope.hosts
	page.group(m.SyncHostSelection, func() {
		page.toggleRow(m.SyncOnlySelectedHosts, m.SyncSelectedHostsDescription, selection.OnlySelected, hitSyncSelectedHosts)
		if !selection.OnlySelected {
			return
		}
		page.hintRow(m.SyncHostSelectionNotice)
		selected := 0
		for _, host := range state.hosts {
			if selection.hasHost(host.CatalogID) {
				selected++
			}
		}
		page.hintRow(fmt.Sprintf(m.SyncHostSelectionCount, selected, len(state.hosts)))
		for index, group := range state.hostGroups {
			if group.Name != "" {
				page.toggleRow(group.Name, m.SyncHostGroupDescription, state.syncGroupSelected(group.Name), hitSyncGroupItems.At(index))
			}
		}
		for index, host := range state.hosts {
			page.toggleRow(host.Label, host.Group, selection.hasHost(host.CatalogID), hitSyncHostItems.At(index))
		}
	})
}

// A group move keeps selected empty groups selected too. The old identities
// stay selected until their tombstones have reached the other computers.
func (state *State) remapSyncHostGroups(path, target string) {
	selection := state.syncScope.hosts.clone()
	for _, group := range state.syncScope.hosts.Groups {
		if hostGroupWithin(group, path) {
			selection.Groups = setSyncSelectionItem(selection.Groups, target+strings.TrimPrefix(group, path), true)
		}
	}
	state.syncScope.hosts = selection
}
