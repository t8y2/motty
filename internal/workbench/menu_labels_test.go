package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// menuLabelState is the state a test built last: the one whose messages name the
// rows of the page menus a headless session shows.
var menuLabelState *State

// menuRowIDs are the rows of page menus, which the page names by their place and
// the tests by what the application calls the choice.
var menuRowIDs = map[gfx.ID]bool{}

func init() {
	for _, id := range []gfx.ID{
		hitHostMenuConnect, hitHostMenuSFTP, hitHostMenuAddress, hitHostMenuName, hitHostMenuCopy, hitHostMenuPaste, hitHostMenuCopyTo,
		hitHostMenuEdit, hitHostMenuCopyEdit, hitHostMenuSetJump, hitHostMenuMove, hitHostMenuDelete,
		hitGroupNewHost, hitGroupNewChild, hitGroupPaste, hitGroupRename, hitGroupMove, hitGroupPin, hitGroupUp, hitGroupDown, hitGroupDelete,
		hitHostBlankNewHost, hitHostBlankNewGroup, hitHostBlankPaste,
	} {
		menuRowIDs[id] = true
	}
}

// menuRowLabel is the label the page paints for a row the application names id.
func menuRowLabel(id gfx.ID) (string, bool) {
	if !menuRowIDs[id] || menuLabelState == nil {
		return "", false
	}
	return menuLabelOf(menuLabelState, id), true
}

// menuLabelOf names the row of a page menu that used to be a hit of its own: the
// identity the application keeps for the row's action, and the label the page paints.
func menuLabelOf(state *State, id gfx.ID) string {
	m := state.messages
	g := m.HostGroupActions
	switch id {
	case hitHostMenuConnect:
		return m.Connect
	case hitHostMenuSFTP:
		return m.SFTPWindow.Open
	case hitHostMenuAddress:
		return m.CopyAddressAndPort
	case hitHostMenuName:
		return m.CopyConnectionName
	case hitHostMenuCopy:
		return m.CopyHost
	case hitHostMenuPaste:
		return m.PasteHost
	case hitHostMenuCopyTo:
		return m.CopyHostTo
	case hitHostMenuEdit:
		return m.EditHost
	case hitHostMenuCopyEdit:
		return m.CopyHostAndEdit
	case hitHostMenuSetJump:
		return m.HostMenuSetJump
	case hitHostMenuMove:
		return m.MoveHostGroup
	case hitHostMenuDelete:
		return m.DeleteHost
	case hitGroupNewHost:
		return m.NewHost
	case hitGroupNewChild:
		return g.NewChild
	case hitGroupPaste:
		return m.PasteHost
	case hitGroupRename:
		return g.Rename
	case hitGroupMove:
		return g.Move
	case hitGroupPin:
		return g.Pin
	case hitGroupUp:
		return g.MoveUp
	case hitGroupDown:
		return g.MoveDown
	case hitGroupDelete:
		return g.Delete
	case hitHostBlankNewHost:
		return m.NewHost
	case hitHostBlankNewGroup:
		return m.NewGroup
	case hitHostBlankPaste:
		return m.PasteHost
	}
	panic("no label is known for the menu row " + tree.Text(id))
}

// chooseMenu chooses the row of the open page menu that id names.
func chooseMenu(t testing.TB, state *State, id gfx.ID) {
	t.Helper()
	label := menuLabelOf(state, id)
	if id == hitGroupPin && menuHasRow(state, state.messages.HostGroupActions.Unpin) {
		label = state.messages.HostGroupActions.Unpin
	}
	clickPageMenuRow(t, state, label)
}

// menuHasRow reports whether the window shows a page menu row with the label.
func menuHasRow(state *State, label string) bool {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	return canvasHasMenuRow(&canvas, label)
}

// menuHit is the painted row of the open page menu that id names.
func menuHit(canvas *gfx.Canvas, state *State, id gfx.ID) (gfx.Hit, bool) {
	label := menuLabelOf(state, id)
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleMenuItem && hit.Label == label {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}
