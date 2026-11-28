package workbench

import "testing"

func TestSnippetPickerMenuCopiesFullCommandBelowEdit(t *testing.T) {
	world, options := newSharingWorld(t)
	var clipboard string
	writes := 0
	options.SetClipboardText = func(value string) error {
		clipboard = value
		writes++
		return nil
	}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	command := "  printf '%s\\n' '${value}'\nls -la\n"
	addTestSnippets(state, snippet{Name: "First", Command: "one"},
		snippet{Name: "Second", Command: command, Group: "Ops/Linux"})
	state.openSnippetPicker()
	state.commandPalette.search.Insert("Second")
	state.refreshCommandPalette()
	view := openPickerView(t, state, 1)
	row := dualHit(t, view, state.commandPalette.rowID(0))
	if err := view.RightClick(row.X+row.Width/2, row.Y+row.Height/2); err != nil {
		t.Fatal(err)
	}
	edit := dualHitLabel(t, view, state.messages.Tools.SnippetEdit)
	copy := dualHitLabel(t, view, state.messages.Tools.SnippetCopyContent)
	remove := dualHitLabel(t, view, state.messages.Tools.SnippetDelete)
	if copy.Y < edit.Y+edit.Height || remove.Y < copy.Y+copy.Height {
		t.Fatal("copy command content was not placed between Edit and Delete")
	}
	if err := view.ClickLabel(state.messages.Tools.SnippetCopyContent); err != nil {
		t.Fatal(err)
	}
	if clipboard != command || writes != 1 {
		t.Fatalf("clipboard=%q writes=%d", clipboard, writes)
	}
	if state.modal != modalCommandPalette || state.commandPalette.search.String() != "Second" || state.snippetMenu.open {
		t.Fatal("copy did not leave the picker and search intact")
	}
	if world.conns[0].shell(t).typed() != "" || state.snippets.list[1].Command != command {
		t.Fatal("copy sent or modified the saved command")
	}
}
