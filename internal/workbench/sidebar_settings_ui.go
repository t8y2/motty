package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The Sidebar group of Settings is one row, like the toolbar groups: it names
// the pages the sidebar shows and opens a dialog to choose them. Hiding is the
// entry only, so the page keeps its section, shortcuts and command entry.

var sidebarSettingsHits = tree.Feature("sidebar-settings")

var (
	hitSidebarSettingsOpen    = tree.ID(settingsHits, "sidebar-customize")
	hitSidebarSettingsClose   = tree.ID(sidebarSettingsHits, "close")
	hitSidebarSettingsDismiss = tree.ID(sidebarSettingsHits, "dismiss")
	hitSidebarSettingsShowAll = tree.ID(sidebarSettingsHits, "show-all")
)

type sidebarSettingsUI struct {
	// shown is what each page's switch shows and writes while the dialog is
	// open: the switches are bound to it, and a change is applied to hiddenNav.
	shown [navPageCount]bool
	// summary is the switch count under the settings row; the counts and the
	// format it was built from keep a paint from formatting it again.
	summary       string
	summaryShown  int
	summaryTotal  int
	summaryFormat string
}

// sidebarShown counts the sidebar pages on show and the pages the dialog offers.
func (state *State) sidebarShown() (shown, total int) {
	for _, item := range sidebarPages {
		if item.page == navPageHosts {
			continue
		}
		total++
		if !state.hiddenNav.has(item.page) {
			shown++
		}
	}
	return shown, total
}

// paintSidebarSettingsRow declares the General page's Sidebar row: the count the
// dialog's switches add up to, and the way in.
func (state *State) paintSidebarSettingsRow(page *settingsPage) {
	m, editor := state.messages, &state.sidebarEditor
	shown, total := state.sidebarShown()
	if editor.summary == "" || editor.summaryShown != shown || editor.summaryTotal != total || editor.summaryFormat != m.SettingsSidebarShownFmt {
		editor.summary = fmt.Sprintf(m.SettingsSidebarShownFmt, shown, total)
		editor.summaryShown, editor.summaryTotal, editor.summaryFormat = shown, total, m.SettingsSidebarShownFmt
	}
	page.actionRow(m.SettingsSidebar, editor.summary, false,
		components.ButtonAction{ID: hitSidebarSettingsOpen, Label: m.Toolbars.Customize, Variant: components.ButtonOutline})
}

func (state *State) openSidebarSettings() {
	state.sidebarEditor = sidebarSettingsUI{}
	state.modal = modalSidebarSettings
}

// clickSidebarSettings handles the dialog's buttons.
func (state *State) clickSidebarSettings(id gfx.ID) {
	switch id {
	case hitSidebarSettingsClose, hitSidebarSettingsDismiss:
		state.closeModal()
		return
	case hitSidebarSettingsShowAll:
		state.hiddenNav = 0
	default:
		return
	}
	state.save()
}

// declareSidebarSettings declares the dialog: one switch per page the sidebar
// may leave out, in the sidebar's order.
func (state *State) declareSidebarSettings(c ui.C) {
	editor, m, theme := &state.sidebarEditor, state.messages, c.Theme()
	const (
		rowHeight = float32(48)
		rowGap    = float32(4)
	)
	rows := 0
	for _, item := range sidebarPages {
		if item.page != navPageHosts {
			rows++
		}
	}
	contentHeight := float32(rows)*rowHeight + float32(rows-1)*rowGap
	viewport := dialogViewport(c)
	bodyHeight := min(contentHeight, max(viewport.Height-250, 96))
	height := dialogContentHeight(c.Canvas(), *theme, viewport, 520, m.SettingsSidebarHint, bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SettingsSidebar, Description: m.SettingsSidebarHint, Width: 520, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitSidebarSettingsDismiss, CloseID: hitSidebarSettingsClose,
	})
	dialog.Body(func() {
		ui.Scroll(c).Fill().Scrollbars().Children(func() {
			ui.Column(c).FillWidth().Gap(rowGap).Children(func() {
				for _, item := range sidebarPages {
					if item.page == navPageHosts {
						continue
					}
					label := state.navPageLabel(item.page)
					editor.shown[item.page] = !state.hiddenNav.has(item.page)
					ui.Row(c.KeyInt(int(item.page))).FillWidth().Height(rowHeight).Shrink(0).Items(ui.Center).Gap(10).PadSides(0, 12, 0, 12).
						Background(theme.Background).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).Children(func() {
						ui.Icon(c, lucideIcons[item.icon]).Size(18, 18).Color(theme.Foreground).Shrink(0)
						ui.Text(c, label).FontSize(theme.Metrics.FontSizeM).Color(theme.Foreground).Grow(1).Shrink(1).Ellipsis()
						if toggle := ui.Switch(c, &editor.shown[item.page], "").Label(label); toggle.Changed() {
							page, hidden := item.page, !editor.shown[item.page]
							c.After(func() {
								state.hiddenNav = state.hiddenNav.set(page, hidden)
								state.save()
							})
						}
					})
				}
			})
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitSidebarSettingsShowAll, Label: m.SettingsSidebarShowAll, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSidebarSettingsClose, Label: m.Toolbars.Close, Variant: components.ButtonSecondary},
		)
	})
}
