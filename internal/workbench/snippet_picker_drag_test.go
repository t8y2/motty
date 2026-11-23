package workbench

import (
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
)

func TestSnippetPickerDragPersistsManualOrderWithoutSending(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "git"},
		snippet{Name: "Second", Command: "two", Group: "ai"}, snippet{Name: "Third", Command: "three", Group: "git"})
	state.snippets.filter = snippetFilter{group: "ai"}
	state.snippets.sort = components.TableSortState{Column: 0, Direction: components.TableSortAscending}
	state.refreshSnippets()
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	handle := dualHit(t, view, hitSnippetPickerHandle.At(2))
	first := dualHit(t, view, hitCommandPaletteItems.At(0))
	x := handle.X + handle.Width/2
	if err := view.Press(x, handle.Y+handle.Height/2); err != nil {
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

func TestSnippetPickerDragWithinGroupAndSearch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "git"},
		snippet{Name: "Other", Command: "two", Group: "ai"}, snippet{Name: "Last", Command: "three", Group: "git"})
	state.snippets.pickerGrouped = true
	state.openSnippetPicker()
	state.foldSnippetGroup("git", true)
	state.commandPalette.search = text.Editor{}
	state.commandPalette.search.Insert("git")
	state.refreshCommandPalette()
	view := openPickerView(t, state, 1)
	handle := dualHit(t, view, hitSnippetPickerHandle.At(1))
	first := dualHit(t, view, hitCommandPaletteItems.At(0))
	x := handle.X + handle.Width/2
	if err := view.Press(x, handle.Y+handle.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, first.Y+2); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x, first.Y+2); err != nil {
		t.Fatal(err)
	}
	if got := pickerShape(state); !sameShape(got, []string{"Last", "First"}) {
		t.Fatalf("group search order: %v", got)
	}
	if state.commandPalette.search.String() != "git" || state.snippets.list[0].Group != "git" {
		t.Fatal("drag changed search or group")
	}
}
