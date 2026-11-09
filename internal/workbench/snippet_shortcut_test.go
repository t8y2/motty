package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func pressChord(state *State, chord keymap.Chord) gfx.EventResult {
	return state.Shortcut(gfx.KeyEvent{Modifiers: chord.Modifiers, Text: string([]byte{chord.Char})})
}

func TestASnippetShortcutSendsItFromTheTerminalAndIsKeptOnThisDevice(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	chord := keymap.Chord{Modifiers: gfx.ModifierMeta | gfx.ModifierShift, Char: '1'}

	state.Click(hitSnippets)
	state.Click(hitSnippetAdd)
	typeInto(state, hitSnippetName, "Restart nginx")
	typeInto(state, hitSnippetCommand, "systemctl restart nginx")
	state.Click(hitSnippetShortcut)
	if !state.snippets.capturing {
		t.Fatal("the shortcut button did not start waiting for a chord")
	}
	pressChord(state, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'r'})
	if !state.snippets.capturing || state.snippets.dialogError != state.messages.Tools.SnippetShortcutInvalid {
		t.Fatalf("Ctrl+R alone belongs to the terminal: capturing=%v error=%q", state.snippets.capturing, state.snippets.dialogError)
	}
	taken := state.shortcuts.chords.Chord(shortcutFind)
	pressChord(state, taken)
	if !state.snippets.capturing || state.snippets.dialogError == "" {
		t.Fatalf("a built-in chord was taken: capturing=%v error=%q", state.snippets.capturing, state.snippets.dialogError)
	}
	pressChord(state, chord)
	if state.snippets.capturing || state.snippets.shortcut != chord || state.snippets.dialogError != "" {
		t.Fatalf("capturing=%v shortcut=%+v error=%q", state.snippets.capturing, state.snippets.shortcut, state.snippets.dialogError)
	}
	state.Click(hitSnippetSubmit)
	if got := state.snippets.list[0].Shortcut; got != chord {
		t.Fatalf("saved shortcut = %+v", got)
	}

	// Away from the terminal the chord must not type into one that is out of sight.
	if got := pressChord(state, chord); got.Handled || shell.typed() != "" {
		t.Fatalf("snippets page: handled=%v, shell heard %q", got.Handled, shell.typed())
	}
	state.activeSection = sectionTerminal
	if got := pressChord(state, chord); !got.Handled || shell.typed() != "systemctl restart nginx\r" {
		t.Fatalf("handled=%v, shell heard %q", got.Handled, shell.typed())
	}
	held := gfx.KeyEvent{Modifiers: chord.Modifiers, Text: "1", Repeat: true}
	if got := state.Shortcut(held); !got.Handled || shell.typed() != "systemctl restart nginx\r" {
		t.Fatalf("a held key sent the snippet again: heard %q", shell.typed())
	}

	// A built-in shortcut cannot be moved onto it.
	state.openShortcutCapture(shortcutFind)
	pressChord(state, chord)
	if state.modal != modalShortcutCapture || state.shortcutError == "" || state.shortcuts.chords.Chord(shortcutFind) != taken {
		t.Fatalf("modal=%d error=%q find=%+v", state.modal, state.shortcutError, state.shortcuts.chords.Chord(shortcutFind))
	}
	state.clickShortcutCapture(hitShortcutCaptureCancel)

	if restarted := newTestState(t, options); len(restarted.snippets.list) != 1 || restarted.snippets.list[0].Shortcut != chord {
		t.Fatalf("restored snippets = %+v", restarted.snippets.list)
	}
	state.Click(hitSnippets)
	state.Click(hitSnippetEdit)
	state.Click(hitSnippetShortcutOff)
	state.Click(hitSnippetSubmit)
	if got := pressChord(state, chord); got.Handled {
		t.Fatal("a cleared shortcut still fired")
	}
	if restarted := newTestState(t, options); restarted.snippets.list[0].Shortcut.Valid() {
		t.Fatalf("a cleared shortcut came back: %+v", restarted.snippets.list[0])
	}
}

func TestAChordTwoSnippetsWantGoesToTheFirst(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	chord := keymap.Chord{Modifiers: gfx.ModifierMeta | gfx.ModifierShift, Char: '2'}
	state.snippets.list = []snippet{{Name: "a", Command: "ls", Shortcut: chord}, {Name: "b", Command: "pwd"}}

	state.openSnippetDialog(1)
	state.Click(hitSnippetShortcut)
	pressChord(state, chord)
	if !state.snippets.capturing || state.snippets.dialogError == "" || state.snippets.shortcut.Valid() {
		t.Fatalf("a chord of snippet a was given to b: %+v", state.snippets)
	}
}

func TestResettingShortcutsUnbindsSnippetsTheyNowCollideWith(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	defaultPalette := state.shortcuts.chords.Chord(shortcutCommandPalette)
	moved := keymap.Chord{Modifiers: defaultPalette.Modifiers | gfx.ModifierAlt, Char: 'p'}
	if _, ok := state.shortcuts.bind(shortcutCommandPalette, moved); !ok {
		t.Fatal("could not move the command palette")
	}
	state.snippets.list = []snippet{{Name: "a", Command: "ls", Shortcut: defaultPalette}}
	state.save()
	if restarted := newTestState(t, options); restarted.snippets.list[0].Shortcut != defaultPalette {
		t.Fatalf("a free chord was not kept: %+v", restarted.snippets.list[0])
	}
	state.Click(hitShortcutReset)
	if state.snippets.list[0].Shortcut.Valid() {
		t.Fatalf("snippet kept a chord the palette now uses: %+v", state.snippets.list[0])
	}
}

func TestSnippetsPageSteadyPaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.snippets.list = []snippet{
		{Name: "Restart nginx", Command: "systemctl restart nginx", Run: true, Shortcut: keymap.Chord{Modifiers: gfx.ModifierMeta | gfx.ModifierShift, Char: '1'}},
		{Name: "Disk", Command: "df -h"},
	}
	state.refreshSnippets()
	state.activeSection = sectionSnippets
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("snippets page paint allocations = %v", allocations)
	}
}

func TestASnippetWithoutAShortcutIsSentFromItsPage(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{{Name: "list all", Command: "ls --all", Run: true}}
	state.refreshSnippets()

	// With no terminal open there is nothing to send to, and the page stays.
	state.Click(hitSnippets)
	state.Click(hitSnippetSend)
	if state.activeSection != sectionSnippets || state.status != state.messages.Tools.StatusSnippetNoTerminal {
		t.Fatalf("section=%d status=%q", state.activeSection, state.status)
	}

	state.Click(hitHosts)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	state.Click(hitSnippets)
	state.Click(hitSnippetSend)
	if state.activeSection != sectionTerminal || shell.typed() != "ls --all\r" {
		t.Fatalf("button: section=%d, shell heard %q", state.activeSection, shell.typed())
	}
	if title := state.sessions[0].Title; !strings.Contains(state.status, title) {
		t.Fatalf("status %q does not name the terminal %q", state.status, title)
	}

	state.Click(hitSnippets)
	if got := doubleClick(state, hitSnippetRowList.At(0)); !got.Handled || state.activeSection != sectionTerminal || shell.typed() != "ls --all\rls --all\r" {
		t.Fatalf("double click: handled=%v section=%d, shell heard %q", got.Handled, state.activeSection, shell.typed())
	}
}

func TestRightClickingASnippetRowOpensItsMenu(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{{Name: "a", Command: "echo a"}, {Name: "b", Command: "echo b", Run: true}}
	state.refreshSnippets()
	state.Click(hitHosts)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)

	state.Click(hitSnippets)
	secondary := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitSnippetRowList.At(1)}
	if got := pointer(state, secondary); !got.Handled || !state.snippetMenu.open || state.snippetMenu.index != 1 || state.snippets.selected != 1 {
		t.Fatalf("handled=%v menu=%+v selected=%d", got.Handled, state.snippetMenu, state.snippets.selected)
	}
	state.Click(hitSnippetMenuSend)
	if state.snippetMenu.open || state.activeSection != sectionTerminal || shell.typed() != "echo b\r" {
		t.Fatalf("menu open=%v section=%d, shell heard %q", state.snippetMenu.open, state.activeSection, shell.typed())
	}

	state.Click(hitSnippets)
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitSnippetRowList.At(0)})
	state.Click(hitSnippetMenuDelete)
	if len(state.snippets.list) != 1 || state.snippets.list[0].Name != "b" {
		t.Fatalf("snippets after delete: %+v", state.snippets.list)
	}
}

func TestSendingASnippetFromThePageAsksWhichTerminalWhenThereAreSeveral(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{{Name: "list", Command: "ls", Run: true}}
	state.refreshSnippets()
	first := world.connectFirst(t, state)
	state.Click(hitPaneSplitHorizontal)
	firstShell, splitShell := first.shell(t), world.shared[0].shell(t)
	firstID := state.sessions[0].ID
	if state.activeSessionID == firstID {
		t.Fatalf("the split should be the active session")
	}

	state.Click(hitSnippets)
	state.Click(hitSnippetSend)
	if !state.snippetMenu.open || !state.snippetMenu.pick || firstShell.typed() != "" || splitShell.typed() != "" {
		t.Fatalf("menu=%+v, first heard %q, split heard %q: nothing is sent before a terminal is chosen", state.snippetMenu, firstShell.typed(), splitShell.typed())
	}
	state.Click(hitSnippetPickList.At(int(firstID)))
	if firstShell.typed() != "ls\r" || splitShell.typed() != "" {
		t.Fatalf("first heard %q, split heard %q", firstShell.typed(), splitShell.typed())
	}
	if state.activeSection != sectionTerminal || state.activeSessionID != firstID {
		t.Fatalf("section=%d active=%d, want the chosen terminal shown", state.activeSection, state.activeSessionID)
	}
}
