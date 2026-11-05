package workbench

import (
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// connectPickerHits is the place of the controls this file paints.
var connectPickerHits = tree.Feature("connect-picker")

// The new-tab picker is the command palette's list showing where a tab can
// connect: this computer, then every saved host under the group it is filed in.
// Choosing one creates the tab; closing it creates nothing.
var (
	hitConnectPickLocal = tree.ID(connectPickerHits, "connect-pick-local")
	hitConnectPickList  = tree.Sub(connectPickerHits, tree.Name("connect-pick")) // + the host's ID
	// hitConnectPickShells is a local terminal running one particular shell, by
	// its place among the shells this computer lists.
	hitConnectPickShells = tree.Sub(connectPickerHits, tree.Name("connect-pick-shell"))
)

func (state *State) openConnectPicker() {
	if localShellSettingVisible() {
		state.refreshLocalShells()
	}
	state.commandPalette.connect, state.commandPalette.snippets = true, false
	state.modal = modalCommandPalette
	state.commandPalette.search = text.Editor{}
	state.commandPalette.filtered = false
	state.rebuildCommandPalette()
}

func (state *State) rebuildConnectPicker() {
	palette := &state.commandPalette
	if state.recentConnectSort {
		palette.filtered = false
		state.refreshCommandPalette()
		return
	}
	var previousGroup *hostGroup
	state.eachConnectPickerCandidate(func(item connectPickerCandidate) {
		if item.group != nil && item.group != previousGroup {
			palette.addHeader(state.hostGroupLabel(*item.group), item.group.Count)
		}
		previousGroup = item.group
		palette.addDetailed(item.label, item.detail, item.target)
	})
	palette.rawQuery = ""
	palette.query = ""
	palette.filtered = false
	state.refreshCommandPalette()
}

// connectPickerCandidate is a selectable row in default order; group is nil
// for local terminals and for hosts when the catalog has no named groups.
type connectPickerCandidate struct {
	label, detail string
	target        ui.ID
	group         *hostGroup
	key           string
}

// eachConnectPickerCandidate keeps both modes' labels, targets and default
// ordering together. Each mode decides how to filter, sort and head its rows.
func (state *State) eachConnectPickerCandidate(add func(connectPickerCandidate)) {
	local := localHost(state.messages.LocalTerminalTitle)
	add(connectPickerCandidate{label: state.messages.LocalTerminal, detail: local.Address, target: hitConnectPickLocal, key: "local-terminal"})
	if choices := state.localShellMenu.choices; localShellSettingVisible() && len(choices) > 1 {
		for index, choice := range choices {
			add(connectPickerCandidate{label: state.messages.LocalTerminal + " · " + choice.Name, detail: local.Address, target: hitConnectPickShells.At(index), key: "local-terminal"})
		}
	}
	addHost := func(host Host, group *hostGroup) {
		add(connectPickerCandidate{label: host.Label, detail: state.sessionAddress(host), target: hitConnectPickList.At(int(host.ID)), group: group, key: host.CatalogID})
	}
	if state.namedHostGroups() == 0 {
		for _, host := range state.hosts {
			addHost(host, nil)
		}
		return
	}
	for index := range state.hostGroups {
		group := &state.hostGroups[index]
		for _, host := range state.hosts {
			if strings.TrimSpace(host.Group) == group.Name {
				addHost(host, group)
			}
		}
	}
}

// shellsListed rebuilds an open picker when the WSL distributions have been
// named, keeping what was typed and where the selection was.
func (state *State) shellsListed() {
	if state.modal != modalCommandPalette || !state.commandPalette.connect {
		return
	}
	palette := &state.commandPalette
	selected, top := palette.selected, palette.top
	state.rebuildCommandPalette()
	if len(palette.visible) > 0 {
		palette.selected = min(selected, len(palette.visible)-1)
		palette.top = min(top, palette.selected)
	}
}

// clickConnectPick connects the picker's choice in a tab of its own.
func (state *State) clickConnectPick(id ui.ID) bool {
	switch {
	case id == hitConnectPickLocal:
		state.pendingWorkspace, state.pendingPane = 0, 0
		state.openLocalTerminal()
	case id.Scope() == hitConnectPickShells:
		choices := state.localShellMenu.choices
		if index := item(id); index < len(choices) {
			state.pendingWorkspace, state.pendingPane = 0, 0
			state.openLocalTerminalWith(choices[index])
		}
	case id.Scope() == hitConnectPickList:
		host, ok := state.hostByID(uint32(item(id)))
		if !ok {
			return true
		}
		state.pendingWorkspace, state.pendingPane = 0, 0
		state.newHostSession(host)
	default:
		return false
	}
	return true
}
