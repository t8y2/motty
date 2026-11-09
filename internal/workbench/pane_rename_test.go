package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// Double clicking a split pane's bar asks for that pane's name, whichever pane
// had the keyboard; a single click only activates the pane.
func TestDoubleClickingAPaneBarRenamesThatPane(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	first, second := &state.sessions[0], &state.sessions[1]

	clickPaneGrid(t, state, driver, first)
	clickPaneBar(t, state, driver, size, second)
	if state.modal != modalNone {
		t.Fatalf("a single click opened modal %d", state.modal)
	}

	bounds := paneBoundsOf(t, state, size, second)
	if err := driver.DoubleClick(bounds.X+bounds.Width/2, bounds.Y+infoBarHeight/2); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalRenameTab || state.tabRename.session != second.ID {
		t.Fatalf("modal %d for session %d, want the rename dialog for session %d", state.modal, state.tabRename.session, second.ID)
	}
	if got := state.tabRename.editor.String(); got != second.Title {
		t.Fatalf("the dialog starts from %q, want the pane's name %q", got, second.Title)
	}
}

// The command palette names the terminal that has the keyboard, for anyone who
// finds neither the context menu nor the bar.
func TestPaletteRenamesTheActiveTerminal(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	active, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no active session")
	}
	state.Click(hitPaneRename)
	if state.modal != modalRenameTab || state.tabRename.session != active.ID {
		t.Fatalf("modal %d for session %d, want the rename dialog for session %d", state.modal, state.tabRename.session, active.ID)
	}
}
