package workbench

import (
	layoutcore "motty/internal/workspace"
	"reflect"
	"testing"
)

func TestDuplicateTerminalNamesAndLayoutPersist(t *testing.T) {
	state, _, _, _ := liveSession(t)
	workspace := state.deck.ActiveWorkspace()
	state.applyTabRename(workspace, "my-shell")
	source, _ := state.sessionByIDPointer(state.activeSessionID)
	state.duplicateTerminal(terminalTestHitByID(state, source.ID))
	state.togglePaneMaximized(0)
	before := state.savedWorkspaceDeck()
	restored := newTestState(t, state.options)
	if !reflect.DeepEqual(before, restored.savedWorkspaceDeck()) {
		t.Fatal("layout, order or maximized state changed on restore")
	}
	for _, session := range restored.sessions {
		if session.Title != "my-shell" || !session.customTitle {
			t.Fatalf("custom title not restored: %q custom=%v", session.Title, session.customTitle)
		}
	}
}

func TestDuplicateTerminalGridColumnPolicy(t *testing.T) {
	for count, columns := range []uint8{1, 2, 2, 2, 3, 3, 3, 3, 3, 4, 4, 4} {
		spec := duplicateTerminalGrid(count + 1)
		if err := spec.Validate(count + 1); err != nil {
			t.Fatalf("count=%d: %v", count+1, err)
		}
		if columns == 4 {
			if spec.Kind != layoutcore.LayoutCustom || spec.Columns != 0 {
				t.Fatal("four columns must use a compatible custom tree")
			}
			continue
		}
		if spec.Kind != layoutcore.LayoutGrid || spec.Columns != columns {
			t.Fatalf("count=%d columns=%d want %d", count+1, spec.Columns, columns)
		}
	}
}

func TestDuplicateTerminalFiveTilesWithoutEmptyPane(t *testing.T) {
	state, _, _, _ := liveSession(t)
	for len(state.sessions) < 5 {
		source, _ := state.sessionByIDPointer(state.activeSessionID)
		state.duplicateTerminal(terminalTestHitByID(state, source.ID))
	}
	panes := state.Panes(nil)
	if len(panes) != 5 {
		t.Fatalf("panes=%d", len(panes))
	}
	for index, pane := range panes {
		if pane.SessionID == 0 || pane.Bounds.Height != .5 {
			t.Fatalf("pane: %+v", pane)
		}
		if index >= 3 && pane.Bounds.Width != .5 {
			t.Fatalf("last row: %+v", pane.Bounds)
		}
	}
}

func TestLegacySessionTitleClassification(t *testing.T) {
	for _, item := range []struct {
		title  string
		custom bool
	}{
		{"server", false}, {"server-2", false}, {"server-12", false},
		{"my-shell", true}, {"server-logs", true},
	} {
		if got := legacyCustomSessionTitle(item.title, "server"); got != item.custom {
			t.Fatalf("%q: custom=%v want %v", item.title, got, item.custom)
		}
	}
}

func TestExplicitSessionTitleSourceSurvivesRestore(t *testing.T) {
	if !restoredCustomSessionTitle(savedCustomSessionTitle(true), "server", "server") {
		t.Fatal("explicit custom name was mistaken for a default name")
	}
	if restoredCustomSessionTitle(savedCustomSessionTitle(false), "old-server", "server") {
		t.Fatal("explicit automatic name was mistaken for a custom name")
	}
}
