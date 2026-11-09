//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func offeredTerminalTool(state *State, id gfx.ID) (barTool, bool) {
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

func openPickerView(t *testing.T, state *State, scale float64) *headless.Session {
	t.Helper()
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(app.Config{Width: 1200, Height: 900, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

// The list's scrollbar is a real one: pressing on it and dragging scrolls the
// rows, a press beyond the thumb jumps there, and while it is held the rows
// under the pointer do not take the hover (they used to, which left the
// highlight stuck on the row the drag started over).
func TestPaletteScrollbarDragsAndKeepsTheHoverOffTheRows(t *testing.T) {
	for _, scale := range []float64{1, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			for i := 0; i < 60; i++ {
				state.snippets.list = append(state.snippets.list, snippet{Name: fmt.Sprintf("cmd %d", i), Command: "x"})
			}
			state.openSnippetPicker()
			view := openPickerView(t, state, scale)
			palette := &state.commandPalette
			last := palette.maxScroll()

			bar := dualHit(t, view, hitCommandPaletteBar)
			rows := dualHit(t, view, hitCommandPaletteItems.At(0))
			if bar.X < rows.X+rows.Width {
				t.Fatalf("the scrollbar at x=%v overlaps the rows, which end at %v", bar.X, rows.X+rows.Width)
			}
			thumb := palette.bar.VerticalThumb
			x, y := thumb.X+thumb.Width/2, thumb.Y+thumb.Height/2
			if err := view.Press(x, y); err != nil {
				t.Fatal(err)
			}
			hover := palette.selected
			if err := view.Move(x, bar.Y+bar.Height-2); err != nil {
				t.Fatal(err)
			}
			if palette.scroll != last {
				t.Fatalf("dragging the thumb to the end left the offset at %g, want %g", palette.scroll, last)
			}
			// Across the rows with the button held: the hover stays.
			if err := view.Move(rows.X+40, rows.Y+rows.Height/2); err != nil {
				t.Fatal(err)
			}
			if palette.selected != hover {
				t.Fatalf("a held drag moved the hover from row %d to %d", hover, palette.selected)
			}
			if err := view.Release(rows.X+40, rows.Y+rows.Height/2); err != nil {
				t.Fatal(err)
			}
			if palette.dragging {
				t.Fatal("the drag outlived the release")
			}
			// Released, the rows answer the pointer again.
			second := dualHit(t, view, hitCommandPaletteItems.At(int(palette.scroll/commandPaletteRowStep)+3))
			if err := view.Move(second.X+40, second.Y+second.Height/2); err != nil {
				t.Fatal(err)
			}
			if want := int(palette.scroll/commandPaletteRowStep) + 3; palette.selected != want {
				t.Fatalf("after the drag the hover is on row %d, want %d", palette.selected, want)
			}

			// A press on the track away from the thumb takes the thumb there.
			bar = dualHit(t, view, hitCommandPaletteBar)
			if err := view.Press(bar.X+bar.Width/2, bar.Y+4); err != nil {
				t.Fatal(err)
			}
			if err := view.Release(bar.X+bar.Width/2, bar.Y+4); err != nil {
				t.Fatal(err)
			}
			if palette.scroll > 2*commandPaletteRowStep {
				t.Fatalf("pressing the top of the track left the offset at %g", palette.scroll)
			}
		})
	}
}
