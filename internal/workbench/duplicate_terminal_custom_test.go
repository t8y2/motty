package workbench

import (
	"reflect"
	"testing"

	layoutcore "motty/internal/workspace"
)

func TestDuplicateTerminalPreservesAdjustedLayout(t *testing.T) {
	for _, managed := range []bool{false, true} {
		state, _, _ := twoLivePanes(t)
		workspace := state.deck.ActiveWorkspace()
		layout, _ := state.deck.Layout(workspace)
		if err := state.SetPaneRatio(layout.Root(), .7); err != nil {
			t.Fatal(err)
		}
		if managed {
			state.managedWorkspaceLayouts[workspace] = layout.Spec()
			state.temporaryWorkspaceLayouts[workspace] = layout.Spec()
		}
		before := state.Panes(nil)
		source, neighbour := before[0], before[1]
		state.togglePaneMaximized(0)
		state.duplicateTerminal(terminalTestHitByID(state, source.SessionID))
		if !state.deck.PaneMaximized(workspace) {
			t.Fatal("duplicate lost maximization")
		}
		state.togglePaneMaximized(0)
		after := state.Panes(nil)
		if len(after) != 3 || after[2].Bounds != neighbour.Bounds {
			t.Fatalf("unrelated pane moved: before=%+v after=%+v", before, after)
		}
		if after[0].Bounds.Width != source.Bounds.Width || after[1].Bounds.Width != source.Bounds.Width ||
			after[0].Bounds.Height+after[1].Bounds.Height != source.Bounds.Height {
			t.Fatal("duplicate did not split only the source's bounds")
		}
		want := state.savedWorkspaceDeck()
		restored := newTestState(t, state.options)
		if !reflect.DeepEqual(want, restored.savedWorkspaceDeck()) {
			t.Fatal("restart changed the adjusted layout")
		}
		if managed && state.managedWorkspaceLayouts[workspace].Kind != layoutcore.LayoutCustom {
			t.Fatal("custom layout was replaced with a preset")
		}
	}
}

func TestDuplicateTerminalFourColumnManagedLayoutRestores(t *testing.T) {
	state, _, _, _ := liveSession(t)
	workspace := state.deck.ActiveWorkspace()
	state.managedWorkspaceLayouts[workspace] = layoutcore.GridSpec(1)
	state.temporaryWorkspaceLayouts[workspace] = layoutcore.GridSpec(1)
	for count := 2; count <= layoutcore.MaxPanes; count++ {
		state.duplicateTerminal(terminalTestHitByID(state, state.activeSessionID))
		if len(state.deck.Panes(workspace, nil)) != count {
			t.Fatalf("duplicate did not reach %d panes", count)
		}
		if count >= 10 {
			spec := state.managedWorkspaceLayouts[workspace]
			if !spec.Equal(duplicateTerminalGrid(count)) || spec.Validate(count) != nil {
				t.Fatalf("incompatible layout at %d panes: %+v", count, spec)
			}
		}
	}
	want := state.savedWorkspaceLayouts()
	restored := newTestState(t, state.options)
	if !reflect.DeepEqual(want, restored.savedWorkspaceLayouts()) {
		t.Fatal("restart lost the managed four-column layout")
	}
}
