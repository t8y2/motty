//go:build windows || darwin || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// A command typed into the picker reaches the terminal the button belongs to,
// with Enter by default and without it when the user turns that off.
func TestSnippetPickerSendsATypedCommandToTheCurrentTerminal(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	state.Wake()
	state.activeSection = sectionTerminal

	state.Click(hitTerminalSnippets)
	app.TextEvent{Kind: app.TextInsert, Text: "uptime"}.Apply(state.EditorFor(hitSnippetSendInput))
	state.Click(hitSendRun)
	state.Click(hitSnippetSendButton)
	if state.modal != modalNone || shell.typed() != "uptime" {
		t.Fatalf("modal = %d, the shell heard %q: with Enter off the command is typed and left for the user to run", state.modal, shell.typed())
	}

	state.Click(hitTerminalSnippets)
	if state.snippetSend.input.String() != "" {
		t.Fatal("the previous command is still in the form")
	}
	if state.snippetSend.runs() {
		t.Fatal("the choice not to press Enter was forgotten between openings")
	}
	state.Click(hitSendRun)
	app.TextEvent{Kind: app.TextInsert, Text: "df"}.Apply(state.EditorFor(hitSnippetSendInput))
	if _, handled := state.commandPaletteKey(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitSnippetSendInput}); !handled {
		t.Fatal("Enter in the command field did not send")
	}
	if got := shell.typed(); got != "uptimedf\r" {
		t.Fatalf("the shell heard %q, want the second command followed by Enter", got)
	}
}

// Choosing terminals lists the connected ones, remembers the ticks, and refuses
// to send until at least one is ticked.
func TestSnippetPickerSendsOnlyToTheTickedTerminals(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	state.Wake()
	state.activeSection = sectionTerminal

	state.Click(hitTerminalSnippets)
	state.Click(hitSendTargetChosen)
	palette := &state.commandPalette
	if len(palette.visible) != 1 || !palette.entries[palette.visible[0]].checkable {
		t.Fatalf("the list holds %d rows, want the one connected terminal as a choice", len(palette.visible))
	}
	app.TextEvent{Kind: app.TextInsert, Text: "id"}.Apply(state.EditorFor(hitSnippetSendInput))
	state.Click(hitSnippetSendButton)
	if shell.typed() != "" || state.modal != modalCommandPalette {
		t.Fatalf("with nothing ticked the shell heard %q (modal %d)", shell.typed(), state.modal)
	}
	state.Click(hitCommandPaletteItems.At(0))
	if state.modal != modalCommandPalette || len(state.snippetSend.chosen) != 1 {
		t.Fatal("ticking a terminal must keep the picker open")
	}
	state.Click(hitSnippetSendButton)
	if shell.typed() != "id\r" {
		t.Fatalf("the ticked terminal heard %q", shell.typed())
	}
}
