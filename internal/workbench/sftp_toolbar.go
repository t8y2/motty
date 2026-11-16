package workbench

import "github.com/vivaui/viva/gfx"

func sftpToolbarSpec(id gfx.ID) (toolbarSpec, bool) {
	for _, spec := range toolbarCatalog[toolbarSFTP] {
		if spec.id == id {
			return spec, true
		}
	}
	return toolbarSpec{}, false
}

func sftpToolbarMenuSource(id gfx.ID) bool {
	_, ok := sftpToolbarSpec(id)
	return ok || id == hitSFTPHeaderMore || id == hitSFTPHiddenToggle
}

// Use the same visibility editor as Settings and the other toolbars. Only
// buttons declared in the current panel can open this menu; list actions and
// controls under an overlay keep their own secondary-press behavior.
func (state *State) sftpToolbarPointer(event gfx.PointerEvent) gfx.EventResult {
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary ||
		state.activeSection != sectionTerminal || !state.filesPanelShown() || state.activeSFTPWindow() != nil {
		return gfx.EventResult{}
	}
	s := &state.chrome.sftpPanel
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || s.view != session.files || !state.sftpHeaderMenuAvailable(s.view) || s.view.active().headerMenuOpen || state.sftpMenu.open {
		return gfx.EventResult{}
	}
	// The hit under the press decides. A disabled header button is still that
	// hit for a secondary press (see headerIcon), and a header button covered by
	// an overlay or a block has no target here, so no geometry is consulted.
	for _, button := range s.buttons[:s.headerButtonCount] {
		if button.id != event.Target || !sftpToolbarMenuSource(button.id) {
			continue
		}
		dock := &state.toolbarDock
		dock.drag = toolbarDockDrag{}
		dock.menu, dock.overflow, dock.menuTool, dock.menuAt = true, false, button.id, gfx.Point{X: event.X, Y: event.Y}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}
