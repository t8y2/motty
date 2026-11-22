package workbench

import (
	"github.com/vivaui/viva/components"

	layoutcore "motty/internal/workspace"
)

// The window bar shows three kinds of tabs in one strip: the workspace tabs,
// the file managers hosted in the window, and the settings page. The settings
// page is the only one that can stand anywhere among them, so it keeps the tab
// it follows rather than a number that would shift when another tab moves.
type windowTabKind uint8

const (
	// windowTabNone is an unset anchor: the settings tab stands last, where it
	// opened.
	windowTabNone windowTabKind = iota
	// windowTabStart is an anchor before the first tab.
	windowTabStart
	windowTabWorkspace
	windowTabFile
	windowTabSettings
)

// windowTab names one tab of the window bar.
type windowTab struct {
	kind windowTabKind
	id   uint32
}

// baseTabCount is how many tabs the bar has before the settings page is put
// where it belongs: the workspace tabs, then the file managers.
func (state *State) baseTabCount() int {
	count := len(state.workspaceTabs)
	for _, tab := range state.sftpTabs {
		if tab != nil && !tab.closed {
			count++
		}
	}
	return count
}

// baseTabAt names the tab at index among the workspace and file tabs, in the
// order the bar shows them.
func (state *State) baseTabAt(index int) (windowTab, bool) {
	if index < 0 {
		return windowTab{}, false
	}
	if index < len(state.workspaceTabs) {
		return windowTab{kind: windowTabWorkspace, id: uint32(state.workspaceTabs[index].ID)}, true
	}
	index -= len(state.workspaceTabs)
	for _, tab := range state.sftpTabs {
		if tab == nil || tab.closed {
			continue
		}
		if index == 0 {
			return windowTab{kind: windowTabFile, id: tab.session.ID}, true
		}
		index--
	}
	return windowTab{}, false
}

// baseTabIndex is where a workspace or file tab stands before the settings
// page is placed, or -1 when the tab is not on the bar.
func (state *State) baseTabIndex(tab windowTab) int {
	switch tab.kind {
	case windowTabWorkspace:
		for index, candidate := range state.workspaceTabs {
			if uint32(candidate.ID) == tab.id {
				return index
			}
		}
	case windowTabFile:
		index := len(state.workspaceTabs)
		for _, candidate := range state.sftpTabs {
			if candidate == nil || candidate.closed {
				continue
			}
			if candidate.session.ID == tab.id {
				return index
			}
			index++
		}
	}
	return -1
}

// settingsTabPlace is the gap among the base tabs the settings tab fills: zero
// puts it first, baseTabCount puts it last. A settings tab whose anchor is gone
// comes back at the end.
func (state *State) settingsTabPlace() int {
	if !state.settingsTabShown() {
		return -1
	}
	switch state.settingsTabAnchor.kind {
	case windowTabStart:
		return 0
	case windowTabNone:
		return state.baseTabCount()
	}
	if index := state.baseTabIndex(state.settingsTabAnchor); index >= 0 {
		return index + 1
	}
	return state.baseTabCount()
}

// windowTabCount is how many tabs the bar shows in the current orientation.
func (state *State) windowTabCount() int {
	count := state.baseTabCount()
	if state.settingsTabShown() {
		count++
	}
	return count
}

// windowTabAt names the tab at index in the order the bar shows them.
func (state *State) windowTabAt(index int) (windowTab, bool) {
	place := state.settingsTabPlace()
	if index < 0 || index >= state.windowTabCount() {
		return windowTab{}, false
	}
	if place >= 0 {
		if index == place {
			return windowTab{kind: windowTabSettings}, true
		}
		if index > place {
			index--
		}
	}
	return state.baseTabAt(index)
}

// windowTabIndex is where tab stands in the bar, or -1 when it is not shown.
func (state *State) windowTabIndex(tab windowTab) int {
	if tab.kind == windowTabSettings {
		return state.settingsTabPlace()
	}
	index := state.baseTabIndex(tab)
	if index < 0 {
		return -1
	}
	if place := state.settingsTabPlace(); place >= 0 && index >= place {
		index++
	}
	return index
}

// activeWindowTab names the tab in front, if the bar reaches it.
func (state *State) activeWindowTab() (windowTab, bool) {
	switch state.activeSection {
	case sectionTerminal:
		if id := state.deck.ActiveWorkspace(); id != 0 {
			return windowTab{kind: windowTabWorkspace, id: uint32(id)}, true
		}
	case sectionSFTP:
		if state.sftpTabIndex(state.activeSFTPTab) >= 0 {
			return windowTab{kind: windowTabFile, id: state.activeSFTPTab}, true
		}
	case sectionSettings:
		if state.settingsTabShown() {
			return windowTab{kind: windowTabSettings}, true
		}
	}
	return windowTab{}, false
}

// activateWindowTab brings a tab of the bar to the front.
func (state *State) activateWindowTab(tab windowTab) bool {
	switch tab.kind {
	case windowTabWorkspace:
		if !state.activateWorkspace(layoutcore.WorkspaceID(tab.id)) {
			return false
		}
		state.save()
		return true
	case windowTabFile:
		return state.activateSFTPTab(tab.id)
	case windowTabSettings:
		return state.activateSettingsTab()
	}
	return false
}

// workspaceInsertIndex is the deck index a drop at slot would take: how many
// workspace tabs stand before the gap, without counting the tab being dragged.
func (state *State) workspaceInsertIndex(slot int, source layoutcore.WorkspaceID) int {
	index := 0
	for at := 0; at < slot; at++ {
		tab, ok := state.windowTabAt(at)
		if !ok {
			break
		}
		if tab.kind == windowTabWorkspace && layoutcore.WorkspaceID(tab.id) != source {
			index++
		}
	}
	return index
}

// moveSettingsTab puts the settings tab at gap slot in the bar.
func (state *State) moveSettingsTab(slot int) bool {
	place := state.settingsTabPlace()
	if place < 0 || slot < 0 || slot > state.windowTabCount() || slot == place || slot == place+1 {
		return false
	}
	at := slot
	if at > place {
		at--
	}
	if at == 0 {
		state.settingsTabAnchor = windowTab{kind: windowTabStart}
		return true
	}
	anchor, ok := state.baseTabAt(at - 1)
	if !ok {
		return false
	}
	state.settingsTabAnchor = anchor
	return true
}

// windowTabOptions lists the bar's tabs in display order, for the arrow keys
// that move between them. The slice is reused between key events.
func (state *State) windowTabOptionsOrdered() []components.TabOption {
	options := state.windowTabOptions[:0]
	for index := 0; index < state.windowTabCount(); index++ {
		tab, ok := state.windowTabAt(index)
		if !ok {
			continue
		}
		switch tab.kind {
		case windowTabWorkspace:
			id := hitTabList.At(int(tab.id))
			for _, option := range state.tabOptions {
				if option.ID == id {
					options = append(options, option)
					break
				}
			}
		case windowTabFile:
			id := sftpFileTabID(tab.id)
			for _, option := range state.fileTabOptions {
				if option.ID == id {
					options = append(options, option)
					break
				}
			}
		case windowTabSettings:
			options = append(options, components.TabOption{ID: hitSettingsTab, Label: state.messages.NavSettings})
		}
	}
	state.windowTabOptions = options
	return options
}
