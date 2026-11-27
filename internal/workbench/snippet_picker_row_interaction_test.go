package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSnippetPickerWholeRowClickStillSendsCommand(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	addTestSnippets(state, snippet{Name: "First", Command: "echo first"})
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	row := dualHit(t, view, hitCommandPaletteItems.At(0))
	if err := view.Click(row.X+row.Width*0.8, row.Y+row.Height/2); err != nil {
		t.Fatal(err)
	}
	if shell.typed() != "echo first" || state.modal != modalNone {
		t.Fatalf("click: sent=%q modal=%v", shell.typed(), state.modal)
	}
}

func TestSnippetPickerWholeGroupRowClickStillExpands(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops"})
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	for _, want := range [][]string{{"#Ops", "First"}, {"#Ops"}} {
		row := dualHit(t, view, hitCommandPaletteItems.At(0))
		if err := view.Click(row.X+row.Width*0.8, row.Y+row.Height/2); err != nil {
			t.Fatal(err)
		}
		if !sameShape(pickerShape(state), want) {
			t.Fatalf("clicked group: %v", pickerShape(state))
		}
	}
}

func TestSnippetPickerShortcutAlwaysShowsGroupingAcrossPages(t *testing.T) {
	for _, page := range []struct {
		name    string
		section uint8
	}{
		{"terminal", sectionTerminal}, {"settings", sectionSettings}, {"snippets", sectionSnippets},
	} {
		t.Run(page.name, func(t *testing.T) {
			options := testOptions(t)
			state := newTestState(t, options)
			addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops"})
			state.activeSection = page.section
			state.snippetSend.target = sendChosen
			result := pressChord(state, state.shortcuts.chords.Chord(shortcutSnippetPicker))
			if !result.Handled || state.snippetSend.target != sendCurrent {
				t.Fatal("shortcut did not reopen the snippets view")
			}
			view := openPickerView(t, state, 1)
			group := dualHit(t, view, hitSnippetPickerGrouped)
			if err := view.Click(group.X+group.Width/2, group.Y+group.Height/2); err != nil {
				t.Fatal(err)
			}
			if state.snippets.pickerGrouped || !sameShape(pickerShape(state), []string{"First"}) {
				t.Fatal("group toggle did not show flat commands")
			}
			restored := newTestState(t, options)
			if restored.snippets.pickerGrouped {
				t.Fatal("grouping choice was not persisted")
			}
			if hit, ok := hitByIDSnapshot(view.Snapshot().Hits, hitSnippetPickerGrouped); !ok || hit.Role != gfx.RoleCheckbox {
				t.Fatal("grouping checkbox disappeared")
			}
		})
	}
}
