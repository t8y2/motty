package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// press delivers a primary press on a row as the runtime does, then the click on
// release when the press was not handled.
func pressSFTPRow(state *State, view *sftpView, index int, modifiers gfx.Modifiers) gfx.EventResult {
	target := contextID(hitSFTPEntryList.At(index - view.first))
	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, Modifiers: modifiers, Target: target})
	if !result.Handled {
		state.Click(target)
	}
	return result
}

// openThreeRowSFTP opens the test panel and gives its list a third row, which the
// shared fixture (two entries, relied on by other tests) does not have.
func openThreeRowSFTP(t testing.TB) (*State, *sftpView) {
	t.Helper()
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	extra := view.rows[1]
	view.rows = append(view.rows, extra)
	return state, view
}

func selectedRows(view *sftpView) []int {
	var rows []int
	for index, row := range view.rows {
		if row.selected {
			rows = append(rows, index)
		}
	}
	return rows
}

func TestClickingRowsOneByOneSelectsOnlyTheLastOne(t *testing.T) {
	state, view := openThreeRowSFTP(t)
	pressSFTPRow(state, view, 0, 0)
	pressSFTPRow(state, view, 1, 0)
	pressSFTPRow(state, view, 2, 0)
	if got := selectedRows(view); len(got) != 1 || got[0] != 2 {
		t.Fatalf("selected rows = %v, want only the last one clicked", got)
	}
	if view.rows[2].cells[0] != sftpSelectionCell(true) || view.rows[0].cells[0] != "" {
		t.Fatalf("selection cells = %q %q", view.rows[0].cells[0], view.rows[2].cells[0])
	}
}

func TestCommandOrControlClickTogglesAndShiftClickSelectsARange(t *testing.T) {
	state, view := openThreeRowSFTP(t)
	pressSFTPRow(state, view, 0, 0)
	pressSFTPRow(state, view, 2, gfx.ModifierMeta)
	if got := selectedRows(view); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("after Command-click: %v", got)
	}
	pressSFTPRow(state, view, 0, gfx.ModifierControl)
	if got := selectedRows(view); len(got) != 1 || got[0] != 2 {
		t.Fatalf("Control-click on a selected row must unselect it: %v", got)
	}
	pressSFTPRow(state, view, 0, 0)
	pressSFTPRow(state, view, 2, gfx.ModifierShift)
	if got := selectedRows(view); len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("after Shift-click from row 0 to 2: %v", got)
	}
	pressSFTPRow(state, view, 1, 0)
	if got := selectedRows(view); len(got) != 1 || got[0] != 1 {
		t.Fatalf("a plain click after a range: %v", got)
	}
}

func TestRowPressIsHandledSoNoClickTogglesItAgain(t *testing.T) {
	state, view := openThreeRowSFTP(t)
	if result := pressSFTPRow(state, view, 0, 0); !result.Handled || !result.Repaint {
		t.Fatalf("press = %+v", result)
	}
	if got := selectedRows(view); len(got) != 1 {
		t.Fatalf("selected = %v", got)
	}
}

func rightClickSFTPRow(state *State, view *sftpView, index int) gfx.EventResult {
	target := contextID(hitSFTPEntryList.At(index - view.first))
	return state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, ClickCount: 1, Target: target})
}

// Right-clicking a row of the file list opens a menu whose rows do what the
// buttons under the list do, for the row that was pointed at.
func TestRightClickingAFileRowOpensItsMenu(t *testing.T) {
	state, view := openThreeRowSFTP(t)
	pressSFTPRow(state, view, 0, 0)
	if result := rightClickSFTPRow(state, view, 1); !result.Handled || !state.sftpMenu.open {
		t.Fatalf("right click: %+v, menu open %v", result, state.sftpMenu.open)
	}
	if got := selectedRows(view); len(got) != 1 || got[0] != 1 {
		t.Fatalf("right-clicking an unselected row must select it alone: %v", got)
	}
	canvas, _ := paintedTexts(state)
	for _, id := range []gfx.ID{hitSFTPMenuOpen, hitSFTPMenuDownload, hitSFTPMenuRename, hitSFTPMenuDelete} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("the menu has no row %s", tree.Text(id))
		}
	}
	state.Click(hitSFTPMenuRename)
	if state.sftpMenu.open || view.dialog != sftpDialogRename {
		t.Fatalf("after choosing Rename: menu open %v, dialog %v", state.sftpMenu.open, view.dialog)
	}
}

func TestRightClickingASelectedRowKeepsTheSelection(t *testing.T) {
	state, view := openThreeRowSFTP(t)
	pressSFTPRow(state, view, 0, 0)
	pressSFTPRow(state, view, 2, gfx.ModifierMeta)
	rightClickSFTPRow(state, view, 2)
	if got := selectedRows(view); len(got) != 2 {
		t.Fatalf("the menu of a selected row acts on the whole selection: %v", got)
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitSFTPMenuRename); ok {
		t.Fatal("Rename is offered for several rows at once")
	}
	if _, ok := hitByID(canvas, hitSFTPMenuDownload); !ok {
		t.Fatal("Download is not offered for the selection")
	}
}
