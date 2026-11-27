//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"testing"
	"time"

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
	// The bar's checkbox is chrome above the search field, so the list holds
	// exactly the saved snippets, even on a terminal page.
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

// The bar's checkbox is real chrome of the dialog: a painted, clickable hit
// above the search field, outside the list, whose tick is the bar's state and
// whose press switches the bar without dismissing the picker.
func TestCommandBarSwitchIsClickableChrome(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	hit, ok := hitByIDSnapshot(view.Snapshot().Hits, hitCommandsBarToggle)
	if !ok {
		t.Fatal("the bar checkbox was not painted as a hit")
	}
	if hit.Role != gfx.RoleCheckbox || !hit.Checked {
		t.Fatalf("the visible bar's switch is role=%v checked=%v", hit.Role, hit.Checked)
	}
	search, hasSearch := hitByIDSnapshot(view.Snapshot().Hits, hitCommandPaletteSearch)
	first, hasFirst := hitByIDSnapshot(view.Snapshot().Hits, hitCommandPaletteItems.At(0))
	if hasSearch && hit.Bounds.Y >= search.Bounds.Y || hasFirst && hit.Bounds.Y >= first.Bounds.Y {
		t.Fatal("the bar checkbox was not above the search field and the list")
	}
	press := func() {
		t.Helper()
		h, ok := hitByIDSnapshot(view.Snapshot().Hits, hitCommandsBarToggle)
		if !ok {
			t.Fatal("the bar checkbox vanished after a press")
		}
		if err := view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	press()
	if state.commands.shown || state.modal != modalCommandPalette {
		t.Fatalf("a press left shown=%v modal=%v", state.commands.shown, state.modal)
	}
	unticked, ok := hitByIDSnapshot(view.Snapshot().Hits, hitCommandsBarToggle)
	if !ok || unticked.Checked {
		t.Fatal("the unticked checkbox did not report its new state")
	}
	press()
	if !state.commands.shown {
		t.Fatal("a second press did not show the bar again")
	}
}

func openPickerView(t *testing.T, state *State, scale float64) *headless.Session {
	t.Helper()
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	state.options.Invalidator = &invalidator
	config := state.WindowConfig(app.Config{Width: 1200, Height: 900, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize})
	view, err := headless.Open(config, nil, headless.Options{Scale: scale})
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
			scroll := palette.scrollState
			if scroll == nil {
				t.Fatal("the palette has no scroll area")
			}
			last := palette.maxScroll()

			rows := dualHit(t, view, hitCommandPaletteItems.At(0))
			if err := view.Move(rows.X+40, rows.Y+rows.Height/2); err != nil {
				t.Fatal(err)
			}
			bar, ok := findSnapshotLabel(view.Snapshot(), "Vertical scrollbar")
			if !ok {
				t.Fatal("the palette list has no scrollbar")
			}
			if bar.Bounds.X < rows.X+rows.Width {
				t.Fatalf("the scrollbar at x=%v overlaps the rows, which end at %v", bar.Bounds.X, rows.X+rows.Width)
			}
			x := bar.Bounds.X + bar.Bounds.Width/2
			if err := view.Press(x, bar.Bounds.Y+2); err != nil {
				t.Fatal(err)
			}
			hover := palette.selected
			if err := view.Move(x, bar.Bounds.Y+bar.Bounds.Height-2); err != nil {
				t.Fatal(err)
			}
			if scroll.Offset != last {
				t.Fatalf("dragging the thumb to the end left the offset at %g, want %g", scroll.Offset, last)
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
			// Released, the rows answer the pointer again.
			second := dualHit(t, view, hitCommandPaletteItems.At(int(palette.scroll/commandPaletteRowStep)+3))
			if err := view.Move(second.X+40, second.Y+second.Height/2); err != nil {
				t.Fatal(err)
			}
			if want := int(palette.scroll/commandPaletteRowStep) + 3; palette.selected != want {
				t.Fatalf("after the drag the hover is on row %d, want %d", palette.selected, want)
			}

			// A press on the track away from the thumb takes the thumb there.
			bar, ok = findSnapshotLabel(view.Snapshot(), "Vertical scrollbar")
			if !ok {
				t.Fatal("the scrollbar disappeared after the release")
			}
			if err := view.Press(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+4); err != nil {
				t.Fatal(err)
			}
			if err := view.Release(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+4); err != nil {
				t.Fatal(err)
			}
			if palette.scroll > 2*commandPaletteRowStep {
				t.Fatalf("pressing the top of the track left the offset at %g", palette.scroll)
			}
		})
	}
}

func openSnippetTerminalView(t *testing.T, state *State) *headless.Session {
	t.Helper()
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	state.options.Invalidator = &invalidator
	var nativeInput app.TextInput
	config := state.WindowConfig(app.Config{
		Width: 1200, Height: 900, Tree: Tree(), Invalidator: &invalidator,
		OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			if editor := state.EditorFor(context.ID); editor != nil {
				nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
				return &nativeInput
			}
			return nil
		},
	})
	view, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

// Regression test for #1725: after opening the quick-command picker from the
// terminal toolbar and executing (or dismissing) it, focus must return to the
// active terminal so pressing Enter does not reopen the picker.
func TestSnippetPickerReturnsKeyboardToTerminal(t *testing.T) {
	setup := func(t *testing.T, snippets ...snippet) (*State, *fakeShell, *headless.Session) {
		t.Helper()
		world, options := newSharingWorld(t)
		state := newTestState(t, options)
		world.connectFirst(t, state)
		shell := world.conns[0].shell(t)
		shell.say("\x1b[0m")
		state.Wake()
		state.activeSection = sectionTerminal
		state.snippets.list = snippets
		state.refreshSnippets()
		view := openSnippetTerminalView(t, state)
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		return state, shell, view
	}

	openViaToolbar := func(t *testing.T, state *State, view *headless.Session) {
		t.Helper()
		btn := dualHit(t, view, hitTerminalSnippets)
		if err := view.Click(btn.X+btn.Width/2, btn.Y+btn.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Wait(100 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if state.modal != modalCommandPalette || !state.commandPalette.snippets {
			t.Fatalf("toolbar click left modal=%d snippets=%v", state.modal, state.commandPalette.snippets)
		}
	}

	assertEnterReachesTerminal := func(t *testing.T, state *State, shell *fakeShell, view *headless.Session, wantTyped string) {
		t.Helper()
		if err := view.Wait(100 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		wantFocus := state.activeTerminalFocusHit()
		if got := view.Snapshot().Focused; got != wantFocus {
			t.Fatalf("focused = %v, want terminal %v", got, wantFocus)
		}
		if err := view.Keys("enter"); err != nil {
			t.Fatal(err)
		}
		if err := view.Wait(100 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if state.modal != modalNone {
			t.Fatalf("Enter reopened modal %d", state.modal)
		}
		if got := shell.typed(); got != wantTyped {
			t.Fatalf("shell typed %q, want %q", got, wantTyped)
		}
	}

	t.Run("click snippet row", func(t *testing.T) {
		state, shell, view := setup(t, snippet{Name: "Disk usage", Command: "df -h"})
		openViaToolbar(t, state, view)
		row := dualHit(t, view, hitCommandPaletteItems.At(0))
		if err := view.Click(row.X+row.Width/2, row.Y+row.Height/2); err != nil {
			t.Fatal(err)
		}
		assertEnterReachesTerminal(t, state, shell, view, "df -h\r")
	})

	t.Run("enter on snippet row", func(t *testing.T) {
		state, shell, view := setup(t, snippet{Name: "Disk usage", Command: "df -h"})
		openViaToolbar(t, state, view)
		if err := view.Keys("enter"); err != nil {
			t.Fatal(err)
		}
		assertEnterReachesTerminal(t, state, shell, view, "df -h\r")
	})

	t.Run("escape dismisses picker", func(t *testing.T) {
		state, shell, view := setup(t, snippet{Name: "Disk usage", Command: "df -h"})
		openViaToolbar(t, state, view)
		if err := view.Keys("escape"); err != nil {
			t.Fatal(err)
		}
		assertEnterReachesTerminal(t, state, shell, view, "\r")
	})

	t.Run("close button dismisses picker", func(t *testing.T) {
		state, shell, view := setup(t, snippet{Name: "Disk usage", Command: "df -h"})
		openViaToolbar(t, state, view)
		closeBtn := dualHit(t, view, hitCommandPaletteClose)
		if err := view.Click(closeBtn.X+closeBtn.Width/2, closeBtn.Y+closeBtn.Height/2); err != nil {
			t.Fatal(err)
		}
		assertEnterReachesTerminal(t, state, shell, view, "\r")
	})

	t.Run("backdrop dismisses picker", func(t *testing.T) {
		state, shell, view := setup(t, snippet{Name: "Disk usage", Command: "df -h"})
		openViaToolbar(t, state, view)
		backdrop := dualHit(t, view, hitCommandPaletteDismiss)
		if err := view.Click(backdrop.X+4, backdrop.Y+4); err != nil {
			t.Fatal(err)
		}
		assertEnterReachesTerminal(t, state, shell, view, "\r")
	})

	t.Run("snippet with variables keeps dialog focus then returns to terminal", func(t *testing.T) {
		state, shell, view := setup(t, snippet{Name: "Deploy", Command: "deploy {{env:prod}}"})
		openViaToolbar(t, state, view)
		if err := view.Keys("enter"); err != nil {
			t.Fatal(err)
		}
		if err := view.Wait(100 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if state.modal != modalSnippetVariables {
			t.Fatalf("modal = %d, want modalSnippetVariables (%d)", state.modal, modalSnippetVariables)
		}
		if got := view.Snapshot().Focused; got != hitSnippetVarList.At(0) {
			t.Fatalf("variables dialog focused = %v, want %v", got, hitSnippetVarList.At(0))
		}
		if err := view.Keys("enter"); err != nil {
			t.Fatal(err)
		}
		assertEnterReachesTerminal(t, state, shell, view, "deploy prod\r")
	})

	t.Run("command palette session switch focuses new terminal", func(t *testing.T) {
		world, options := newSharingWorld(t)
		state := newTestState(t, options)
		world.connectFirst(t, state)
		state.Click(hitPaneSplitHorizontal)
		if len(world.shared) != 1 || len(state.sessions) != 2 {
			t.Fatalf("shared=%d sessions=%d, want 1 and 2", len(world.shared), len(state.sessions))
		}
		shell2 := world.shared[0].shell(t)
		state.Wake()
		state.ActivatePane(state.sessions[0].ID)
		state.activeSection = sectionTerminal
		view := openSnippetTerminalView(t, state)
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		btn := dualHit(t, view, hitCommandPalette)
		if err := view.Click(btn.X+btn.Width/2, btn.Y+btn.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Wait(100 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if state.modal != modalCommandPalette {
			t.Fatalf("modal = %d, want modalCommandPalette", state.modal)
		}
		row := commandPaletteRow(t, state, hitCommandSessionList.At(int(state.sessions[1].ID)))
		state.commandPalette.selected = row
		if err := view.Keys("enter"); err != nil {
			t.Fatal(err)
		}
		if state.activeSessionID != state.sessions[1].ID {
			t.Fatalf("activeSessionID = %d, want %d", state.activeSessionID, state.sessions[1].ID)
		}
		assertEnterReachesTerminal(t, state, shell2, view, "\r")
	})
}
