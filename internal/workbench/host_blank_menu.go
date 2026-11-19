package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// hostBlankMenuHits is the place of the rows the empty part of the session
// manager offers.
var (
	hostBlankMenuHits    = tree.Feature("host-blank-menu")
	hitHostBlankDismiss  = tree.ID(hostBlankMenuHits, "dismiss")
	hitHostBlankNewHost  = tree.ID(hostBlankMenuHits, "new-host")
	hitHostBlankNewGroup = tree.ID(hostBlankMenuHits, "new-group")
	hitHostBlankPaste    = tree.ID(hostBlankMenuHits, "paste")
)

// hostBlankMenu is what a secondary press on the empty part of the session
// manager opens: the ways to start a catalog at the top level.
type hostBlankMenu struct {
	open bool
	at   gfx.Point
}

func (state *State) openHostBlankMenu(at gfx.Point) {
	state.hostMenu.open = false
	state.groupActions.open = false
	state.hostGroupMenu, state.hostGroupCopy = false, false
	state.hostBlankMenu = hostBlankMenu{open: true, at: at}
}

func (state *State) declareHostBlankMenu(c ui.C) {
	menu := &state.hostBlankMenu
	if !menu.open {
		return
	}
	if state.modal != modalNone || !state.hostGroupsManaged() || state.hostMenu.open || state.groupActions.open {
		menu.open = false
		return
	}
	messages := state.messages
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitHostBlankNewHost, Label: messages.NewHost, Icon: lucideIcons[iconPlus]})
	rows.add(components.ContextMenuItem{ID: hitHostBlankNewGroup, Label: messages.NewGroup, Icon: lucideIcons[iconFolderOpen]})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitHostBlankPaste, Label: messages.PasteHost, Icon: lucideIcons[iconCopy], Disabled: len(state.hostClipboard) == 0})
	state.declareRowsMenu(c, &menu.open, menu.at, hitHostBlankDismiss, &rows, state.chooseHostBlankMenu)
}

// chooseHostBlankMenu runs a choice of the menu, which closed with it.
func (state *State) chooseHostBlankMenu(id gfx.ID) {
	switch id {
	case hitHostBlankNewHost:
		state.openHostDialog()
	case hitHostBlankNewGroup:
		state.groupActions = hostGroupActions{op: hostGroupCreateRoot}
		state.hostGroupEditor = text.Editor{}
		state.dialogError = ""
		state.modal = modalMoveHostGroup
	case hitHostBlankPaste:
		state.pasteHostsInto("")
	}
}
