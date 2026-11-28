package workbench

import (
	"testing"
)

func TestSnippetPickerDragPersistsManualOrderWithoutSending(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: ""},
		snippet{Name: "Second", Command: "two", Group: ""}, snippet{Name: "Third", Command: "three", Group: ""})
	state.snippets.pickerGrouped = true
	state.snippets.filter = snippetFilter{group: "ai"}
	state.refreshSnippets()
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	source := dualHit(t, view, state.commandPalette.rowID(2))
	first := dualHit(t, view, state.commandPalette.rowID(0))
	x := source.X + source.Width*0.75
	if err := view.Press(x, source.Y+source.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, first.Y+2); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x, first.Y+2); err != nil {
		t.Fatal(err)
	}
	if got := pickerShape(state); !sameShape(got, []string{"Third", "First", "Second"}) {
		t.Fatalf("drag order: %v", got)
	}
	if shell.typed() != "" || state.modal != modalCommandPalette {
		t.Fatal("drag sent a command or closed the picker")
	}
	restored := newTestState(t, options)
	restored.openSnippetPicker()
	if got := pickerShape(restored); !sameShape(got, []string{"Third", "First", "Second"}) {
		t.Fatalf("saved order: %v", got)
	}
}

func TestSnippetPickerTreeDragWithinGroup(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "git"},
		snippet{Name: "Other", Command: "two", Group: "ai"}, snippet{Name: "Last", Command: "three", Group: "git"})
	state.snippets.pickerGrouped = true
	state.openSnippetPicker()
	state.foldSnippetGroup("git", true)
	view := openPickerView(t, state, 1)
	source := dualHit(t, view, state.commandPalette.rowID(3))
	first := dualHit(t, view, state.commandPalette.rowID(2))
	x := source.X + source.Width*0.75
	if err := view.Press(x, source.Y+source.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, first.Y+2); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x, first.Y+2); err != nil {
		t.Fatal(err)
	}
	if got := pickerShape(state); !sameShape(got, []string{"#ai", "#git", "Last", "First"}) {
		t.Fatalf("group search order: %v", got)
	}
	if state.commandPalette.search.String() != "" || state.snippets.list[0].Group != "git" {
		t.Fatal("drag changed search or group")
	}
}
