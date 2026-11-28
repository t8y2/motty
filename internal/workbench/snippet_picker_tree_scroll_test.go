package workbench

import (
	"fmt"
	"testing"
	"time"
)

func TestSnippetPickerTreeKeepsScrollingWhileDragRestsAtEdge(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	for i := 0; i < 40; i++ {
		state.snippets.list = append(state.snippets.list, snippet{Name: fmt.Sprintf("Command %02d", i), Command: "echo test"})
	}
	state.refreshSnippets()
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	source := dualHit(t, view, state.commandPalette.rowID(0))
	x := source.X + source.Width/2
	if err := view.Press(x, source.Y+source.Height/2); err != nil {
		t.Fatal(err)
	}
	viewport := state.commandPalette.listRect
	y := viewport.Y + viewport.Height - 3
	if err := view.Move(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Advance(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	first := state.commandPalette.snippetTree.state.Scroll.Offset
	if err := view.Advance(400 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	second := state.commandPalette.snippetTree.state.Scroll.Offset
	if first <= 0 || second <= first {
		t.Fatalf("stationary edge scroll: first=%v second=%v", first, second)
	}
	if err := view.Release(x, y); err != nil {
		t.Fatal(err)
	}
	if state.snippets.list[0].Name == "Command 00" {
		t.Fatal("edge drag did not reorder the command")
	}
	if world.conns[0].shell(t).typed() != "" {
		t.Fatal("drag sent a command")
	}
}

func TestSnippetPickerTreeFoldKeepsScrollPosition(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for i := 0; i < 30; i++ {
		state.snippets.list = append(state.snippets.list, snippet{Name: fmt.Sprintf("Command %02d", i), Command: "echo test", Group: fmt.Sprintf("Group %02d", i)})
	}
	state.refreshSnippets()
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	state.commandPalette.scrollState.ScrollTo(200)
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	before := state.commandPalette.scroll
	state.foldSnippetGroup("group 08", true)
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if got := state.commandPalette.scroll; got != before || got == 0 {
		t.Fatalf("expand scroll: got %v, want %v", got, before)
	}
	state.foldSnippetGroup("group 08", false)
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if got := state.commandPalette.scroll; got != before {
		t.Fatalf("collapse scroll: got %v, want %v", got, before)
	}
}
