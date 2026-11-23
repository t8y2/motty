package workbench

import (
	"github.com/vivaui/viva/gfx"
	"math"
	layoutcore "motty/internal/workspace"
	"testing"
)

func TestDuplicateTerminalMaximizedTwelveAndRestore(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	workspace := state.deck.ActiveWorkspace()
	state.Resize(gfx.Size{Width: 1000, Height: 700})
	state.togglePaneMaximized(0)
	for count := 3; count <= 12; count++ {
		panes := state.deck.Panes(workspace, nil)
		source := panes[0]
		state.activatePane(workspace, source.ID)
		result := state.duplicateTerminal(terminalTestHitByID(state, uint32(source.SessionID)))
		after := state.deck.Panes(workspace, nil)
		if !result.Handled || len(after) != count || !state.deck.PaneMaximized(workspace) {
			t.Fatalf("copy %d: panes=%d maximized=%v", count, len(after), state.deck.PaneMaximized(workspace))
		}
		if after[0].ID != source.ID || after[1].ID != state.deck.ActivePane(workspace) || after[2].ID != panes[1].ID {
			t.Fatal("copy was not inserted immediately after source")
		}
	}
	saved := state.savedWorkspaceDeck()
	restored, err := layoutcore.RestoreDeck(saved)
	if err != nil || !restored.PaneMaximized(workspace) || len(restored.Panes(workspace, nil)) != 12 {
		t.Fatalf("restore deck: %v", err)
	}
	state.togglePaneMaximized(0)
	panes := state.Panes(nil)
	for index, pane := range panes {
		want := FractionBounds{X: float32(index%4) / 4, Y: float32(index/4) / 3, Width: .25, Height: 1.0 / 3}
		if math.Abs(float64(pane.Bounds.X-want.X)) > .0001 || math.Abs(float64(pane.Bounds.Y-want.Y)) > .0001 || math.Abs(float64(pane.Bounds.Width-want.Width)) > .0001 || math.Abs(float64(pane.Bounds.Height-want.Height)) > .0001 {
			t.Fatalf("pane %d: got %+v want %+v", index, pane.Bounds, want)
		}
	}
}

func TestDuplicateTerminalCustomTitleAndDefaultTitle(t *testing.T) {
	for _, custom := range []bool{false, true} {
		state, _, _, _ := liveSession(t)
		source, _ := state.sessionByIDPointer(state.activeSessionID)
		if custom {
			state.tabRename.session = source.ID
			state.tabRename.editor.Insert("my-terminal")
			state.clickTabRenameDialog(hitTabRenameSubmit)
		}
		title, mention := source.Title, source.Mention
		want := cleanSessionTitle(source.host.Label)
		if custom {
			want = title
		}
		state.duplicateTerminal(terminalTestHitByID(state, source.ID))
		copied, _ := state.sessionByIDPointer(state.activeSessionID)
		if copied.Title != want || copied.customTitle != custom || copied.Mention == mention {
			t.Fatalf("custom=%v source=%q copy=%q", custom, title, copied.Title)
		}
	}
}
