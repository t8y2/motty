package workbench

import "github.com/vivaui/viva/ui"

// declareDesktopBarDockMenu declares the menu under the fullscreen bar's dock
// button: the four edges the bar can move to. The menu hangs off the button, so
// the keyboard returns to it when the menu closes.
func (state *State) declareDesktopBarDockMenu(c ui.C, view *desktopView, anchor ui.Box) {
	if !view.dockMenu {
		return
	}
	m := state.messages.Desktop
	ui.PopupLayer(c, dialogLayer)
	ui.Menu(c, anchor, &view.dockMenu, func(menu *ui.MenuItems) {
		switch state.desktopBar.dock {
		case desktopBarDockBottom:
			menu.Above()
		case desktopBarDockLeft, desktopBarDockRight:
			menu.Beside()
		}
		menu.WidthRange(160, 200)
		for _, choice := range [...]struct {
			dock  desktopBarDock
			label string
		}{
			{desktopBarDockTop, m.FullscreenBarDockTop},
			{desktopBarDockBottom, m.FullscreenBarDockBottom},
			{desktopBarDockLeft, m.FullscreenBarDockLeft},
			{desktopBarDockRight, m.FullscreenBarDockRight},
		} {
			if menu.Item(choice.label).Chosen(state.desktopBar.dock == choice.dock).Clicked() {
				dock := choice.dock
				c.After(func() { state.setDesktopFullscreenBarDock(dock) })
			}
		}
	})
}
