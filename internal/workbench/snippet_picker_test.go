package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func offeredTerminalTool(state *State, id ui.ID) (barTool, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		return barTool{}, false
	}
	var buffer [maxBarTools]barTool
	for _, tool := range state.activeTerminalTools(session, &buffer) {
		if tool.id == id {
			return tool, true
		}
	}
	return barTool{}, false
}

func TestTerminalToolbarSendsASnippetToItsTerminal(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	state.snippets.list = []snippet{
		{Name: "Restart nginx", Command: "systemctl restart nginx", Run: true},
		{Name: "Disk usage", Command: "df -h"},
	}
	state.refreshSnippets()
	state.activeSection = sectionTerminal

	tool, ok := offeredTerminalTool(state, hitTerminalSnippets)
	if !ok || tool.disabled {
		t.Fatalf("a connected terminal's toolbar offers the snippets button = %v, disabled = %v", ok, tool.disabled)
	}
	state.Click(hitTerminalSnippets)
	if state.modal != modalCommandPalette || len(state.commandPalette.visible) != len(state.snippets.list) {
		t.Fatalf("the button opened modal %d listing %d of %d snippets", state.modal, len(state.commandPalette.visible), len(state.snippets.list))
	}

	// The list answers to what the command says too, not only to its name.
	app.TextEvent{Kind: app.TextInsert, Text: "df -h"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	state.refreshCommandPalette()
	if len(state.commandPalette.visible) != 1 {
		t.Fatalf("searching a command matched %d snippets, want 1", len(state.commandPalette.visible))
	}
	state.Click(hitCommandPaletteItems.At(0))
	if state.modal != modalNone || shell.typed() != "df -h" {
		t.Fatalf("modal = %d, the shell heard %q: the chosen snippet must reach the terminal the button belongs to", state.modal, shell.typed())
	}
}

func TestSnippetPickerListsEverySnippetTheCatalogKeeps(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for i := 0; i < maxSnippets; i++ {
		state.snippets.list = append(state.snippets.list, snippet{Name: fmt.Sprintf("cmd %d", i), Command: fmt.Sprintf("echo %d", i)})
	}
	state.openSnippetPicker()
	if got := len(state.commandPalette.visible); got != maxSnippets {
		t.Fatalf("the picker lists %d snippets, want all %d: a long list imported from another tool must not be cut off", got, maxSnippets)
	}
	last := state.commandPalette.entries[state.commandPalette.visible[maxSnippets-1]]
	if want := hitSnippetSendList.At(maxSnippets - 1); last.target != want {
		t.Fatalf("the last row sends %s, want %s", tree.Text(last.target), tree.Text(want))
	}
	state.closeCommandPalette()
	if state.commandPalette.entries != nil || state.commandPalette.visible != nil {
		t.Fatal("a closed palette still holds its list")
	}
}

func TestSnippetPickerWithNoSnippetsLeadsToWhereTheyAreAdded(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openSnippetPicker()
	if len(state.commandPalette.visible) != 1 {
		t.Fatalf("the empty picker offers %d rows, want the one that leads to the page", len(state.commandPalette.visible))
	}
	state.Click(hitCommandPaletteItems.At(0))
	if state.modal != modalNone || state.activeSection != sectionSnippets {
		t.Fatalf("modal = %d, section = %d: the row must open the Snippets page", state.modal, state.activeSection)
	}
}

func TestSnippetPickerDoesNotLeakIntoThePalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openSnippetPicker()
	state.closeCommandPalette()
	state.openCommandPalette()
	if state.commandPalette.snippets {
		t.Fatal("the command palette opened as the snippet picker")
	}
}
