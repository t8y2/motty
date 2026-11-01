package workbench

import (
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

// The settings page is a list of categories on the left and one category's
// groups on the right, so a long page is never one long scroll.

type settingsCategory uint8

const (
	settingsGeneral settingsCategory = iota
	settingsTerminal
	settingsConnection
	settingsFiles
	settingsSecurity
	settingsBackup
	settingsAssistant
	settingsMCP
	settingsShortcuts
	settingsAbout
	settingsCategoryCount
)

// settingsNavHits is the place of the controls this file paints.
var settingsNavHits = tree.Feature("settings-nav")

// hitSettingsCategoryList starts the ids of the category entries.
var hitSettingsCategoryList = tree.Sub(settingsNavHits, tree.Name("settings-category"))

const settingsNavWidthMax = float32(208)

// The category rows are a fixed height, so what it takes to reveal one is known
// without laying the list out.
const (
	settingsNavEntryHeight = float32(36)
	settingsNavEntryGap    = float32(4)
)

func (category settingsCategory) icon() uint8 {
	switch category {
	case settingsTerminal:
		return iconTerminal
	case settingsConnection:
		return iconLink
	case settingsFiles:
		return iconFolderOpen
	case settingsSecurity:
		return iconLock
	case settingsBackup:
		return iconArchive
	case settingsAssistant:
		return iconBot
	case settingsMCP:
		return iconPlug
	case settingsShortcuts:
		return iconKeyboard
	case settingsAbout:
		return iconInfo
	}
	return iconSettings
}

// settingsCategoryName is a category's name, in the list and as the page's title.
func (state *State) settingsCategoryName(category settingsCategory) string {
	m := state.messages.Tools
	switch category {
	case settingsTerminal:
		return m.CatTerminal
	case settingsConnection:
		return m.CatConnection
	case settingsFiles:
		return m.CatFiles
	case settingsSecurity:
		return m.CatSecurity
	case settingsBackup:
		return m.CatBackup
	case settingsAssistant:
		return state.messages.AISettings.Title
	case settingsMCP:
		return m.CatMCP
	case settingsShortcuts:
		return state.messages.SettingsShortcuts
	case settingsAbout:
		return state.messages.About.Title
	}
	return m.CatGeneral
}

func (state *State) selectSettingsCategory(category settingsCategory) bool {
	if category >= settingsCategoryCount {
		return false
	}
	if localShellSettingVisible() && category == settingsTerminal && !state.localShellMenu.loaded {
		state.refreshLocalShells()
	}
	if category == settingsFiles {
		state.refreshEditors()
	}
	if state.settingsCategory == category {
		return false
	}
	state.settingsCategory = category
	state.settingsScroll = 0
	state.settings.aboutCopied = false
	state.revealSettingsCategory()
	return true
}

// nav declares the category list: the page's title, the search box and one entry
// per category, in a column of the given width. The entries live in a scroll of
// their own: with enough categories the list is taller than the window, and it
// must stay usable however short the window is.
func (page *settingsPage) nav(width float32, searching bool) {
	state, f := page.state, page.f
	view := &state.settings
	pal, m := state.pal, state.messages
	f.Column().Width(width).FillHeight().Children(func() {
		f.Text(m.SettingsTitle).FontSize(19).Bold().Color(pal.text).LineHeight(28).MarginSides(26, 8, 10, 22).Shrink(0)
		page.add(settingsControl{kind: controlSearch, slot: f.Slot().Height(38).MarginSides(0, 12, 12, 12).Shrink(0)})
		page.navList = f.Scroll(view.navScroll).FillWidth().Grow(1).Basis(0)
		page.navList.Children(func() {
			f.Column().Gap(settingsNavEntryGap).MarginSides(0, 0, 8, 0).Shrink(0).Children(func() {
				for category := settingsCategory(0); category < settingsCategoryCount; category++ {
					page.navEntry(hitSettingsCategoryList.At(int(category)), category.icon(), state.settingsCategoryName(category), !searching && category == state.settingsCategory)
				}
			})
		})
	})
}

// revealSettingsCategory moves the category list so the chosen category is in
// view, using the room the list had when it was last painted.
func (state *State) revealSettingsCategory() {
	view := &state.settings
	if view.navView <= 0 {
		return
	}
	top := float32(state.settingsCategory) * (settingsNavEntryHeight + settingsNavEntryGap)
	bottom := top + settingsNavEntryHeight
	switch {
	case top < view.navScroll:
		view.navScroll = top
	case bottom > view.navScroll+view.navView:
		view.navScroll = bottom - view.navView
	}
	view.navScroll = max(view.navScroll, 0)
}

// navEntry declares one entry of the list: it fills while it is the one shown or
// under the pointer, and its icon and name are brighter then. The entry is a
// pointer target recorded with the page's controls, so the keyboard visits it
// after the search box above it.
func (page *settingsPage) navEntry(id ui.ID, icon uint8, label string, active bool) {
	f, pal := page.f, page.state.pal
	hovered := f.Canvas().Hovered() == id
	foreground, background := pal.muted, ui.Color{}
	switch {
	case active:
		foreground, background = pal.text, pal.surfaceHot
	case hovered:
		foreground, background = pal.text, pal.surfaceSoft
	}
	entry := f.Row().Height(settingsNavEntryHeight).Shrink(0).MarginX(12).PadSides(0, 8, 0, 12).Gap(12).Items(flex.Center).Radius(radius.control).Background(background)
	page.hitOn(entry, id, label, ui.HitOptions{Role: ui.RoleMenuItem, Selected: active})
	entry.Children(func() {
		f.Icon(lucideIcons[icon]).IconTransform(page.state.iconMotion(f.Canvas(), id, icon)).Color(foreground)
		f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(foreground).LineHeight(22).Grow(1)
	})
}
