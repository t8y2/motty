package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The settings page is a tab of the window bar, like a browser's settings tab:
// opening settings adds it at the end of the workspace and file-manager tabs,
// dragging it puts it anywhere among them, and closing it takes it away. While
// it is open the other tabs stay one press away. Its page is the section the
// sidebar's Settings row already opens; the tab is the way the bar shows and
// leaves it.
var (
	hitSettingsTab      = tree.ID(fileTabsHits, "settings-tab")
	hitSettingsTabClose = tree.ID(fileTabsHits, "settings-tab-close")
)

// settingsTabShown says the bar carries the settings tab. With the tabs in the
// sidebar the top bar holds no session tabs, and the sidebar's own Settings row
// is the way in and out, so the tab only exists in the horizontal layout. The
// tab stays open meanwhile and is back when the tabs are moved to the top.
func (state *State) settingsTabShown() bool {
	return state.settingsTab && state.tabOrientation == tabsHorizontal
}

// settingsTabCount is how many tabs the settings page adds to the bar.
func (state *State) settingsTabCount() int {
	if state.settingsTabShown() {
		return 1
	}
	return 0
}

// fileTabCount is how many tabs the bar shows after the workspace tabs: the
// file managers hosted in the window, then the settings page.
func (state *State) fileTabCount() int {
	return len(state.sftpTabs) + state.settingsTabCount()
}

// settingsTabActive says the settings page is the one in front.
func (state *State) settingsTabActive() bool {
	return state.settingsTabShown() && state.activeSection == sectionSettings
}

// settingsTabIndex is the tab's place among the window's tabs, counted the way
// the number shortcuts count them.
func (state *State) settingsTabIndex() int {
	return state.settingsTabPlace()
}

// activateSettingsTab brings an open settings tab to the front. Pressing the
// tab that is already in front leaves the page as it is.
func (state *State) activateSettingsTab() bool {
	if !state.settingsTabShown() {
		return false
	}
	if state.activeSection != sectionSettings {
		state.openSettings()
	}
	return true
}

// closeSettingsTab takes the settings tab off the bar. When it was in front, the
// tab before it takes over, the way a file tab hands over when it closes.
func (state *State) closeSettingsTab() {
	if !state.settingsTab {
		return
	}
	wasActive := state.activeSection == sectionSettings
	state.settingsTab = false
	state.schemeBrowserOpen = false
	if wasActive {
		switch {
		case len(state.sftpTabs) > 0:
			state.activeSFTPTab = state.sftpTabs[len(state.sftpTabs)-1].session.ID
			state.activeSection = sectionSFTP
		case state.deck.ActiveWorkspace() != 0:
			state.activeSection = sectionTerminal
		default:
			state.activeSection = sectionHosts
		}
	}
	state.save()
	if invalidate := state.options.Invalidate; invalidate != nil {
		invalidate()
	}
}

// settingsTabHit reports whether id is the settings tab or its close button.
func settingsTabHit(id gfx.ID) bool {
	return id == hitSettingsTab || id == hitSettingsTabClose
}

// declareSettingsTab declares the settings tab in the window bar's tab row, in
// the look of the file-manager tabs beside it: the same fill, edge, number and
// close button. With probe set only the place is declared, for the bar's
// geometry.
func (state *State) declareSettingsTab(c ui.C, probe bool, geometry barGeometry, first float32) barTab {
	chrome := &state.chrome
	if probe {
		minimum := barTabMin
		if geometry.locked {
			minimum += 18
		}
		if geometry.numbers {
			minimum += tabNumberMinExtra
		}
		tab := ui.Slot(c).Width(barTabMax).MinWidth(minimum).Height(barTabHeight).Shrink(1).MarginSides(0, barGap, 0, 0)
		chrome.probeTabs = append(chrome.probeTabs, tab)
		return barTab{}
	}
	m, pal := state.messages, state.pal
	index := state.settingsTabIndex()
	entry := barTab{settings: true, id: hitSettingsTab, closeID: hitSettingsTabClose}
	if state.tabNumbers {
		entry.number = tabNumber(index + 1)
	}
	active := state.settingsTabActive()
	hovered := c.Canvas().Hovered() == hitSettingsTab || c.Canvas().Hovered() == hitSettingsTabClose
	foreground := pal.muted
	fill, edge := pal.tab, pal.line
	switch {
	case active:
		fill, edge, foreground = pal.tabActive, pal.windowStroke, pal.text
	case hovered:
		fill, foreground = pal.tabHover, pal.text
	}
	entry.base, entry.background, entry.foreground = fill, fill, foreground
	label := m.NavSettings
	entry.label = label
	entry.box = ui.Row(c).Size(geometry.tabWidth, barTabHeight).Shrink(0).MarginSides(0, barGap, 0, first).
		HitOutset(barTabReach, 0, barTabReach, 0).PadSides(0, 3, 0, 4).Items(ui.Center).Radius(radius.item).Border(1, edge).Background(fill).
		HitWith(hitSettingsTab, label, gfx.HitOptions{Role: gfx.RoleTab, Selected: active, Hint: label}).Children(func() {
		entry.badge = ui.Slot(c).Size(20, 20).MarginSides(0, 7, 0, 0)
		if entry.number != "" {
			ui.Text(c, entry.number).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).LineHeight(20).Shrink(0).MarginSides(0, tabNumberGap, 0, 0)
		}
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).LineHeight(20).Ellipsis()
		ui.Spacer(c)
		if !active && !hovered {
			ui.Slot(c).Size(20, 20)
			return
		}
		var closeFill gfx.Color
		if c.Canvas().Hovered() == hitSettingsTabClose {
			closeFill = pal.line
			if active {
				closeFill = pal.tabCloseHot
			}
		}
		ui.Row(c).Size(20, 20).Shrink(0).Center().Radius(radius.item).Background(closeFill).
			HitWith(hitSettingsTabClose, m.LabelCloseTab, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true}).Children(func() {
			ui.Icon(c, lucideIcons[iconClose]).Size(14, 14).Color(foreground)
		})
	})
	return entry
}
